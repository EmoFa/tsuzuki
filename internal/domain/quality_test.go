package domain

import "testing"

func TestSelectStream(t *testing.T) {
	s := func(h int, label string) Stream { return Stream{Height: h, Label: label} }
	all := []Stream{s(720, "a720"), s(0, "unknown"), s(1080, "a1080"), s(360, "a360"), s(1080, "b1080")}

	tests := []struct {
		pref    string
		streams []Stream
		want    string
	}{
		{"best", all, "a1080"},
		{"", all, "a1080"},
		{"worst", all, "a360"},
		{"1080", all, "a1080"},
		{"720", all, "a720"},
		{"480", all, "a360"},
		{"240", all, "a360"},
		{"best", []Stream{s(0, "only")}, "only"},
		{"worst", []Stream{s(0, "u1"), s(0, "u2")}, "u2"},
		{"720", []Stream{s(0, "u1")}, "u1"},
	}
	for _, tt := range tests {
		got, err := SelectStream(tt.streams, tt.pref)
		if err != nil {
			t.Fatalf("pref %q: %v", tt.pref, err)
		}
		if got.Label != tt.want {
			t.Errorf("pref %q: got %s, want %s", tt.pref, got.Label, tt.want)
		}
	}

	if _, err := SelectStream(nil, "best"); err == nil {
		t.Error("expected error for no streams")
	}
	if _, err := SelectStream(all, "hd"); err == nil {
		t.Error("expected error for invalid preference")
	}
}

func TestEpisodeLabel(t *testing.T) {
	if l := (Episode{Number: 12}).Label(); l != "12" {
		t.Errorf("got %q", l)
	}
	if l := (Episode{Number: 12.5}).Label(); l != "12.5" {
		t.Errorf("got %q", l)
	}
}
