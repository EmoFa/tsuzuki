package hls

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestRewrite(t *testing.T) {
	in := "#EXTM3U\r\n" +
		"#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\",IV=0x1\r\n" +
		"#EXT-X-MAP:URI=\"/init.mp4\"\r\n" +
		"#EXTINF:10,\r\n" +
		"seg-1.ts\r\n" +
		"\r\n" +
		"#EXTINF:10,\r\n" +
		"https://cdn.example/abs/seg-2.ts?x=1\r\n" +
		"#EXT-X-ENDLIST"
	base, _ := url.Parse("https://host.example/path/uwu.m3u8")

	got := string(Rewrite([]byte(in), base, func(abs string) string { return "P(" + abs + ")" }))
	want := "#EXTM3U\r\n" +
		"#EXT-X-KEY:METHOD=AES-128,URI=\"P(https://host.example/path/key.bin)\",IV=0x1\r\n" +
		"#EXT-X-MAP:URI=\"P(https://host.example/init.mp4)\"\r\n" +
		"#EXTINF:10,\r\n" +
		"P(https://host.example/path/seg-1.ts)\r\n" +
		"\r\n" +
		"#EXTINF:10,\r\n" +
		"P(https://cdn.example/abs/seg-2.ts?x=1)\r\n" +
		"#EXT-X-ENDLIST"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRewriteKwikFixture(t *testing.T) {
	body, err := os.ReadFile("../extractor/testdata/kwik_media.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	if !IsPlaylist(body) {
		t.Fatal("fixture not detected as playlist")
	}
	base, _ := url.Parse("https://vault-08.uwucdn.top/stream/x/uwu.m3u8")
	var uris []string
	out := string(Rewrite(body, base, func(abs string) string {
		uris = append(uris, abs)
		return "http://proxy/"
	}))
	// 156 segments + 1 key.
	if len(uris) != 157 {
		t.Fatalf("rewrote %d URIs", len(uris))
	}
	if strings.Contains(out, "uwucdn") {
		t.Fatal("upstream URL left in rewritten playlist")
	}
}

func TestIsPlaylist(t *testing.T) {
	if IsPlaylist([]byte("\x00\x00\x01\xba")) {
		t.Error("binary detected as playlist")
	}
	if !IsPlaylist([]byte("\uFEFF#EXTM3U\n")) {
		t.Error("BOM-prefixed playlist not detected")
	}
}

const master = `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="a",NAME="Japanese",LANGUAGE="ja",URI="audio/ja.m3u8"

#EXT-X-STREAM-INF:BANDWIDTH=3476000,RESOLUTION=1920x1080,AUDIO="a"
video/1080.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=1200000,RESOLUTION=854x480,AUDIO="a"
video/480.m3u8
`

func TestKeepVariant(t *testing.T) {
	got := string(KeepVariant([]byte(master), 480))
	if strings.Contains(got, "1080") || !strings.Contains(got, "video/480.m3u8") || !strings.Contains(got, "audio/ja.m3u8") {
		t.Fatalf("got:\n%s", got)
	}
	if got := string(KeepVariant([]byte(master), 720)); got != master {
		t.Errorf("unknown height should leave playlist unchanged:\n%s", got)
	}
	media := "#EXTM3U\n#EXTINF:10,\nseg.ts\n"
	if got := string(KeepVariant([]byte(media), 480)); got != media {
		t.Errorf("media playlist changed:\n%s", got)
	}
}

func TestVariantHeights(t *testing.T) {
	if got := VariantHeights([]byte(master)); len(got) != 2 || got[0] != 1080 || got[1] != 480 {
		t.Fatalf("got %v", got)
	}
}
