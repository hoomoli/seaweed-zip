package retention

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"seaweed-zip/internal/store"
	"strconv"
	"time"
)

func Next(now time.Time) time.Time {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	n := now.In(loc)
	next := time.Date(n.Year(), n.Month(), n.Day(), 4, 10, 0, 0, loc)
	if !next.After(n) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
func CalendarExpiry(t time.Time) time.Time {
	first := time.Date(t.Year(), t.Month()+3, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(t.Day(), last)-1)
}

type Cleaner struct {
	Store *store.Store
	Dir   string
}

func DownloadDeadline(expiry time.Time, maxLease time.Duration) time.Time {
	return Next(expiry.Add(-time.Nanosecond)).Add(maxLease)
}
func (c Cleaner) Sweep(ctx context.Context, now time.Time) { c.sweep(ctx, now, now) }
func (c Cleaner) sweep(ctx context.Context, now, cutoff time.Time) {
	list, e := c.Store.List(ctx, 0, 100000, "")
	if e != nil {
		return
	}
	for _, p := range list {
		if p.State != "READY" && p.State != "DELETING" {
			continue
		}
		if c.Store.CleanClaim(ctx, p.ID, now) != nil {
			continue
		}
		if p.Artifact != "" {
			if filepath.Base(p.Artifact) != p.Artifact {
				c.Store.Event(ctx, p.ID, "DELETING", "UNSAFE_PATH")
				continue
			}
			root, e := os.OpenRoot(filepath.Join(c.Dir, "artifacts"))
			if e != nil {
				c.Store.Event(ctx, p.ID, "DELETING", "DELETE_FAILED")
				continue
			}
			e = root.Remove(p.Artifact)
			root.Close()
			if e != nil && !os.IsNotExist(e) {
				c.Store.Event(ctx, p.ID, "DELETING", "DELETE_FAILED")
				continue
			}
		}
		if c.Store.Deleted(ctx, p.ID, p.Generation, now) == nil {
			c.Store.Event(ctx, p.ID, "DELETED", "ARTIFACT_REMOVED")
		}
	}
	remaining := c.orphans(ctx)
	// Evaluate calendar expiry per record, rather than subtracting a fixed duration.
	for _, p := range list {
		if CalendarExpiry(time.Unix(p.CreatedAt, 0)).After(now) {
			continue
		}
		if !remaining[p.ID] && (p.State == "DELETED" || p.State == "FAILED" || p.State == "CANCELLED") {
			c.Store.DB.ExecContext(ctx, "DELETE FROM packs WHERE id=? AND artifact='' AND state IN ('DELETED','FAILED','CANCELLED')", p.ID)
		}
	}
}
func (c Cleaner) Run(ctx context.Context) {
	cutoff := time.Now()
	c.sweep(ctx, cutoff, cutoff)
	timer := time.NewTimer(time.Until(Next(time.Now())))
	defer timer.Stop()
	retry := time.NewTicker(time.Minute)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			cutoff = time.Now()
			c.sweep(ctx, cutoff, cutoff)
			timer.Reset(time.Until(Next(time.Now())))
		case <-retry.C:
			c.sweep(ctx, time.Now(), time.Now())
		}
	}
}

var artifactName = regexp.MustCompile(`^([a-f0-9]{64})-([0-9]+)\.zip(\.partial)?$`)
var stagingName = regexp.MustCompile(`^([a-f0-9]{64})-([0-9]+)\.staging-[0-9]+$`)

func (c Cleaner) orphans(ctx context.Context) map[string]bool {
	remaining := map[string]bool{}
	dir := filepath.Join(c.Dir, "artifacts")
	entries, e := os.ReadDir(dir)
	if e != nil {
		return remaining
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return remaining
	}
	defer root.Close()
	for _, entry := range entries {
		if ctx.Err() != nil {
			return remaining
		}
		match := artifactName.FindStringSubmatch(entry.Name())
		stage := false
		if match == nil {
			match = stagingName.FindStringSubmatch(entry.Name())
			stage = match != nil
		}
		if match == nil {
			continue
		}
		p, e := c.Store.Get(ctx, match[1])
		// A timeout or storage failure is not evidence that a live worker's files
		// are orphaned. Only a confirmed missing row permits unknown-ID cleanup.
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			remaining[match[1]] = true
			continue
		}
		generation, _ := strconv.ParseInt(match[2], 10, 64)
		if e == nil && (p.Artifact == entry.Name() || p.State == "PACKING" && p.Generation == generation) {
			remaining[match[1]] = true
			continue
		}
		if stage {
			e = root.RemoveAll(entry.Name())
		} else {
			e = root.Remove(entry.Name())
		}
		if e != nil && !os.IsNotExist(e) {
			remaining[match[1]] = true
			c.Store.Event(ctx, match[1], "CLEANUP", "ORPHAN_DELETE_FAILED")
		}
	}
	return remaining
}
