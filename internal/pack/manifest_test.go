package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func manifestFixture(t *testing.T) ([]byte, Request) {
	t.Helper()
	h := strings.Repeat("a", 64)
	m := map[string]any{"schemaVersion": 1, "gameId": "example", "platform": "android", "releaseId": "r1",
		"source": map[string]any{"commitSha": strings.Repeat("b", 40), "branch": "main", "projectId": "1"},
		"files":  []any{map[string]any{"path": "yoo/a.bundle", "sha256": h, "size": 3, "contentType": "application/octet-stream", "objectKey": "example/blobs/sha256/aa/" + h}}}
	b, _ := json.Marshal(m)
	b = append(b, '\n')
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	return b, Request{ContractVersion: ContractVersion, Game: "example", Platform: "android", Release: "r1", ManifestSHA256: hash, CanonicalManifestHash: hash, Objects: []Object{{SHA256: h, Size: 3}}}
}

func TestReleaseManifestStrictSubsetAndHashes(t *testing.T) {
	b, r := manifestFixture(t)
	if e := VerifyManifest(b, r); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Game = "other" },
		func(r *Request) { r.ManifestSHA256 = strings.Repeat("c", 64) },
		func(r *Request) { r.CanonicalManifestHash = strings.Repeat("c", 64) },
		func(r *Request) { r.Objects = []Object{{SHA256: strings.Repeat("c", 64), Size: 3}} },
		func(r *Request) { r.Objects = []Object{{SHA256: strings.Repeat("a", 64), Size: 4}} },
	} {
		bad := r
		mutate(&bad)
		if VerifyManifest(b, bad) == nil {
			t.Fatal("manifest mismatch accepted")
		}
	}
	for _, bad := range []string{
		strings.TrimSuffix(string(b), "\n"),
		strings.Replace(string(b), `"gameId":"example"`, `"gameId":"example","gameId":"example"`, 1),
		strings.Replace(string(b), `"path":"yoo/a.bundle"`, `"path":"../a"`, 1),
		strings.Replace(string(b), `"size":3`, `"size":3.0`, 1),
		strings.Replace(string(b), `"schemaVersion":1`, `"schemaVersion":1,"extra":true`, 1),
		strings.Replace(string(b), `"files":`, `"files" :`, 1),
	} {
		sum := sha256.Sum256([]byte(bad))
		changed := r
		changed.ManifestSHA256 = hex.EncodeToString(sum[:])
		changed.CanonicalManifestHash = changed.ManifestSHA256
		if VerifyManifest([]byte(bad), changed) == nil {
			t.Fatal("malformed manifest accepted")
		}
	}
}

func TestManifestPathCollision(t *testing.T) {
	b, r := manifestFixture(t)
	var m map[string]any
	json.Unmarshal(b, &m)
	files := m["files"].([]any)
	for _, name := range []string{"yoo/a.bundle", "Yoo/b.bundle", "yoo/a.bundle/x"} {
		copyFile := map[string]any{}
		for k, v := range files[0].(map[string]any) {
			copyFile[k] = v
		}
		copyFile["path"] = name
		m["files"] = append(files, copyFile)
		bad, _ := json.Marshal(m)
		bad = append(bad, '\n')
		sum := sha256.Sum256(bad)
		r.ManifestSHA256 = hex.EncodeToString(sum[:])
		r.CanonicalManifestHash = r.ManifestSHA256
		if VerifyManifest(bad, r) == nil {
			t.Fatal("colliding path accepted", name)
		}
	}
}

func TestPrivatePackagesAreNotMissingBlobPayload(t *testing.T) {
	for _, platform := range []string{"wechat", "android"} {
		b, r := manifestFixture(t)
		r.Platform = platform
		var m map[string]any
		json.Unmarshal(b, &m)
		m["platform"] = platform
		privateHash := strings.Repeat("c", 64)
		if platform == "wechat" {
			m["miniGamePackage"] = map[string]any{"objectKey": "example/private/wechat/r1/source.zip", "size": 1, "sha256": privateHash, "commitSha": strings.Repeat("b", 40)}
		} else {
			m["androidPackages"] = []any{map[string]any{"objectKey": "example/private/android/r1/source.apk", "bytes": 1, "sha256": privateHash, "outputFormat": "apk", "deploymentConfigPath": "assets/deployment-config.json"}}
		}
		encode := func() []byte {
			b, _ := json.Marshal(m)
			b = append(b, '\n')
			sum := sha256.Sum256(b)
			r.ManifestSHA256 = hex.EncodeToString(sum[:])
			r.CanonicalManifestHash = r.ManifestSHA256
			return b
		}
		b = encode()
		if e := VerifyManifest(b, r); e != nil {
			t.Fatal("valid private metadata rejected", platform, e)
		}
		r.Objects = []Object{{SHA256: privateHash, Size: 1}}
		if VerifyManifest(b, r) == nil {
			t.Fatal("private package selected", platform)
		}
		f := m["files"].([]any)[0].(map[string]any)
		f["sha256"] = privateHash
		f["size"] = 1
		f["objectKey"] = "example/blobs/sha256/cc/" + privateHash
		b = encode()
		if VerifyManifest(b, r) == nil {
			t.Fatal("private package entered public files", platform)
		}
	}
}

func TestCanonicalRejectsNegativeZero(t *testing.T) {
	b, r := manifestFixture(t)
	r.Objects[0].Size = 0
	b = []byte(strings.Replace(string(b), `"size":3`, `"size":-0`, 1))
	sum := sha256.Sum256(b)
	r.ManifestSHA256 = hex.EncodeToString(sum[:])
	r.CanonicalManifestHash = r.ManifestSHA256
	if VerifyManifest(b, r) == nil {
		t.Fatal("negative zero is not canonical integer zero")
	}
}

func TestUnicodeCaseFoldCollision(t *testing.T) {
	b, r := manifestFixture(t)
	var m map[string]any
	json.Unmarshal(b, &m)
	files := m["files"].([]any)
	first := files[0].(map[string]any)
	first["path"] = "data/i\u0307.bundle"
	second := map[string]any{}
	for k, v := range first {
		second[k] = v
	}
	second["path"] = "data/\u0130.bundle"
	m["files"] = append(files, second)
	b, _ = json.Marshal(m)
	b = append(b, '\n')
	sum := sha256.Sum256(b)
	r.ManifestSHA256 = hex.EncodeToString(sum[:])
	r.CanonicalManifestHash = r.ManifestSHA256
	if VerifyManifest(b, r) == nil {
		t.Fatal("Unicode expanding lowercase collision accepted")
	}
}

func TestAdministratorJacksonCanonicalGolden(t *testing.T) {
	// Generated by Jackson 2.22.1 with TreeMaps, the same byte serializer used
	// by the administrator parser. Includes Unicode/control/HTML escaping edges.
	b, e := os.ReadFile("testdata/manifest-jackson.json")
	if e != nil {
		t.Fatal(e)
	}
	h := "d492d8a34dec84e4d640d1d538b30e25dd088590e48b261fb61fe1f6320e9821"
	r := Request{Game: "game", Platform: "android", Release: "v1", ManifestSHA256: h, CanonicalManifestHash: h, Objects: []Object{{SHA256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", Size: 5}}}
	if e = VerifyManifest(b, r); e != nil {
		t.Fatal("cross-language canonical divergence", e)
	}
}
