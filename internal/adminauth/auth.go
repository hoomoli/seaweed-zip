// Package adminauth owns only management authentication, never consumer capabilities.
package adminauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const sessionCookie = "__Host-seaweed-zip-session"
const transactionCookie = "__Host-seaweed-zip-login"

type Settings struct {
	Issuer, ClientID, ClientSecret, PublicURL, SessionKey string
	AdminGroups                                           []string
}
type session struct {
	CSRF    string
	Expires time.Time
}
type transaction struct {
	State, Nonce, Verifier string
	Expires                time.Time
}
type Auth struct {
	settings     Settings
	key          []byte
	oauth        oauth2.Config
	verifier     *oidc.IDTokenVerifier
	client       *http.Client
	mu           sync.Mutex
	sessions     map[string]session
	transactions map[string]transaction
}

func New(s Settings) (*Auth, error) {
	if s.Issuer == "" && s.ClientID == "" && s.ClientSecret == "" && s.PublicURL == "" && s.SessionKey == "" && len(s.AdminGroups) == 0 {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(s.SessionKey)
	if err != nil || len(key) != 32 || s.ClientID == "" || s.ClientSecret == "" || len(s.AdminGroups) == 0 {
		return nil, errors.New("incomplete OIDC configuration")
	}
	for _, v := range []string{s.Issuer, s.PublicURL} {
		u, e := url.Parse(v)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("OIDC requires fixed HTTPS URLs")
		}
	}
	u, _ := url.Parse(s.PublicURL)
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("public URL must be an origin")
	}
	s.PublicURL = strings.TrimSuffix(s.PublicURL, "/")
	for _, g := range s.AdminGroups {
		if strings.TrimSpace(g) == "" {
			return nil, errors.New("empty administrator group")
		}
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" {
			return errors.New("insecure OIDC redirect")
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(context.Background(), client), 20*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, s.Issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery failed")
	}
	endpoint := provider.Endpoint()
	for _, v := range []string{endpoint.AuthURL, endpoint.TokenURL} {
		u, e := url.Parse(v)
		if e != nil || u.Scheme != "https" || u.Host == "" {
			return nil, errors.New("insecure OIDC endpoint")
		}
	}
	var discovery struct {
		JWKS string   `json:"jwks_uri"`
		PKCE []string `json:"code_challenge_methods_supported"`
	}
	if provider.Claims(&discovery) != nil {
		return nil, errors.New("invalid discovery")
	}
	j, e := url.Parse(discovery.JWKS)
	if e != nil || j.Scheme != "https" || j.Host == "" {
		return nil, errors.New("insecure JWKS endpoint")
	}
	supported := false
	for _, m := range discovery.PKCE {
		if m == "S256" {
			supported = true
		}
	}
	if !supported {
		return nil, errors.New("issuer does not advertise S256")
	}
	return &Auth{settings: s, key: key, client: client, oauth: oauth2.Config{ClientID: s.ClientID, ClientSecret: s.ClientSecret, Endpoint: endpoint, RedirectURL: s.PublicURL + "/auth/callback", Scopes: []string{oidc.ScopeOpenID, "profile", "email", "groups"}}, verifier: provider.Verifier(&oidc.Config{ClientID: s.ClientID}), sessions: map[string]session{}, transactions: map[string]transaction{}}, nil
}
func random() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic("secure randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func (a *Auth) sign(v string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(v))
	return v + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (a *Auth) read(r *http.Request, name string) string {
	c, e := r.Cookie(name)
	if e != nil {
		return ""
	}
	v, _, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(c.Value), []byte(a.sign(v))) {
		return ""
	}
	return v
}
func (a *Auth) cookie(name, value string, age int) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: age, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}
func (a *Auth) prune() {
	now := time.Now()
	for id, s := range a.sessions {
		if !now.Before(s.Expires) {
			delete(a.sessions, id)
		}
	}
	for id, t := range a.transactions {
		if !now.Before(t.Expires) {
			delete(a.transactions, id)
		}
	}
}
func (a *Auth) authorized(r *http.Request) (session, bool) {
	if a == nil {
		return session{}, false
	}
	id := a.read(r, sessionCookie)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune()
	s, ok := a.sessions[id]
	if !ok {
		return s, false
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		if r.Header.Get("Origin") != a.settings.PublicURL || !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) {
			return s, false
		}
	}
	return s, true
}
func (a *Auth) Allowed(r *http.Request) bool { _, ok := a.authorized(r); return ok }
func (a *Auth) allowed(groups []string) bool {
	for _, g := range groups {
		for _, want := range a.settings.AdminGroups {
			if g == want {
				return true
			}
		}
	}
	return false
}
func fail(w http.ResponseWriter, code int) { http.Error(w, "Management authentication denied", code) }
func (a *Auth) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("GET /auth/session", a.current)
	mux.HandleFunc("POST /auth/logout", a.logout)
}
func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	if a == nil {
		fail(w, 403)
		return
	}
	id := random()
	t := transaction{State: random(), Nonce: random(), Verifier: oauth2.GenerateVerifier(), Expires: time.Now().Add(5 * time.Minute)}
	a.mu.Lock()
	a.prune()
	if len(a.transactions) >= 1024 {
		a.mu.Unlock()
		fail(w, 429)
		return
	}
	a.transactions[id] = t
	a.mu.Unlock()
	http.SetCookie(w, a.cookie(transactionCookie, a.sign(id), 300))
	http.Redirect(w, r, a.oauth.AuthCodeURL(t.State, oidc.Nonce(t.Nonce), oauth2.S256ChallengeOption(t.Verifier)), http.StatusSeeOther)
}
func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	if a == nil {
		fail(w, 403)
		return
	}
	id := a.read(r, transactionCookie)
	a.mu.Lock()
	t, ok := a.transactions[id]
	delete(a.transactions, id)
	a.mu.Unlock()
	http.SetCookie(w, a.cookie(transactionCookie, "", -1))
	if !ok || !time.Now().Before(t.Expires) || !hmac.Equal([]byte(r.URL.Query().Get("state")), []byte(t.State)) || r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		fail(w, 403)
		return
	}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(r.Context(), a.client), 20*time.Second)
	defer cancel()
	token, e := a.oauth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(t.Verifier))
	if e != nil {
		fail(w, 403)
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		fail(w, 403)
		return
	}
	verified, e := a.verifier.Verify(ctx, raw)
	if e != nil || verified.Subject == "" || !hmac.Equal([]byte(verified.Nonce), []byte(t.Nonce)) {
		fail(w, 403)
		return
	}
	var claims struct {
		Groups []string `json:"groups"`
	}
	if verified.Claims(&claims) != nil || !a.allowed(claims.Groups) {
		fail(w, 403)
		return
	}
	expires := time.Now().Add(time.Hour)
	if verified.Expiry.Before(expires) {
		expires = verified.Expiry
	}
	if !time.Now().Before(expires) {
		fail(w, 403)
		return
	}
	sid := random()
	a.mu.Lock()
	a.prune()
	if len(a.sessions) >= 4096 {
		a.mu.Unlock()
		fail(w, 429)
		return
	}
	delete(a.sessions, a.read(r, sessionCookie))
	a.sessions[sid] = session{CSRF: random(), Expires: expires}
	a.mu.Unlock()
	http.SetCookie(w, a.cookie(sessionCookie, a.sign(sid), int(time.Until(expires).Seconds())))
	http.Redirect(w, r, a.settings.PublicURL+"/", http.StatusSeeOther)
}
func (a *Auth) current(w http.ResponseWriter, r *http.Request) {
	s, ok := a.authorized(r)
	if !ok {
		fail(w, 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"csrfToken":"` + s.CSRF + `"}`))
}
func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authorized(r); !ok {
		fail(w, 403)
		return
	}
	a.mu.Lock()
	delete(a.sessions, a.read(r, sessionCookie))
	a.mu.Unlock()
	http.SetCookie(w, a.cookie(sessionCookie, "", -1))
	w.WriteHeader(http.StatusNoContent)
}
