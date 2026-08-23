package adminauth

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestConfigurationFailsClosed(t *testing.T) {
	if _, err := New(Settings{Issuer: "https://issuer.invalid"}); err == nil {
		t.Fatal("partial configuration accepted")
	}
	if a, err := New(Settings{}); err != nil || a != nil {
		t.Fatal("disabled configuration")
	}
}
func TestSessionAndCSRF(t *testing.T) {
	a := &Auth{settings: Settings{PublicURL: "https://app.invalid"}, key: make([]byte, 32), sessions: map[string]session{}}
	id := random()
	a.sessions[id] = session{CSRF: "csrf", Expires: time.Now().Add(time.Hour)}
	r := httptest.NewRequest("POST", "https://app.invalid/api", nil)
	r.AddCookie(a.cookie(sessionCookie, a.sign(id), 3600))
	if _, ok := a.authorized(r); ok {
		t.Fatal("missing CSRF accepted")
	}
	r.Header.Set("Origin", "https://app.invalid")
	r.Header.Set("X-CSRF-Token", "csrf")
	if _, ok := a.authorized(r); !ok {
		t.Fatal("valid session rejected")
	}
	r.Header.Set("Origin", "https://evil.invalid")
	if _, ok := a.authorized(r); ok {
		t.Fatal("cross origin accepted")
	}
	r.Header.Set("Origin", "https://app.invalid")
	r.Header.Set("Cookie", sessionCookie+"="+a.sign(id)+"x")
	if _, ok := a.authorized(r); ok {
		t.Fatal("tampered session accepted")
	}

}
func TestGroupAuthorization(t *testing.T) {
	a := &Auth{settings: Settings{AdminGroups: []string{"seaweed-zip-admins"}}}
	if a.allowed([]string{"users"}) || a.allowed(nil) {
		t.Fatal("unprivileged group accepted")
	}
	if !a.allowed([]string{"users", "seaweed-zip-admins"}) {
		t.Fatal("admin rejected")
	}
}

func TestInvalidTransactionsAndExpiry(t *testing.T) {
	for _, mode := range []string{"missing cookie", "tampered cookie", "wrong state", "expired"} {
		t.Run(mode, func(t *testing.T) {
			a := &Auth{key: make([]byte, 32), sessions: map[string]session{}, transactions: map[string]transaction{"id": {State: "state", Expires: time.Now().Add(time.Minute)}}}
			r := httptest.NewRequest("GET", "https://app.invalid/auth/callback?state=state&code=code", nil)
			if mode != "missing cookie" {
				value := a.sign("id")
				if mode == "tampered cookie" {
					value += "x"
				}
				r.AddCookie(a.cookie(transactionCookie, value, 300))
			}
			if mode == "wrong state" {
				r.URL.RawQuery = "state=wrong&code=code"
			}
			if mode == "expired" {
				a.transactions["id"] = transaction{State: "state", Expires: time.Now().Add(-time.Minute)}
			}
			w := httptest.NewRecorder()
			a.callback(w, r)
			if w.Code != 403 {
				t.Fatal("invalid transaction accepted")
			}
		})
	}
	a := &Auth{key: make([]byte, 32), sessions: map[string]session{"id": {Expires: time.Now().Add(-time.Minute)}}}
	r := httptest.NewRequest("GET", "https://app.invalid/", nil)
	r.AddCookie(a.cookie(sessionCookie, a.sign("id"), 3600))
	if a.Allowed(r) {
		t.Fatal("expired session accepted")
	}
}
