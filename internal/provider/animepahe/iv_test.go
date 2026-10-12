package animepahe

import (
	"strings"
	"testing"
)

// kwik's playlists skip segment numbers, and the IVs follow the file names.
func TestExplicitIVsFollowFileNames(t *testing.T) {
	in := `#EXTM3U
#EXT-X-TARGETDURATION:18
#EXT-X-KEY:METHOD=AES-128,URI="https://vault-13.owocdn.top/stream/13/02/x/mon.key"
#EXT-X-MEDIA-SEQUENCE:1
#EXTINF:3.879,
https://vault-13.owocdn.top/stream/13/02/x/segment-1-v1-a1.jpg
#EXTINF:4.171,
https://vault-13.owocdn.top/stream/13/02/x/segment-2-v1-a1.jpg
#EXTINF:5.214,
https://vault-13.owocdn.top/stream/13/02/x/segment-4-v1-a1.jpg?t=1
#EXT-X-ENDLIST`
	out, err := explicitIVs([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		`#EXT-X-KEY:METHOD=AES-128,URI="https://vault-13.owocdn.top/stream/13/02/x/mon.key",IV=0x00000000000000000000000000000001` +
			"\nhttps://vault-13.owocdn.top/stream/13/02/x/segment-1-v1-a1.jpg",
		`IV=0x00000000000000000000000000000004` + "\nhttps://vault-13.owocdn.top/stream/13/02/x/segment-4-v1-a1.jpg?t=1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The key is restated just before each segment's URI, and every original
	// line survives in order.
	if n := strings.Count(got, "#EXT-X-KEY"); n != 4 {
		t.Errorf("%d key lines, want the original and one per segment", n)
	}
	if !strings.Contains(got, "segment-4-v1-a1.jpg?t=1\n#EXT-X-ENDLIST") {
		t.Errorf("segments moved:\n%s", got)
	}
}

// Playlists that already give an IV, aren't encrypted, or are masters pass through.
func TestExplicitIVsLeavesOtherPlaylistsAlone(t *testing.T) {
	for _, in := range []string{
		"#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"k\",IV=0x01\n#EXTINF:4,\nsegment-1-v1-a1.jpg",
		"#EXTM3U\n#EXTINF:4,\nsegment-1-v1-a1.jpg",
		"#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nuwu.m3u8",
	} {
		out, _ := explicitIVs([]byte(in))
		if string(out) != in {
			t.Errorf("changed:\n%s\ninto:\n%s", in, out)
		}
	}
}
