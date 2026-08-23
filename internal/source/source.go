package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"seaweed-zip/internal/pack"
	"strings"
	"time"
)

var ErrAuthorization = errors.New("source GET authorization rejected")

type Rule struct {
	Host   string `json:"host"`
	Prefix string `json:"prefix"`
	Dial   string `json:"dial,omitempty"`
	Bucket string `json:"bucket"`
}
type Client struct{ Rules map[string]Rule }

func New(r map[string]Rule) *Client { return &Client{Rules: r} }
func Metadata(ip net.IP) bool {
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.Equal(net.ParseIP("168.63.129.16")) || ip.Equal(net.ParseIP("100.100.100.200")) || ip.Equal(net.ParseIP("fd00:ec2::254")) || inCIDR(ip, "192.0.0.0/24")
}
func Public(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !net.ParseIP("100.64.0.0").Equal(ip) && !inCIDR(ip, "100.64.0.0/10") && !inCIDR(ip, "198.18.0.0/15") && !inCIDR(ip, "192.0.0.0/24")
}
func inCIDR(ip net.IP, s string) bool { _, n, _ := net.ParseCIDR(s); return n.Contains(ip) }
func (c *Client) Get(ctx context.Context, id, raw string) (io.ReadCloser, error) {
	fail := func() (io.ReadCloser, error) { return nil, errors.New("source GET rejected") }
	rule, ok := c.Rules[id]
	if !ok {
		return fail()
	}
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, rule.Host) || rule.Prefix == "" || !strings.HasPrefix(u.Path, rule.Prefix) || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") || strings.ContainsAny(u.Path, "\\\x00") {
		return fail()
	}
	target := u.Host
	if rule.Dial != "" {
		target = rule.Dial
	} else if u.Port() == "" {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		target = net.JoinHostPort(u.Hostname(), port)
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(target)
		if e != nil {
			return nil, errors.New("invalid source route")
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("source resolution failed")
		}
		for _, ip := range ips {
			if Metadata(ip.IP) || rule.Dial == "" && !Public(ip.IP) {
				return nil, errors.New("source address denied")
			}
		}
		var d net.Dialer
		d.Timeout = 15 * time.Second
		// Connect the checked address directly: DNS is never resolved a second time.
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect denied") }}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return fail()
	}
	resp, e := client.Do(req)
	if e != nil {
		return fail()
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		resp.Body.Close()
		return nil, ErrAuthorization
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return fail()
	}
	return resp.Body, nil
}
func (c *Client) Authorize(ctx context.Context, p pack.Plan, maxManifest int64) error {
	rule, ok := c.Rules[p.Request.SourceID]
	if !ok || rule.Bucket != p.Request.Bucket {
		return errors.New("source scope denied")
	}
	r, e := c.Get(ctx, p.Request.SourceID, p.Request.ManifestURL)
	if e != nil {
		return e
	}
	b, e := io.ReadAll(io.LimitReader(r, maxManifest+1))
	r.Close()
	if e != nil || int64(len(b)) > maxManifest {
		return errors.New("manifest limit")
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != p.Request.ManifestSHA256 {
		return errors.New("manifest hash mismatch")
	}
	var identity struct {
		Game     string `json:"game"`
		Platform string `json:"platform"`
		Release  string `json:"release"`
	}
	if json.Unmarshal(b, &identity) != nil || identity.Game != p.Request.Game || identity.Platform != p.Request.Platform || identity.Release != p.Request.Release {
		return errors.New("manifest identity mismatch")
	}
	for _, o := range p.Objects {
		r, e = c.Get(ctx, p.Request.SourceID, o.URL)
		if e != nil {
			return e
		}
		n, e := io.Copy(io.Discard, io.LimitReader(r, min(o.Size, 4096)))
		r.Close()
		if e != nil || n != min(o.Size, 4096) {
			return errors.New("object authorization failed")
		}
	}
	return nil
}
