package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"seaweed-zip/internal/pack"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubsetAuthorizationBoundedAndURLBound(t *testing.T) {
	files := []any{}
	objects := []pack.Object{}
	for i := 0; i < 12; i++ {
		h := fmt.Sprintf("%064x", i+1)
		files = append(files, map[string]any{"path": fmt.Sprintf("a%d", i), "sha256": h, "size": 3, "contentType": "text/plain", "objectKey": "g/blobs/sha256/" + h[:2] + "/" + h})
		objects = append(objects, pack.Object{SHA256: h, Size: 3, URL: "http://signed.test/b/g/blobs/sha256/" + h[:2] + "/" + h})
	}
	b, _ := json.Marshal(map[string]any{"schemaVersion": 1, "gameId": "g", "platform": "android", "releaseId": "r", "source": map[string]any{"commitSha": strings.Repeat("a", 40), "branch": "main", "projectId": "1"}, "files": files})
	// Canonical file order is lexical, not numeric.
	var m map[string]any
	json.Unmarshal(b, &m)
	fs := m["files"].([]any)
	fs[2], fs[10] = fs[10], fs[2]
	fs[3], fs[11] = fs[11], fs[3]
	// Use lexical order explicitly to avoid fixture assumptions.
	for i := 0; i < len(fs); i++ {
		for j := i + 1; j < len(fs); j++ {
			if fs[j].(map[string]any)["path"].(string) < fs[i].(map[string]any)["path"].(string) {
				fs[i], fs[j] = fs[j], fs[i]
			}
		}
	}
	b, _ = json.Marshal(m)
	b = append(b, '\n')
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	var active, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "manifests") {
			w.Write(b)
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		w.Write([]byte("abc"))
	}))
	defer srv.Close()
	c := New(map[string]Rule{"s": {Scheme: "http", Host: "signed.test", Bucket: "b", Prefix: "/b/", Dial: strings.TrimPrefix(srv.URL, "http://")}})
	r := pack.Request{ContractVersion: pack.ContractVersion, SourceID: "s", Bucket: "b", Game: "g", Platform: "android", Release: "r", ManifestSHA256: hash, CanonicalManifestHash: hash, ManifestURL: "http://signed.test/b/g/manifests/android/r.json", Objects: objects}
	p, _ := pack.Normalize(r)
	if e := c.Authorize(context.Background(), p, 1<<20); e != nil {
		t.Fatal(e)
	}
	if peak.Load() < 2 || peak.Load() > 4 {
		t.Fatal("authorization concurrency", peak.Load())
	}
	r.Objects[0].URL = "http://signed.test/b/unrelated?Signature=SECRET"
	p, _ = pack.Normalize(r)
	if e := c.Authorize(context.Background(), p, 1<<20); e == nil || strings.Contains(e.Error(), "SECRET") {
		t.Fatal("URL was not bound", e)
	}
}
