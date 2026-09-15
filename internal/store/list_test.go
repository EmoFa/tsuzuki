package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestListEntries(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()
	for _, e := range []ListEntry{
		{MediaID: 1, Status: "CURRENT", Progress: 3, UpdatedAt: now.Add(-time.Hour)},
		{MediaID: 2, Status: "COMPLETED", Progress: 12, Score: 8.5, UpdatedAt: now.Add(-2 * time.Hour)},
		{MediaID: 3, Status: "CURRENT", Progress: 1, UpdatedAt: now},
	} {
		if err := s.SaveListEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	current, err := s.ListEntries(ctx, "CURRENT")
	if err != nil || len(current) != 2 || current[0].MediaID != 3 {
		t.Fatalf("current = %+v err=%v", current, err)
	}
	all, _ := s.ListEntries(ctx, "")
	if len(all) != 3 {
		t.Fatalf("all = %+v", all)
	}
	e, err := s.ListEntry(ctx, 2)
	if err != nil || e.Score != 8.5 || e.Progress != 12 {
		t.Fatalf("entry = %+v err=%v", e, err)
	}
	s.DeleteListEntry(ctx, 2)
	if e, _ := s.ListEntry(ctx, 2); e != nil {
		t.Fatal("not deleted")
	}
}

func TestSyncQueue(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	s.QueueSync(ctx, 1, "CURRENT", 3)
	s.SyncFailed(ctx, 1, errors.New("offline"))
	pending, err := s.PendingSyncs(ctx)
	if err != nil || len(pending) != 1 || pending[0].Attempts != 1 || pending[0].LastError != "offline" {
		t.Fatalf("pending = %+v err=%v", pending, err)
	}
	old := pending[0]

	// A newer change replaces the pending one and resets attempts.
	time.Sleep(2 * time.Millisecond)
	s.QueueSync(ctx, 1, "CURRENT", 4)
	pending, _ = s.PendingSyncs(ctx)
	if len(pending) != 1 || pending[0].Progress != 4 || pending[0].Attempts != 0 {
		t.Fatalf("pending = %+v", pending)
	}

	// Finishing the older push must not drop the newer change.
	s.SyncDone(ctx, old)
	if pending, _ = s.PendingSyncs(ctx); len(pending) != 1 {
		t.Fatal("newer pending change was removed")
	}
	s.SyncDone(ctx, pending[0])
	if pending, _ = s.PendingSyncs(ctx); len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}
