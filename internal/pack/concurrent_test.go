package pack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentBoundedSpoolingAndCancellation(t *testing.T) {
	var active, peak atomic.Int32
	objects := []Object{}
	payload := map[string]string{}
	for i := 0; i < 12; i++ {
		s := strings.Repeat(string(rune('a'+i)), 512)
		h := sha256.Sum256([]byte(s))
		key := hex.EncodeToString(h[:])
		objects = append(objects, Object{SHA256: key, Size: 512})
		payload[key] = s
	}
	p := Plan{Objects: objects, Request: Request{ManifestSHA256: strings.Repeat("a", 64)}}
	dir := t.TempDir()
	var out bytes.Buffer
	open := func(ctx context.Context, o Object) (io.ReadCloser, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
		return io.NopCloser(strings.NewReader(payload[o.SHA256])), nil
	}
	if e := WriteConcurrent(context.Background(), &out, p, open, nil, 1<<20, dir, 4); e != nil {
		t.Fatal(e)
	}
	if peak.Load() < 2 || peak.Load() > 4 {
		t.Fatal("download bound", peak.Load())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("staging leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if WriteConcurrent(ctx, &out, p, open, nil, 1<<20, dir, 4) == nil {
		t.Fatal("cancel accepted")
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("cancel leaked staging")
	}
}
