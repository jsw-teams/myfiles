package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jsw-teams/myfiles/internal/config"
)

type UploadInput struct {
	TempPath string
	FileID   string
	Filename string
	MIME     string
	SHA256   string
	Size     int64
}

type UploadResult struct {
	Provider        string `json:"provider"`
	FileID          string `json:"fileId"`
	ThumbnailFileID string `json:"thumbnailFileId,omitempty"`
	URL             string `json:"url"`
	PublicURL       string `json:"publicUrl"`
}

type Uploader interface {
	Upload(ctx context.Context, in UploadInput) (UploadResult, error)
}

func NewUploader(cfg config.StorageConfig) Uploader {
	if cfg.Mode == "tgbots" {
		return &TGBotsUploader{cfg: cfg, client: NewTGBotsHTTPClient(cfg)}
	}
	return &LocalUploader{cfg: cfg}
}

func NewTGBotsHTTPClient(cfg config.StorageConfig) *http.Client {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err == nil && strings.EqualFold(host, "gateway.js.gripe") {
			return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		}
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

type LocalUploader struct{ cfg config.StorageConfig }

func (u *LocalUploader) Upload(ctx context.Context, in UploadInput) (UploadResult, error) {
	if err := os.MkdirAll(u.cfg.LocalDir, 0750); err != nil {
		return UploadResult{}, err
	}
	ext := filepath.Ext(in.Filename)
	name := in.FileID + ext
	dst := filepath.Join(u.cfg.LocalDir, name)

	src, err := os.Open(in.TempPath)
	if err != nil {
		return UploadResult{}, err
	}
	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		return UploadResult{}, err
	}
	buf := make([]byte, 1024*1024)
	if _, err := io.CopyBuffer(out, src, buf); err != nil {
		_ = out.Close()
		return UploadResult{}, err
	}
	if err := out.Close(); err != nil {
		return UploadResult{}, err
	}

	return UploadResult{Provider: "local", FileID: name, URL: dst, PublicURL: u.cfg.PublicBaseURL + publicFilePath(in.FileID, in.Filename)}, nil
}

type TGBotsUploader struct {
	cfg    config.StorageConfig
	client *http.Client
}

func (u *TGBotsUploader) Upload(ctx context.Context, in UploadInput) (UploadResult, error) {
	if !ValidBotToken(u.cfg.APIKey) {
		return UploadResult{}, fmt.Errorf("missing telegram bot token")
	}
	if strings.TrimSpace(u.cfg.ChatID) == "" {
		return UploadResult{}, fmt.Errorf("missing telegram chat_id")
	}

	media := telegramUploadMedia(in)
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		err := streamMultipartTelegramFile(writer, in, u.cfg.ChatID, media)
		closeErr := writer.Close()
		if err == nil {
			err = closeErr
		}
		_ = pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tgbotsMethodURL(u.cfg.UploadURL, u.cfg.APIKey, media.Method), pr)
	if err != nil {
		return UploadResult{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "myfiles/storage-tgbots")
	if u.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+u.cfg.APIKey)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return UploadResult{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		var payload struct {
			Description string `json:"description"`
		}
		_ = json.Unmarshal(raw, &payload)
		if payload.Description != "" {
			return UploadResult{}, fmt.Errorf("tgbots upload failed: HTTP %d: %s", resp.StatusCode, payload.Description)
		}
		return UploadResult{}, fmt.Errorf("tgbots upload failed: HTTP %d", resp.StatusCode)
	}

	var payload struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			Document *struct {
				FileID   string `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"document"`
			Video *struct {
				FileID    string `json:"file_id"`
				Thumbnail *struct {
					FileID string `json:"file_id"`
				} `json:"thumbnail"`
			} `json:"video"`
			Audio *struct {
				FileID string `json:"file_id"`
			} `json:"audio"`
			Voice *struct {
				FileID string `json:"file_id"`
			} `json:"voice"`
			Animation *struct {
				FileID    string `json:"file_id"`
				Thumbnail *struct {
					FileID string `json:"file_id"`
				} `json:"thumbnail"`
			} `json:"animation"`
			Photo []struct {
				FileID   string `json:"file_id"`
				FileSize int64  `json:"file_size"`
				Width    int    `json:"width"`
				Height   int    `json:"height"`
			} `json:"photo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return UploadResult{}, fmt.Errorf("tgbots response parse failed: %w", err)
	}
	if !payload.OK {
		if payload.Description == "" {
			payload.Description = "telegram sendDocument failed"
		}
		return UploadResult{}, errors.New(payload.Description)
	}
	fileID := ""
	thumbID := ""
	if payload.Result.Document != nil {
		fileID = payload.Result.Document.FileID
	}
	if payload.Result.Video != nil {
		fileID = payload.Result.Video.FileID
		if payload.Result.Video.Thumbnail != nil {
			thumbID = payload.Result.Video.Thumbnail.FileID
		}
	}
	if payload.Result.Audio != nil {
		fileID = payload.Result.Audio.FileID
	}
	if payload.Result.Voice != nil {
		fileID = payload.Result.Voice.FileID
	}
	if payload.Result.Animation != nil {
		fileID = payload.Result.Animation.FileID
		if payload.Result.Animation.Thumbnail != nil {
			thumbID = payload.Result.Animation.Thumbnail.FileID
		}
	}
	if len(payload.Result.Photo) > 0 {
		best := payload.Result.Photo[0]
		for _, p := range payload.Result.Photo[1:] {
			if p.FileSize > best.FileSize || (p.FileSize == best.FileSize && p.Width*p.Height > best.Width*best.Height) {
				best = p
			}
		}
		fileID = best.FileID
		thumbID = best.FileID
	}
	if fileID == "" {
		return UploadResult{}, fmt.Errorf("tgbots response missing file id")
	}
	return UploadResult{Provider: "tgbots", FileID: fileID, ThumbnailFileID: thumbID, URL: "", PublicURL: u.cfg.PublicBaseURL + publicFilePath(in.FileID, in.Filename)}, nil
}

type telegramMedia struct {
	Method string
	Field  string
}

func telegramUploadMedia(in UploadInput) telegramMedia {
	return telegramMedia{Method: "sendDocument", Field: "document"}
}

func streamMultipartTelegramFile(writer *multipart.Writer, in UploadInput, chatID string, media telegramMedia) error {
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if err := writer.WriteField("caption", fmt.Sprintf("myfiles:%s sha256:%s size:%d", in.FileID, in.SHA256, in.Size)); err != nil {
		return err
	}
	if media.Method == "sendVideo" {
		if err := writer.WriteField("supports_streaming", "true"); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile(media.Field, in.Filename)
	if err != nil {
		return err
	}
	f, err := os.Open(in.TempPath)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 256*1024)
	_, err = io.CopyBuffer(part, f, buf)
	return err
}

func tgbotsMethodURL(baseURL, botToken, method string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://gateway.js.gripe/api/v1/tgbots"
	}
	return base + "/bot" + strings.TrimSpace(botToken) + "/" + method
}

func ValidBotToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.HasPrefix(token, "[") && strings.HasSuffix(token, "]") {
		return false
	}
	if strings.Contains(token, "MYFILES_TGBOTS") {
		return false
	}
	return true
}

func FetchURL(baseURL, botToken, fileID string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/fetch") {
		base = strings.TrimSuffix(base, "/fetch")
	}
	if base == "" {
		base = "https://gateway.js.gripe/api/v1/tgbots"
	}
	q := url.Values{}
	q.Set("bot_token", strings.TrimSpace(botToken))
	q.Set("file_id", strings.TrimSpace(fileID))
	return base + "/fetch?" + q.Encode()
}

func publicFilePath(id, name string) string {
	ext := filepath.Ext(name)
	if ext == "" || len(ext) > 12 {
		return "/files/" + id
	}
	return "/files/" + id + ext
}
