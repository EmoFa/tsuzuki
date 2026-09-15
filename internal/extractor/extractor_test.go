package extractor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/EmoFa/anitui/internal/httpx"
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
