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

func TestNewGenerationDoesNotReviveConsumedConsumer(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "android", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "first", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	_, old, _ := s.Create(ctx, p, "oldhash")
	j, _ := s.Claim(ctx, p.ID, time.Now())
	s.Ready(ctx, p.ID, j.Generation, "old.zip", "ziphash", 3, time.Now())
	s.Ack(ctx, old, j.Generation, "ziphash")
	s.CleanClaim(ctx, p.ID, time.Now())
	s.Deleted(ctx, p.ID, j.Generation, time.Now())
	p.Request.ClientRequestID = "second"
	_, current, e := s.Create(ctx, p, "newhash")
	if e != nil {
		t.Fatal(e)
	}
	next, _ := s.Claim(ctx, p.ID, time.Now())
	s.Ready(ctx, p.ID, next.Generation, "new.zip", "newziphash", 3, time.Now())
	old, e = s.Consumer(ctx, old.ID)
	if e != nil || !old.Ack {
		t.Fatal("new generation revived consumed consumer", e)
	}
	if _, e = s.BeginDownload(ctx, old, time.Now().Add(time.Minute)); e != ErrConflict {
		t.Fatal("old ACK token downloaded new generation")
	}
	if e = s.Ack(ctx, current, next.Generation, "newziphash"); e != nil {
		t.Fatal(e)
	}
	if e = s.CleanClaim(ctx, p.ID, time.Now()); e != nil {
		t.Fatal("consumed old generation blocked cleanup", e)
	}
}
