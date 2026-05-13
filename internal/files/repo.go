package files

import (
	"database/sql"
	"strings"
	"time"

	"github.com/jsw-teams/myfiles/internal/ids"
)

type File struct {
	ID              string `json:"id"`
	BatchID         string `json:"batchId,omitempty"`
	OwnerUserID     string `json:"ownerUserId,omitempty"`
	OriginalName    string `json:"originalName"`
	StoredName      string `json:"storedName"`
	MIME            string `json:"mime"`
	Size            int64  `json:"size"`
	SHA256          string `json:"sha256"`
	ImageWidth      *int   `json:"imageWidth,omitempty"`
	ImageHeight     *int   `json:"imageHeight,omitempty"`
	StorageProvider string `json:"storageProvider"`
	StorageFileID   string `json:"storageFileId"`
	StorageURL      string `json:"storageUrl,omitempty"`
	PublicURL       string `json:"publicUrl"`
	IsPublic        bool   `json:"isPublic"`
	RequireConfirm  bool   `json:"requireConfirm"`
	RegionPolicy    string `json:"regionPolicy"`
	HotlinkPolicy   string `json:"hotlinkPolicy"`
	Status          string `json:"status"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type Batch struct {
	ID           string `json:"id"`
	OwnerUserID  string `json:"ownerUserId,omitempty"`
	Status       string `json:"status"`
	TotalFiles   int    `json:"totalFiles"`
	SuccessCount int    `json:"successCount"`
	FailedCount  int    `json:"failedCount"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type CreateFileInput struct {
	ID              string
	BatchID         string
	OwnerUserID     string
	OriginalName    string
	StoredName      string
	MIME            string
	Size            int64
	SHA256          string
	ImageWidth      *int
	ImageHeight     *int
	StorageProvider string
	StorageFileID   string
	StorageURL      string
	PublicURL       string
	IsPublic        bool
	RequireConfirm  bool
	RegionPolicy    string
	HotlinkPolicy   string
}

func CreateBatch(conn *sql.DB, ownerUserID string) (Batch, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	b := Batch{ID: ids.New("bat"), OwnerUserID: ownerUserID, Status: "created", CreatedAt: now, UpdatedAt: now}
	_, err := conn.Exec(`INSERT INTO upload_batches
		(id, owner_user_id, status, total_files, success_count, failed_count, created_at, updated_at)
		VALUES (?, ?, ?, 0, 0, 0, ?, ?)`, b.ID, nullEmpty(ownerUserID), b.Status, now, now)
	return b, err
}

func UpdateBatchCounts(conn *sql.DB, batchID string, total, success, failed int, status string) error {
	_, err := conn.Exec(`UPDATE upload_batches SET total_files=?, success_count=?, failed_count=?, status=?, updated_at=? WHERE id=?`,
		total, success, failed, status, time.Now().UTC().Format(time.RFC3339), batchID)
	return err
}

func CreateFile(conn *sql.DB, in CreateFileInput) (File, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if in.ID == "" {
		in.ID = ids.New("fil")
	}
	if in.RegionPolicy == "" {
		in.RegionPolicy = "global"
	}
	if in.HotlinkPolicy == "" {
		in.HotlinkPolicy = "allow"
	}
	isPublic := 0
	if in.IsPublic {
		isPublic = 1
	}
	requireConfirm := 0
	if in.RequireConfirm {
		requireConfirm = 1
	}
	var iw any
	var ih any
	if in.ImageWidth != nil {
		iw = *in.ImageWidth
	}
	if in.ImageHeight != nil {
		ih = *in.ImageHeight
	}
	_, err := conn.Exec(`INSERT INTO files
		(id, batch_id, owner_user_id, original_name, stored_name, mime, size, sha256, image_width, image_height,
		 storage_provider, storage_file_id, storage_url, public_url, is_public, require_confirm,
		 region_policy, hotlink_policy, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		in.ID, nullEmpty(in.BatchID), nullEmpty(in.OwnerUserID), in.OriginalName, in.StoredName, in.MIME, in.Size, in.SHA256, iw, ih,
		in.StorageProvider, in.StorageFileID, in.StorageURL, in.PublicURL, isPublic, requireConfirm, in.RegionPolicy, in.HotlinkPolicy, now, now)
	if err != nil {
		return File{}, err
	}
	return GetFile(conn, in.ID, false)
}

func GetBatch(conn *sql.DB, id string) (Batch, []File, error) {
	var b Batch
	var owner sql.NullString
	err := conn.QueryRow(`SELECT id, owner_user_id, status, total_files, success_count, failed_count, created_at, updated_at
		FROM upload_batches WHERE id=?`, id).Scan(&b.ID, &owner, &b.Status, &b.TotalFiles, &b.SuccessCount, &b.FailedCount, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return Batch{}, nil, err
	}
	b.OwnerUserID = owner.String
	list, err := ListFiles(conn, ListOptions{BatchID: id, IncludeDeleted: true, Limit: 200})
	return b, list, err
}

type ListOptions struct {
	OwnerUserID    string
	OwnerFilter    string
	Query          string
	BatchID        string
	All            bool
	IncludeDeleted bool
	Limit          int
	Offset         int
}

func ListFiles(conn *sql.DB, opt ListOptions) ([]File, error) {
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 100
	}
	where := []string{"1=1"}
	args := []any{}
	if !opt.IncludeDeleted {
		where = append(where, "status <> 'deleted'")
	}
	if opt.BatchID != "" {
		where = append(where, "batch_id = ?")
		args = append(args, opt.BatchID)
	}
	if !opt.All && opt.BatchID == "" {
		where = append(where, "owner_user_id = ?")
		args = append(args, opt.OwnerUserID)
	}
	if opt.OwnerFilter != "" {
		where = append(where, "owner_user_id LIKE ?")
		args = append(args, "%"+opt.OwnerFilter+"%")
	}
	if q := strings.TrimSpace(opt.Query); q != "" {
		where = append(where, "(original_name LIKE ? OR id LIKE ? OR sha256 LIKE ? OR mime LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	args = append(args, opt.Limit, opt.Offset)
	query := `SELECT id, COALESCE(batch_id,''), COALESCE(owner_user_id,''), original_name, stored_name, mime, size, sha256,
		image_width, image_height, storage_provider, storage_file_id, COALESCE(storage_url,''), public_url,
		is_public, require_confirm, region_policy, hotlink_policy, status, created_at, updated_at
		FROM files WHERE ` + strings.Join(where, " AND ") + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func GetFile(conn *sql.DB, id string, includeDeleted bool) (File, error) {
	where := "id=?"
	if !includeDeleted {
		where += " AND status <> 'deleted'"
	}
	row := conn.QueryRow(`SELECT id, COALESCE(batch_id,''), COALESCE(owner_user_id,''), original_name, stored_name, mime, size, sha256,
		image_width, image_height, storage_provider, storage_file_id, COALESCE(storage_url,''), public_url,
		is_public, require_confirm, region_policy, hotlink_policy, status, created_at, updated_at
		FROM files WHERE `+where, id)
	return scanFile(row)
}

func SoftDelete(conn *sql.DB, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := conn.Exec(`UPDATE files SET status='deleted', deleted_at=?, updated_at=? WHERE id=? AND status <> 'deleted'`, now, now, id)
	return err
}

func PatchAdmin(conn *sql.DB, id string, isPublic *bool, requireConfirm *bool, regionPolicy, hotlinkPolicy, status string) error {
	sets := []string{"updated_at=?"}
	args := []any{time.Now().UTC().Format(time.RFC3339)}
	if isPublic != nil {
		sets = append(sets, "is_public=?")
		if *isPublic {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if requireConfirm != nil {
		sets = append(sets, "require_confirm=?")
		if *requireConfirm {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if regionPolicy != "" {
		sets = append(sets, "region_policy=?")
		args = append(args, regionPolicy)
	}
	if hotlinkPolicy != "" {
		sets = append(sets, "hotlink_policy=?")
		args = append(args, hotlinkPolicy)
	}
	if status != "" {
		sets = append(sets, "status=?")
		args = append(args, status)
	}
	args = append(args, id)
	_, err := conn.Exec(`UPDATE files SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...)
	return err
}

type scanner interface{ Scan(dest ...any) error }

func scanFile(s scanner) (File, error) {
	var f File
	var iw, ih sql.NullInt64
	var isPublic, requireConfirm int
	if err := s.Scan(&f.ID, &f.BatchID, &f.OwnerUserID, &f.OriginalName, &f.StoredName, &f.MIME, &f.Size, &f.SHA256,
		&iw, &ih, &f.StorageProvider, &f.StorageFileID, &f.StorageURL, &f.PublicURL,
		&isPublic, &requireConfirm, &f.RegionPolicy, &f.HotlinkPolicy, &f.Status, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return File{}, err
	}
	if iw.Valid {
		v := int(iw.Int64)
		f.ImageWidth = &v
	}
	if ih.Valid {
		v := int(ih.Int64)
		f.ImageHeight = &v
	}
	f.IsPublic = isPublic == 1
	f.RequireConfirm = requireConfirm == 1
	return f, nil
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
