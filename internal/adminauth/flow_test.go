package adminauth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

func TestOIDCFlow(t *testing.T) {
	for _, tc := range []struct {
		name                                                          string
		group, nonce, audience, issuer                                string
		expired, badSignature, stringGroups, noPKCE, insecureEndpoint bool
		want                                                          int
	}{
		{name: "admin", group: "seaweed-zip-admins", want: 303},
		{name: "ordinary user", group: "users", want: 403},
		{name: "missing group", want: 403},
		{name: "wrong nonce", group: "seaweed-zip-admins", nonce: "wrong", want: 403},
		{name: "wrong audience", group: "seaweed-zip-admins", audience: "other", want: 403},
		{name: "wrong issuer", group: "seaweed-zip-admins", issuer: "https://other.invalid", want: 403},
		{name: "expired", group: "seaweed-zip-admins", expired: true, want: 403},
		{name: "bad signature", group: "seaweed-zip-admins", badSignature: true, want: 403},
		{name: "string groups", group: "seaweed-zip-admins", stringGroups: true, want: 403},
		{name: "missing S256", noPKCE: true, want: 403},
		{name: "insecure endpoint", insecureEndpoint: true, want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, _ := rsa.GenerateKey(rand.Reader, 2048)
			signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"))
			var issuer, nonce, verifier string
			exchanges := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					pkce := []string{"S256"}
					if tc.noPKCE {
						pkce = nil
					}
					endpoint := issuer + "/token"
					if tc.insecureEndpoint {
						endpoint = "http://issuer.invalid/token"
					}
					json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": endpoint, "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": pkce})
				case "/keys":
					json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
				case "/token":
					exchanges++
					r.ParseForm()
					if r.Form.Get("code_verifier") != verifier {
						t.Error("PKCE verifier not sent")
					}
					id, secret, ok := r.BasicAuth()
					if !ok || id != "client" || secret != "secret" {
						t.Error("confidential client authentication missing")
					}
					n := nonce
					if tc.nonce != "" {
						n = tc.nonce
					}
					aud := "client"
					if tc.audience != "" {
						aud = tc.audience
					}
					iss := issuer
					if tc.issuer != "" {
						iss = tc.issuer
					}
					exp := time.Now().Add(time.Hour).Unix()
					if tc.expired {
						exp = time.Now().Add(-time.Hour).Unix()
					}
					var groups any = []string{tc.group}
					if tc.stringGroups {
						groups = tc.group
					}
					claims, _ := json.Marshal(map[string]any{"iss": iss, "aud": aud, "sub": "subject", "exp": exp, "iat": time.Now().Unix(), "nonce": n, "groups": groups})
					tokenSigner := signer
					if tc.badSignature {
						other, _ := rsa.GenerateKey(rand.Reader, 2048)
						tokenSigner, _ = jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: other}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
					}
					obj, _ := tokenSigner.Sign(claims)
					raw, _ := obj.CompactSerialize()
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{"access_token": "unused", "token_type": "Bearer", "id_token": raw})
				default:
					t.Error("unexpected endpoint")
				}
			}))
			defer server.Close()
			issuer = server.URL
			originalTransport := http.DefaultTransport
			http.DefaultTransport = server.Client().Transport
			defer func() { http.DefaultTransport = originalTransport }()
			a, e := New(Settings{Issuer: issuer, ClientID: "client", ClientSecret: "secret", PublicURL: "https://app.invalid", SessionKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), AdminGroups: []string{"seaweed-zip-admins"}})
			if tc.noPKCE || tc.insecureEndpoint {
				if e == nil {
					t.Fatal("unsafe discovery accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			start := httptest.NewRecorder()
			a.login(start, httptest.NewRequest("GET", "https://app.invalid/auth/login", nil))
			if start.Code != 303 {
				t.Fatal(start.Code)
			}
			location, _ := url.Parse(start.Header().Get("Location"))
			state := location.Query().Get("state")
			nonce = location.Query().Get("nonce")
			if state == "" || nonce == "" || location.Query().Get("code_challenge_method") != "S256" {
				t.Fatal("missing OIDC binding")
			}
			for _, tx := range a.transactions {
				verifier = tx.Verifier
			}
			if location.Query().Get("code_challenge") != oauth2.S256ChallengeFromVerifier(verifier) {
				t.Fatal("wrong PKCE challenge")
			}
			request := httptest.NewRequest("GET", "https://app.invalid/auth/callback?code=code&state="+state, nil)
			request.AddCookie(start.Result().Cookies()[0])
			response := httptest.NewRecorder()
			a.callback(response, request)
			if response.Code != tc.want {
				t.Fatalf("status %d expected %d", response.Code, tc.want)
			}
			replay := httptest.NewRecorder()
			a.callback(replay, request)
			if replay.Code != 403 || exchanges != 1 {
				t.Fatal("callback replay exchanged code")
			}
			if tc.want == 303 {
				var cookie *http.Cookie
				for _, c := range response.Result().Cookies() {
					if c.Name == sessionCookie {
						cookie = c
					}
				}
				if cookie == nil || !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode {
					t.Fatal("unsafe cookie")
				}
				r := httptest.NewRequest("GET", "https://app.invalid/auth/session", nil)
				r.AddCookie(cookie)
				s, ok := a.authorized(r)
				if !ok {
					t.Fatal("session rejected")
				}
				r.Method = "POST"
				r.Header.Set("Origin", "https://app.invalid")
				r.Header.Set("X-CSRF-Token", s.CSRF)
				a.logout(httptest.NewRecorder(), r)
				if a.Allowed(r) {
					t.Fatal("logout did not revoke")
				}
			}
		})
	}
}
func TestConfigurationValidation(t *testing.T) {
	base := Settings{Issuer: "https://issuer.invalid", ClientID: "client", ClientSecret: "secret", PublicURL: "https://app.invalid", SessionKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), AdminGroups: []string{"admins"}}
	for _, mutate := range []func(*Settings){func(s *Settings) { s.Issuer = "http://issuer.invalid" }, func(s *Settings) { s.PublicURL = "https://app.invalid/path" }, func(s *Settings) { s.PublicURL = "https://app.invalid?x=1" }, func(s *Settings) { s.AdminGroups = []string{" "} }, func(s *Settings) { s.SessionKey = strings.Repeat("x", 20) }} {
		s := base
		mutate(&s)
		if _, err := New(s); err == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
}
