package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/moby/moby/client"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// fakeEngine is a minimal Docker Engine API: it records every call and
// answers from a table of path substrings.
type fakeEngine struct {
	mu    sync.Mutex
	calls []string
	reply map[string]int // "METHOD path-substring" -> status
}

func (e *fakeEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	call := r.Method + " " + r.URL.Path
	e.mu.Lock()
	e.calls = append(e.calls, call)
	e.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Api-Version", "1.47")
	for k, status := range e.reply {
		if m, p, _ := strings.Cut(k, " "); m == r.Method && strings.Contains(r.URL.Path, p) {
			w.WriteHeader(status)
			if status >= 400 {
				w.Write([]byte(`{"message":"engine says no"}`))
			} else {
				w.Write([]byte(`{}`))
			}
			return
		}
	}
	w.Write([]byte(`{}`))
}

func (e *fakeEngine) called(method, path string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.calls {
		if strings.HasPrefix(c, method+" ") && strings.Contains(c, path) {
			return true
		}
	}
	return false
}

func newFakeDocker(t *testing.T, reply map[string]int) (*Docker, *fakeEngine) {
	t.Helper()
	e := &fakeEngine{reply: reply}
	ts := httptest.NewServer(e)
	t.Cleanup(ts.Close)
	cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(ts.URL, "http://")), client.WithAPIVersion("1.47"))
	if err != nil {
		t.Fatal(err)
	}
	return &Docker{cli: cli}, e
}

// Recreating a server's container (new limits) must not delete the old one
// when the image cannot be pulled, or the server is left with no container.
func TestCreateKeepsTheOldContainerWhenThePullFails(t *testing.T) {
	// The pull fails and the node has no copy of the image either.
	d, e := newFakeDocker(t, map[string]int{"POST /images/create": 500, "GET /images/": 404})
	err := d.Create(context.Background(), Spec{ServerSpec: proto.ServerSpec{UUID: "11111111-2222-4333-8444-555555555555", Image: "example/game:1"}, DataDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "engine says no") {
		t.Fatalf("expected the pull error, got %v", err)
	}
	if e.called("DELETE", "/containers/") {
		t.Fatal("the old container was removed although the new image could not be pulled")
	}
}

// A node that already has the image (built there, or the registry is down) still starts servers.
func TestPullFallsBackToTheImageOnTheNode(t *testing.T) {
	d, e := newFakeDocker(t, map[string]int{"POST /images/create": 500})
	if err := d.pull(context.Background(), "example/game:1"); err != nil {
		t.Fatalf("the copy on the node should be used: %v", err)
	}
	if !e.called("GET", "/images/example/game:1/json") {
		t.Fatal("the image on the node was not looked up")
	}
}

func TestRemoveTreatsAMissingContainerAsRemoved(t *testing.T) {
	d, _ := newFakeDocker(t, map[string]int{"DELETE /containers/": 404})
	if err := d.Remove(context.Background(), "x"); err != nil {
		t.Fatalf("a missing container should count as removed: %v", err)
	}
	d, _ = newFakeDocker(t, map[string]int{"DELETE /containers/": 500})
	if err := d.Remove(context.Background(), "x"); err == nil {
		t.Fatal("a failed removal must be reported")
	}
}
