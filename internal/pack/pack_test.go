package pack

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestIdentityAndConflicts(t *testing.T) {
	h := strings.Repeat("a", 64)
	r := Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: h, Objects: []Object{{SHA256: h, Size: 3}, {SHA256: h, Size: 3}}}
	a, err := Normalize(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Objects = r.Objects[:1]
	b, _ := Normalize(r)
	if a.ID != b.ID || len(a.Objects) != 1 {
		t.Fatal("duplicate changed identity")
	}
	r.Release = "other"
	c, _ := Normalize(r)
	if c.ID == a.ID {
		t.Fatal("release not bound")
	}
	r.Objects = append(r.Objects, Object{SHA256: h, Size: 4})
	if _, err = Normalize(r); err == nil {
		t.Fatal("conflicting sizes accepted")
	}
}
func TestBagItAndIntegrity(t *testing.T) {
	payload := []byte("abc")
	sum := sha256.Sum256(payload)
	h := hex.EncodeToString(sum[:])
	r, _ := Normalize(Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), Objects: []Object{{SHA256: h, Size: 3}}})
	var out bytes.Buffer
	open := func(context.Context, Object) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	if err := Write(context.Background(), &out, r, open, nil, 1024*1024); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 4 {
		t.Fatal(len(z.File))
	}
	for _, f := range z.File {
		if f.Method != zip.Store {
			t.Fatal("compressed")
		}
	}
	for _, p := range [][]byte{[]byte("abd"), []byte("ab"), []byte("abcd")} {
		payload = p
		out.Reset()
		if Write(context.Background(), &out, r, open, nil, 1024*1024) == nil {
			t.Fatal("invalid object accepted")
		}
	}
}

func TestCanonicalLabelDoesNotChangePackIdentity(t *testing.T) {
	r := Request{SourceID: "s", Bucket: "b", Game: "g", Platform: "p", Release: "r", ManifestSHA256: strings.Repeat("a", 64), Objects: []Object{{SHA256: strings.Repeat("b", 64), Size: 3}}}
	a, _ := Normalize(r)
	r.CanonicalManifestHash = "upstream-algorithm-label"
	b, _ := Normalize(r)
	if a.ID != b.ID {
		t.Fatal("canonical label changed raw-byte content identity")
	}
}
