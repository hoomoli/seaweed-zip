package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"seaweed-zip/internal/config"
	"seaweed-zip/internal/pack"
	"seaweed-zip/internal/source"
	"seaweed-zip/internal/store"
	"sync"
	"time"
)

type authorization struct {
	Plan     pack.Plan
	Revision uint64
	Expires  time.Time
}
type Manager struct {
	Store    *store.Store
	Source   *source.Client
	Config   config.Config
	mu       sync.Mutex
	auth     map[string]authorization
	revision uint64
	step     sync.Mutex
}

func New(s *store.Store, c *source.Client, cfg config.Config) *Manager {
	return &Manager{Store: s, Source: c, Config: cfg, auth: map[string]authorization{}}
}
func (m *Manager) cacheRoom(p pack.Plan) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	var used int64
	count := 0
	for id, a := range m.auth {
		if id != p.ID {
			count++
			used += authorizationWeight(a.Plan)
		}
	}
	return count < 128 && used+authorizationWeight(p) <= 64<<20
}
func authorizationWeight(p pack.Plan) int64 {
	n := int64(len(p.Request.ManifestURL) + len(p.Request.CanonicalManifestHash) + 4096)
	for _, o := range p.Objects {
		n += int64(len(o.URL) + 256)
	}
	return n
}
func (m *Manager) Authorize(ctx context.Context, p pack.Plan) error {
	if existing, e := m.Store.Get(ctx, p.ID); e != nil || existing.State != "READY" {
		if !m.cacheRoom(p) {
			return errors.New("authorization cache limit")
		}
	}

	if len(p.Objects) > m.Config.MaxObjects || p.Bytes > m.Config.MaxBytes {
		return errors.New("task limit")
	}
	for _, o := range p.Objects {
		if o.Size > m.Config.MaxObjectBytes {
			return errors.New("object limit")
		}
	}
	if e := m.Capacity(ctx, p.ID); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return m.Source.Authorize(ctx, p, m.Config.MaxManifestBytes)
}
func (m *Manager) Capacity(ctx context.Context, id string) error {
	if p, e := m.Store.Get(ctx, id); e == nil && p.State == "READY" {
		return nil
	}
	var used int64
	e := filepath.WalkDir(m.Config.DataDir, func(_ string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink denied")
		}
		if !d.IsDir() {
			s, e := d.Info()
			if e != nil {
				return e
			}
			used += s.Size()
		}
		return nil
	})
	if e != nil {
		return errors.New("storage unavailable")
	}
	if used > m.Config.DiskQuota-m.Config.MaxZIPBytes {
		return errors.New("disk quota")
	}

	var pending int64
	if e = m.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM packs WHERE id<>? AND state IN ('QUEUED','VALIDATING','PACKING','WAITING_AUTH')", id).Scan(&pending); e != nil {
		return errors.New("storage unavailable")
	}
	if pending > (m.Config.DiskQuota-used-m.Config.MaxZIPBytes)/m.Config.MaxZIPBytes {
		return errors.New("disk quota")
	}
	return nil
}
func (m *Manager) Remember(p pack.Plan) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	existing, e := m.Store.Get(ctx, p.ID)
	if e != nil || existing.State == "READY" || existing.State == "DELETING" || existing.State == "DELETED" {
		return
	}
	m.Store.DB.ExecContext(ctx, "UPDATE packs SET state='QUEUED' WHERE id=? AND state='WAITING_AUTH'", p.ID)
	m.revision++
	m.auth[p.ID] = authorization{p, m.revision, time.Now().Add(15 * time.Minute)}
}
func (m *Manager) purge(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, a := range m.auth {
		p, e := m.Store.Get(ctx, id)
		if e != nil || (p.State != "QUEUED" && p.State != "PACKING") || time.Now().After(a.Expires) {
			if e == nil && p.State == "QUEUED" {
				m.Store.DB.ExecContext(ctx, "UPDATE packs SET state='WAITING_AUTH' WHERE id=? AND state='QUEUED' AND generation=?", id, p.Generation)
			}
			delete(m.auth, id)
		}
	}
}
func (m *Manager) Run(ctx context.Context) {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.purge(ctx)
			}
		}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Step(ctx)
		}
	}
}
func (m *Manager) Step(ctx context.Context) {
	m.step.Lock()
	defer m.step.Unlock()
	m.Store.DB.ExecContext(ctx, "UPDATE packs SET state='WAITING_AUTH',generation=generation+1,lease=0,error='WORKER_LEASE_EXPIRED' WHERE state='PACKING' AND lease<=?", time.Now().Unix())
	m.purge(ctx)
	list, e := m.Store.List(ctx, 0, 100000, "QUEUED")
	if e != nil {
		return
	}
	for _, item := range list {
		m.mu.Lock()
		a, ok := m.auth[item.ID]
		p := a.Plan
		m.mu.Unlock()
		if !ok || time.Now().After(a.Expires) {
			continue
		}
		job, e := m.Store.Claim(ctx, p.ID, time.Now())
		if e != nil {
			continue
		}
		m.build(ctx, job, p)
		m.mu.Lock()
		if current, ok := m.auth[p.ID]; ok && current.Revision == a.Revision {
			delete(m.auth, p.ID)
		}
		m.mu.Unlock()
		return
	}
}
func (m *Manager) build(parent context.Context, job store.Pack, p pack.Plan) {
	defer func() {
		recovery, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if m.Store.Fail(recovery, p.ID, job.Generation, "WAITING_AUTH", "WORKER_PUBLICATION_INTERRUPTED") == nil {
			m.Store.Event(recovery, p.ID, "WAITING_AUTH", "WORKER_PUBLICATION_INTERRUPTED")
		}
	}()
	ctx, cancel := context.WithTimeout(parent, m.Config.TaskTimeout)
	defer cancel()
	m.Store.Event(ctx, p.ID, "PACKING", "START")
	dir := filepath.Join(m.Config.DataDir, "artifacts")
	if e := os.MkdirAll(dir, 0700); e != nil {
		m.Store.Fail(parent, p.ID, job.Generation, "FAILED", "STORAGE_ERROR")
		return
	}
	name := fmt.Sprintf("%s-%d.zip", p.ID, job.Generation)
	tmp := filepath.Join(dir, name+".partial")
	final := filepath.Join(dir, name)
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		m.Store.Fail(parent, p.ID, job.Generation, "FAILED", "STORAGE_ERROR")
		return
	}
	defer os.Remove(tmp)
	var progressMu sync.Mutex
	var done int64
	var count int
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				progressMu.Lock()
				n, c := done, count
				progressMu.Unlock()
				if m.Store.Progress(ctx, p.ID, job.Generation, n, c) != nil {
					cancel()
				}
			}
		}
	}()
	h := sha256.New()
	e = pack.Write(ctx, io.MultiWriter(f, h), p, func(ctx context.Context, o pack.Object) (io.ReadCloser, error) {
		return m.Source.Get(ctx, p.Request.SourceID, o.URL)
	}, func(n int64, c int) { progressMu.Lock(); done, count = n, c; progressMu.Unlock() }, m.Config.MaxZIPBytes)
	close(stop)
	<-stopped
	if e == nil {
		e = f.Sync()
	}
	info, statErr := f.Stat()
	closeErr := f.Close()
	if e == nil {
		e = statErr
	}
	if e == nil {
		e = closeErr
	}
	if e != nil {
		state := "FAILED"
		code := "PACK_INTEGRITY_OR_IO"
		if errors.Is(e, source.ErrAuthorization) {
			state = "WAITING_AUTH"
			code = "SOURCE_AUTHORIZATION_EXPIRED"
		}
		if ctx.Err() != nil {
			code = "TASK_TIMEOUT_OR_CANCELLED"
		}
		m.Store.Fail(parent, p.ID, job.Generation, state, code)
		m.Store.Event(parent, p.ID, state, code)
		return
	}
	if e = m.Store.Progress(ctx, p.ID, job.Generation, p.Bytes, len(p.Objects)); e != nil {
		return
	}
	if e = os.Rename(tmp, final); e != nil {
		m.Store.Fail(parent, p.ID, job.Generation, "FAILED", "PUBLISH_ERROR")
		return
	}
	directory, openErr := os.Open(dir)
	if openErr != nil {
		os.Remove(final)
		return
	}
	syncErr := directory.Sync()
	directory.Close()
	if syncErr != nil {
		os.Remove(final)
		return
	}
	if e = m.Store.Ready(ctx, p.ID, job.Generation, name, hex.EncodeToString(h.Sum(nil)), info.Size(), time.Now()); e != nil {
		os.Remove(final)
		return
	}
	m.Store.Event(ctx, p.ID, "READY", "VERIFIED")

}
