package domain

import (
	"strings"
	"testing"
)

func TestSortSubtitles(t *testing.T) {
	subs := []Subtitle{
		{Label: "Arabic", Lang: "ar"},
		{Label: "English (AI)", Lang: "en"},
		{Label: "English", Lang: "en"},
		{Label: "Unknown"},
		{Label: "Spanish", Lang: "es"},
	}
	for _, tc := range []struct {
		langs []string
		want  string
	}{
		{[]string{"en"}, "English|Arabic|Unknown|Spanish|English (AI)"},
		{[]string{"es", "en"}, "Spanish|English|Arabic|Unknown|English (AI)"},
		{[]string{"fr"}, "Arabic|English|Unknown|Spanish|English (AI)"},
		{nil, "Arabic|English|Unknown|Spanish|English (AI)"},
	} {
		var got []string
		for _, s := range SortSubtitles(subs, tc.langs) {
			got = append(got, s.Label)
		}
		if strings.Join(got, "|") != tc.want {
			t.Errorf("%v: order = %v, want %s", tc.langs, got, tc.want)
		}
	}
	if subs[0].Label != "Arabic" {
		t.Error("input was modified")
	}
}
