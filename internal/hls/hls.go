// Package hls handles the parts of HLS playlists tsuzuki needs to touch.
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

// Variant is one #EXT-X-STREAM-INF entry of a master playlist.
type Variant struct {
	Height int    // 0 when RESOLUTION is missing
	URI    string // resolved against the master's URL
}

// Variants lists a master playlist's variants in playlist order.
func Variants(body []byte, base *url.URL) []Variant {
	var out []Variant
	lines := bytes.Split(body, []byte("\n"))
	for i := 0; i < len(lines); i++ {
		line := bytes.TrimSpace(lines[i])
		if !bytes.HasPrefix(line, []byte("#EXT-X-STREAM-INF")) {
			continue
		}
		v := Variant{}
		if m := resolution.FindSubmatch(line); m != nil {
			v.Height, _ = strconv.Atoi(string(m[1]))
		}
		for i+1 < len(lines) {
			i++
			next := bytes.TrimSpace(lines[i])
			if len(next) == 0 || next[0] == '#' {
				continue
			}
			v.URI = string(next)
			if base != nil {
				if u, err := base.Parse(v.URI); err == nil {
					v.URI = u.String()
				}
			}
			break
		}
		out = append(out, v)
	}
	return out
}

// VariantHeights lists the resolution heights of a master playlist's variants,
// in playlist order.
func VariantHeights(body []byte) []int {
	var heights []int
	for _, v := range Variants(body, nil) {
		if v.Height > 0 {
			heights = append(heights, v.Height)
		}
	}
	return heights
}

// audioMedia matches an audio rendition's language in an EXT-X-MEDIA line.
var audioMedia = regexp.MustCompile(`(?i)^#EXT-X-MEDIA:.*TYPE=AUDIO`)
var mediaLanguage = regexp.MustCompile(`(?i)LANGUAGE="([^"]*)"`)

// KeepAudioLanguage drops the audio renditions of a master playlist that aren't
// in lang, so a player doesn't open every language's playlist before starting.
// A playlist without a matching rendition is returned unchanged.
func KeepAudioLanguage(body []byte, lang string) []byte {
	if lang == "" {
		return body
	}
	lines := bytes.SplitAfter(body, []byte("\n"))
	out := make([][]byte, 0, len(lines))
	kept, dropped := 0, 0
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if !audioMedia.Match(trimmed) {
			out = append(out, line)
			continue
		}
		m := mediaLanguage.FindSubmatch(trimmed)
		if m != nil && sameLanguage(string(m[1]), lang) {
			out = append(out, line)
			kept++
		} else {
			dropped++
		}
	}
	if kept == 0 || dropped == 0 {
		return body
	}
	return bytes.Join(out, nil)
}

// sameLanguage compares ISO 639 codes, treating the two- and three-letter forms
// of a language as equal ("ja" and "jpn").
func sameLanguage(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == b {
		return true
	}
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(short) != 2 || len(long) != 3 {
		return false
	}
	return iso639[short] == long
}

// iso639 maps the two-letter codes providers use to their three-letter form.
var iso639 = map[string]string{
	"ar": "ara", "de": "ger", "en": "eng", "es": "spa", "fr": "fra", "hi": "hin",
	"it": "ita", "ja": "jpn", "ko": "kor", "pt": "por", "ru": "rus", "ta": "tam",
	"th": "tha", "tr": "tur", "vi": "vie", "zh": "chi",
}
