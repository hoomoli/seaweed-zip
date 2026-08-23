package store

import (
	"context"
	"path/filepath"
	"seaweed-zip/internal/pack"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentIdempotencyAndFencing(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "key", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, e := s.Create(context.Background(), p, "hash"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	list, e := s.List(context.Background(), 0, 20, "")
	if e != nil || len(list) != 1 {
		t.Fatalf("%v %d", e, len(list))
	}
	q := p
	q.ID = strings.Repeat("c", 64)
	if _, _, e = s.Create(context.Background(), q, "hash"); e != ErrConflict {
		t.Fatal(e)
	}
	job, e := s.Claim(context.Background(), p.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Ready(context.Background(), p.ID, job.Generation-1, "bad", "hash", 3, time.Now()); e != ErrConflict {
		t.Fatal("stale worker published", e)
	}
	if e = s.Ready(context.Background(), p.ID, job.Generation, "ok", "hash", 3, time.Now()); e != nil {
		t.Fatal(e)
	}
	s.Recover(context.Background())
	got, _ := s.Get(context.Background(), p.ID)
	if got.State != "READY" {
		t.Fatal(got.State)
	}
}
