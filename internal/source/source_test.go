package source

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFixedRoutePreservesSignedRequest(t *testing.T) {
	var host, uri string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		uri = r.RequestURI
		io.WriteString(w, "abc")
	}))
	defer srv.Close()
	c := New(map[string]Rule{"s": {Scheme: "http", Host: "signed.example:8333", Prefix: "/bucket/", Dial: strings.TrimPrefix(srv.URL, "http://")}})
	r, e := c.Get(context.Background(), "s", "http://signed.example:8333/bucket/a%2Fb?z=2&x=a%2Bb")
	if e != nil {
		t.Fatal(e)
	}
	r.Close()
	if host != "signed.example:8333" || uri != "/bucket/a%2Fb?z=2&x=a%2Bb" {
		t.Fatalf("%s %s", host, uri)
	}
	for _, u := range []string{"http://evil/bucket/a", "http://u@ signed.example:8333/bucket/a", "http://signed.example:8333/other/a", "file:///bucket/a", "http://signed.example:8333/bucket/../secret"} {
		if _, e = c.Get(context.Background(), "s", u); e == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
}
func TestRedirectAndPrivateDenied(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://169.254.169.254/", 302) }))
	defer s.Close()
	c := New(map[string]Rule{"s": {Scheme: "http", Host: "signed.test", Prefix: "/b/", Dial: strings.TrimPrefix(s.URL, "http://")}})
	if _, e := c.Get(context.Background(), "s", "http://signed.test/b/x"); e == nil || strings.Contains(e.Error(), "169.254") {
		t.Fatal("unsafe redirect/error")
	}
	c = New(map[string]Rule{"s": {Scheme: "http", Host: strings.TrimPrefix(s.URL, "http://"), Prefix: "/"}})
	if _, e := c.Get(context.Background(), "s", s.URL+"/x"); e == nil {
		t.Fatal("private target accepted")
	}
}

func TestUnauthorizedSourceHasSafeStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		io.WriteString(w, "secret=DO_NOT_LEAK")
	}))
	defer srv.Close()
	c := New(map[string]Rule{"s": {Scheme: "http", Host: "signed.test", Prefix: "/b/", Dial: strings.TrimPrefix(srv.URL, "http://")}})
	_, err := c.Get(context.Background(), "s", "http://signed.test/b/blob?Signature=SECRET")
	if err != ErrAuthorization {
		t.Fatal("not recoverable auth error", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatal("secret leaked")
	}
}

func TestMetadataAddressesDenied(t *testing.T) {
	for _, addr := range []string{"169.254.169.254", "168.63.129.16", "100.100.100.200", "fd00:ec2::254"} {
		if !Metadata(net.ParseIP(addr)) {
			t.Fatal("metadata not denied", addr)
		}
	}
	if Metadata(net.ParseIP("127.0.0.1")) {
		t.Fatal("explicit loopback test route incorrectly denied")
	}
}

func TestConfiguredSchemeAndTLSDoNotDowngrade(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	c := New(map[string]Rule{"s": {Scheme: "https", Host: "signed.example", Bucket: "b", Prefix: "/b/", Dial: strings.TrimPrefix(srv.URL, "https://")}})
	for _, raw := range []string{"http://signed.example/b/x?Signature=SECRET", "https://signed.example/b/x?Signature=SECRET"} {
		if _, e := c.Get(context.Background(), "s", raw); e == nil || strings.Contains(e.Error(), "SECRET") {
			t.Fatal("scheme/certificate bypass", e)
		}
	}
}
