package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"seaweed-zip/internal/config"
	"seaweed-zip/internal/jobs"
	"seaweed-zip/internal/pack"
	"seaweed-zip/internal/source"
	"seaweed-zip/internal/store"
	"strings"
	"testing"
	"time"
)

func TestRangeAckAndLease(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "artifacts"), 0700)
	os.WriteFile(filepath.Join(dir, "artifacts", "test.zip"), []byte("abcdef"), 0600)
	s, e := store.Open(filepath.Join(dir, "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "key", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	h := sha256.Sum256([]byte("secret"))
	_, c, _ := s.Create(context.Background(), p, hex.EncodeToString(h[:]))
	j, _ := s.Claim(context.Background(), p.ID, time.Now())
	s.Ready(context.Background(), p.ID, j.Generation, "test.zip", "ziphash", 6, time.Now())
	a := New(jobs.New(s, source.New(nil), config.Config{DataDir: dir, DownloadTimeout: time.Minute}), nil)
	for _, tc := range []struct {
		method, rangeValue, ifRange string
		status                      int
		body                        string
	}{{"GET", "bytes=1-3", "", 206, "bcd"}, {"HEAD", "", "", 200, ""}, {"GET", "bytes=100-200", "", 416, ""}, {"GET", "bytes=1-3", `"wrong"`, 200, "abcdef"}} {
		r := httptest.NewRequest(tc.method, "/api/v1/consumers/"+c.ID+"/artifact", nil)
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Range", tc.rangeValue)
		r.Header.Set("If-Range", tc.ifRange)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.status != 416 && w.Body.String() != tc.body {
			t.Fatal(w.Body.String())
		}
	}
	c, _ = s.Consumer(context.Background(), c.ID)
	if c.Ack {
		t.Fatal("download automatically acknowledged")
	}
	if s.Ack(context.Background(), c, j.Generation-1, "ziphash") != store.ErrConflict {
		t.Fatal("stale ack accepted")
	}
	if e = s.Ack(context.Background(), c, j.Generation, "ziphash"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.BeginDownload(context.Background(), c, time.Now().Add(time.Minute)); e == nil {
		t.Fatal("acknowledged consumer reopened download")
	}
	if s.CleanClaim(context.Background(), p.ID, time.Now()) != nil {
		t.Fatal("all ACK cleanup not eligible")
	}

}

func TestRepeatedAcknowledgmentHasOneEvent(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "key", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	_, c, _ := s.Create(ctx, p, "hash")
	j, _ := s.Claim(ctx, p.ID, time.Now())
	s.Ready(ctx, p.ID, j.Generation, "x.zip", "ziphash", 3, time.Now())
	for i := 0; i < 10; i++ {
		if e := s.Ack(ctx, c, j.Generation, "ziphash"); e != nil {
			t.Fatal(e)
		}
	}
	events, _ := s.Events(ctx, p.ID)
	if len(events) != 1 {
		t.Fatal("ack not idempotent", len(events))
	}
	if e := s.CleanClaim(ctx, p.ID, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e := s.Deleted(ctx, p.ID, j.Generation, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e := s.Ack(ctx, c, j.Generation, "ziphash"); e != nil {
		t.Fatal("lost ACK response cannot recover after cleanup", e)
	}
	if e := s.Ack(ctx, c, j.Generation-1, "ziphash"); e != store.ErrConflict {
		t.Fatal("stale historical ACK accepted")
	}
}
