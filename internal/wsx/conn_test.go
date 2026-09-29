package wsx

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// serve starts a WebSocket server whose handler owns each accepted connection.
func serve(t *testing.T, opts *websocket.AcceptOptions, h func(ctx context.Context, c *websocket.Conn, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, opts)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		h(r.Context(), c, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func echo(ctx context.Context, c *websocket.Conn, _ *http.Request) {
	c.SetReadLimit(-1)
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if err := c.Write(ctx, typ, data); err != nil {
			return
		}
	}
}

func wsURL(srv *httptest.Server) string { return "ws" + strings.TrimPrefix(srv.URL, "http") }

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func dial(t *testing.T, ctx context.Context, url string, o Options) *Conn {
	t.Helper()
	c, err := Dial(ctx, &Target{URL: url}, o)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(c.Abort)
	return c
}

// next returns the next event or fails the test.
func next(t *testing.T, ctx context.Context, c *Conn) Event {
	t.Helper()
	ev, err := c.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return ev
}

// until reads events up to and including the first one for which stop is true.
func until(t *testing.T, ctx context.Context, c *Conn, stop func(Event) bool) []Event {
	t.Helper()
	var out []Event
	for {
		ev := next(t, ctx, c)
		out = append(out, ev)
		if stop(ev) {
			return out
		}
	}
}

func isType(typ EventType) func(Event) bool { return func(e Event) bool { return e.Type == typ } }

func TestTextAndBinaryEcho(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{})

	open := next(t, ctx, c)
	if open.Type != EventOpen || open.Info == nil || open.Info.Status != 101 || open.Seq != 1 {
		t.Fatalf("first event: %+v", open)
	}
	if strings.Contains(open.Info.URL, "?") {
		t.Fatalf("info URL must not carry a query: %s", open.Info.URL)
	}

	if err := c.Send(ctx, Text, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(ctx, Binary, []byte{0, 1, 2, 255}); err != nil {
		t.Fatal(err)
	}
	evs := until(t, ctx, c, func(e Event) bool { return e.Dir == DirIn && e.Msg == Binary })
	var got []string
	for _, e := range evs {
		got = append(got, e.Dir.String()+"/"+e.Msg.String())
	}
	want := []string{"out/text", "out/binary", "in/text", "in/binary"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if string(evs[2].Data) != "hello" || evs[2].Size != 5 || len(evs[3].Data) != 4 || evs[3].Data[3] != 255 {
		t.Fatalf("payloads: %+v %+v", evs[2], evs[3])
	}
	for i := 1; i < len(evs); i++ {
		if evs[i].Seq <= evs[i-1].Seq || evs[i].Time.IsZero() {
			t.Fatalf("events not ordered/timestamped: %+v", evs)
		}
	}
}

func TestSendDisplayMasksLogNotWire(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{})
	if err := c.Send(ctx, Text, []byte("token=SECRET1"), WithDisplay([]byte("token=****"))); err != nil {
		t.Fatal(err)
	}
	evs := until(t, ctx, c, func(e Event) bool { return e.Dir == DirIn })
	out, in := evs[len(evs)-2], evs[len(evs)-1]
	if string(out.Data) != "token=****" || out.Size != len("token=SECRET1") {
		t.Fatalf("out event: %+v", out)
	}
	if string(in.Data) != "token=SECRET1" {
		t.Fatalf("the wire must carry the real value: %q", in.Data)
	}
}

func TestPingPong(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{})
	rtt, err := c.Ping(ctx)
	if err != nil || rtt < 0 {
		t.Fatalf("Ping = %v, %v", rtt, err)
	}
	evs := until(t, ctx, c, isType(EventPong))
	ping, pong := evs[len(evs)-2], evs[len(evs)-1]
	if ping.Type != EventPing || ping.Dir != DirOut || pong.Dir != DirIn {
		t.Fatalf("ping/pong events: %+v %+v", ping, pong)
	}
	if pong.Latency <= 0 {
		t.Fatalf("pong latency not measured: %v", pong.Latency)
	}
}

func TestServerPing(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, func(ctx context.Context, c *websocket.Conn, _ *http.Request) {
		go func() { _, _, _ = c.Read(ctx) }() // needed so the pong is processed
		_ = c.Ping(ctx)
		<-ctx.Done()
	})
	c := dial(t, ctx, wsURL(srv), Options{})
	ev := until(t, ctx, c, isType(EventPing))
	if last := ev[len(ev)-1]; last.Dir != DirIn {
		t.Fatalf("server ping should be inbound: %+v", last)
	}
}

func TestKeepAlivePings(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{PingInterval: 20 * time.Millisecond})
	seen := 0
	until(t, ctx, c, func(e Event) bool {
		if e.Type == EventPong {
			seen++
		}
		return seen >= 2
	})
}

func TestServerCloseWithCode(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, func(ctx context.Context, c *websocket.Conn, _ *http.Request) {
		_ = c.Write(ctx, websocket.MessageText, []byte("bye"))
		_ = c.Close(websocket.StatusPolicyViolation, "not allowed")
	})
	c := dial(t, ctx, wsURL(srv), Options{})
	evs := until(t, ctx, c, isType(EventClose))
	last := evs[len(evs)-1]
	if last.Code != 1008 || last.Reason != "not allowed" || last.Err != nil {
		t.Fatalf("close event: %+v", last)
	}
	if string(evs[len(evs)-2].Data) != "bye" {
		t.Fatalf("message before the close was lost: %+v", evs)
	}
	if _, err := c.Next(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("Next after close = %v, want io.EOF", err)
	}
	if err := c.Send(ctx, Text, []byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Send after close = %v, want ErrClosed", err)
	}
	if _, err := c.Ping(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("Ping after close = %v, want ErrClosed", err)
	}
}

func TestClientClose(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{})
	if err := c.Close(1000, "done"); err != nil {
		t.Fatal(err)
	}
	evs := until(t, ctx, c, isType(EventClose))
	last := evs[len(evs)-1]
	if last.Code != 1000 || last.Reason != "done" || last.Err != nil {
		t.Fatalf("close event: %+v", last)
	}
	if err := c.Close(1000, ""); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
}

func TestAbruptDropIsAbnormal(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, func(_ context.Context, c *websocket.Conn, _ *http.Request) {
		_ = c.CloseNow()
	})
	c := dial(t, ctx, wsURL(srv), Options{})
	evs := until(t, ctx, c, isType(EventClose))
	last := evs[len(evs)-1]
	if last.Code != CloseAbnormal || last.Err == nil {
		t.Fatalf("close event: %+v", last)
	}
}

func TestHandshakeFailureSurfacesStatusAndBody(t *testing.T) {
	ctx := testCtx(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad token"}`))
	}))
	defer srv.Close()

	_, err := Dial(ctx, &Target{URL: wsURL(srv) + "/feed?token=SUPERSECRET"}, Options{})
	var he *HandshakeError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v (%T), want *HandshakeError", err, err)
	}
	if he.Status != 401 || he.StatusText != "Unauthorized" || he.Body != `{"error":"bad token"}` {
		t.Fatalf("handshake error: %+v", he)
	}
	if he.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("response header lost: %v", he.Header)
	}
	if !errors.Is(err, ErrHandshake) {
		t.Fatal("errors.Is(err, ErrHandshake) must hold")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error leaks the URL: %v", err)
	}
}

func TestHandshakeBodyIsBounded(t *testing.T) {
	ctx := testCtx(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(strings.Repeat("x", 10000)))
	}))
	defer srv.Close()
	_, err := Dial(ctx, &Target{URL: wsURL(srv)}, Options{})
	var he *HandshakeError
	if !errors.As(err, &he) || len(he.Body) > 1024 || he.Status != 403 {
		t.Fatalf("err = %v", err)
	}
}

func TestConnectionRefusedHasNoURL(t *testing.T) {
	ctx := testCtx(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := wsURL(srv)
	srv.Close()
	_, err := Dial(ctx, &Target{URL: addr + "/x?api_key=SUPERSECRET"}, Options{})
	var de *DialError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T), want *DialError", err, err)
	}
	if strings.Contains(err.Error(), "SUPERSECRET") || strings.Contains(err.Error(), "/x") {
		t.Fatalf("error leaks the URL: %v", err)
	}
}

func TestBadURLs(t *testing.T) {
	ctx := testCtx(t)
	tests := []struct {
		url  string
		want error
	}{
		{"", ErrNoURL},
		{"   ", ErrNoURL},
		{"ftp://x.test", ErrBadScheme},
		{"localhost:8080/x", ErrBadScheme},
	}
	for _, tt := range tests {
		if _, err := Dial(ctx, &Target{URL: tt.url}, Options{}); !errors.Is(err, tt.want) {
			t.Errorf("Dial(%q) = %v, want %v", tt.url, err, tt.want)
		}
	}
	if _, err := Dial(ctx, nil, Options{}); !errors.Is(err, ErrNoURL) {
		t.Errorf("nil target = %v", err)
	}
}

func TestHTTPSchemeIsAccepted(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, srv.URL, Options{}) // http://127.0.0.1:port
	if next(t, ctx, c).Type != EventOpen {
		t.Fatal("expected open")
	}
}

func TestCancelDuringHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(testCtx(t))
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := Dial(ctx, &Target{URL: wsURL(srv)}, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestHandshakeTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	_, err := Dial(testCtx(t), &Target{URL: wsURL(srv)}, Options{HandshakeTimeout: 50 * time.Millisecond})
	var de *DialError
	if !errors.As(err, &de) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v (%T), want a DialError wrapping DeadlineExceeded", err, err)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("message: %v", err)
	}
}

func TestCancelAfterConnectClosesTheConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(testCtx(t))
	srv := serve(t, nil, echo)
	c, err := Dial(ctx, &Target{URL: wsURL(srv)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	evs := until(t, testCtx(t), c, isType(EventClose))
	last := evs[len(evs)-1]
	if !errors.Is(last.Err, context.Canceled) {
		t.Fatalf("close event: %+v", last)
	}
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed after cancel")
	}
}

func TestNextHonorsItsContext(t *testing.T) {
	srv := serve(t, nil, echo)
	c := dial(t, testCtx(t), wsURL(srv), Options{})
	next(t, testCtx(t), c) // open
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Next = %v, want DeadlineExceeded", err)
	}
}

// TestConcurrentSendAndReceive runs under -race: many goroutines send while
// one consumer drains events.
func TestConcurrentSendAndReceive(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{})

	const senders, each = 8, 40
	var wg sync.WaitGroup
	for s := 0; s < senders; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := c.Send(ctx, Text, []byte("m")); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	var in, out, ping atomic.Int32
	stop := make(chan struct{})
	go func() {
		wg.Wait()
		for i := 0; i < 5; i++ {
			_, _ = c.Ping(ctx)
		}
		close(stop)
	}()
	for in.Load() < senders*each || out.Load() < senders*each {
		ev := next(t, ctx, c)
		switch {
		case ev.Type == EventMessage && ev.Dir == DirIn:
			in.Add(1)
		case ev.Type == EventMessage && ev.Dir == DirOut:
			out.Add(1)
		case ev.Type == EventPing:
			ping.Add(1)
		}
	}
	<-stop
}

func TestMessageTooBig(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{MaxMessageBytes: 64})
	if err := c.Send(ctx, Text, []byte(strings.Repeat("a", 500))); err != nil {
		t.Fatal(err)
	}
	evs := until(t, ctx, c, isType(EventClose))
	last := evs[len(evs)-1]
	if last.Code != 1009 || last.Err == nil || !strings.Contains(last.Err.Error(), "larger than the limit") {
		t.Fatalf("close event: %+v", last)
	}
}

func TestLargeMessageUnderDefaultLimit(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{})
	big := strings.Repeat("z", 200_000) // coder's own default limit is 32 KiB
	if err := c.Send(ctx, Text, []byte(big)); err != nil {
		t.Fatal(err)
	}
	evs := until(t, ctx, c, func(e Event) bool { return e.Dir == DirIn })
	if got := evs[len(evs)-1]; got.Size != len(big) {
		t.Fatalf("received %d bytes, want %d", got.Size, len(big))
	}
}

func TestQueueDropsOldestMessagesNotTheClose(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, func(ctx context.Context, c *websocket.Conn, _ *http.Request) {
		for i := 0; i < 50; i++ {
			_ = c.Write(ctx, websocket.MessageText, []byte("x"))
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
	})
	c := dial(t, ctx, wsURL(srv), Options{QueueLimit: 10})
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("connection never finished")
	}
	var last Event
	n := 0
	for {
		ev, err := c.Next(ctx)
		if err != nil {
			break
		}
		last = ev
		n++
	}
	if last.Type != EventClose || n > 11 || c.Dropped() == 0 {
		t.Fatalf("kept %d events, dropped %d, last %+v", n, c.Dropped(), last)
	}
}

func TestSubprotocolNegotiation(t *testing.T) {
	ctx := testCtx(t)
	var offered atomic.Value
	srv := serve(t, &websocket.AcceptOptions{Subprotocols: []string{"v2.test"}}, func(ctx context.Context, c *websocket.Conn, r *http.Request) {
		offered.Store(r.Header.Get("Sec-WebSocket-Protocol"))
		echo(ctx, c, r)
	})
	c, err := Dial(ctx, &Target{URL: wsURL(srv), Subprotocols: []string{"v1.test", "v2.test"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Abort()
	if got := c.Info().Subprotocol; got != "v2.test" {
		t.Fatalf("subprotocol = %q", got)
	}
	if got, _ := offered.Load().(string); got != "v1.test,v2.test" {
		t.Fatalf("offered = %q", got)
	}
}

func TestHeadersAndHostAreSent(t *testing.T) {
	ctx := testCtx(t)
	got := make(chan http.Header, 1)
	var host atomic.Value
	srv := serve(t, &websocket.AcceptOptions{OriginPatterns: []string{"*"}}, func(ctx context.Context, c *websocket.Conn, r *http.Request) {
		host.Store(r.Host)
		got <- r.Header.Clone()
		echo(ctx, c, r)
	})
	h := http.Header{}
	h.Set("X-Trace", "abc")
	h.Set("Origin", "https://app.test")
	c, err := Dial(ctx, &Target{URL: wsURL(srv), Header: h, Host: "virtual.test"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Abort()
	hdr := <-got
	if hdr.Get("X-Trace") != "abc" || hdr.Get("Origin") != "https://app.test" {
		t.Fatalf("headers: %v", hdr)
	}
	if h.Get("Sec-WebSocket-Key") != "" {
		t.Fatal("the caller's header map must not be modified")
	}
	if host.Load() != "virtual.test" {
		t.Fatalf("host = %v", host.Load())
	}
}

func TestRedial(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{})
	c2, err := c.Redial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Abort()
	select {
	case <-c.Done():
	default:
		t.Fatal("the old connection must be closed")
	}
	if ev := next(t, ctx, c2); ev.Type != EventOpen || ev.Seq != 1 {
		t.Fatalf("first event of the new connection: %+v", ev)
	}
	if err := c2.Send(ctx, Text, []byte("again")); err != nil {
		t.Fatal(err)
	}
}

func TestTLS(t *testing.T) {
	ctx := testCtx(t)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		echo(r.Context(), c, r)
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	defer srv.Close()
	url := "wss" + strings.TrimPrefix(srv.URL, "https")

	_, err := Dial(ctx, &Target{URL: url}, Options{})
	var de *DialError
	if !errors.As(err, &de) || !strings.Contains(err.Error(), "not trusted") || !strings.Contains(err.Error(), "Verify TLS") {
		t.Fatalf("untrusted certificate: %v", err)
	}

	c, err := Dial(ctx, &Target{URL: url}, Options{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("insecure dial: %v", err)
	}
	defer c.Abort()
	if err := c.Send(ctx, Text, []byte("secure")); err != nil {
		t.Fatal(err)
	}
	evs := until(t, ctx, c, func(e Event) bool { return e.Dir == DirIn })
	if string(evs[len(evs)-1].Data) != "secure" {
		t.Fatalf("events: %+v", evs)
	}
}

// serveWithCookie accepts WebSockets and sets a cookie on the 101 response.
func serveWithCookie(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "1", Path: "/"})
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		echo(r.Context(), c, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDrainTakesQueuedEventsWithoutBlocking(t *testing.T) {
	ctx := testCtx(t)
	srv := serve(t, nil, echo)
	c := dial(t, ctx, wsURL(srv), Options{})
	if got := next(t, ctx, c); got.Type != EventOpen {
		t.Fatalf("first event: %+v", got)
	}
	if got := c.Drain(10); len(got) != 0 {
		t.Fatalf("nothing is queued yet, got %+v", got)
	}
	for _, s := range []string{"a", "b", "c"} {
		if err := c.Send(ctx, Text, []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	// Each send queues an outgoing event at once; the echoes follow.
	first := c.Drain(2)
	if len(first) != 2 || first[0].Dir != DirOut || string(first[0].Data) != "a" || string(first[1].Data) != "b" {
		t.Fatalf("first drain: %+v", first)
	}
	var seen []string
	deadline := time.Now().Add(3 * time.Second)
	for len(seen) < 4 && time.Now().Before(deadline) {
		for _, e := range c.Drain(100) {
			seen = append(seen, e.Dir.String()+":"+string(e.Data))
		}
		time.Sleep(time.Millisecond)
	}
	if len(seen) < 4 || seen[0] != "out:c" {
		t.Fatalf("remaining events: %v", seen)
	}
	if c.Drain(0) != nil || c.Drain(-1) != nil {
		t.Fatal("a non-positive count returns nothing")
	}
}
