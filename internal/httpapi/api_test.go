package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"testing"
)

func TestBoundaryAndCapabilities(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_, n, _ := net.ParseCIDR("127.0.0.0/8")
	cfg := config.Config{Admin: []*net.IPNet{n}, Trusted: []*net.IPNet{n}}
	api := New(jobs.New(s, source.New(nil), cfg), nil)
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "key", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	sum := sha256.Sum256([]byte("secret"))
	_, c, _ := s.Create(context.Background(), p, hex.EncodeToString(sum[:]))
	for _, tc := range []struct {
		path, token string
		want        int
	}{{"/api/v1/consumers/" + c.ID, "", 401}, {"/api/v1/consumers/" + c.ID, "bad", 401}, {"/api/v1/consumers/" + c.ID, "secret", 200}, {"/api/v1/consumers/" + c.ID + "/artifact", "secret", 409}, {"/api/v1/admin/packs", "secret", 403}} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.RemoteAddr = "10.0.0.1:4000"
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestBareAuthorizationRejected(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	p, _ := pack.Normalize(pack.Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), ClientRequestID: "key", Objects: []pack.Object{{SHA256: strings.Repeat("b", 64), Size: 3}}})
	h := sha256.Sum256([]byte("secret"))
	_, c, _ := s.Create(context.Background(), p, hex.EncodeToString(h[:]))
	a := New(jobs.New(s, source.New(nil), config.Config{}), nil)
	r := httptest.NewRequest("GET", "/api/v1/consumers/"+c.ID, nil)
	r.Header.Set("Authorization", "secret")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("bare capability accepted")
	}
}

func TestAdmissionRateAndDuplicateJSON(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	a := New(jobs.New(s, source.New(nil), config.Config{}), nil)
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest("POST", "/api/v1/packs", nil)
		w := httptest.NewRecorder()
		ok := a.take(w, r)
		if ok {
			<-a.slots
		}
		if i < 4 && !ok {
			t.Fatal("initial burst denied")
		}
		if i == 4 && (ok || w.Code != 429) {
			t.Fatal("unbounded submission rate")
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/packs", strings.NewReader(`{"game":"a","game":"b"}`))
	w := httptest.NewRecorder()
	var body pack.Request
	if a.decode(w, r, &body) || w.Code != 400 {
		t.Fatal("duplicate JSON accepted")
	}
}
