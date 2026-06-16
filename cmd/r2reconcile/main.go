package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jsw-teams/myfiles/internal/config"
	"github.com/jsw-teams/myfiles/internal/storage"
)

func main() {
	configPath := flag.String("config", "/etc/myfiles/config.json", "myfiles config path")
	apply := flag.Bool("apply", false, "delete orphan objects and abort multipart uploads")
	prefix := flag.String("prefix", "", "R2 key prefix to scan, defaults to storage.r2_prefix or all objects")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	scanPrefix := strings.TrimSpace(*prefix)
	if scanPrefix == "" {
		scanPrefix = strings.Trim(strings.TrimSpace(cfg.Storage.R2Prefix), "/")
	}

	db, err := sql.Open("sqlite", cfg.Database.Path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	keep, dbBytes, err := referencedR2Keys(db)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	objects, err := storage.ListR2Objects(ctx, cfg.Storage, scanPrefix)
	if err != nil {
		log.Fatal(err)
	}
	uploads, err := storage.ListR2MultipartUploads(ctx, cfg.Storage, scanPrefix)
	if err != nil {
		log.Fatal(err)
	}

	var r2Bytes, orphanBytes int64
	var orphanKeys []string
	objectSizes := map[string]int64{}
	for _, obj := range objects {
		key := strings.TrimSpace(obj.Key)
		objectSizes[key] = obj.Size
		r2Bytes += obj.Size
		if !keep[key] {
			orphanBytes += obj.Size
			orphanKeys = append(orphanKeys, key)
		}
	}
	var missingKeys []string
	for key := range keep {
		if _, ok := objectSizes[key]; !ok {
			missingKeys = append(missingKeys, key)
		}
	}
	sort.Slice(orphanKeys, func(i, j int) bool {
		if objectSizes[orphanKeys[i]] == objectSizes[orphanKeys[j]] {
			return orphanKeys[i] < orphanKeys[j]
		}
		return objectSizes[orphanKeys[i]] > objectSizes[orphanKeys[j]]
	})
	sort.Strings(missingKeys)

	log.Printf("db referenced R2 keys: %d, db bytes: %d", len(keep), dbBytes)
	log.Printf("r2 objects scanned: %d, r2 bytes: %d, prefix: %q", len(objects), r2Bytes, scanPrefix)
	log.Printf("orphan objects: %d, orphan bytes: %d", len(orphanKeys), orphanBytes)
	for _, key := range orphanKeys {
		log.Printf("orphan: %d %s", objectSizes[key], key)
	}
	log.Printf("missing referenced objects: %d", len(missingKeys))
	for _, key := range missingKeys {
		log.Printf("missing: %s", key)
	}
	log.Printf("unfinished multipart uploads: %d", len(uploads))
	for _, up := range uploads {
		log.Printf("multipart: %s %s initiated=%s", up.Key, up.UploadID, up.Initiated)
	}
	if !*apply {
		log.Printf("dry-run only; rerun with -apply to delete orphan objects and abort unfinished multipart uploads")
		return
	}

	for _, key := range orphanKeys {
		log.Printf("delete orphan: %d %s", objectSizes[key], key)
		if err := storage.DeleteR2Object(ctx, cfg.Storage, key); err != nil {
			log.Fatalf("delete orphan %s: %v", key, err)
		}
	}
	for _, up := range uploads {
		log.Printf("abort multipart: %s %s", up.Key, up.UploadID)
		if err := storage.AbortR2MultipartUpload(ctx, cfg.Storage, up.Key, up.UploadID); err != nil {
			log.Fatalf("abort multipart %s %s: %v", up.Key, up.UploadID, err)
		}
	}
	log.Printf("cleanup complete: deleted %d orphan objects, aborted %d multipart uploads", len(orphanKeys), len(uploads))
}

func referencedR2Keys(db *sql.DB) (map[string]bool, int64, error) {
	rows, err := db.Query(`SELECT storage_file_id, COALESCE(thumbnail_file_id, ''), size FROM files WHERE storage_provider='r2' AND status='active'`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	keep := map[string]bool{}
	var total int64
	for rows.Next() {
		var key, thumbnailKey string
		var size int64
		if err := rows.Scan(&key, &thumbnailKey, &size); err != nil {
			return nil, 0, err
		}
		key = strings.TrimSpace(key)
		thumbnailKey = strings.TrimSpace(thumbnailKey)
		if key != "" {
			keep[key] = true
			total += size
		}
		if thumbnailKey != "" {
			keep[thumbnailKey] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return keep, total, nil
}
