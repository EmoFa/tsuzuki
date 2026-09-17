package store

import (
	"context"
	"slices"
	"testing"
)

func TestShowPrefs(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	if p, err := st.ShowPrefs(ctx, 1); p != nil || err != nil {
		t.Fatalf("missing: %+v %v", p, err)
	}

	hidden := false
	if err := st.SaveShowPrefs(ctx, ShowPrefs{MediaID: 1, SubLanguages: []string{"es", "en"}, SubShow: &hidden}); err != nil {
		t.Fatal(err)
	}
	p, err := st.ShowPrefs(ctx, 1)
	if err != nil || !slices.Equal(p.SubLanguages, []string{"es", "en"}) || p.SubShow == nil || *p.SubShow {
		t.Fatalf("saved: %+v %v", p, err)
	}

	// Only visibility overridden; languages inherit the config.
	show := true
	if err := st.SaveShowPrefs(ctx, ShowPrefs{MediaID: 1, SubShow: &show}); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.ShowPrefs(ctx, 1); p.SubLanguages != nil || p.SubShow == nil || !*p.SubShow {
		t.Fatalf("partial: %+v", p)
	}

	if err := st.DeleteShowPrefs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.ShowPrefs(ctx, 1); p != nil {
		t.Fatalf("deleted: %+v", p)
	}
}
