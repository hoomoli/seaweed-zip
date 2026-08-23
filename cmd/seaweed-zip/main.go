package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"seaweed-zip/internal/adminauth"
	"seaweed-zip/internal/config"
	"seaweed-zip/internal/httpapi"
	"seaweed-zip/internal/jobs"
	"seaweed-zip/internal/retention"
	"seaweed-zip/internal/source"
	"seaweed-zip/internal/store"
	"seaweed-zip/web"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		c := http.Client{Timeout: 3 * time.Second}
		r, e := c.Get("http://127.0.0.1:8080/readyz")
		if e != nil {
			os.Exit(1)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	cfg, e := config.Load()
	if e != nil {
		log.Fatal("invalid configuration")
	}
	var groups []string
	if value := os.Getenv("SEAWEED_ZIP_OIDC_ADMIN_GROUPS"); value != "" {
		for _, group := range strings.Split(value, ",") {
			groups = append(groups, strings.TrimSpace(group))
		}
	}
	auth, e := adminauth.New(adminauth.Settings{
		Issuer:       os.Getenv("SEAWEED_ZIP_OIDC_ISSUER"),
		ClientID:     os.Getenv("SEAWEED_ZIP_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("SEAWEED_ZIP_OIDC_CLIENT_SECRET"),
		PublicURL:    os.Getenv("SEAWEED_ZIP_PUBLIC_URL"),
		SessionKey:   os.Getenv("SEAWEED_ZIP_SESSION_KEY"), AdminGroups: groups,
	})
	if e != nil {
		log.Fatal("OIDC initialization failed: ", e)
	}
	if e = os.MkdirAll(cfg.DataDir, 0700); e != nil {
		log.Fatal("data directory unavailable")
	}
	info, e := os.Lstat(cfg.DataDir)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		log.Fatal("unsafe data directory")
	}
	if e = os.MkdirAll(filepath.Join(cfg.DataDir, "artifacts"), 0700); e != nil {
		log.Fatal("artifact directory unavailable")
	}
	s, e := store.Open(filepath.Join(cfg.DataDir, "state.sqlite"))
	if e != nil {
		log.Fatal("SQLite initialization failed")
	}
	defer s.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if s.Recover(ctx) != nil {
		log.Fatal("state recovery failed")
	}
	m := jobs.New(s, source.New(cfg.Sources), cfg)
	ui, e := fs.Sub(web.Files, "dist")
	if e != nil {
		log.Fatal("web assets unavailable")
	}
	server := &http.Server{Addr: cfg.Address, Handler: httpapi.NewWithAuth(m, ui, auth), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	finished := make(chan struct{})
	go func() { defer close(finished); m.Run(ctx) }()
	go retention.Cleaner{Store: s, Dir: cfg.DataDir}.Run(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Print("seaweed-zip listening; source URLs and tokens are never logged")
	if e = server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
		stop()
		log.Fatal("HTTP listener failed")
	}
	stop()
	<-finished
}
