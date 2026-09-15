package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/domain"
)

type healthFake struct {
	shows      []domain.Show
	streamsErr error
}

func (f healthFake) Name() string { return "fake" }
func (f healthFake) Search(context.Context, string, domain.Mode) ([]domain.Show, error) {
	return f.shows, nil
}
func (f healthFake) Episodes(context.Context, string, domain.Mode) ([]domain.Episode, error) {
	return []domain.Episode{{ID: "1", Number: 1}}, nil
}
func (f healthFake) Streams(context.Context, string, domain.Episode, domain.Mode) ([]domain.Stream, error) {
	if f.streamsErr != nil {
		return nil, f.streamsErr
	}
	return []domain.Stream{{URL: "u", Height: 1080, Label: "1080p"}}, nil
}

func TestCheckHealth(t *testing.T) {
	ctx := context.Background()
	shows := []domain.Show{{ID: "s", Title: "Frieren"}}
	ok := func(context.Context, domain.Stream) error { return nil }

	r := CheckHealth(ctx, healthFake{shows: shows}, HealthOptions{Query: "frieren"})
	if !r.OK() || r.Stage != "search" || r.Detail != "1 result" {
		t.Fatalf("search only: %+v", r)
	}

	r = CheckHealth(ctx, healthFake{shows: shows}, HealthOptions{Query: "frieren", Streams: true, CheckStream: ok, Quality: "best"})
	if !r.OK() || r.Stage != "playback" || !strings.Contains(r.Detail, "episode 1 plays (1080p)") {
		t.Fatalf("deep: %+v", r)
	}

	r = CheckHealth(ctx, healthFake{}, HealthOptions{Query: "frieren"})
	if r.OK() || r.Stage != "search" {
		t.Fatalf("no results: %+v", r)
	}

	r = CheckHealth(ctx, healthFake{shows: shows, streamsErr: ErrStreamsUnavailable}, HealthOptions{Query: "q", Streams: true, CheckStream: ok})
	if r.OK() || !r.Limited || r.Stage != "streams" {
		t.Fatalf("limited: %+v", r)
	}

	dead := func(context.Context, domain.Stream) error { return errors.New("HTTP 404") }
	r = CheckHealth(ctx, healthFake{shows: shows}, HealthOptions{Query: "q", Streams: true, CheckStream: dead})
	if r.OK() || r.Limited || r.Stage != "playback" {
		t.Fatalf("dead stream: %+v", r)
	}
}
