package graph_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/outlook-cli/internal/adapter/graph"
)

// fakeClock never really sleeps and records the waits.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.mu.Unlock()
	return ctx.Err()
}

// rec records requests a test server saw.
type rec struct {
	mu   sync.Mutex
	reqs []seen
}

type seen struct {
	Method, Path, RawQuery string
	Header                 http.Header
	Body                   string
}

func (r *rec) all() []seen { r.mu.Lock(); defer r.mu.Unlock(); return append([]seen(nil), r.reqs...) }
func (r *rec) last(t *testing.T) seen {
	t.Helper()
	a := r.all()
	if len(a) == 0 {
		t.Fatal("no request seen")
	}
	return a[len(a)-1]
}

// env is a client against an httptest "Graph". The handler is wrapped so the
// bearer token is checked by authtest's fake resource server.
type env struct {
	srv   *httptest.Server
	c     *graph.Client
	rec   *rec
	clk   *fakeClock
	fake  *authtest.Fake
	token *auth.Authorizer
}

// testPageKey is the HMAC key provider newEnv wires in.
func testPageKey() ([]byte, error) { return []byte("0123456789abcdef0123456789abcdef"), nil }

func newEnv(t *testing.T, sc authtest.Scenario, h http.HandlerFunc) *env {
	t.Helper()
	return newEnvKey(t, sc, testPageKey, h)
}

// newEnvKey is newEnv with an explicit page token key provider (nil allowed).
func newEnvKey(t *testing.T, sc authtest.Scenario, key func() ([]byte, error), h http.HandlerFunc) *env {
	t.Helper()
	f := authtest.New(sc)
	r := &rec{}
	gate := f.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.reqs = append(r.reqs, seen{req.Method, req.URL.EscapedPath(), req.URL.RawQuery, req.Header.Clone(), string(b)})
		r.mu.Unlock()
		req.Body = io.NopCloser(strings.NewReader(string(b)))
		probe := httptest.NewRecorder()
		gate.ServeHTTP(probe, req)
		if probe.Code == http.StatusUnauthorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h(w, req)
	}))
	t.Cleanup(srv.Close)
	src, err := auth.NewDaemonTokenSource(f, "msgraph")
	if err != nil {
		t.Fatal(err)
	}
	az := auth.NewAuthorizer(src)
	clk := &fakeClock{now: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}
	c, err := graph.New(graph.Config{
		Refresher:    az,
		BaseURL:      srv.URL + "/v1.0",
		PageTokenKey: key,
		HTTP:         httpx.Config{MaxRetries: 2, Clock: clk, Jitter: -1, Rand: func() float64 { return 0.5 }},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &env{srv: srv, c: c, rec: r, clk: clk, fake: f, token: az}
}

func jsonReply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
