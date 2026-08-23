package store

import (
	"context"
	"path/filepath"
	"seaweed-zip/internal/pack"
	"strings"
	"testing"
	"time"
)

func TestRecoveryAndReauthorizationResetAck(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "k", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	_, c, _ := s.Create(ctx, p, "first")
	j, _ := s.Claim(ctx, p.ID, time.Now())
	s.Ready(ctx, p.ID, j.Generation, "x.zip", "h", 3, time.Now())
	c, _ = s.Consumer(ctx, c.ID)
	s.Ack(ctx, c, j.Generation, "h")
	s.DB.Exec("UPDATE packs SET state='DELETED',artifact='' WHERE id=?", p.ID)
	_, renew, e := s.Create(ctx, p, "second")
	if e != nil {
		t.Fatal(e)
	}
	if renew.Ack {
		t.Fatal("new generation inherits old ack")
	}
	s.Recover(ctx)
	got, _ := s.Get(ctx, p.ID)
	if got.State != "WAITING_AUTH" {
		t.Fatal(got.State)
	}
	_, _, e = s.Create(ctx, p, "third")
	if e != nil {
		t.Fatal(e)
	}
	next, _ := s.Claim(ctx, p.ID, time.Now())
	if next.Generation <= j.Generation {
		t.Fatal("generation not fenced")
	}
}
