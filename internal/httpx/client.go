package httpx

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/shubham/postctl/internal/model"
)

const defaultMaxBodyBytes int64 = 50 << 20

// Client sends resolved model requests using net/http.
type Client struct {
	version string
}

// NewClient returns an HTTP sender whose default User-Agent includes version.
func NewClient(version string) *Client {
	return &Client{version: version}
}

// Send implements Sender.
func (c *Client) Send(
	ctx context.Context,
	req *model.Request,
	opts Options,
) (*Response, error) {
	if req == nil {
		return nil, errors.New("send: nil request")
	}
	if req.Kind != model.KindHTTP {
		return nil, fmt.Errorf("send: unsupported request kind %q", req.Kind)
	}
	settings := resolveSettings(req.Settings, opts.Defaults)
	sendBody := settings.disableBodyPruning ||
		(req.Method != http.MethodGet && req.Method != http.MethodHead)
	body, err := buildBody(req, opts.WorkspaceDir, sendBody)
	if err != nil {
		return nil, err
	}
	request, suppressed, err := c.buildRequest(ctx, req, body)
	if err != nil {
		return nil, err
	}
	authData, err := body.authData(req.Auth)
	if err != nil {
		return nil, err
	}
	if err := applyAuth(ctx, request, req.Auth, authData, opts.OAuth2); err != nil {
		return nil, err
	}

	trace := &traceRecorder{}
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace.trace()))
	capture := &captureTransport{
		base: newTransport(settings),
	}
	defer capture.base.CloseIdleConnections()

	redirects := make([]Redirect, 0)
	client := &http.Client{
		Transport: capture,
		Timeout:   settings.timeout,
	}
	if opts.CookieJar != nil && !settings.disableCookies {
		client.Jar = opts.CookieJar
	}
	client.CheckRedirect = redirectPolicy(
		request,
		settings,
		suppressed,
		&redirects,
	)

	started := time.Now()
	httpResponse, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if req.Auth != nil && req.Auth.Type == model.AuthDigest &&
		httpResponse.StatusCode == http.StatusUnauthorized {
		challenge := httpResponse.Header.Get("WWW-Authenticate")
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "digest ") {
			_, _ = io.Copy(io.Discard, httpResponse.Body)
			_ = httpResponse.Body.Close()
			retry, retryErr := cloneHTTPRequest(request)
			if retryErr != nil {
				return nil, retryErr
			}
			authorization, authErr := digestAuthorization(
				retry,
				authData,
				req.Auth.Digest,
				challenge,
			)
			if authErr != nil {
				return nil, authErr
			}
			retry.Header.Set("Authorization", authorization)
			retry = retry.WithContext(httptrace.WithClientTrace(retry.Context(), trace.trace()))
			httpResponse, err = client.Do(retry)
			if err != nil {
				return nil, err
			}
		}
	}
	defer httpResponse.Body.Close()

	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}
	wireCounter := &countingReader{reader: httpResponse.Body}
	decoded, cleanup, err := decodeBody(wireCounter, httpResponse.Header.Get("Content-Encoding"))
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer func() { _ = cleanup() }()
	}
	kept, truncated, err := readLimited(decoded, maxBody)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(io.Discard, decoded); err != nil {
		return nil, err
	}
	finished := time.Now()
	timings := trace.timings(finished)
	timings.Total = finished.Sub(started)

	response := &Response{
		StatusCode:    httpResponse.StatusCode,
		Status:        statusText(httpResponse),
		Proto:         httpResponse.Proto,
		Headers:       headerKVs(httpResponse.Header),
		Cookies:       responseCookies(httpResponse.Cookies()),
		Body:          kept,
		BodyTruncated: truncated,
		Size: Size{
			Headers: responseHeaderSize(httpResponse),
			Body:    wireCounter.count,
		},
		Timings:    timings,
		Redirects:  redirects,
		RemoteAddr: trace.remoteAddress(),
		TLS:        httpResponse.TLS,
		Sent:       capture.sentRequest(),
	}
	return response, nil
}

func (c *Client) buildRequest(
	ctx context.Context,
	modelRequest *model.Request,
	body builtBody,
) (*http.Request, map[string]bool, error) {
	var reader io.Reader
	var closeReader io.ReadCloser
	if body.open != nil {
		var err error
		closeReader, err = body.open()
		if err != nil {
			return nil, nil, err
		}
		reader = closeReader
	} else if body.data != nil {
		reader = bytes.NewReader(body.data)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		modelRequest.Method,
		modelRequest.URL.Raw,
		reader,
	)
	if err != nil {
		if closeReader != nil {
			_ = closeReader.Close()
		}
		return nil, nil, err
	}
	if body.open != nil {
		request.GetBody = body.open
		request.ContentLength = body.size
	}
	suppressed := make(map[string]bool)
	for _, header := range modelRequest.Headers {
		canonical := http.CanonicalHeaderKey(header.Key)
		if header.Disabled {
			suppressed[canonical] = true
			continue
		}
		request.Header.Add(header.Key, header.Value)
	}
	for name := range suppressed {
		if request.Header.Values(name) == nil {
			request.Header[name] = nil
		}
	}
	setDefaultHeader(request.Header, suppressed, "User-Agent", "postctl/"+c.userAgentVersion())
	setDefaultHeader(request.Header, suppressed, "Accept", "*/*")
	setDefaultHeader(request.Header, suppressed, "Accept-Encoding", "gzip, deflate, br")
	setDefaultHeader(request.Header, suppressed, "Connection", "keep-alive")
	if body.contentType != "" {
		setDefaultHeader(request.Header, suppressed, "Content-Type", body.contentType)
	}
	return request, suppressed, nil
}

func (c *Client) userAgentVersion() string {
	if c != nil && c.version != "" {
		return c.version
	}
	return "dev"
}

type resolvedSettings struct {
	followRedirects      bool
	maxRedirects         int
	followOriginalMethod bool
	followAuthHeader     bool
	verifyTLS            bool
	timeout              time.Duration
	disableBodyPruning   bool
	disableCookies       bool
	removeReferer        bool
	httpVersion          string
}

func resolveSettings(request, defaults model.RequestSettings) resolvedSettings {
	return resolvedSettings{
		followRedirects:      boolSetting(request.FollowRedirects, defaults.FollowRedirects, true),
		maxRedirects:         intSetting(request.MaxRedirects, defaults.MaxRedirects, 10),
		followOriginalMethod: boolSetting(request.FollowOriginalMethod, defaults.FollowOriginalMethod, false),
		followAuthHeader:     boolSetting(request.FollowAuthHeader, defaults.FollowAuthHeader, false),
		verifyTLS:            boolSetting(request.VerifyTLS, defaults.VerifyTLS, true),
		timeout:              durationSetting(request.Timeout, defaults.Timeout, 30*time.Second),
		disableBodyPruning:   boolSetting(request.DisableBodyPruning, defaults.DisableBodyPruning, false),
		disableCookies:       boolSetting(request.DisableCookies, defaults.DisableCookies, false),
		removeReferer:        boolSetting(request.RemoveRefererOnRedirect, defaults.RemoveRefererOnRedirect, false),
		httpVersion:          stringSetting(request.HTTPVersion, defaults.HTTPVersion, "auto"),
	}
}

func newTransport(settings resolvedSettings) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{ //nolint:gosec // user-selected Postman-compatible option
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: !settings.verifyTLS,
	}
	// postctl advertises and decodes gzip itself so wire-size accounting stays
	// accurate and disabled Accept-Encoding rows remain disabled.
	transport.DisableCompression = true
	transport.ForceAttemptHTTP2 = settings.httpVersion != "http1"
	if settings.httpVersion == "http1" {
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	}
	return transport
}

func redirectPolicy(
	original *http.Request,
	settings resolvedSettings,
	suppressed map[string]bool,
	redirects *[]Redirect,
) func(*http.Request, []*http.Request) error {
	return func(next *http.Request, via []*http.Request) error {
		if !settings.followRedirects {
			return http.ErrUseLastResponse
		}
		if len(via) > settings.maxRedirects {
			return fmt.Errorf("stopped after %d redirects", settings.maxRedirects)
		}
		previous := via[len(via)-1]
		redirectResponse := next.Response
		if redirectResponse == nil {
			redirectResponse = previous.Response
		}
		if redirectResponse != nil {
			*redirects = append(*redirects, Redirect{
				StatusCode: redirectResponse.StatusCode,
				Location:   redirectResponse.Header.Get("Location"),
			})
		}
		if settings.followOriginalMethod {
			next.Method = original.Method
			if original.GetBody != nil {
				body, err := original.GetBody()
				if err != nil {
					return err
				}
				next.Body = body
				next.GetBody = original.GetBody
				next.ContentLength = original.ContentLength
			}
		}
		if settings.followAuthHeader {
			if authorization := original.Header.Get("Authorization"); authorization != "" {
				next.Header.Set("Authorization", authorization)
			}
		} else if !sameHost(original.URL.Host, next.URL.Host) {
			next.Header.Del("Authorization")
		}
		if settings.removeReferer || suppressed["Referer"] {
			next.Header.Del("Referer")
		}
		return nil
	}
}

func sameHost(first, second string) bool {
	return strings.EqualFold(first, second)
}

func cloneHTTPRequest(source *http.Request) (*http.Request, error) {
	clone := source.Clone(source.Context())
	if source.GetBody != nil {
		body, err := source.GetBody()
		if err != nil {
			return nil, err
		}
		clone.Body = body
	}
	return clone, nil
}

type captureTransport struct {
	base *http.Transport
	mu   sync.Mutex
	sent SentRequest
}

// RoundTrip captures the wire request before delegating to net/http.
func (c *captureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var sentBody bytes.Buffer
	if request.Body != nil {
		request.Body = &teeReadCloser{
			Reader: io.TeeReader(request.Body, &sentBody),
			Closer: request.Body,
		}
	}
	response, err := c.base.RoundTrip(request)
	c.mu.Lock()
	c.sent = SentRequest{
		Method:  request.Method,
		URL:     request.URL.String(),
		Headers: headerKVs(request.Header),
	}
	if request.Body != nil {
		c.sent.Body = append([]byte(nil), sentBody.Bytes()...)
	}
	c.mu.Unlock()
	return response, err
}

func (c *captureTransport) sentRequest() SentRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := c.sent
	result.Headers = append([]model.KV(nil), result.Headers...)
	result.Body = append([]byte(nil), result.Body...)
	return result
}

type teeReadCloser struct {
	io.Reader
	io.Closer
}

type traceRecorder struct {
	mu sync.Mutex

	dnsStart     time.Time
	dnsDuration  time.Duration
	connectStart time.Time
	connect      time.Duration
	tlsStart     time.Time
	tlsDuration  time.Duration
	wrote        time.Time
	firstByte    time.Time
	remote       string
}

func (t *traceRecorder) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn: func(string) {
			t.mu.Lock()
			t.dnsStart = time.Time{}
			t.dnsDuration = 0
			t.connectStart = time.Time{}
			t.connect = 0
			t.tlsStart = time.Time{}
			t.tlsDuration = 0
			t.wrote = time.Time{}
			t.firstByte = time.Time{}
			t.remote = ""
			t.mu.Unlock()
		},
		DNSStart: func(httptrace.DNSStartInfo) {
			t.mu.Lock()
			t.dnsStart = time.Now()
			t.mu.Unlock()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			t.mu.Lock()
			t.dnsDuration = time.Since(t.dnsStart)
			t.mu.Unlock()
		},
		ConnectStart: func(_, _ string) {
			t.mu.Lock()
			t.connectStart = time.Now()
			t.mu.Unlock()
		},
		ConnectDone: func(_, _ string, _ error) {
			t.mu.Lock()
			t.connect = time.Since(t.connectStart)
			t.mu.Unlock()
		},
		TLSHandshakeStart: func() {
			t.mu.Lock()
			t.tlsStart = time.Now()
			t.mu.Unlock()
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			t.mu.Lock()
			t.tlsDuration = time.Since(t.tlsStart)
			t.mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			t.mu.Lock()
			t.remote = info.Conn.RemoteAddr().String()
			t.mu.Unlock()
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			t.mu.Lock()
			t.wrote = time.Now()
			t.mu.Unlock()
		},
		GotFirstResponseByte: func() {
			t.mu.Lock()
			t.firstByte = time.Now()
			t.mu.Unlock()
		},
	}
}

func (t *traceRecorder) timings(finished time.Time) Timings {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := Timings{
		DNS:     t.dnsDuration,
		Connect: t.connect,
		TLS:     t.tlsDuration,
	}
	if !t.wrote.IsZero() && !t.firstByte.IsZero() {
		result.TTFB = t.firstByte.Sub(t.wrote)
	}
	if !t.firstByte.IsZero() {
		result.Download = finished.Sub(t.firstByte)
	}
	return result
}

func (t *traceRecorder) remoteAddress() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.remote
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.count += int64(n)
	return n, err
}

func decodeBody(source io.Reader, encoding string) (io.Reader, func() error, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		return source, nil, nil
	case "gzip":
		reader, err := gzip.NewReader(source)
		if err != nil {
			return nil, nil, err
		}
		return reader, reader.Close, nil
	case "br":
		return brotli.NewReader(source), nil, nil
	case "deflate":
		data, err := io.ReadAll(source)
		if err != nil {
			return nil, nil, err
		}
		reader, err := zlib.NewReader(bytes.NewReader(data))
		if err == nil {
			return reader, reader.Close, nil
		}
		raw := flate.NewReader(bytes.NewReader(data))
		return raw, raw.Close, nil
	default:
		return source, nil, nil
	}
}

func readLimited(reader io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func headerKVs(header http.Header) []model.KV {
	keys := make([]string, 0, len(header))
	for key := range header {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]model.KV, 0, len(header))
	for _, key := range keys {
		for _, value := range header.Values(key) {
			result = append(result, model.KV{Key: key, Value: value})
		}
	}
	return result
}

func responseCookies(cookies []*http.Cookie) []model.Cookie {
	result := make([]model.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		expires := ""
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.Format(time.RFC1123)
		}
		maxAge := ""
		if cookie.MaxAge != 0 {
			maxAge = strconv.Itoa(cookie.MaxAge)
		}
		result = append(result, model.Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  expires,
			MaxAge:   maxAge,
			HTTPOnly: cookie.HttpOnly,
			Secure:   cookie.Secure,
			Session:  cookie.Expires.IsZero() && cookie.MaxAge == 0,
		})
	}
	return result
}

func responseHeaderSize(response *http.Response) int64 {
	var buffer bytes.Buffer
	writer := bufio.NewWriter(&buffer)
	_, _ = fmt.Fprintf(writer, "%s %s\r\n", response.Proto, response.Status)
	_ = response.Header.Write(writer)
	_, _ = writer.WriteString("\r\n")
	_ = writer.Flush()
	return int64(buffer.Len())
}

func statusText(response *http.Response) string {
	prefix := strconv.Itoa(response.StatusCode) + " "
	return strings.TrimPrefix(response.Status, prefix)
}

func setDefaultHeader(
	header http.Header,
	suppressed map[string]bool,
	name, value string,
) {
	canonical := http.CanonicalHeaderKey(name)
	if suppressed[canonical] || header.Values(name) != nil {
		return
	}
	header.Set(name, value)
}

func boolSetting(primary, fallback *bool, builtIn bool) bool {
	if primary != nil {
		return *primary
	}
	if fallback != nil {
		return *fallback
	}
	return builtIn
}

func intSetting(primary, fallback *int, builtIn int) int {
	if primary != nil {
		return *primary
	}
	if fallback != nil {
		return *fallback
	}
	return builtIn
}

func durationSetting(primary, fallback *time.Duration, builtIn time.Duration) time.Duration {
	if primary != nil {
		return *primary
	}
	if fallback != nil {
		return *fallback
	}
	return builtIn
}

func stringSetting(primary, fallback, builtIn string) string {
	if primary != "" {
		return primary
	}
	if fallback != "" {
		return fallback
	}
	return builtIn
}
