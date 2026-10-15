package core

import (
	"slices"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/domain"
)

func thumbs(eps []domain.Episode) []string {
	var out []string
	for _, e := range eps {
		out = append(out, e.Thumbnail)
	}
	return out
}

func TestFillThumbnails(t *testing.T) {
	ep := func(n float64, thumb string) domain.Episode { return domain.Episode{Number: n, Thumbnail: thumb} }
	tests := []struct {
		name         string
		eps, other   []domain.Episode
		want         []string
		wantHasAfter bool
	}{
		{"matches by number", []domain.Episode{ep(1, ""), ep(2, "")}, []domain.Episode{ep(2, "b"), ep(1, "a")}, []string{"a", "b"}, true},
		{"keeps existing", []domain.Episode{ep(1, "mine"), ep(2, "")}, []domain.Episode{ep(1, "a"), ep(2, "b")}, []string{"mine", "b"}, true},
		{"other has extra episodes", []domain.Episode{ep(1, "")}, []domain.Episode{ep(1, "a"), ep(2, "b")}, []string{"a"}, true},
		{"other is missing episodes", []domain.Episode{ep(1, ""), ep(2, "")}, []domain.Episode{ep(2, "b")}, []string{"", "b"}, true},
		{"no overlap", []domain.Episode{ep(1, "")}, []domain.Episode{ep(5, "e")}, []string{""}, false},
		{"other without thumbnails", []domain.Episode{ep(1, "")}, []domain.Episode{ep(1, "")}, []string{""}, false},
		{"nothing to fill from", []domain.Episode{ep(1, "")}, nil, []string{""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fillThumbnails(tt.eps, tt.other)
			if got := thumbs(tt.eps); !slices.Equal(got, tt.want) {
				t.Errorf("thumbnails = %q, want %q", got, tt.want)
			}
			if got := hasThumbnails(tt.eps); got != tt.wantHasAfter {
				t.Errorf("hasThumbnails = %v, want %v", got, tt.wantHasAfter)
			}
		})
	}
	if hasThumbnails(nil) {
		t.Error("hasThumbnails(nil) = true")
	}
}
