package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const clearUA = "SolvedBrowser/1.0"

// challengeServer serves a Cloudflare challenge unless the request carries the
// clearance cookie with the matching User-Agent.
func challengeServer(t *testing.T, validValue *atomic.Value) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("cf_clearance")
		if err == nil && ck.Value == validValue.Load().(string) && r.UserAgent() == clearUA {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("<title>Just a moment...</title>"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type fakeSolver struct {
	value atomic.Value
	calls atomic.Int32
	delay time.Duration
	err   error
}

func (s *fakeSolver) Solve(ctx context.Context, pageURL string) (*Clearance, error) {
	s.calls.Add(1)
	time.Sleep(s.delay)
	if s.err != nil {
		return nil, s.err
	}
	return &Clearance{
		UserAgent: clearUA,
		Cookies:   []*http.Cookie{{Name: "cf_clearance", Value: s.value.Load().(string)}},
	}, nil
}

type memStore struct {
	mu sync.Mutex
	m  map[string]*Clearance
}

func (s *memStore) LoadClearance(_ context.Context, host string) (*Clearance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[host], nil
}

func (s *memStore) SaveClearance(_ context.Context, host string, c *Clearance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[host] = c
	return nil
}

func (s *memStore) DeleteClearance(_ context.Context, host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, host)
	return nil
}

func TestChallengeSolvedAndPersisted(t *testing.T) {
	var valid atomic.Value
	valid.Store("token-1")
	srv := challengeServer(t, &valid)
	solver := &fakeSolver{}
	solver.value.Store("token-1")
	store := &memStore{m: map[string]*Clearance{}}

	c := New(Options{Solver: solver, Store: store})
	var out struct{ OK bool }
	if err := c.GetJSON(context.Background(), srv.URL, nil, &out); err != nil || !out.OK {
		t.Fatalf("ok=%v err=%v", out.OK, err)
	}
	if solver.calls.Load() != 1 {
		t.Fatalf("solver calls = %d", solver.calls.Load())
	}
	if store.m["127.0.0.1"] == nil {
		t.Fatal("clearance not persisted")
	}

	// A fresh client reuses the stored clearance without solving.
	c2 := New(Options{Solver: solver, Store: store})
	if err := c2.GetJSON(context.Background(), srv.URL, nil, &out); err != nil {
		t.Fatal(err)
	}
	if solver.calls.Load() != 1 {
		t.Fatalf("stored clearance not reused; solver calls = %d", solver.calls.Load())
	}
}

func TestStaleClearanceIsReplaced(t *testing.T) {
	var valid atomic.Value
	valid.Store("new")
	srv := challengeServer(t, &valid)
	solver := &fakeSolver{}
	solver.value.Store("new")
	store := &memStore{m: map[string]*Clearance{
		"127.0.0.1": {UserAgent: clearUA, Cookies: []*http.Cookie{{Name: "cf_clearance", Value: "old"}}},
	}}

	c := New(Options{Solver: solver, Store: store})
	if _, err := c.Get(context.Background(), srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	if got := store.m["127.0.0.1"].Cookies[0].Value; got != "new" {
		t.Fatalf("stored cookie = %q", got)
	}
}

func TestChallengeWithoutSolver(t *testing.T) {
	var valid atomic.Value
	valid.Store("x")
	srv := challengeServer(t, &valid)

	_, err := New(Options{}).Get(context.Background(), srv.URL, nil)
	var ce *ChallengeError
	if !errors.As(err, &ce) || ce.Kind != "cloudflare" {
		t.Fatalf("err = %v", err)
	}
}

func TestSolverThatDoesNotHelpGivesUp(t *testing.T) {
	var valid atomic.Value
	valid.Store("server-wants-this")
	srv := challengeServer(t, &valid)
	solver := &fakeSolver{}
	solver.value.Store("wrong")

	_, err := New(Options{Solver: solver}).Get(context.Background(), srv.URL, nil)
	var ce *ChallengeError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	if solver.calls.Load() != 1 {
		t.Fatalf("solver calls = %d, want exactly 1", solver.calls.Load())
	}
}

func TestSolverError(t *testing.T) {
	var valid atomic.Value
	valid.Store("x")
	srv := challengeServer(t, &valid)
	boom := errors.New("no browser")
	solver := &fakeSolver{err: boom}

	_, err := New(Options{Solver: solver}).Get(context.Background(), srv.URL, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentChallengesSolveOnce(t *testing.T) {
	var valid atomic.Value
	valid.Store("tok")
	srv := challengeServer(t, &valid)
	solver := &fakeSolver{delay: 50 * time.Millisecond}
	solver.value.Store("tok")
	c := New(Options{Solver: solver})

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := c.Get(context.Background(), srv.URL, nil)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := solver.calls.Load(); n != 1 {
		t.Fatalf("solver calls = %d, want 1", n)
	}
}

func TestWAFBlockIsNotAChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("<title>Attention Required! | Cloudflare</title>"))
	}))
	defer srv.Close()
	solver := &fakeSolver{}
	solver.value.Store("x")

	_, err := New(Options{Solver: solver}).Get(context.Background(), srv.URL, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != 403 {
		t.Fatalf("err = %v", err)
	}
	if solver.calls.Load() != 0 {
		t.Fatal("solver must not run for a WAF block")
	}
}

func TestRetriesTransientErrorsAndReplaysBody(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 64)
		n, _ := r.Body.Read(body)
		if string(body[:n]) != `{"q":"x"}` {
			t.Errorf("attempt %d body = %q", hits.Load()+1, body[:n])
		}
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{"n":3}`))
	}))
	defer srv.Close()

	c := New(Options{Backoff: time.Millisecond})
	var out struct{ N int }
	if err := c.PostJSON(context.Background(), srv.URL, nil, map[string]string{"q": "x"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.N != 3 || hits.Load() != 3 {
		t.Fatalf("n=%d hits=%d", out.N, hits.Load())
	}
}

func TestDefaultUserAgentAndHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.UserAgent() + "|" + r.Referer()))
	}))
	defer srv.Close()

	body, err := New(Options{}).Get(context.Background(), srv.URL, map[string]string{"Referer": "https://example.com/"})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != DefaultUserAgent+"|https://example.com/" {
		t.Fatalf("got %q", body)
	}
}

func TestParentDomain(t *testing.T) {
	for in, want := range map[string]string{
		"a.b.c":        "b.c",
		"animepahe.pw": "",
		"x":            "",
		"127.0.0.1":    "",
	} {
		if got := parentDomain(in); got != want {
			t.Errorf("parentDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFamilyFor(t *testing.T) {
	store := &memStore{m: map[string]*Clearance{
		"v4.example": {RemoteIP: "172.64.80.1"},
		"v6.example": {RemoteIP: "2606:4700:130:436c:6f75:6466:6c61:7265"},
		"br.example": {RemoteIP: "[2606:4700::1]"},
		"no.example": {},
	}}
	c := New(Options{Store: store})
	ctx := context.Background()
	for host, want := range map[string]string{
		"v4.example": "tcp4", "api.v4.example": "tcp4",
		"v6.example": "tcp6", "br.example": "tcp6",
		"no.example": "", "other.example": "",
	} {
		if got := c.familyFor(ctx, host); got != want {
			t.Errorf("familyFor(%q) = %q, want %q", host, got, want)
		}
	}
}
