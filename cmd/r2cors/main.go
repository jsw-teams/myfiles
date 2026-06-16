package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/jsw-teams/myfiles/internal/config"
	"github.com/jsw-teams/myfiles/internal/storage"
)

func main() {
	configPath := flag.String("config", "/etc/myfiles/config.json", "myfiles config path")
	origin := flag.String("origin", "https://files.js.gripe", "allowed CORS origin")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := storage.PutR2BucketCORS(ctx, cfg.Storage, []string{*origin}); err != nil {
		log.Fatal(err)
	}
	log.Printf("configured R2 CORS for %s", *origin)
}
