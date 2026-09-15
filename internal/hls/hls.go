// Package hls handles the parts of HLS playlists anitui needs to touch.
package hls

import (
	"bytes"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	uriAttr    = regexp.MustCompile(`URI="([^"]*)"`)
	resolution = regexp.MustCompile(`RESOLUTION=\d+x(\d+)`)
)

// IsPlaylist reports whether body looks like an M3U8 playlist.
func IsPlaylist(body []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(body, "\uFEFF \t\r\n"), []byte("#EXTM3U"))
}

// Rewrite maps every URI in a playlist (segment and variant lines, plus URI="…"
// attributes on tags like EXT-X-KEY and EXT-X-MAP) through fn, after resolving
// it against base. Line endings are preserved.
func Rewrite(body []byte, base *url.URL, fn func(absolute string) string) []byte {
	resolve := func(ref string) string {
		u, err := base.Parse(strings.TrimSpace(ref))
		if err != nil {
			return ref
		}
		return fn(u.String())
	}

	lines := bytes.SplitAfter(body, []byte("\n"))
	var out bytes.Buffer
	out.Grow(len(body) * 2)
	for _, raw := range lines {
		content := bytes.TrimRight(raw, "\r\n")
		ending := raw[len(content):]
		trimmed := bytes.TrimSpace(content)
		switch {
		case len(trimmed) == 0:
			out.Write(raw)
		case trimmed[0] == '#':
			out.Write(uriAttr.ReplaceAllFunc(content, func(m []byte) []byte {
				ref := uriAttr.FindSubmatch(m)[1]
				return []byte(`URI="` + resolve(string(ref)) + `"`)
			}))
			out.Write(ending)
		default:
			out.WriteString(resolve(string(trimmed)))
			out.Write(ending)
		}
	}
	return out.Bytes()
}

// KeepVariant removes every #EXT-X-STREAM-INF variant whose resolution height
// isn't height, leaving renditions and other tags alone. If no variant has that
// height, or body isn't a master playlist, body is returned unchanged.
func KeepVariant(body []byte, height int) []byte {
	lines := bytes.SplitAfter(body, []byte("\n"))
	out := make([][]byte, 0, len(lines))
	kept, dropped := 0, 0
	for i := 0; i < len(lines); i++ {
		line := bytes.TrimSpace(lines[i])
		if !bytes.HasPrefix(line, []byte("#EXT-X-STREAM-INF")) {
			out = append(out, lines[i])
			continue
		}
		// The variant's URI is the next non-blank, non-tag line.
		j := i + 1
		for j < len(lines) {
			next := bytes.TrimSpace(lines[j])
			if len(next) > 0 && next[0] != '#' {
				break
			}
			j++
		}
		m := resolution.FindSubmatch(line)
		if m != nil && string(m[1]) == strconv.Itoa(height) {
			out = append(out, lines[i:min(j+1, len(lines))]...)
			kept++
		} else {
			dropped++
		}
		i = j
	}
	if kept == 0 || dropped == 0 {
		return body
	}
	return bytes.Join(out, nil)
}

// VariantHeights lists the resolution heights of a master playlist's variants,
// in playlist order.
func VariantHeights(body []byte) []int {
	var heights []int
	for _, line := range bytes.Split(body, []byte("\n")) {
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("#EXT-X-STREAM-INF")) {
			continue
		}
		if m := resolution.FindSubmatch(line); m != nil {
			h, _ := strconv.Atoi(string(m[1]))
			heights = append(heights, h)
		}
	}
	return heights
}
