package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"seaweed-zip/internal/config"
	"seaweed-zip/internal/jobs"
	"seaweed-zip/internal/pack"
	"seaweed-zip/internal/source"
	"seaweed-zip/internal/store"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthorizeBuildReuseAndRotate(t *testing.T) {
	bh := sha256.Sum256([]byte("abc"))
	blobHash := hex.EncodeToString(bh[:])
	manifest, _ := json.Marshal(map[string]any{"schemaVersion": 1, "gameId": "g", "platform": "android", "releaseId": "r", "source": map[string]any{"commitSha": strings.Repeat("a", 40), "branch": "main", "projectId": "1"}, "files": []any{map[string]any{"path": "a.bundle", "sha256": blobHash, "size": 3, "contentType": "application/octet-stream", "objectKey": "g/blobs/sha256/" + blobHash[:2] + "/" + blobHash}}})
	manifest = append(manifest, '\n')
	mh := sha256.Sum256(manifest)
	var allowed atomic.Bool
	allowed.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b/g/manifests/android/r.json" {
			w.Write(manifest)
			return
		}
		if !allowed.Load() {
			w.WriteHeader(403)
			return
		}
		io.WriteString(w, "abc")
	}))
	defer upstream.Close()
	dir := t.TempDir()
	s, e := store.Open(filepath.Join(dir, "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_, network, _ := net.ParseCIDR("127.0.0.0/8")
	cfg := config.Config{DataDir: dir, Trusted: nil, Admin: []*net.IPNet{network}, MaxObjects: 10, MaxBytes: 1 << 20, MaxObjectBytes: 1 << 20, MaxZIPBytes: 1 << 20, MaxManifestBytes: 1024, DiskQuota: 50 << 20, TaskTimeout: time.Minute, DownloadTimeout: time.Minute}
	m := jobs.New(s, source.New(map[string]source.Rule{"s": {Scheme: "http", Host: "signed.test", Bucket: "b", Prefix: "/b/", Dial: strings.TrimPrefix(upstream.URL, "http://")}}), cfg)
	a := New(m, nil)
	body := pack.Request{ContractVersion: pack.ContractVersion, SourceID: "s", Bucket: "b", Game: "g", Platform: "android", Release: "r", CanonicalManifestHash: hex.EncodeToString(mh[:]), ManifestSHA256: hex.EncodeToString(mh[:]), ManifestURL: "http://signed.test/b/g/manifests/android/r.json?Signature=SECRET", ClientRequestID: "key", Objects: []pack.Object{{SHA256: hex.EncodeToString(bh[:]), Size: 3, URL: "http://signed.test/b/g/blobs/sha256/" + blobHash[:2] + "/" + blobHash + "?Signature=SECRET"}}}
	post := func(body pack.Request) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/v1/packs", bytes.NewReader(b))
		r.RemoteAddr = "203.0.113.8:9000"
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	first := post(body)
	if first.Code != 202 {
		t.Fatal(first.Code, first.Body.String())
	}
	var c struct {
		PackID     string `json:"packId"`
		ConsumerID string `json:"consumerId"`
		Token      string `json:"consumerToken"`
	}
	json.Unmarshal(first.Body.Bytes(), &c)
	m.Step(context.Background())
	r := httptest.NewRequest("GET", "/api/v1/consumers/"+c.ConsumerID+"/artifact", nil)
	r.Header.Set("Authorization", "Bearer "+c.Token)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	zipFile, e := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range zipFile.File {
		if f.Name == "data/blobs/"+body.Objects[0].SHA256 {
			reader, _ := f.Open()
			b, _ := io.ReadAll(reader)
			reader.Close()
			if string(b) != "abc" {
				t.Fatal("invalid payload")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("blob missing")
	}
	allowed.Store(false)
	bad := body
	bad.ClientRequestID = "new-consumer"
	if post(bad).Code != 422 {
		t.Fatal("unauthorized consumer got READY capability")
	}
	cs, _ := s.Consumers(context.Background(), c.PackID)
	if len(cs) != 1 {
		t.Fatal("unauthorized consumer persisted")
	}
	allowed.Store(true)
	second := post(body)
	if second.Code != 202 {
		t.Fatal(second.Body.String())
	}
	var renewed struct {
		Token string `json:"consumerToken"`
	}
	json.Unmarshal(second.Body.Bytes(), &renewed)
	if renewed.Token == c.Token {
		t.Fatal("token not rotated")
	}
	r = httptest.NewRequest("GET", "/api/v1/consumers/"+c.ConsumerID, nil)
	r.Header.Set("Authorization", "Bearer "+c.Token)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("old token remains valid")
	}
	job, _ := s.Get(context.Background(), c.PackID)
	safe, _ := json.Marshal(job)
	if bytes.Contains(safe, []byte("SECRET")) || bytes.Contains(safe, []byte("http://")) {
		t.Fatal("source URL persisted")
	}
	cs, _ = s.Consumers(context.Background(), c.PackID)
	if cs[0].TokenHash == renewed.Token {
		t.Fatal("plaintext token persisted")
	}
	// Restart fences unfinished work and loses source URLs, not the independent
	// consumer capability. Recovery must prove source access again and rotate it.
	s.DB.Exec("UPDATE packs SET state='PACKING' WHERE id=?", c.PackID)
	if e = s.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	m = jobs.New(s, m.Source, cfg)
	a = New(m, nil)
	auth := func(req pack.Request, token string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(req)
		r := httptest.NewRequest("POST", "/api/v1/consumers/"+c.ConsumerID+"/authorization", bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	allowed.Store(false)
	if w = auth(body, renewed.Token); w.Code != 422 || !strings.Contains(w.Body.String(), "SOURCE_AUTHORIZATION_EXPIRED") {
		t.Fatal("restart skipped fresh authorization", w.Body.String())
	}
	allowed.Store(true)
	changed := body
	changed.Objects = append([]pack.Object(nil), body.Objects...)
	changed.Objects[0].Size = 2
	if w = auth(changed, renewed.Token); w.Code != 409 {
		t.Fatal("recovery changed original missing subset")
	}
	w = auth(body, renewed.Token)
	if w.Code != 200 {
		t.Fatal("recovery failed", w.Body.String())
	}
	var recovery struct {
		Token string `json:"consumerToken"`
	}
	json.Unmarshal(w.Body.Bytes(), &recovery)
	if recovery.Token == "" || recovery.Token == renewed.Token {
		t.Fatal("recovery did not rotate capability")
	}
	m.Step(context.Background())
	job, _ = s.Get(context.Background(), c.PackID)
	if job.State != "READY" || job.Generation < 2 {
		t.Fatal("restart did not build a fenced generation", job.State, job.Generation)
	}
}
