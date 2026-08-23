package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var gamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
var releasePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var pipelinePattern = regexp.MustCompile(`.*/-/pipelines/[0-9]+$`)

const manifestMaxBytes int64 = 100 << 30

// StrictJSON rejects duplicate members at every depth, trailing JSON and invalid
// UTF-8 before typed decoding can silently replace or overwrite them.
func StrictJSON(b []byte) (any, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("invalid JSON encoding")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 16 {
			return nil, errors.New("JSON nesting limit")
		}
		t, e := d.Token()
		if e != nil {
			return nil, e
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				m := map[string]any{}
				for d.More() {
					key, e := d.Token()
					if e != nil {
						return nil, e
					}
					k, ok := key.(string)
					if !ok {
						return nil, errors.New("invalid JSON key")
					}
					if _, ok := m[k]; ok {
						return nil, errors.New("duplicate JSON key")
					}
					v, e := read(depth + 1)
					if e != nil {
						return nil, e
					}
					m[k] = v
				}
				_, e = d.Token()
				return m, e
			case '[':
				a := []any{}
				for d.More() {
					v, e := read(depth + 1)
					if e != nil {
						return nil, e
					}
					a = append(a, v)
				}
				_, e = d.Token()
				return a, e
			default:
				return nil, errors.New("invalid JSON delimiter")
			}
		}
		return t, nil
	}
	v, e := read(0)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return v, nil
}

func fields(v any, required string, optional ...string) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	allowed := map[string]bool{}
	for _, k := range strings.Fields(required) {
		allowed[k] = true
		if _, ok := m[k]; !ok {
			return nil, false
		}
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return nil, false
		}
	}
	return m, true
}
func text(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func integer(v any, low, high int64) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, e := strconv.ParseInt(string(n), 10, 64)
	return i, e == nil && i >= low && i <= high
}
func unsafePath(s string) bool {
	if s == "" || len(s) > 4096 || strings.HasPrefix(s, "/") || strings.ContainsAny(s, "\\:") {
		return true
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return true
		}
	}
	for _, p := range strings.Split(s, "/") {
		if p == "" || p == "." || p == ".." {
			return true
		}
	}
	return false
}

func foldPath(s string) string {
	// ROOT lowercasing expands dotted I. Fold both sigma spellings conservatively
	// so context-sensitive Java lowercase cannot hide a filesystem collision.
	s = strings.ReplaceAll(s, "\u0130", "i\u0307")
	return strings.ReplaceAll(strings.ToLower(s), "\u03c2", "\u03c3")
}

// canonical matches Jackson's UTF-8 generator, including uppercase surrogate
// escapes; encoding/json differs on HTML, U+2028/U+2029 and supplementary runes.
func canonical(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			canonical(b, k)
			b.WriteByte(':')
			canonical(b, x[k])
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			canonical(b, e)
		}
		b.WriteByte(']')
	case string:
		b.WriteByte('"')
		for _, r := range x {
			switch r {
			case '"', '\\':
				b.WriteByte('\\')
				b.WriteRune(r)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				if r < 32 {
					fmt.Fprintf(b, `\u%04X`, r)
				} else if r > 0xffff {
					hi, lo := utf16.EncodeRune(r)
					fmt.Fprintf(b, `\u%04X\u%04X`, hi, lo)
				} else {
					b.WriteRune(r)
				}
			}
		}
		b.WriteByte('"')
	case json.Number:
		n, _ := strconv.ParseInt(string(x), 10, 64)
		b.WriteString(strconv.FormatInt(n, 10))
	}
}

func VerifyManifest(b []byte, r Request) error {
	fail := func() error { return errors.New("manifest schema or selection mismatch") }
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != r.ManifestSHA256 {
		return errors.New("manifest hash mismatch")
	}
	v, e := StrictJSON(b)
	if e != nil {
		return fail()
	}
	m, ok := fields(v, "schemaVersion gameId platform releaseId source files", "miniGamePackage", "androidPackages")
	if !ok {
		return fail()
	}
	if n, ok := integer(m["schemaVersion"], 1, 1); !ok || n != 1 {
		return fail()
	}
	game, platform, release := text(m, "gameId"), text(m, "platform"), text(m, "releaseId")
	if game != r.Game || platform != r.Platform || release != r.Release {
		return errors.New("manifest identity mismatch")
	}
	if !gamePattern.MatchString(game) || !releasePattern.MatchString(release) || strings.HasSuffix(release, ".") || (platform != "android" && platform != "ios" && platform != "wechat") {
		return fail()
	}
	src, ok := fields(m["source"], "commitSha branch projectId")
	if !ok || !commitPattern.MatchString(text(src, "commitSha")) || strings.TrimSpace(text(src, "branch")) == "" || len(text(src, "branch")) > 1024 || strings.TrimSpace(text(src, "projectId")) == "" || len(text(src, "projectId")) > 100 {
		return fail()
	}
	private := map[string]bool{}
	if raw, exists := m["miniGamePackage"]; exists {
		p, ok := fields(raw, "objectKey size sha256 commitSha", "pipelineUrl")
		if !ok || platform != "wechat" || !ValidHash(text(p, "sha256")) || text(p, "commitSha") != text(src, "commitSha") {
			return fail()
		}
		if _, ok = integer(p["size"], 1, 2<<30); !ok {
			return fail()
		}
		key := text(p, "objectKey")
		prefix := game + "/private/wechat/" + release + "/"
		if key != prefix+"source.zip" && key != prefix+text(p, "sha256")+".zip" {
			return fail()
		}
		if raw, exists := p["pipelineUrl"]; exists {
			s, ok := raw.(string)
			u, e := url.Parse(s)
			if !ok || e != nil || len(s) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || !pipelinePattern.MatchString(u.Path) {
				return fail()
			}
		}
		private[text(p, "sha256")] = true
	}
	if raw, exists := m["androidPackages"]; exists {
		a, ok := raw.([]any)
		if !ok || platform != "android" || len(a) < 1 || len(a) > 2 {
			return fail()
		}
		seen := map[string]bool{}
		for _, raw := range a {
			p, ok := fields(raw, "objectKey sha256 bytes outputFormat deploymentConfigPath")
			if !ok {
				return fail()
			}
			format := text(p, "outputFormat")
			cp := "assets/deployment-config.json"
			if format == "aab" {
				cp = "base/" + cp
			}
			if (format != "apk" && format != "aab") || seen[format] || !ValidHash(text(p, "sha256")) || text(p, "objectKey") != game+"/private/android/"+release+"/source."+format || text(p, "deploymentConfigPath") != cp {
				return fail()
			}
			seen[format] = true
			if _, ok = integer(p["bytes"], 1, 2<<30); !ok {
				return fail()
			}
			private[text(p, "sha256")] = true
		}
	}
	files, ok := m["files"].([]any)
	if !ok || len(files) > 20000 {
		return fail()
	}
	sizes := map[string]int64{}
	paths := map[string]bool{}
	dirs := map[string]bool{}
	spelling := map[string]string{}
	var total int64
	for _, raw := range files {
		f, ok := fields(raw, "path sha256 size contentType objectKey")
		if !ok {
			return fail()
		}
		name, h, ct := text(f, "path"), text(f, "sha256"), text(f, "contentType")
		n, ok := integer(f["size"], 0, manifestMaxBytes)
		if !ok || unsafePath(name) || !ValidHash(h) || private[h] || text(f, "objectKey") != game+"/blobs/sha256/"+h[:2]+"/"+h || ct == "" || len(ct) > 255 {
			return fail()
		}
		for _, c := range ct {
			if c < 32 || c == 127 {
				return fail()
			}
		}
		lower, ctype := foldPath(name), strings.ToLower(ct)
		if platform == "wechat" && (strings.HasSuffix(lower, ".zip") || ctype == "application/zip" || ctype == "application/x-zip-compressed") || platform == "android" && (strings.HasSuffix(lower, ".apk") || strings.HasSuffix(lower, ".aab") || ctype == "application/vnd.android.package-archive") {
			return fail()
		}
		if prior, exists := sizes[h]; exists && prior != n {
			return fail()
		}
		sizes[h] = n
		total += n
		if total > manifestMaxBytes {
			return fail()
		}
		if paths[lower] || dirs[lower] {
			return fail()
		}
		paths[lower] = true
		parts := strings.Split(name, "/")
		for i := range parts {
			original := strings.Join(parts[:i+1], "/")
			folded := foldPath(original)
			if prior, exists := spelling[folded]; exists && prior != original {
				return fail()
			}
			spelling[folded] = original
			if i < len(parts)-1 {
				if paths[folded] {
					return fail()
				}
				dirs[folded] = true
			}
		}
	}
	for _, o := range r.Objects {
		n, ok := sizes[o.SHA256]
		if !ok || n != o.Size {
			return fail()
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return text(files[i].(map[string]any), "path") < text(files[j].(map[string]any), "path")
	})
	var out bytes.Buffer
	canonical(&out, m)
	out.WriteByte('\n')
	canonicalSum := sha256.Sum256(out.Bytes())
	if !ValidHash(r.CanonicalManifestHash) || hex.EncodeToString(canonicalSum[:]) != r.CanonicalManifestHash {
		return errors.New("canonical manifest hash mismatch")
	}
	if !bytes.Equal(b, out.Bytes()) {
		return errors.New("manifest is not canonical")
	}
	return nil
}
