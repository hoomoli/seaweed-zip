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

func TestRetentionDownloadAndDeletionFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "artifacts"), 0700)
	s, e := store.Open(filepath.Join(dir, "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "key", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	_, c, _ := s.Create(ctx, p, "hash")
	now := time.Now()
	j, _ := s.Claim(ctx, p.ID, now)
	s.Ready(ctx, p.ID, j.Generation, "x.zip", "hash", 3, now)
	os.WriteFile(filepath.Join(dir, "artifacts", "x.zip"), []byte("zip"), 0600)
	clean := Cleaner{s, dir}
	clean.Sweep(ctx, now.Add(23*time.Hour))
	got, _ := s.Get(ctx, p.ID)
	if got.State != "READY" {
		t.Fatal("deleted early")
	}
	lease, _ := s.BeginDownload(ctx, c, now.Add(26*time.Hour))
	clean.Sweep(ctx, now.Add(25*time.Hour))
	got, _ = s.Get(ctx, p.ID)
	if got.State != "READY" {
		t.Fatal("active download deleted")
	}
	s.EndDownload(lease)
	os.Remove(filepath.Join(dir, "artifacts", "x.zip"))
	os.Mkdir(filepath.Join(dir, "artifacts", "x.zip"), 0700)
	os.WriteFile(filepath.Join(dir, "artifacts", "x.zip", "child"), []byte("x"), 0600)
	clean.Sweep(ctx, now.Add(25*time.Hour))
	got, _ = s.Get(ctx, p.ID)
	if got.State != "DELETING" {
		t.Fatal("failed deletion lost tracking")
	}
	os.Remove(filepath.Join(dir, "artifacts", "x.zip", "child"))
	clean.Sweep(ctx, now.Add(25*time.Hour))
	got, _ = s.Get(ctx, p.ID)
	if got.State != "DELETED" {
		t.Fatal(got.State)
	}
}

func TestOrphanGenerationRemoved(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "artifacts"), 0700)
	s, _ := store.Open(filepath.Join(dir, "db"))
	defer s.Close()
	name := strings.Repeat("a", 64) + "-2.zip.partial"
	os.WriteFile(filepath.Join(dir, "artifacts", name), []byte("partial"), 0600)
	Cleaner{s, dir}.Sweep(ctx, time.Now())
	if _, e := os.Stat(filepath.Join(dir, "artifacts", name)); !os.IsNotExist(e) {
		t.Fatal("orphan generation survived")
	}
}
