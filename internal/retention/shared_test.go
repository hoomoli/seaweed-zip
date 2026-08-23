package retention

import (
	"context"
	"os"
	"path/filepath"
	"seaweed-zip/internal/pack"
	"seaweed-zip/internal/store"
	"strings"
	"testing"
	"time"
)

func TestSharedAckWaitsForOtherConsumerAndDownload(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "artifacts"), 0700)
	s, e := store.Open(filepath.Join(dir, "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "android", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "first", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	if e != nil {
		t.Fatal(e)
	}
	_, first, e := s.Create(ctx, p, "one")
	if e != nil {
		t.Fatal(e)
	}
	p.Request.ClientRequestID = "second"
	_, second, e := s.Create(ctx, p, "two")
	if e != nil {
		t.Fatal(e)
	}
	j, e := s.Claim(ctx, p.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Ready(ctx, p.ID, j.Generation, "shared.zip", "ziphash", 3, time.Now()); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "artifacts", "shared.zip"), []byte("zip"), 0600)
	lease, e := s.BeginDownload(ctx, second, time.Now().Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Ack(ctx, first, j.Generation, "ziphash"); e != nil {
		t.Fatal(e)
	}
	clean := Cleaner{s, dir}
	clean.Sweep(ctx, time.Now())
	got, _ := s.Get(ctx, p.ID)
	if got.State != "READY" {
		t.Fatal("other consumer lost pack")
	}
	if e = s.Ack(ctx, second, j.Generation, "ziphash"); e != nil {
		t.Fatal(e)
	}
	clean.Sweep(ctx, time.Now())
	got, _ = s.Get(ctx, p.ID)
	if got.State != "READY" {
		t.Fatal("active download lost pack")
	}
	s.EndDownload(lease)
	clean.Sweep(ctx, time.Now())
	got, _ = s.Get(ctx, p.ID)
	if got.State != "DELETED" {
		t.Fatal("ACK cleanup deferred", got.State)
	}
	if _, e = os.Stat(filepath.Join(dir, "artifacts", "shared.zip")); !os.IsNotExist(e) {
		t.Fatal("file not deleted")
	}
}

func TestRestartRemovesOnlyDeadStaging(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "artifacts"), 0700)
	s, _ := store.Open(filepath.Join(dir, "db"))
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "android", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "first", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	s.Create(ctx, p, "tokenhash")
	s.Claim(ctx, p.ID, time.Now())
	stage := filepath.Join(dir, "artifacts", p.ID+"-1.staging-1234")
	os.Mkdir(stage, 0700)
	os.WriteFile(filepath.Join(stage, "blob-x"), []byte("x"), 0600)
	clean := Cleaner{s, dir}
	clean.Sweep(ctx, time.Now())
	if _, e := os.Stat(stage); e != nil {
		t.Fatal("live staging deleted")
	}
	s.Recover(ctx)
	clean.Sweep(ctx, time.Now())
	if _, e := os.Stat(stage); !os.IsNotExist(e) {
		t.Fatal("dead staging leaked")
	}
}

func TestCancelledCleanupPreservesLiveGeneration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "artifacts"), 0700)
	s, _ := store.Open(filepath.Join(dir, "db"))
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "android", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "first", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	s.Create(ctx, p, "tokenhash")
	s.Claim(ctx, p.ID, time.Now())
	stage := filepath.Join(dir, "artifacts", p.ID+"-1.staging-1234")
	os.Mkdir(stage, 0700)
	os.WriteFile(filepath.Join(stage, "blob-x"), []byte("x"), 0600)
	partial := filepath.Join(dir, "artifacts", p.ID+"-1.zip.partial")
	os.WriteFile(partial, []byte("x"), 0600)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	Cleaner{s, dir}.orphans(cancelled)
	for _, name := range []string{stage, partial} {
		if _, e := os.Stat(name); e != nil {
			t.Fatal("cancelled cleanup destroyed live generation")
		}
	}
}
