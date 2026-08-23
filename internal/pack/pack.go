package pack

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

const FormatVersion = "bagit-store-v1"
const ContractVersion = "release-manifest-v1"

type Object struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	URL    string `json:"url,omitempty"`
}
type Request struct {
	ContractVersion       string   `json:"contractVersion"`
	SourceID              string   `json:"sourceId"`
	Bucket                string   `json:"bucket"`
	Game                  string   `json:"game"`
	Platform              string   `json:"platform"`
	Release               string   `json:"release"`
	ManifestSHA256        string   `json:"manifestSha256"`
	CanonicalManifestHash string   `json:"canonicalManifestHash,omitempty"`
	ManifestURL           string   `json:"manifestUrl,omitempty"`
	Objects               []Object `json:"objects"`
	ClientRequestID       string   `json:"clientRequestId"`
	Node                  string   `json:"node,omitempty"`
	Deployment            string   `json:"deployment,omitempty"`
}
type Plan struct {
	ID      string
	Request Request
	Objects []Object
	Bytes   int64
}

func ValidHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func Normalize(r Request) (Plan, error) {
	var p Plan
	label := regexp.MustCompile(`^[A-Za-z0-9._-]{1,256}$`)
	for _, s := range []string{r.SourceID, r.Bucket, r.Game, r.Platform, r.Release} {
		if !label.MatchString(s) {
			return p, errors.New("invalid identity")
		}
	}
	for _, s := range []string{r.Node, r.Deployment, r.ClientRequestID} {
		if s != "" && !label.MatchString(s) {
			return p, errors.New("invalid label")
		}
	}
	if !ValidHash(r.ManifestSHA256) || len(r.Objects) == 0 {
		return p, errors.New("invalid manifest or empty selection")
	}
	unique := map[string]Object{}
	for _, o := range r.Objects {
		if !ValidHash(o.SHA256) || o.Size < 0 {
			return p, errors.New("invalid object")
		}
		if prev, ok := unique[o.SHA256]; ok && prev.Size != o.Size {
			return p, errors.New("conflicting size")
		}
		unique[o.SHA256] = o
	}
	for _, o := range unique {
		if o.Size > 1<<50 || p.Bytes > 1<<50-o.Size {
			return p, errors.New("size overflow")
		}
		p.Bytes += o.Size
		p.Objects = append(p.Objects, o)
	}
	sort.Slice(p.Objects, func(i, j int) bool { return p.Objects[i].SHA256 < p.Objects[j].SHA256 })
	safe := r
	safe.ManifestURL = ""
	safe.CanonicalManifestHash = ""
	safe.ClientRequestID = ""
	safe.Node = ""
	safe.Deployment = ""
	safe.Objects = make([]Object, len(p.Objects))
	for i, o := range p.Objects {
		o.URL = ""
		safe.Objects[i] = o
	}
	// JSON encodes a fixed struct, not the upstream canonical manifest algorithm.
	b, _ := json.Marshal(struct {
		Request Request
		Format  string
	}{safe, FormatVersion})
	h := sha256.Sum256(b)
	p.ID = hex.EncodeToString(h[:])
	r.Objects = p.Objects
	p.Request = r
	return p, nil
}

type limitedWriter struct {
	w      io.Writer
	n, max int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.max-w.n {
		return 0, errors.New("zip limit")
	}
	n, e := w.w.Write(p)
	w.n += int64(n)
	return n, e
}
func Write(ctx context.Context, out io.Writer, p Plan, open func(context.Context, Object) (io.ReadCloser, error), progress func(int64, int), max int64) error {
	z := zip.NewWriter(&limitedWriter{w: out, max: max})
	add := func(name, body string) error {
		w, e := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if e != nil {
			return e
		}
		_, e = io.WriteString(w, body)
		return e
	}
	if e := add("bagit.txt", "BagIt-Version: 1.0\nTag-File-Character-Encoding: UTF-8\n"); e != nil {
		return e
	}
	if e := add("bag-info.txt", fmt.Sprintf("Source-Manifest-SHA256: %s\nPackage-Format-Version: %s\n", p.Request.ManifestSHA256, FormatVersion)); e != nil {
		return e
	}
	var manifest strings.Builder
	var total int64
	for i, o := range p.Objects {
		if e := ctx.Err(); e != nil {
			return e
		}
		r, e := open(ctx, o)
		if e != nil {
			return e
		}
		w, e := z.CreateHeader(&zip.FileHeader{Name: "data/blobs/" + o.SHA256, Method: zip.Store})
		if e != nil {
			r.Close()
			return e
		}
		h := sha256.New()
		buf := make([]byte, 64*1024)
		var n int64
		for {
			if e = ctx.Err(); e != nil {
				break
			}
			var k int
			k, e = r.Read(buf)
			if k > 0 {
				if n+int64(k) > o.Size {
					e = errors.New("object length mismatch")
					break
				}
				var written int
				written, e = io.MultiWriter(w, h).Write(buf[:k])
				n += int64(written)
				if progress != nil {
					progress(total+n, i)
				}
				if e != nil {
					break
				}
			}
			if e != nil {
				break
			}
		}
		r.Close()
		if e != io.EOF {
			return e
		}
		if n != o.Size || hex.EncodeToString(h.Sum(nil)) != o.SHA256 {
			return errors.New("object integrity mismatch")
		}
		total += n
		if progress != nil {
			progress(total, i+1)
		}
		fmt.Fprintf(&manifest, "%s  data/blobs/%s\n", o.SHA256, o.SHA256)
	}
	if e := add("manifest-sha256.txt", manifest.String()); e != nil {
		return e
	}
	return z.Close()
}
