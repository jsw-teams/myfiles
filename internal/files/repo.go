package files

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
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
	ThumbnailFileID string `json:"thumbnailFileId,omitempty"`
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
	ID              string `json:"id"`
	OwnerUserID     string `json:"ownerUserId,omitempty"`
	PickupCode      string `json:"pickupCode,omitempty"`
	PickupExpiresAt string `json:"pickupExpiresAt,omitempty"`
	Status          string `json:"status"`
	TotalFiles      int    `json:"totalFiles"`
	SuccessCount    int    `json:"successCount"`
	FailedCount     int    `json:"failedCount"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type PickupShare struct {
	ID              string `json:"id"`
	OwnerUserID     string `json:"ownerUserId,omitempty"`
	PickupCode      string `json:"pickupCode"`
	PickupExpiresAt string `json:"pickupExpiresAt"`
	RevokedAt       string `json:"revokedAt,omitempty"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
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
	ThumbnailFileID string
	StorageURL      string
	PublicURL       string
	IsPublic        bool
	RequireConfirm  bool
	RegionPolicy    string
	HotlinkPolicy   string
}

func CreateBatch(conn *sql.DB, ownerUserID string) (Batch, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	expiresAt := ""
	if strings.TrimSpace(ownerUserID) == "" {
		expiresAt = time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	}
	var lastErr error
	for range 5 {
		pickupCode := ""
		if strings.TrimSpace(ownerUserID) == "" {
			pickupCode = NewPickupCode()
		}
		b := Batch{ID: ids.New("bat"), OwnerUserID: ownerUserID, PickupCode: pickupCode, PickupExpiresAt: expiresAt, Status: "created", CreatedAt: now, UpdatedAt: now}
		_, err := conn.Exec(`INSERT INTO upload_batches
			(id, owner_user_id, pickup_code, pickup_expires_at, status, total_files, success_count, failed_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 0, 0, 0, ?, ?)`, b.ID, nullEmpty(ownerUserID), nullEmpty(b.PickupCode), nullEmpty(b.PickupExpiresAt), b.Status, now, now)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	return Batch{}, lastErr
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
		 storage_provider, storage_file_id, thumbnail_file_id, storage_url, public_url, is_public, require_confirm,
		 region_policy, hotlink_policy, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
		in.ID, nullEmpty(in.BatchID), nullEmpty(in.OwnerUserID), in.OriginalName, in.StoredName, in.MIME, in.Size, in.SHA256, iw, ih,
		in.StorageProvider, in.StorageFileID, in.ThumbnailFileID, in.StorageURL, in.PublicURL, isPublic, requireConfirm, in.RegionPolicy, in.HotlinkPolicy, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "thumbnail_file_id") {
			log.Printf("files insert with thumbnail_file_id failed, retrying without thumbnail_file_id: %v", err)
			_, err = conn.Exec(`INSERT INTO files
				(id, batch_id, owner_user_id, original_name, stored_name, mime, size, sha256, image_width, image_height,
				 storage_provider, storage_file_id, storage_url, public_url, is_public, require_confirm,
				 region_policy, hotlink_policy, status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
				in.ID, nullEmpty(in.BatchID), nullEmpty(in.OwnerUserID), in.OriginalName, in.StoredName, in.MIME, in.Size, in.SHA256, iw, ih,
				in.StorageProvider, in.StorageFileID, in.StorageURL, in.PublicURL, isPublic, requireConfirm, in.RegionPolicy, in.HotlinkPolicy, now, now)
		}
		if err != nil {
			log.Printf("files insert failed: id=%s name=%q mime=%q provider=%s storageFileID=%q thumbnailFileID=%q err=%v", in.ID, in.OriginalName, in.MIME, in.StorageProvider, in.StorageFileID, in.ThumbnailFileID, err)
			return File{}, err
		}
	}
	return GetFile(conn, in.ID, false)
}

func GetBatch(conn *sql.DB, id string) (Batch, []File, error) {
	var b Batch
	var owner sql.NullString
	var pickupCode, pickupExpiresAt sql.NullString
	err := conn.QueryRow(`SELECT id, owner_user_id, pickup_code, pickup_expires_at, status, total_files, success_count, failed_count, created_at, updated_at
		FROM upload_batches WHERE id=?`, id).Scan(&b.ID, &owner, &pickupCode, &pickupExpiresAt, &b.Status, &b.TotalFiles, &b.SuccessCount, &b.FailedCount, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return Batch{}, nil, err
	}
	b.OwnerUserID = owner.String
	b.PickupCode = pickupCode.String
	b.PickupExpiresAt = pickupExpiresAt.String
	list, err := ListFiles(conn, ListOptions{BatchID: id, IncludeDeleted: true, Limit: 200})
	return b, list, err
}

func GetBatchByPickupCode(conn *sql.DB, code string) (Batch, []File, error) {
	code = NormalizePickupCode(code)
	if code == "" {
		return Batch{}, nil, sql.ErrNoRows
	}
	var b Batch
	var owner sql.NullString
	var pickupExpiresAt sql.NullString
	err := conn.QueryRow(`SELECT id, owner_user_id, pickup_code, pickup_expires_at, status, total_files, success_count, failed_count, created_at, updated_at
		FROM upload_batches WHERE pickup_code=?`, code).Scan(&b.ID, &owner, &b.PickupCode, &pickupExpiresAt, &b.Status, &b.TotalFiles, &b.SuccessCount, &b.FailedCount, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return Batch{}, nil, err
	}
	if pickupExpiresAt.String == "" {
		return Batch{}, nil, sql.ErrNoRows
	}
	expiresAt, err := time.Parse(time.RFC3339, pickupExpiresAt.String)
	if err != nil || time.Now().UTC().After(expiresAt) {
		return Batch{}, nil, sql.ErrNoRows
	}
	b.OwnerUserID = owner.String
	b.PickupExpiresAt = pickupExpiresAt.String
	list, err := ListFiles(conn, ListOptions{BatchID: b.ID, IncludeDeleted: false, Limit: 200})
	return b, list, err
}

func CreatePickupShare(conn *sql.DB, ownerUserID string, fileIDs []string) (PickupShare, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	var lastErr error
	for range 5 {
		share := PickupShare{ID: ids.New("shr"), OwnerUserID: ownerUserID, PickupCode: NewPickupCode(), PickupExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now}
		tx, err := conn.Begin()
		if err != nil {
			return PickupShare{}, err
		}
		_, err = tx.Exec(`INSERT INTO pickup_shares
			(id, owner_user_id, pickup_code, expires_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, share.ID, nullEmpty(ownerUserID), share.PickupCode, share.PickupExpiresAt, now, now)
		if err == nil {
			for _, fileID := range fileIDs {
				_, err = tx.Exec(`INSERT INTO pickup_share_files (share_id, file_id, created_at) VALUES (?, ?, ?)`, share.ID, fileID, now)
				if err != nil {
					break
				}
			}
		}
		if err == nil {
			if err = tx.Commit(); err == nil {
				return share, nil
			}
		}
		_ = tx.Rollback()
		lastErr = err
	}
	return PickupShare{}, lastErr
}

func ListPickupSharesForFile(conn *sql.DB, fileID string) ([]PickupShare, error) {
	rows, err := conn.Query(`SELECT s.id, COALESCE(s.owner_user_id,''), s.pickup_code, s.expires_at, COALESCE(s.revoked_at,''), s.created_at, s.updated_at
		FROM pickup_shares s
		JOIN pickup_share_files sf ON sf.share_id = s.id
		WHERE sf.file_id = ?
		ORDER BY s.created_at DESC
		LIMIT 20`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PickupShare
	for rows.Next() {
		share, err := scanPickupShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, share)
	}
	return out, rows.Err()
}

func GetShareByPickupCode(conn *sql.DB, code string) (PickupShare, []File, error) {
	code = NormalizePickupCode(code)
	if code == "" {
		return PickupShare{}, nil, sql.ErrNoRows
	}
	row := conn.QueryRow(`SELECT id, COALESCE(owner_user_id,''), pickup_code, expires_at, COALESCE(revoked_at,''), created_at, updated_at
		FROM pickup_shares WHERE pickup_code=?`, code)
	share, err := scanPickupShare(row)
	if err != nil {
		return PickupShare{}, nil, err
	}
	if share.RevokedAt != "" {
		return PickupShare{}, nil, sql.ErrNoRows
	}
	expiresAt, err := time.Parse(time.RFC3339, share.PickupExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		return PickupShare{}, nil, sql.ErrNoRows
	}
	rows, err := conn.Query(`SELECT f.id, COALESCE(f.batch_id,''), COALESCE(f.owner_user_id,''), f.original_name, f.stored_name, f.mime, f.size, f.sha256,
		f.image_width, f.image_height, f.storage_provider, f.storage_file_id, COALESCE(f.thumbnail_file_id,''), COALESCE(f.storage_url,''), f.public_url,
		f.is_public, f.require_confirm, f.region_policy, f.hotlink_policy, f.status, f.created_at, f.updated_at
		FROM files f
		JOIN pickup_share_files sf ON sf.file_id = f.id
		WHERE sf.share_id=? AND f.status <> 'deleted'
		ORDER BY sf.created_at ASC`, share.ID)
	if err != nil {
		return PickupShare{}, nil, err
	}
	defer rows.Close()
	var list []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return PickupShare{}, nil, err
		}
		list = append(list, f)
	}
	return share, list, rows.Err()
}

func RevokePickupShare(conn *sql.DB, code, ownerUserID string, allowAll bool) (PickupShare, error) {
	code = NormalizePickupCode(code)
	share, _, err := GetShareByPickupCode(conn, code)
	if err != nil {
		return PickupShare{}, err
	}
	if !allowAll && share.OwnerUserID != ownerUserID {
		return PickupShare{}, sql.ErrNoRows
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = conn.Exec(`UPDATE pickup_shares SET revoked_at=?, updated_at=? WHERE id=? AND revoked_at IS NULL`, now, now, share.ID)
	if err != nil {
		return PickupShare{}, err
	}
	share.RevokedAt = now
	share.UpdatedAt = now
	return share, nil
}

func NormalizePickupCode(code string) string {
	return strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(code)), ""))
}

func NewPickupCode() string {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("pickup code entropy failed: %w", err))
	}
	for i, v := range b {
		b[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(b)
}

func ShareActive(share PickupShare) bool {
	if share.RevokedAt != "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, share.PickupExpiresAt)
	return err == nil && time.Now().UTC().Before(expiresAt)
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
		image_width, image_height, storage_provider, storage_file_id, COALESCE(thumbnail_file_id,''), COALESCE(storage_url,''), public_url,
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
		image_width, image_height, storage_provider, storage_file_id, COALESCE(thumbnail_file_id,''), COALESCE(storage_url,''), public_url,
		is_public, require_confirm, region_policy, hotlink_policy, status, created_at, updated_at
		FROM files WHERE `+where, id)
	return scanFile(row)
}

func HardDelete(conn *sql.DB, id string) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM pickup_share_files WHERE file_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM file_events WHERE file_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
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

func SetThumbnailFileID(conn *sql.DB, id, thumbnailFileID string) error {
	_, err := conn.Exec(`UPDATE files SET thumbnail_file_id=?, updated_at=? WHERE id=?`,
		nullEmpty(thumbnailFileID), time.Now().UTC().Format(time.RFC3339), id)
	return err
}

type scanner interface{ Scan(dest ...any) error }

func scanFile(s scanner) (File, error) {
	var f File
	var iw, ih sql.NullInt64
	var isPublic, requireConfirm int
	if err := s.Scan(&f.ID, &f.BatchID, &f.OwnerUserID, &f.OriginalName, &f.StoredName, &f.MIME, &f.Size, &f.SHA256,
		&iw, &ih, &f.StorageProvider, &f.StorageFileID, &f.ThumbnailFileID, &f.StorageURL, &f.PublicURL,
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

func scanPickupShare(s scanner) (PickupShare, error) {
	var share PickupShare
	if err := s.Scan(&share.ID, &share.OwnerUserID, &share.PickupCode, &share.PickupExpiresAt, &share.RevokedAt, &share.CreatedAt, &share.UpdatedAt); err != nil {
		return PickupShare{}, err
	}
	return share, nil
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
