package config

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"seaweed-zip/internal/source"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address, DataDir                                                   string
	Sources                                                            map[string]source.Rule
	Trusted, Admin                                                     []*net.IPNet
	MaxObjects                                                         int
	MaxBytes, MaxObjectBytes, MaxZIPBytes, MaxManifestBytes, DiskQuota int64
	TaskTimeout, DownloadTimeout                                       time.Duration
}

func Load() (Config, error) {
	c := Config{Address: env("SEAWEED_ZIP_ADDR", ":8080"), DataDir: env("SEAWEED_ZIP_DATA", "data"), Sources: map[string]source.Rule{}, MaxObjects: 10000, MaxBytes: 10 << 30, MaxObjectBytes: 2 << 30, MaxZIPBytes: 11 << 30, MaxManifestBytes: 16 << 20, DiskQuota: 50 << 30, TaskTimeout: time.Hour, DownloadTimeout: 2 * time.Hour}
	if s := os.Getenv("SEAWEED_ZIP_SOURCES"); s != "" {
		if e := json.Unmarshal([]byte(s), &c.Sources); e != nil {
			return c, errors.New("invalid source configuration")
		}
	}
	for _, r := range c.Sources {
		if r.Host == "" || r.Bucket == "" || !strings.HasPrefix(r.Prefix, "/") || !strings.HasSuffix(r.Prefix, "/") {
			return c, errors.New("invalid source scope")
		}
		if r.Dial != "" {
			if _, _, e := net.SplitHostPort(r.Dial); e != nil {
				return c, errors.New("invalid dial target")
			}
		}
	}
	var e error
	c.Trusted, e = cidrs(os.Getenv("SEAWEED_ZIP_TRUSTED_CIDRS"))
	if e != nil {
		return c, e
	}
	c.Admin, e = cidrs(os.Getenv("SEAWEED_ZIP_ADMIN_CIDRS"))
	if e != nil {
		return c, e
	}
	for key, dst := range map[string]*int64{"MAX_BYTES": &c.MaxBytes, "MAX_OBJECT_BYTES": &c.MaxObjectBytes, "MAX_ZIP_BYTES": &c.MaxZIPBytes, "MAX_MANIFEST_BYTES": &c.MaxManifestBytes, "DISK_QUOTA": &c.DiskQuota} {
		if s := os.Getenv("SEAWEED_ZIP_" + key); s != "" {
			n, e := strconv.ParseInt(s, 10, 64)
			if e != nil || n <= 0 || n > 1<<50 {
				return c, errors.New("invalid limit")
			}
			*dst = n
		}
	}
	if value := os.Getenv("SEAWEED_ZIP_MAX_OBJECTS"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > 100000 {
			return c, errors.New("invalid object count limit")
		}
		c.MaxObjects = n
	}
	for key, dst := range map[string]*time.Duration{"TASK_TIMEOUT_SECONDS": &c.TaskTimeout, "DOWNLOAD_TIMEOUT_SECONDS": &c.DownloadTimeout} {
		if value := os.Getenv("SEAWEED_ZIP_" + key); value != "" {
			n, e := strconv.ParseInt(value, 10, 64)
			if e != nil || n <= 0 || n > 86400 {
				return c, errors.New("invalid timeout")
			}
			*dst = time.Duration(n) * time.Second
		}
	}
	if c.DiskQuota < c.MaxZIPBytes {
		return c, errors.New("disk quota below artifact limit")
	}
	return c, nil
}
func env(k, d string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return d
}
func cidrs(s string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, v := range strings.Split(s, ",") {
		if strings.TrimSpace(v) == "" {
			continue
		}
		_, n, e := net.ParseCIDR(strings.TrimSpace(v))
		if e != nil {
			return nil, errors.New("invalid CIDR")
		}
		out = append(out, n)
	}
	return out, nil
}
func Allowed(addr string, nets []*net.IPNet) bool {
	host, _, e := net.SplitHostPort(addr)
	if e != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
