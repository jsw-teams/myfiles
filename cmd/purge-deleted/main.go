package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jsw-teams/myfiles/internal/config"
	myfiles "github.com/jsw-teams/myfiles/internal/files"
	"github.com/jsw-teams/myfiles/internal/storage"
)

func main() {
	configPath := flag.String("config", "/etc/myfiles/config.json", "myfiles config path")
	dryRun := flag.Bool("dry-run", false, "print candidates without deleting")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite", cfg.Database.Path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, storage_file_id, size, original_name FROM files WHERE status='deleted' AND storage_provider='r2' ORDER BY size DESC`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	type item struct {
		id, key, name string
		size          int64
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.key, &it.size, &it.name); err != nil {
			log.Fatal(err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	var total int64
	for _, it := range items {
		total += it.size
	}
	log.Printf("deleted R2 candidates: %d objects, %d bytes", len(items), total)
	if *dryRun {
		for _, it := range items {
			log.Printf("dry-run: %s %d %s", it.key, it.size, it.name)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for _, it := range items {
		log.Printf("delete: %s %d %s", it.key, it.size, it.name)
		if err := storage.DeleteR2Object(ctx, cfg.Storage, it.key); err != nil {
			log.Fatalf("delete object %s: %v", it.key, err)
		}
		if err := myfiles.HardDelete(db, it.id); err != nil {
			log.Fatalf("delete database row %s: %v", it.id, err)
		}
	}
	log.Printf("purged %d deleted R2 objects", len(items))
}
