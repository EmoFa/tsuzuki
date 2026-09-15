package extractor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/browser"
	"github.com/EmoFa/tsuzuki/internal/httpx"
)

func TestKwikSourceFixture(t *testing.T) {
	page, err := os.ReadFile("testdata/kwik_embed.html")
	if err != nil {
		t.Fatal(err)
	}
	got, err := KwikSource(string(page))
	if err != nil {
		t.Fatal(err)
	}
	want := "https://vault-08.uwucdn.top/stream/08/13/2c42dca1bbf0c061e3add6d1874a1d01498fb3e29c62e17071b0b2802b4e8b06/uwu.m3u8"
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestUnpackSynthetic(t *testing.T) {
	// Radix-62 packed script. Word 0 is empty, so "0" maps to itself (the
	// packer's fallback) and survives into the output.
	src := `eval(function(p,a,c,k,e,d){}('2 0=\'3\';4.5(0)',62,6,'|x|var|hello|console|log'.split('|'),0,{}))`
	scripts, err := UnpackAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if want := "var 0='hello';console.log(0)"; scripts[0] != want {
		t.Fatalf("got %q, want %q", scripts[0], want)
	}
}

func TestEncodeBase(t *testing.T) {
	for n, want := range map[int]string{0: "0", 9: "9", 10: "a", 35: "z", 36: "A", 61: "Z", 62: "10", 125: "21"} {
		if got := encodeBase(n, 62); got != want {
			t.Errorf("encodeBase(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestUnpackNoScripts(t *testing.T) {
	if _, err := KwikSource("<html></html>"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveKwikSendsReferer(t *testing.T) {
	page, _ := os.ReadFile("testdata/kwik_embed.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Referer() != "https://animepahe.pw/" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write(page)
	}))
	defer srv.Close()

	got, err := ResolveKwik(context.Background(), httpx.New(httpx.Options{}), srv.URL+"/e/abc", "https://animepahe.pw/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "/uwu.m3u8") {
		t.Fatalf("got %s", got)
	}
}

type fakeSniffer struct {
	got map[string]browser.Captured
	req browser.SniffRequest
}

func (f *fakeSniffer) Sniff(_ context.Context, req browser.SniffRequest) (map[string]browser.Captured, error) {
	f.req = req
	return f.got, nil
}

func TestResolveMegaplay(t *testing.T) {
	sources, _ := os.ReadFile("testdata/megaplay_getsources.json")
	master, _ := os.ReadFile("testdata/megaplay_master.m3u8")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Referer() != MegaplayOrigin {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write(master)
	}))
	defer srv.Close()

	sn := &fakeSniffer{got: map[string]browser.Captured{
		"sources": {Body: sources},
		"master":  {URL: srv.URL + "/806c/c512/master.m3u8?token=abc"},
	}}
	res, err := ResolveMegaplay(context.Background(), sn, httpx.New(httpx.Options{}), "https://megaplay.buzz/stream/s-2/163517/sub?s=tcdn", "https://anikototv.to/")
	if err != nil {
		t.Fatal(err)
	}
	if sn.req.Referer != "https://anikototv.to/" || len(sn.req.Matches) != 2 {
		t.Errorf("sniff request = %+v", sn.req)
	}
	if len(res.Variants) < 2 || res.Variants[0].Height != 1080 || res.Variants[0].URI != srv.URL+"/806c/c512/index-f1-v1-a1.m3u8" {
		t.Errorf("variants = %+v", res.Variants)
	}
	if len(res.Tracks) != 1 || res.Tracks[0].Label != "English" || res.Intro.Start != 116 || res.Outro.End != 1470 {
		t.Errorf("result = %+v", res)
	}
}
