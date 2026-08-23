package pack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
)

type zeros struct{}

func (zeros) Read(b []byte) (int, error) { clear(b); return len(b), nil }

type tail struct {
	bytes int64
	data  []byte
}

func (w *tail) Write(b []byte) (int, error) {
	w.bytes += int64(len(b))
	if len(b) > 512 {
		w.data = append(w.data[:0], b[len(b)-512:]...)
	} else {
		w.data = append(w.data, b...)
		if len(w.data) > 512 {
			w.data = w.data[len(w.data)-512:]
		}
	}
	return len(b), nil
}
func TestZIP64BoundedStreaming(t *testing.T) {
	if os.Getenv("SEAWEED_ZIP_TEST_ZIP64") != "1" {
		t.Skip("opt-in 4 GiB synthetic streaming test")
	}
	const n = int64(1<<32) + 1
	h := sha256.New()
	io.Copy(h, io.LimitReader(zeros{}, n))
	digest := hex.EncodeToString(h.Sum(nil))
	p, _ := Normalize(Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), Objects: []Object{{SHA256: digest, Size: n}}})
	out := &tail{}
	if e := Write(context.Background(), out, p, func(context.Context, Object) (io.ReadCloser, error) {
		return io.NopCloser(io.LimitReader(zeros{}, n)), nil
	}, nil, n+1<<20); e != nil {
		t.Fatal(e)
	}
	if out.bytes <= n || !bytes.Contains(out.data, []byte{'P', 'K', 6, 6}) {
		t.Fatal("ZIP64 end record absent")
	}
}
