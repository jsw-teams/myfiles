package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jsw-teams/myfiles/internal/account"
	"github.com/jsw-teams/myfiles/internal/config"
	"github.com/jsw-teams/myfiles/internal/db"
	"github.com/jsw-teams/myfiles/internal/server"
	"github.com/jsw-teams/myfiles/internal/storage"
)

func main() {
	configPath := flag.String("config", "/etc/myfiles/config.json", "path to config json")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if err := os.MkdirAll(cfg.App.DataDir, 0750); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(cfg.App.TempDir, 0750); err != nil {
		log.Fatalf("create temp dir: %v", err)
	}

	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	accountClient := account.NewClient(cfg.Account)
	uploader := storage.NewUploader(cfg.Storage)
	app := server.New(cfg, database, accountClient, uploader)

	srv := &http.Server{
		Addr:              cfg.App.ListenAddr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("myfiles listening on %s", cfg.App.ListenAddr)
		errCh <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("received signal: %s", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
