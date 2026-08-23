package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"seaweed-zip/internal/config"
	"seaweed-zip/internal/jobs"
	"seaweed-zip/internal/pack"
	"seaweed-zip/internal/retention"
	"seaweed-zip/internal/source"
	"seaweed-zip/internal/store"
	"strconv"
	"strings"
	"time"
)

type API struct {
	Manager *jobs.Manager
	UI      fs.FS
	mux     *http.ServeMux
	slots   chan struct{}
}

func New(m *jobs.Manager, ui fs.FS) *API {
	a := &API{Manager: m, UI: ui, mux: http.NewServeMux(), slots: make(chan struct{}, 1)}
	a.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, map[string]string{"status": "ok"}) })
	a.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if m.Store.DB.PingContext(r.Context()) != nil {
			a.problem(w, r, 503, "storage", "NOT_READY")
			return
		}
		jsonOut(w, 200, map[string]string{"status": "ready"})
	})
	a.mux.HandleFunc("POST /api/v1/packs", a.create)
	a.mux.HandleFunc("GET /api/v1/consumers/{id}", a.status)
	a.mux.HandleFunc("GET /api/v1/consumers/{id}/artifact", a.artifact)
	a.mux.HandleFunc("POST /api/v1/consumers/{id}/authorization", a.authorization)
	a.mux.HandleFunc("POST /api/v1/consumers/{id}/ack", a.ack)
	a.mux.HandleFunc("POST /api/v1/consumers/{id}/cancel", a.cancel)
	a.mux.HandleFunc("GET /api/v1/admin/packs", a.list)
	a.mux.HandleFunc("GET /api/v1/admin/packs/{id}", a.detail)
	a.mux.HandleFunc("POST /api/v1/admin/consumers/{id}/cancel", a.adminCancel)
	a.mux.HandleFunc("/", a.ui)
	return a
}
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-ID", store.ID())
	a.mux.ServeHTTP(w, r)
}
func jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
func (a *API) problem(w http.ResponseWriter, r *http.Request, status int, phase, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"type": "urn:seaweed-zip:error:" + code, "title": http.StatusText(status), "status": status, "phase": phase, "code": code, "requestId": w.Header().Get("X-Request-ID")})
}
func (a *API) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		a.problem(w, r, 400, "request", "INVALID_JSON")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		a.problem(w, r, 400, "request", "INVALID_JSON")
		return false
	}
	return true
}
func tokenHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (a *API) consumer(w http.ResponseWriter, r *http.Request) (store.Consumer, bool) {
	c, e := a.Manager.Store.Consumer(r.Context(), r.PathValue("id"))
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if e != nil || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || token == "" || subtle.ConstantTimeCompare([]byte(tokenHash(token)), []byte(c.TokenHash)) != 1 {
		a.problem(w, r, 401, "authorization", "INVALID_CAPABILITY")
		return c, false
	}
	if c.Cancelled {
		a.problem(w, r, 409, "consumer", "CANCELLED")
		return c, false
	}
	return c, true
}
func (a *API) boundary(w http.ResponseWriter, r *http.Request, admin bool) bool {
	nets := a.Manager.Config.Trusted
	if admin {
		nets = a.Manager.Config.Admin
	}
	if !config.Allowed(r.RemoteAddr, nets) {
		a.problem(w, r, 403, "network", "NETWORK_DENIED")
		return false
	}
	return true
}
func (a *API) authorize(w http.ResponseWriter, r *http.Request, p pack.Plan) bool {
	if e := a.Manager.Authorize(r.Context(), p); e != nil {
		code := "SOURCE_AUTHORIZATION_OR_LIMIT_FAILED"
		if e.Error() == "disk quota" {
			code = "DISK_QUOTA"
		}
		if e.Error() == "task limit" || e.Error() == "object limit" {
			code = "TASK_LIMIT"
		}
		if e.Error() == "manifest hash mismatch" {
			code = "MANIFEST_HASH_MISMATCH"
		}
		if e.Error() == "manifest identity mismatch" {
			code = "MANIFEST_IDENTITY_MISMATCH"
		}
		if errors.Is(e, source.ErrAuthorization) {
			code = "SOURCE_AUTHORIZATION_EXPIRED"
		}
		a.problem(w, r, 422, "authorization", code)
		return false
	}
	return true
}
func (a *API) take(w http.ResponseWriter, r *http.Request) bool {
	select {
	case a.slots <- struct{}{}:
		return true
	default:
		a.problem(w, r, 429, "admission", "BUSY")
		return false
	}
}
func (a *API) create(w http.ResponseWriter, r *http.Request) {
	if !a.boundary(w, r, false) {
		return
	}
	if !a.take(w, r) {
		return
	}
	defer func() { <-a.slots }()

	var req pack.Request
	if !a.decode(w, r, &req) {
		return
	}
	if req.ClientRequestID == "" || len(req.ClientRequestID) > 256 || len(req.Node) > 256 || len(req.Deployment) > 256 {
		a.problem(w, r, 400, "request", "INVALID_IDEMPOTENCY_KEY")
		return
	}
	p, e := pack.Normalize(req)
	if e != nil {
		a.problem(w, r, 400, "request", "INVALID_PLAN")
		return
	}
	if !a.authorize(w, r, p) {
		return
	}
	token := store.ID()
	job, c, e := a.Manager.Store.Create(r.Context(), p, tokenHash(token))
	if e != nil {
		a.problem(w, r, 409, "create", "IDEMPOTENCY_OR_STATE_CONFLICT")
		return
	}
	a.Manager.Remember(p)
	location := "/api/v1/consumers/" + c.ID
	w.Header().Set("Location", location)
	jsonOut(w, 202, map[string]any{"packId": job.ID, "consumerId": c.ID, "consumerToken": token, "state": job.State, "statusUrl": location})
}
func (a *API) status(w http.ResponseWriter, r *http.Request) {
	c, ok := a.consumer(w, r)
	if !ok {
		return
	}
	p, e := a.Manager.Store.Get(r.Context(), c.PackID)
	if e != nil {
		a.problem(w, r, 404, "status", "NOT_FOUND")
		return
	}
	jsonOut(w, 200, map[string]any{"consumer": c, "pack": p, "downloadPath": "/api/v1/consumers/" + c.ID + "/artifact"})
}
func (a *API) authorization(w http.ResponseWriter, r *http.Request) {
	if !a.boundary(w, r, false) {
		return
	}
	if !a.take(w, r) {
		return
	}
	defer func() { <-a.slots }()

	c, ok := a.consumer(w, r)
	if !ok {
		return
	}
	var req pack.Request
	if !a.decode(w, r, &req) {
		return
	}
	p, e := pack.Normalize(req)
	if e != nil || p.ID != c.PackID {
		a.problem(w, r, 409, "authorization", "CONTENT_MISMATCH")
		return
	}
	if !a.authorize(w, r, p) {
		return
	}
	var key string
	if a.Manager.Store.DB.QueryRowContext(r.Context(), "SELECT client_key FROM consumers WHERE id=?", c.ID).Scan(&key) != nil {
		a.problem(w, r, 404, "authorization", "NOT_FOUND")
		return
	}
	p.Request.ClientRequestID = key
	token := store.ID()
	job, _, e := a.Manager.Store.Create(r.Context(), p, tokenHash(token))
	if e != nil {
		a.problem(w, r, 409, "authorization", "STATE_CONFLICT")
		return
	}
	a.Manager.Remember(p)
	jsonOut(w, 200, map[string]any{"consumerToken": token, "state": job.State})
}
func (a *API) ack(w http.ResponseWriter, r *http.Request) {
	c, ok := a.consumer(w, r)
	if !ok {
		return
	}
	var body struct {
		Generation int64  `json:"generation"`
		Hash       string `json:"artifactSha256"`
	}
	if !a.decode(w, r, &body) {
		return
	}
	if a.Manager.Store.Ack(r.Context(), c, body.Generation, body.Hash) != nil {
		a.problem(w, r, 409, "ack", "ARTIFACT_MISMATCH")
		return
	}

	jsonOut(w, 200, map[string]bool{"ack": true})
}
func (a *API) cancel(w http.ResponseWriter, r *http.Request) {
	c, ok := a.consumer(w, r)
	if !ok {
		return
	}
	if a.Manager.Store.Cancel(r.Context(), c) != nil {
		a.problem(w, r, 409, "cancel", "STATE_CONFLICT")
		return
	}
	jsonOut(w, 200, map[string]bool{"cancelled": true})
}
func (a *API) artifact(w http.ResponseWriter, r *http.Request) {
	c, ok := a.consumer(w, r)
	if !ok {
		return
	}
	p, e := a.Manager.Store.Get(r.Context(), c.PackID)
	if e != nil || p.State != "READY" {
		a.problem(w, r, 409, "download", "NOT_READY")
		return
	}
	hard := retention.DownloadDeadline(time.Unix(p.DeleteAfter, 0), a.Manager.Config.DownloadTimeout)
	if !time.Now().Before(hard) {
		a.problem(w, r, 410, "download", "ARTIFACT_LEASE_WINDOW_EXPIRED")
		return
	}
	until := time.Now().Add(a.Manager.Config.DownloadTimeout)
	if hard.Before(until) {
		until = hard
	}
	lease, e := a.Manager.Store.BeginDownload(r.Context(), c, until)
	if e != nil {
		a.problem(w, r, 409, "download", "STATE_CONFLICT")
		return
	}
	defer func() {
		a.Manager.Store.EndDownload(lease)
		a.Manager.Store.Event(context.Background(), p.ID, "DOWNLOAD", "SEND_EXIT_NOT_ACK")
	}()
	if filepath.Base(p.Artifact) != p.Artifact {
		a.problem(w, r, 500, "download", "UNSAFE_PATH")
		return
	}
	path := filepath.Join(a.Manager.Config.DataDir, "artifacts", p.Artifact)
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		a.problem(w, r, 503, "download", "ARTIFACT_UNAVAILABLE")
		return
	}
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		a.problem(w, r, 503, "download", "ARTIFACT_UNAVAILABLE")
		return
	}
	defer root.Close()
	f, e := root.Open(p.Artifact)
	if e != nil {
		a.problem(w, r, 503, "download", "ARTIFACT_UNAVAILABLE")
		return
	}
	defer f.Close()
	http.NewResponseController(w).SetWriteDeadline(until)
	w.Header().Set("ETag", `"`+p.Hash+`"`)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+p.ID+`.zip"`)
	a.Manager.Store.Event(r.Context(), p.ID, "DOWNLOAD", "START")
	http.ServeContent(w, r, p.ID+".zip", time.Unix(p.ReadyAt, 0), f)
}
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	if !a.boundary(w, r, true) {
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	p, e := a.Manager.Store.List(r.Context(), offset, limit, r.URL.Query().Get("state"))
	if e != nil {
		a.problem(w, r, 503, "admin", "STORAGE_ERROR")
		return
	}
	jsonOut(w, 200, map[string]any{"items": p, "offset": offset, "limit": limit, "hasMore": len(p) == limit})
}
func (a *API) detail(w http.ResponseWriter, r *http.Request) {
	if !a.boundary(w, r, true) {
		return
	}
	p, e := a.Manager.Store.Get(r.Context(), r.PathValue("id"))
	if e != nil {
		a.problem(w, r, 404, "admin", "NOT_FOUND")
		return
	}
	c, _ := a.Manager.Store.Consumers(r.Context(), p.ID)
	events, _ := a.Manager.Store.Events(r.Context(), p.ID)
	jsonOut(w, 200, map[string]any{"pack": p, "consumers": c, "events": events})
}
func (a *API) adminCancel(w http.ResponseWriter, r *http.Request) {
	if !a.boundary(w, r, true) {
		return
	}
	c, e := a.Manager.Store.Consumer(r.Context(), r.PathValue("id"))
	if e != nil {
		a.problem(w, r, 404, "cancel", "NOT_FOUND")
		return
	}
	if a.Manager.Store.Cancel(r.Context(), c) != nil {
		a.problem(w, r, 409, "cancel", "STATE_CONFLICT")
		return
	}
	jsonOut(w, 200, map[string]bool{"cancelled": true})
}
func (a *API) ui(w http.ResponseWriter, r *http.Request) {
	if !a.boundary(w, r, true) {
		return
	}
	if a.UI == nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		a.problem(w, r, 404, "routing", "NOT_FOUND")
		return
	}
	http.FileServer(http.FS(a.UI)).ServeHTTP(w, r)
}
