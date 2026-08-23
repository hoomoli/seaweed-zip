package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"seaweed-zip/internal/config"
	"seaweed-zip/internal/pack"
	"seaweed-zip/internal/source"
	"seaweed-zip/internal/store"
	"strings"
	"testing"
	"time"
)

func TestWorkerBuildsAndRecovers(t *testing.T) {
	blob := sha256.Sum256([]byte("abc"))
	blobHash := hex.EncodeToString(blob[:])
	manifest, _ := json.Marshal(map[string]any{"schemaVersion": 1, "gameId": "g", "platform": "android", "releaseId": "r", "source": map[string]any{"commitSha": strings.Repeat("a", 40), "branch": "main", "projectId": "1"}, "files": []any{map[string]any{"path": "a.bundle", "sha256": blobHash, "size": 3, "contentType": "application/octet-stream", "objectKey": "g/blobs/sha256/" + blobHash[:2] + "/" + blobHash}}})
	manifest = append(manifest, '\n')
	h := sha256.Sum256(manifest)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b/g/manifests/android/r.json" {
			w.Write(manifest)
		} else {
			io.WriteString(w, "abc")
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	s, e := store.Open(filepath.Join(dir, "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cfg := config.Config{DataDir: dir, MaxZIPBytes: 1 << 20, TaskTimeout: time.Minute, DiskQuota: 1 << 30, MaxBytes: 1 << 20, MaxObjectBytes: 1 << 20, MaxObjects: 10, MaxManifestBytes: 1024}
	c := source.New(map[string]source.Rule{"s": {Scheme: "http", Host: "signed.test", Bucket: "b", Prefix: "/b/", Dial: strings.TrimPrefix(srv.URL, "http://")}})
	m := New(s, c, cfg)
	p, _ := pack.Normalize(pack.Request{ContractVersion: pack.ContractVersion, SourceID: "s", Bucket: "b", Game: "g", Platform: "android", Release: "r", CanonicalManifestHash: hex.EncodeToString(h[:]), ManifestSHA256: hex.EncodeToString(h[:]), ManifestURL: "http://signed.test/b/g/manifests/android/r.json", ClientRequestID: "key", Objects: []pack.Object{{SHA256: hex.EncodeToString(blob[:]), Size: 3, URL: "http://signed.test/b/g/blobs/sha256/" + blobHash[:2] + "/" + blobHash}}})
	if e = m.Authorize(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	s.Create(context.Background(), p, "hash")
	m.Remember(p)
	m.Step(context.Background())
	got, _ := s.Get(context.Background(), p.ID)
	if got.State != "READY" || got.Bytes == 0 {
		t.Fatalf("%+v", got)
	}
}

func TestPendingWorkReservesCapacity(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.Open(filepath.Join(dir, "db"))
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "k", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 600000}}})
	s.Create(context.Background(), p, "hash")
	cfg := config.Config{DataDir: dir, DiskQuota: 1000000, MaxZIPBytes: 600000}
	m := New(s, source.New(nil), cfg)
	if e := m.Capacity(context.Background(), strings.Repeat("c", 64)); e == nil {
		t.Fatal("queued artifacts not reserved")
	}
}

func TestTerminalAuthorizationIsDiscarded(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.Open(filepath.Join(dir, "db"))
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "k", ManifestURL: "http://signed/secret", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3, URL: "http://signed/secret"}}})
	s.Create(context.Background(), p, "hash")
	s.DB.Exec("UPDATE packs SET state='READY' WHERE id=?", p.ID)
	m := New(s, source.New(nil), config.Config{})
	m.Remember(p)
	m.Step(context.Background())
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.auth) != 0 {
		t.Fatal("terminal plan retained signed URLs")
	}
}

func TestAuthorizationCacheBoundedDuringBlockedWorker(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.Open(filepath.Join(dir, "db"))
	defer s.Close()
	m := New(s, source.New(nil), config.Config{})
	base, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "k", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	m.mu.Lock()
	for i := 0; i < 128; i++ {
		p := base
		p.ID = store.ID()
		m.auth[p.ID] = authorization{Plan: p, Revision: uint64(i), Expires: time.Now().Add(time.Minute)}
	}
	m.mu.Unlock()
	if m.cacheRoom(base) {
		t.Fatal("unbounded authorization admission")
	}
	base.Objects[0].URL = strings.Repeat("x", 65<<20)
	if m.cacheRoom(base) {
		t.Fatal("unbounded authorization bytes")
	}
}

func TestLastConsumerCancellationStopsSourceAndRemovesStaging(t *testing.T) {
	started := make(chan struct{})
	exited := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("a"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(exited)
	}))
	defer srv.Close()
	dir := t.TempDir()
	s, _ := store.Open(filepath.Join(dir, "db"))
	defer s.Close()
	c := source.New(map[string]source.Rule{"s": {Scheme: "http", Host: "signed.test", Bucket: "b", Prefix: "/b/", Dial: strings.TrimPrefix(srv.URL, "http://")}})
	m := New(s, c, config.Config{DataDir: dir, MaxZIPBytes: 1 << 20, TaskTimeout: time.Minute})
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "android", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "key", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3, URL: "http://signed.test/b/blob"}}})
	_, consumer, e := s.Create(context.Background(), p, "tokenhash")
	if e != nil {
		t.Fatal(e)
	}
	m.Remember(p)
	finished := make(chan struct{})
	go func() { m.Step(context.Background()); close(finished) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("download not started")
	}
	if e = s.Cancel(context.Background(), consumer); e != nil {
		t.Fatal(e)
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not stop worker")
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("upstream request not cancelled")
	}
	entries, e := os.ReadDir(filepath.Join(dir, "artifacts"))
	if e != nil || len(entries) != 0 {
		t.Fatal("staging remains", e)
	}
	got, _ := s.Get(context.Background(), p.ID)
	if got.State != "CANCELLED" {
		t.Fatal("cancel fencing lost", got.State)
	}
	events, _ := s.Events(context.Background(), p.ID)
	for _, event := range events {
		if event.Phase == "FAILED" {
			t.Fatal("cancelled generation reported false failure")
		}
	}
}
