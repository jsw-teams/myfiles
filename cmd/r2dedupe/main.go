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
	apply := flag.Bool("apply", false, "delete older active files that share the same original_name")
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

	rows, err := db.Query(`WITH ranked AS (
		SELECT id, original_name, size, storage_file_id, created_at,
			ROW_NUMBER() OVER (PARTITION BY original_name ORDER BY created_at DESC, id DESC) AS rn,
			COUNT(*) OVER (PARTITION BY original_name) AS cnt
		FROM files
		WHERE storage_provider='r2' AND status='active'
	)
	SELECT id, original_name, size, storage_file_id, created_at
	FROM ranked
	WHERE cnt > 1 AND rn > 1
	ORDER BY original_name, created_at DESC`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	type candidate struct {
		id, name, key, createdAt string
		size                     int64
	}
	var candidates []candidate
	var total int64
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.name, &c.size, &c.key, &c.createdAt); err != nil {
			log.Fatal(err)
		}
		candidates = append(candidates, c)
		total += c.size
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	log.Printf("same-name older active candidates: %d objects, %d bytes", len(candidates), total)
	for _, c := range candidates {
		log.Printf("candidate: %s %d %s created=%s key=%s", c.id, c.size, c.name, c.createdAt, c.key)
	}
	if !*apply {
		log.Printf("dry-run only; rerun with -apply to delete these R2 objects and hard-delete DB rows")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	for _, c := range candidates {
		log.Printf("delete duplicate: %s %d %s", c.id, c.size, c.name)
		if err := storage.DeleteR2Object(ctx, cfg.Storage, c.key); err != nil {
			log.Fatalf("delete object %s: %v", c.key, err)
		}
		if err := myfiles.HardDelete(db, c.id); err != nil {
			log.Fatalf("delete database row %s: %v", c.id, err)
		}
	}
	log.Printf("dedupe complete: deleted %d older same-name objects, %d bytes", len(candidates), total)
}
