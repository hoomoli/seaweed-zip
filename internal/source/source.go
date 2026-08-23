package source

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"seaweed-zip/internal/pack"
	"strings"
	"sync"
	"time"
)

var ErrAuthorization = errors.New("source GET authorization rejected")

type Rule struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Prefix string `json:"prefix"`
	Dial   string `json:"dial,omitempty"`
	Bucket string `json:"bucket"`
}
type Client struct {
	Rules       map[string]Rule
	Concurrency int
}

func New(r map[string]Rule) *Client { return &Client{Rules: r, Concurrency: 4} }
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
	if e != nil || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Scheme != rule.Scheme) || !strings.EqualFold(u.Host, rule.Host) || rule.Prefix == "" || !strings.HasPrefix(u.Path, rule.Prefix) || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") || strings.ContainsAny(u.Path, "\\\x00") {
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
	if p.Request.ContractVersion != pack.ContractVersion {
		return errors.New("unsupported contract")
	}
	rule, ok := c.Rules[p.Request.SourceID]
	if !ok || rule.Bucket != p.Request.Bucket {
		return errors.New("source scope denied")
	}
	bound := func(raw, key string) bool {
		u, e := url.Parse(raw)
		return e == nil && u.Path == "/"+rule.Bucket+"/"+key
	}
	if !bound(p.Request.ManifestURL, p.Request.Game+"/manifests/"+p.Request.Platform+"/"+p.Request.Release+".json") {
		return errors.New("manifest locator mismatch")
	}
	for _, o := range p.Objects {
		if !bound(o.URL, p.Request.Game+"/blobs/sha256/"+o.SHA256[:2]+"/"+o.SHA256) {
			return errors.New("object locator mismatch")
		}
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
	if e := pack.VerifyManifest(b, p.Request); e != nil {
		return e
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	workers := min(max(c.Concurrency, 1), 16, len(p.Objects))
	queue := make(chan pack.Object)
	var wg sync.WaitGroup
	var once sync.Once
	var first error
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for o := range queue {
				r, e := c.Get(ctx, p.Request.SourceID, o.URL)
				if e == nil {
					n, copyErr := io.Copy(io.Discard, io.LimitReader(r, min(o.Size, 4096)))
					r.Close()
					if copyErr != nil || n != min(o.Size, 4096) {
						e = errors.New("object authorization failed")
					}
				}
				if e != nil {
					once.Do(func() { first = e; cancel() })
					return
				}
			}
		}()
	}
send:
	for _, o := range p.Objects {
		select {
		case queue <- o:
		case <-ctx.Done():
			break send
		}
	}
	close(queue)
	wg.Wait()
	if first != nil {
		return first
	}
	return ctx.Err()
}
