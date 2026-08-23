package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sync"
)

type stagedReader struct {
	*os.File
	name string
}

func (r *stagedReader) Close() error {
	e := r.File.Close()
	remove := os.Remove(r.name)
	if e != nil {
		return e
	}
	return remove
}

// Each window fetches at most concurrency blobs to disk. The single ZIP writer
// consumes and removes the window before admitting more source downloads.
func WriteConcurrent(ctx context.Context, out io.Writer, p Plan, open func(context.Context, Object) (io.ReadCloser, error), progress func(int64, int), limit int64, dir string, concurrency int) error {
	width := min(max(concurrency, 1), 16)
	names := make([]string, len(p.Objects))
	defer func() {
		for _, name := range names {
			if name != "" {
				os.Remove(name)
			}
		}
	}()
	index := 0
	next := func(ctx context.Context, o Object) (io.ReadCloser, error) {
		if index%width == 0 {
			batch, cancel := context.WithCancel(ctx)
			var wg sync.WaitGroup
			var once sync.Once
			var first error
			for i := index; i < min(index+width, len(p.Objects)); i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					e := func() error {
						if e := batch.Err(); e != nil {
							return e
						}
						f, e := os.CreateTemp(dir, "blob-*")
						if e != nil {
							return errors.New("staging unavailable")
						}
						names[i] = f.Name()
						defer f.Close()
						r, e := open(batch, p.Objects[i])
						if e != nil {
							return e
						}
						defer r.Close()
						h := sha256.New()
						n, e := io.CopyBuffer(io.MultiWriter(f, h), io.LimitReader(r, p.Objects[i].Size+1), make([]byte, 64<<10))
						if e != nil {
							return errors.New("object read failed")
						}
						if n != p.Objects[i].Size || hex.EncodeToString(h.Sum(nil)) != p.Objects[i].SHA256 {
							return errors.New("object integrity mismatch")
						}
						if e = batch.Err(); e != nil {
							return e
						}
						return f.Close()
					}()
					if e != nil {
						once.Do(func() { first = e; cancel() })
					}
				}(i)
			}
			wg.Wait()
			cancel()
			if first != nil {
				return nil, first
			}
		}
		name := names[index]
		index++
		f, e := os.Open(name)
		if e != nil {
			return nil, errors.New("staging unavailable")
		}
		return &stagedReader{f, name}, nil
	}
	return Write(ctx, out, p, next, progress, limit)
}
