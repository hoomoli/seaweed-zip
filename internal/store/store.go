package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"seaweed-zip/internal/pack"
	"time"
)

var ErrConflict = errors.New("state or idempotency conflict")

type Store struct{ DB *sql.DB }
type Pack struct {
	ConsumerCount   int          `json:"consumerCount"`
	AckCount        int          `json:"ackCount"`
	ActiveDownloads int          `json:"activeDownloads"`
	ID              string       `json:"packId"`
	State           string       `json:"state"`
	Generation      int64        `json:"generation"`
	Plan            pack.Request `json:"identity"`
	DoneBytes       int64        `json:"doneBytes"`
	DoneObjects     int          `json:"doneObjects"`
	Artifact        string       `json:"-"`
	Hash            string       `json:"artifactSha256,omitempty"`
	Bytes           int64        `json:"bytes"`
	CreatedAt       int64        `json:"createdAt"`
	ReadyAt         int64        `json:"readyAt,omitempty"`
	DeleteAfter     int64        `json:"deleteAfter,omitempty"`
	DeletedAt       int64        `json:"deletedAt,omitempty"`
	Error           string       `json:"error,omitempty"`
	Lease           int64        `json:"-"`
}
type Consumer struct {
	ID            string `json:"consumerId"`
	PackID        string `json:"packId"`
	Generation    int64  `json:"generation"`
	Ack           bool   `json:"ack"`
	Cancelled     bool   `json:"cancelled"`
	Node          string `json:"node,omitempty"`
	Deployment    string `json:"deployment,omitempty"`
	CreatedAt     int64  `json:"createdAt"`
	DownloadLease int64  `json:"downloadLease,omitempty"`
	TokenHash     string `json:"-"`
}
type Event struct {
	Phase string `json:"phase"`
	Code  string `json:"code"`
	At    int64  `json:"at"`
}

func Open(file string) (*Store, error) {
	db, e := sql.Open("sqlite", file)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS migrations(version INTEGER PRIMARY KEY); INSERT OR IGNORE INTO migrations VALUES(1);
 CREATE TABLE IF NOT EXISTS packs(id TEXT PRIMARY KEY,state TEXT NOT NULL,generation INTEGER NOT NULL DEFAULT 0,plan TEXT NOT NULL,done_bytes INTEGER NOT NULL DEFAULT 0,done_objects INTEGER NOT NULL DEFAULT 0,artifact TEXT NOT NULL DEFAULT '',hash TEXT NOT NULL DEFAULT '',bytes INTEGER NOT NULL DEFAULT 0,created_at INTEGER NOT NULL,ready_at INTEGER NOT NULL DEFAULT 0,delete_after INTEGER NOT NULL DEFAULT 0,deleted_at INTEGER NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '',lease INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS pack_objects(pack_id TEXT REFERENCES packs(id) ON DELETE CASCADE,sha256 TEXT,size INTEGER,PRIMARY KEY(pack_id,sha256));
 CREATE TABLE IF NOT EXISTS consumers(id TEXT PRIMARY KEY,pack_id TEXT NOT NULL REFERENCES packs(id) ON DELETE CASCADE,client_key TEXT UNIQUE NOT NULL,token_hash TEXT NOT NULL,generation INTEGER NOT NULL,ack INTEGER NOT NULL DEFAULT 0,cancelled INTEGER NOT NULL DEFAULT 0,node TEXT NOT NULL,deployment TEXT NOT NULL,created_at INTEGER NOT NULL,download_lease INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY,pack_id TEXT REFERENCES packs(id) ON DELETE CASCADE,phase TEXT NOT NULL,code TEXT NOT NULL,at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS downloads(id TEXT PRIMARY KEY,consumer_id TEXT REFERENCES consumers(id) ON DELETE CASCADE,pack_id TEXT REFERENCES packs(id) ON DELETE CASCADE,expires INTEGER NOT NULL);
 `)
	if e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func ID() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}

const columns = `id,state,generation,plan,done_bytes,done_objects,artifact,hash,bytes,created_at,ready_at,delete_after,deleted_at,error,lease,(SELECT count(*) FROM consumers WHERE pack_id=packs.id),(SELECT count(*) FROM consumers WHERE pack_id=packs.id AND ack=1 AND generation=packs.generation),(SELECT count(*) FROM downloads WHERE pack_id=packs.id AND expires>CAST(strftime('%s','now') AS INTEGER))`

func scan(row interface{ Scan(...any) error }) (Pack, error) {
	var p Pack
	var b string
	e := row.Scan(&p.ID, &p.State, &p.Generation, &b, &p.DoneBytes, &p.DoneObjects, &p.Artifact, &p.Hash, &p.Bytes, &p.CreatedAt, &p.ReadyAt, &p.DeleteAfter, &p.DeletedAt, &p.Error, &p.Lease, &p.ConsumerCount, &p.AckCount, &p.ActiveDownloads)
	if e == nil {
		e = json.Unmarshal([]byte(b), &p.Plan)
	}
	return p, e
}
func (s *Store) Get(ctx context.Context, id string) (Pack, error) {
	return scan(s.DB.QueryRowContext(ctx, "SELECT "+columns+" FROM packs WHERE id=?", id))
}
func (s *Store) List(ctx context.Context, offset, limit int, state string) ([]Pack, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT "+columns+" FROM packs WHERE (?='' OR state=?) ORDER BY created_at DESC,id LIMIT ? OFFSET ?", state, state, limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Pack{}
	for rows.Next() {
		p, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) Create(ctx context.Context, p pack.Plan, hash string) (Pack, Consumer, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Pack{}, Consumer{}, e
	}
	defer tx.Rollback()
	safe := p.Request
	safe.ManifestURL = ""
	safe.ClientRequestID = ""
	safe.Objects = make([]pack.Object, len(p.Objects))
	for i, o := range p.Objects {
		o.URL = ""
		safe.Objects[i] = o
	}
	b, _ := json.Marshal(safe)
	now := time.Now().Unix()
	if _, e = tx.ExecContext(ctx, "INSERT INTO packs(id,state,plan,created_at) VALUES(?,'QUEUED',?,?) ON CONFLICT(id) DO NOTHING", p.ID, string(b), now); e != nil {
		return Pack{}, Consumer{}, e
	}
	job, e := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM packs WHERE id=?", p.ID))
	if e != nil {
		return job, Consumer{}, e
	}
	if job.State == "DELETING" {
		return job, Consumer{}, ErrConflict
	}
	var c Consumer
	var oldPack string
	e = tx.QueryRowContext(ctx, "SELECT id,pack_id FROM consumers WHERE client_key=?", p.Request.ClientRequestID).Scan(&c.ID, &oldPack)
	if e == nil && oldPack != p.ID {
		return job, c, ErrConflict
	}
	if e != nil && e != sql.ErrNoRows {
		return job, c, e
	}
	if e == sql.ErrNoRows {
		c.ID = ID()
	}
	if job.State == "DELETED" || job.State == "FAILED" || job.State == "CANCELLED" || job.State == "WAITING_AUTH" {
		_, e = tx.ExecContext(ctx, "UPDATE packs SET state='QUEUED',error='',done_bytes=0,done_objects=0 WHERE id=?", p.ID)
		job.State = "QUEUED"
		if e != nil {
			return job, c, e
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO consumers(id,pack_id,client_key,token_hash,generation,node,deployment,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(client_key) DO UPDATE SET token_hash=excluded.token_hash,generation=excluded.generation,cancelled=0,ack=0`, c.ID, p.ID, p.Request.ClientRequestID, hash, job.Generation, p.Request.Node, p.Request.Deployment, now)
	if e != nil {
		return job, c, e
	}
	for _, o := range p.Objects {
		if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO pack_objects VALUES(?,?,?)", p.ID, o.SHA256, o.Size); e != nil {
			return job, c, e
		}
	}
	if e = tx.Commit(); e != nil {
		return job, c, e
	}
	c, e = s.Consumer(ctx, c.ID)
	return job, c, e
}
func (s *Store) Consumer(ctx context.Context, id string) (Consumer, error) {
	var c Consumer
	e := s.DB.QueryRowContext(ctx, `SELECT id,pack_id,token_hash,generation,ack,cancelled,node,deployment,created_at,COALESCE((SELECT max(expires) FROM downloads WHERE consumer_id=consumers.id AND expires>strftime('%s','now')),0) FROM consumers WHERE id=?`, id).Scan(&c.ID, &c.PackID, &c.TokenHash, &c.Generation, &c.Ack, &c.Cancelled, &c.Node, &c.Deployment, &c.CreatedAt, &c.DownloadLease)
	return c, e
}
func (s *Store) Consumers(ctx context.Context, id string) ([]Consumer, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id FROM consumers WHERE pack_id=? ORDER BY created_at", id)
	if e != nil {
		return nil, e
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	out := []Consumer{}
	for _, id := range ids {
		c, e := s.Consumer(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, nil
}
func changed(r sql.Result, e error) error {
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}
func (s *Store) Claim(ctx context.Context, id string, now time.Time) (Pack, error) {
	e := changed(s.DB.ExecContext(ctx, "UPDATE packs SET state='PACKING',generation=generation+1,lease=?,done_bytes=0,done_objects=0 WHERE id=? AND state='QUEUED'", now.Add(time.Minute).Unix(), id))
	if e != nil {
		return Pack{}, e
	}
	p, e := s.Get(ctx, id)
	if e == nil {
		_, e = s.DB.ExecContext(ctx, "UPDATE consumers SET generation=?,ack=0 WHERE pack_id=? AND cancelled=0", p.Generation, id)
	}
	return p, e
}
func (s *Store) Progress(ctx context.Context, id string, g, n int64, count int) error {
	return changed(s.DB.ExecContext(ctx, "UPDATE packs SET done_bytes=?,done_objects=?,lease=? WHERE id=? AND generation=? AND state='PACKING' AND lease>?", n, count, time.Now().Add(time.Minute).Unix(), id, g, time.Now().Unix()))
}
func (s *Store) Ready(ctx context.Context, id string, g int64, file, hash string, n int64, now time.Time) error {
	return changed(s.DB.ExecContext(ctx, "UPDATE packs SET state='READY',artifact=?,hash=?,bytes=?,ready_at=?,delete_after=?,lease=0 WHERE id=? AND generation=? AND state='PACKING' AND lease>?", file, hash, n, now.Unix(), now.Add(24*time.Hour).Unix(), id, g, now.Unix()))
}
func (s *Store) Fail(ctx context.Context, id string, g int64, state, code string) error {
	return changed(s.DB.ExecContext(ctx, "UPDATE packs SET state=?,error=?,lease=0 WHERE id=? AND generation=? AND state='PACKING'", state, code, id, g))
}
func (s *Store) Recover(ctx context.Context) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE packs SET state='WAITING_AUTH',generation=generation+1,lease=0 WHERE state IN ('QUEUED','VALIDATING','PACKING'); DELETE FROM downloads; UPDATE consumers SET download_lease=0;")
	return e
}
func (s *Store) Event(ctx context.Context, id, phase, code string) {
	s.DB.ExecContext(ctx, "INSERT INTO events(pack_id,phase,code,at) SELECT ?,?,?,? WHERE (SELECT count(*) FROM events WHERE pack_id=?)<1000", id, phase, code, time.Now().Unix(), id)
}
func (s *Store) Events(ctx context.Context, id string) ([]Event, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT phase,code,at FROM events WHERE pack_id=? ORDER BY id", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.Phase, &v.Code, &v.At); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Ack(ctx context.Context, c Consumer, g int64, hash string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var ack bool
	e = tx.QueryRowContext(ctx, "SELECT consumers.ack FROM consumers JOIN packs ON packs.id=consumers.pack_id WHERE consumers.id=? AND consumers.cancelled=0 AND consumers.generation=? AND packs.state='READY' AND packs.generation=? AND packs.hash=?", c.ID, g, g, hash).Scan(&ack)
	if e == sql.ErrNoRows {
		return ErrConflict
	}
	if e != nil {
		return e
	}
	if !ack {
		if _, e = tx.ExecContext(ctx, "UPDATE consumers SET ack=1 WHERE id=?", c.ID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO events(pack_id,phase,code,at) SELECT ?,'CONSUMER','ACK_VERIFIED',? WHERE (SELECT count(*) FROM events WHERE pack_id=?)<1000", c.PackID, time.Now().Unix(), c.PackID); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Cancel(ctx context.Context, c Consumer) error {
	_, e := s.DB.ExecContext(ctx, "UPDATE consumers SET cancelled=1 WHERE id=?", c.ID)
	if e != nil {
		return e
	}
	_, e = s.DB.ExecContext(ctx, "UPDATE packs SET state='CANCELLED',generation=generation+1,lease=0 WHERE id=? AND state IN ('QUEUED','WAITING_AUTH','PACKING') AND NOT EXISTS(SELECT 1 FROM consumers WHERE pack_id=? AND cancelled=0)", c.PackID, c.PackID)
	return e
}
func (s *Store) BeginDownload(ctx context.Context, c Consumer, until time.Time) (string, error) {
	id := ID()
	e := changed(s.DB.ExecContext(ctx, "INSERT INTO downloads(id,consumer_id,pack_id,expires) SELECT ?,?,?,? WHERE EXISTS(SELECT 1 FROM packs JOIN consumers ON consumers.pack_id=packs.id WHERE packs.id=? AND packs.state='READY' AND consumers.id=? AND consumers.cancelled=0)", id, c.ID, c.PackID, until.Unix(), c.PackID, c.ID))
	return id, e
}
func (s *Store) EndDownload(id string) { s.DB.Exec("DELETE FROM downloads WHERE id=?", id) }
func (s *Store) CleanClaim(ctx context.Context, id string, now time.Time) error {
	return changed(s.DB.ExecContext(ctx, "UPDATE packs SET state='DELETING' WHERE id=? AND (state='READY' AND delete_after<=? OR state='DELETING') AND NOT EXISTS(SELECT 1 FROM downloads WHERE pack_id=? AND expires>?)", id, now.Unix(), id, now.Unix()))
}
func (s *Store) Deleted(ctx context.Context, id string, g int64, now time.Time) error {
	return changed(s.DB.ExecContext(ctx, "UPDATE packs SET state='DELETED',deleted_at=?,artifact='',error='' WHERE id=? AND generation=? AND state='DELETING'", now.Unix(), id, g))
}
func (s *Store) Prune(ctx context.Context, before time.Time) error {
	_, e := s.DB.ExecContext(ctx, "DELETE FROM packs WHERE created_at<? AND state IN ('DELETED','FAILED','CANCELLED') AND artifact=''", before.Unix())
	return e
}
