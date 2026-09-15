package store

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/EmoFa/anitui/internal/httpx"
)

func TestClearanceRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if cl, err := s.LoadClearance(ctx, "animepahe.pw"); cl != nil || err != nil {
		t.Fatalf("empty store: %v %v", cl, err)
	}

	exp := time.Now().Add(time.Hour).Truncate(time.Second)
	in := &httpx.Clearance{
		UserAgent:  "UA/1",
		RemoteIP:   "172.64.80.1",
		Cookies:    []*http.Cookie{{Name: "cf_clearance", Value: "abc", Domain: ".animepahe.pw", Expires: exp}},
		ObtainedAt: time.Now(),
	}
	if err := s.SaveClearance(ctx, "animepahe.pw", in); err != nil {
		t.Fatal(err)
	}
	in.UserAgent = "UA/2"
	if err := s.SaveClearance(ctx, "animepahe.pw", in); err != nil {
		t.Fatal(err) // upsert
	}

	out, err := s.LoadClearance(ctx, "animepahe.pw")
	if err != nil {
		t.Fatal(err)
	}
	if out.UserAgent != "UA/2" || out.RemoteIP != "172.64.80.1" || len(out.Cookies) != 1 || out.Cookies[0].Value != "abc" || !out.Cookies[0].Expires.Equal(exp) {
		t.Fatalf("got %+v %+v", out, out.Cookies[0])
	}

	if list, err := s.Clearances(ctx); err != nil || len(list) != 1 || list[0].Host != "animepahe.pw" {
		t.Fatalf("clearances = %+v err=%v", list, err)
	}

	if err := s.DeleteClearance(ctx, "animepahe.pw"); err != nil {
		t.Fatal(err)
	}
	if cl, _ := s.LoadClearance(ctx, "animepahe.pw"); cl != nil {
		t.Fatal("not deleted")
	}
}
