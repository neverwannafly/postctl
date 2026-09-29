package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestRunNoPersistSkipsDiskButSharesMemory(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.env.Variables = append(f.env.Variables, model.Variable{Key: "tok", Value: "initial-token-1234", Type: model.VarSecret})
	rs := addRequests(f, f.col, "a", "b", "c")
	rs[0].Scripts.Test = js(`pm.environment.set("tok", "run-token-5678");`)
	rs[1].Headers = []model.KV{{Key: "X-Seen", Value: "{{tok}}"}}
	f.save()
	before := filesUnder(t, f.dir)
	for p := range before {
		if strings.Contains(p, ".postctl") {
			delete(before, p)
		}
	}

	sum, _ := f.run(RunInput{NoPersist: true})
	if sum.Requests != 3 {
		t.Fatalf("requests: %d", sum.Requests)
	}
	if got := srv.hdrs[1].Get("X-Seen"); got != "run-token-5678" {
		t.Fatalf("second request saw %q", got)
	}
	after := filesUnder(t, f.dir)
	for p := range after {
		if strings.Contains(p, ".postctl") {
			delete(after, p)
		}
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("workspace YAML changed with NoPersist")
	}
	env := f.reloadEnv()
	for _, v := range env.Variables {
		if v.Key == "tok" {
			got, err := f.secrets.Get(context.Background(), v.SecretRef)
			if err != nil || got != "initial-token-1234" {
				t.Fatalf("secret on disk should be unchanged, got %q err=%v", got, err)
			}
		}
	}
}

func TestRunOnlyRequestsSubset(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "a", "b", "c")
	f.save()
	idB := rs[1].ID

	sum, events := f.run(RunInput{OnlyRequests: []model.ID{idB}})
	if sum.Requests != 1 {
		t.Fatalf("requests: %d", sum.Requests)
	}
	if len(srv.hits()) != 1 {
		t.Fatalf("hits: %v", srv.hits())
	}
	names := requestNames(events)
	if len(names) != 1 || names[0] != "b" {
		t.Fatalf("ran %v", names)
	}
}

func TestRunOnlyRequestsSetNextOutsideStops(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "a", "b")
	rs[0].Scripts.Test = js(`pm.execution.setNextRequest("b");`)
	f.save()

	sum, _ := f.run(RunInput{OnlyRequests: []model.ID{rs[0].ID}})
	if sum.StopReason != StopNextMissing {
		t.Fatalf("stop: %q", sum.StopReason)
	}
	if len(srv.hits()) != 1 {
		t.Fatalf("hits: %v", srv.hits())
	}
}

func requestNames(events []RunEvent) []string {
	var names []string
	for _, ev := range events {
		if rs, ok := ev.(RequestStarted); ok {
			names = append(names, rs.RequestName)
		}
	}
	return names
}
