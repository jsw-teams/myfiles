package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type Uploader interface {
	Upload(ctx context.Context, in UploadInput) (UploadResult, error)
}

func NewUploader(cfg config.StorageConfig) Uploader {
	if cfg.Mode == "r2" {
		return NewR2Uploader(cfg)
	}
	if cfg.Mode == "tgbots" {
		return &TGBotsUploader{cfg: cfg, client: NewTGBotsHTTPClient(cfg)}
	}
	return &LocalUploader{cfg: cfg}
}

func NewR2Uploader(cfg config.StorageConfig) Uploader {
	return &R2Uploader{cfg: cfg, client: NewR2HTTPClient(cfg)}
}

func storageTimeout(cfg config.StorageConfig) time.Duration {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return timeout
}

func NewTGBotsHTTPClient(cfg config.StorageConfig) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err == nil && strings.EqualFold(host, "gateway.js.gripe") {
			return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		}
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{Timeout: storageTimeout(cfg), Transport: transport}
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

type R2Uploader struct {
	cfg    config.StorageConfig
	client *http.Client
}

type R2MultipartPart struct {
	PartNumber int
	ETag       string
}

type R2Object struct {
	Key          string
	Size         int64
	LastModified string
	ETag         string
}

type R2MultipartUpload struct {
	Key       string
	UploadID  string `xml:"UploadId"`
	Initiated string
}

func (u *R2Uploader) Upload(ctx context.Context, in UploadInput) (UploadResult, error) {
	if err := ValidateR2Config(u.cfg); err != nil {
		return UploadResult{}, err
	}
	key := R2ObjectKey(u.cfg, in.FileID, in.Filename)
	payloadHash := strings.TrimSpace(in.SHA256)
	if len(payloadHash) != 64 {
		payloadHash = "UNSIGNED-PAYLOAD"
	}
	if err := curlR2Upload(ctx, u.cfg, key, in.TempPath, in.MIME, in.FileID, payloadHash); err != nil {
		if !R2ObjectExists(ctx, u.cfg, key) {
			return UploadResult{}, err
		}
	}
	return UploadResult{Provider: "r2", FileID: key, URL: key, PublicURL: u.cfg.PublicBaseURL + publicFilePath(in.FileID, in.Filename)}, nil
}

func curlR2Upload(ctx context.Context, cfg config.StorageConfig, key, path, contentType, fileID, payloadHash string) error {
	timeout := int(storageTimeout(cfg).Seconds())
	if timeout <= 0 {
		timeout = 120
	}
	args := []string{
		"--fail", "--silent", "--show-error",
		"--max-time", fmt.Sprintf("%d", timeout),
		"--aws-sigv4", "aws:amz:auto:s3",
		"-u", cfg.R2AccessKeyID + ":" + cfg.R2SecretAccessKey,
		"-X", http.MethodPut,
		"--upload-file", path,
		"-H", "Content-Type: " + contentType,
		"-H", "x-amz-content-sha256: " + payloadHash,
		"-H", "x-amz-meta-myfiles-file-id: " + fileID,
		"-H", "x-amz-meta-myfiles-sha256: " + payloadHash,
		R2FetchURL(cfg, key),
	}
	out, err := exec.CommandContext(ctx, "curl", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("r2 curl upload failed: %w: %s", err, msg)
		}
		return fmt.Errorf("r2 curl upload failed: %w", err)
	}
	return nil
}

func UploadR2Object(ctx context.Context, cfg config.StorageConfig, key, path, contentType, fileID, payloadHash string) error {
	if err := ValidateR2Config(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("missing r2 object key")
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	if strings.TrimSpace(fileID) == "" {
		fileID = "myfiles"
	}
	payloadHash = strings.TrimSpace(payloadHash)
	if len(payloadHash) != 64 {
		payloadHash = "UNSIGNED-PAYLOAD"
	}
	return curlR2Upload(ctx, cfg, key, path, contentType, fileID, payloadHash)
}

func ValidateR2Config(cfg config.StorageConfig) error {
	if strings.TrimSpace(cfg.R2Endpoint) == "" {
		return errors.New("missing storage.r2_endpoint")
	}
	if strings.TrimSpace(cfg.R2Bucket) == "" {
		return errors.New("missing storage.r2_bucket")
	}
	if strings.TrimSpace(cfg.R2AccessKeyID) == "" {
		return errors.New("missing storage.r2_access_key_id")
	}
	if strings.TrimSpace(cfg.R2SecretAccessKey) == "" {
		return errors.New("missing storage.r2_secret_access_key")
	}
	return nil
}

func R2ObjectKey(cfg config.StorageConfig, fileID, filename string) string {
	rel := strings.TrimPrefix(publicFilePath(fileID, filename), "/")
	prefix := strings.Trim(strings.TrimSpace(cfg.R2Prefix), "/")
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
}

func R2FetchURL(cfg config.StorageConfig, key string) string {
	u := r2EndpointURL(cfg, strings.TrimLeft(key, "/"))
	return u.String()
}

func R2PublicURL(cfg config.StorageConfig, key string) string {
	base := strings.TrimRight(strings.TrimSpace(cfg.R2PublicBaseURL), "/")
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		u = &url.URL{Scheme: "https", Host: base}
	}
	if strings.TrimLeft(key, "/") != "" {
		basePath := strings.TrimRight(u.Path, "/")
		u.Path = basePath + "/" + pathEscapePath(strings.TrimLeft(key, "/"))
	}
	return u.String()
}

func NewR2FetchRequest(ctx context.Context, cfg config.StorageConfig, key string) (*http.Request, error) {
	req, err := r2Request(ctx, cfg, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	signR2Request(req, cfg, emptySHA256)
	return req, nil
}

func PresignR2PutURL(cfg config.StorageConfig, key string, expires time.Duration) (string, error) {
	return presignR2URL(cfg, http.MethodPut, key, nil, expires)
}

func PresignR2GetURL(cfg config.StorageConfig, key, contentType, contentDisposition string, expires time.Duration) (string, error) {
	return PresignR2ReadURL(cfg, http.MethodGet, key, contentType, contentDisposition, expires)
}

func PresignR2ReadURL(cfg config.StorageConfig, method, key, contentType, contentDisposition string, expires time.Duration) (string, error) {
	if method != http.MethodHead {
		method = http.MethodGet
	}
	query := url.Values{}
	if method == http.MethodGet {
		if strings.TrimSpace(contentType) != "" {
			query.Set("response-content-type", strings.TrimSpace(contentType))
		}
		if strings.TrimSpace(contentDisposition) != "" {
			query.Set("response-content-disposition", strings.TrimSpace(contentDisposition))
		}
	}
	return presignR2URL(cfg, method, key, query, expires)
}

func PresignR2UploadPartURL(cfg config.StorageConfig, key, uploadID string, partNumber int, expires time.Duration) (string, error) {
	query := url.Values{}
	query.Set("partNumber", strconv.Itoa(partNumber))
	query.Set("uploadId", uploadID)
	return presignR2URL(cfg, http.MethodPut, key, query, expires)
}

func CreateR2MultipartUpload(ctx context.Context, cfg config.StorageConfig, key, contentType string) (string, error) {
	if err := ValidateR2Config(cfg); err != nil {
		return "", err
	}
	query := url.Values{}
	query.Set("uploads", "")
	req, err := r2RequestWithQuery(ctx, cfg, http.MethodPost, key, query, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", strings.TrimSpace(contentType))
	signR2Request(req, cfg, emptySHA256)
	resp, err := NewR2HTTPClient(cfg).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("r2 multipart create failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		UploadID string `xml:"UploadId"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.UploadID) == "" {
		return "", fmt.Errorf("r2 multipart create response missing upload id")
	}
	return out.UploadID, nil
}

func CompleteR2MultipartUpload(ctx context.Context, cfg config.StorageConfig, key, uploadID string, parts []R2MultipartPart) error {
	if err := ValidateR2Config(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(uploadID) == "" {
		return fmt.Errorf("missing multipart upload id")
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	type partXML struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	}
	payload := struct {
		XMLName xml.Name  `xml:"CompleteMultipartUpload"`
		Parts   []partXML `xml:"Part"`
	}{}
	for _, part := range parts {
		etag := strings.TrimSpace(part.ETag)
		if part.PartNumber <= 0 || etag == "" {
			return fmt.Errorf("invalid multipart part")
		}
		payload.Parts = append(payload.Parts, partXML{PartNumber: part.PartNumber, ETag: etag})
	}
	if len(payload.Parts) == 0 {
		return fmt.Errorf("missing multipart parts")
	}
	raw, err := xml.Marshal(payload)
	if err != nil {
		return err
	}
	body := append([]byte(xml.Header), raw...)
	query := url.Values{}
	query.Set("uploadId", uploadID)
	req, err := r2RequestWithQuery(ctx, cfg, http.MethodPost, key, query, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")
	payloadHash := sha256BytesHex(body)
	signR2Request(req, cfg, payloadHash)
	resp, err := NewR2HTTPClient(cfg).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("r2 multipart complete failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func AbortR2MultipartUpload(ctx context.Context, cfg config.StorageConfig, key, uploadID string) error {
	if strings.TrimSpace(uploadID) == "" {
		return nil
	}
	query := url.Values{}
	query.Set("uploadId", uploadID)
	req, err := r2RequestWithQuery(ctx, cfg, http.MethodDelete, key, query, nil)
	if err != nil {
		return err
	}
	signR2Request(req, cfg, emptySHA256)
	resp, err := NewR2HTTPClient(cfg).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("r2 multipart abort failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func ListR2Objects(ctx context.Context, cfg config.StorageConfig, prefix string) ([]R2Object, error) {
	if err := ValidateR2Config(cfg); err != nil {
		return nil, err
	}
	var objects []R2Object
	token := ""
	for {
		query := url.Values{}
		query.Set("list-type", "2")
		if strings.TrimSpace(prefix) != "" {
			query.Set("prefix", strings.TrimSpace(prefix))
		}
		if token != "" {
			query.Set("continuation-token", token)
		}
		req, err := r2RequestWithQuery(ctx, cfg, http.MethodGet, "", query, nil)
		if err != nil {
			return nil, err
		}
		signR2Request(req, cfg, emptySHA256)
		resp, err := NewR2HTTPClient(cfg).Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("r2 list objects failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var out struct {
			Contents              []R2Object `xml:"Contents"`
			IsTruncated           bool       `xml:"IsTruncated"`
			NextContinuationToken string     `xml:"NextContinuationToken"`
		}
		err = xml.NewDecoder(resp.Body).Decode(&out)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		objects = append(objects, out.Contents...)
		if !out.IsTruncated || strings.TrimSpace(out.NextContinuationToken) == "" {
			break
		}
		token = out.NextContinuationToken
	}
	return objects, nil
}

func ListR2MultipartUploads(ctx context.Context, cfg config.StorageConfig, prefix string) ([]R2MultipartUpload, error) {
	if err := ValidateR2Config(cfg); err != nil {
		return nil, err
	}
	var uploads []R2MultipartUpload
	keyMarker := ""
	uploadIDMarker := ""
	for {
		query := url.Values{}
		query.Set("uploads", "")
		if strings.TrimSpace(prefix) != "" {
			query.Set("prefix", strings.TrimSpace(prefix))
		}
		if keyMarker != "" {
			query.Set("key-marker", keyMarker)
		}
		if uploadIDMarker != "" {
			query.Set("upload-id-marker", uploadIDMarker)
		}
		req, err := r2RequestWithQuery(ctx, cfg, http.MethodGet, "", query, nil)
		if err != nil {
			return nil, err
		}
		signR2Request(req, cfg, emptySHA256)
		resp, err := NewR2HTTPClient(cfg).Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("r2 list multipart uploads failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var out struct {
			Uploads            []R2MultipartUpload `xml:"Upload"`
			IsTruncated        bool                `xml:"IsTruncated"`
			NextKeyMarker      string              `xml:"NextKeyMarker"`
			NextUploadIDMarker string              `xml:"NextUploadIdMarker"`
		}
		err = xml.NewDecoder(resp.Body).Decode(&out)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		uploads = append(uploads, out.Uploads...)
		if !out.IsTruncated || strings.TrimSpace(out.NextKeyMarker) == "" {
			break
		}
		keyMarker = out.NextKeyMarker
		uploadIDMarker = out.NextUploadIDMarker
	}
	return uploads, nil
}

func PutR2BucketCORS(ctx context.Context, cfg config.StorageConfig, origins []string) error {
	if err := ValidateR2Config(cfg); err != nil {
		return err
	}
	if len(origins) == 0 {
		return fmt.Errorf("missing CORS origins")
	}
	type corsRule struct {
		AllowedOrigins []string `xml:"AllowedOrigin"`
		AllowedMethods []string `xml:"AllowedMethod"`
		AllowedHeaders []string `xml:"AllowedHeader"`
		ExposeHeaders  []string `xml:"ExposeHeader"`
		MaxAgeSeconds  int      `xml:"MaxAgeSeconds"`
	}
	payload := struct {
		XMLName xml.Name `xml:"CORSConfiguration"`
		XMLNS   string   `xml:"xmlns,attr,omitempty"`
		Rule    corsRule `xml:"CORSRule"`
	}{
		XMLNS: "http://s3.amazonaws.com/doc/2006-03-01/",
		Rule: corsRule{
			AllowedOrigins: origins,
			AllowedMethods: []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPost, http.MethodDelete},
			AllowedHeaders: []string{"*"},
			ExposeHeaders:  []string{"ETag", "Accept-Ranges", "Content-Range", "Content-Length", "Content-Type"},
			MaxAgeSeconds:  3600,
		},
	}
	raw, err := xml.Marshal(payload)
	if err != nil {
		return err
	}
	body := append([]byte(xml.Header), raw...)
	query := url.Values{}
	query.Set("cors", "")
	req, err := r2RequestWithQuery(ctx, cfg, http.MethodPut, "", query, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")
	signR2Request(req, cfg, sha256BytesHex(body))
	resp, err := NewR2HTTPClient(cfg).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("r2 put bucket cors failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func NewR2HTTPClient(cfg config.StorageConfig) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.IdleConnTimeout = 30 * time.Second
	return &http.Client{Timeout: storageTimeout(cfg), Transport: transport}
}

func R2ObjectExists(ctx context.Context, cfg config.StorageConfig, key string) bool {
	args := []string{
		"--fail", "--silent", "--show-error", "--head",
		"--max-time", "20",
		"--aws-sigv4", "aws:amz:auto:s3",
		"-u", cfg.R2AccessKeyID + ":" + cfg.R2SecretAccessKey,
		"-H", "x-amz-content-sha256: " + emptySHA256,
		R2FetchURL(cfg, key),
	}
	return exec.CommandContext(ctx, "curl", args...).Run() == nil
}

func DeleteR2Object(ctx context.Context, cfg config.StorageConfig, key string) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	if err := ValidateR2Config(cfg); err != nil {
		return err
	}
	timeout := int(storageTimeout(cfg).Seconds())
	if timeout <= 0 {
		timeout = 120
	}
	args := []string{
		"--silent", "--show-error",
		"--output", os.DevNull,
		"--write-out", "%{http_code}",
		"--max-time", fmt.Sprintf("%d", timeout),
		"--aws-sigv4", "aws:amz:auto:s3",
		"-u", cfg.R2AccessKeyID + ":" + cfg.R2SecretAccessKey,
		"-X", http.MethodDelete,
		"-H", "x-amz-content-sha256: " + emptySHA256,
		R2FetchURL(cfg, key),
	}
	out, err := exec.CommandContext(ctx, "curl", args...).CombinedOutput()
	code := strings.TrimSpace(string(out))
	if err != nil {
		if code == "404" {
			return nil
		}
		if code != "" {
			return fmt.Errorf("r2 delete failed: HTTP %s: %w", code, err)
		}
		return fmt.Errorf("r2 delete failed: %w", err)
	}
	switch code {
	case "200", "202", "204", "404":
		return nil
	default:
		return fmt.Errorf("r2 delete failed: HTTP %s", code)
	}
}

func r2Request(ctx context.Context, cfg config.StorageConfig, method, key string, body io.Reader) (*http.Request, error) {
	return r2RequestWithQuery(ctx, cfg, method, key, nil, body)
}

func r2RequestWithQuery(ctx context.Context, cfg config.StorageConfig, method, key string, query url.Values, body io.Reader) (*http.Request, error) {
	u := r2EndpointURL(cfg, strings.TrimLeft(key, "/"))
	if query != nil {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Host", u.Host)
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	return req, nil
}

func presignR2URL(cfg config.StorageConfig, method, key string, query url.Values, expires time.Duration) (string, error) {
	if err := ValidateR2Config(cfg); err != nil {
		return "", err
	}
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	if expires > 7*24*time.Hour {
		expires = 7 * 24 * time.Hour
	}
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	region := strings.TrimSpace(cfg.R2Region)
	if region == "" {
		region = "auto"
	}
	scope := dateStamp + "/" + region + "/s3/aws4_request"
	u := r2EndpointURL(cfg, strings.TrimLeft(key, "/"))
	q := u.Query()
	for k, vals := range query {
		for _, v := range vals {
			q.Add(k, v)
		}
	}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", cfg.R2AccessKeyID+"/"+scope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", strconv.Itoa(int(expires.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")
	q.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	u.RawQuery = q.Encode()
	canonicalRequest := strings.Join([]string{
		method,
		u.EscapedPath(),
		canonicalQuery(u.Query()),
		"host:" + u.Host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(signingKey(cfg.R2SecretAccessKey, dateStamp, region, "s3"), stringToSign))
	q.Set("X-Amz-Signature", signature)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func r2EndpointURL(cfg config.StorageConfig, key string) *url.URL {
	base := strings.TrimRight(strings.TrimSpace(cfg.R2Endpoint), "/")
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		u = &url.URL{Scheme: "https", Host: base}
	}
	basePath := strings.Trim(u.Path, "/")
	if basePath == "" {
		bucket := strings.Trim(strings.TrimSpace(cfg.R2Bucket), "/")
		u.Path = "/" + pathEscape(bucket)
	} else {
		u.Path = "/" + pathEscapePath(basePath)
	}
	if key != "" {
		u.Path += "/" + pathEscapePath(key)
	}
	return u
}

func pathEscapePath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = pathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func pathEscape(value string) string {
	return strings.ReplaceAll(url.PathEscape(value), "+", "%20")
}

func signR2Request(req *http.Request, cfg config.StorageConfig, payloadHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := strings.TrimSpace(cfg.R2Region)
	if region == "" {
		region = "auto"
	}
	service := "s3"
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	signedHeaders, canonicalHeaders := canonicalR2Headers(req.Header, req.URL.Host)
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		canonicalQuery(req.URL.Query()),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(signingKey(cfg.R2SecretAccessKey, dateStamp, region, service), stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+cfg.R2AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func canonicalR2Headers(header http.Header, host string) (string, string) {
	keys := make([]string, 0, len(header)+1)
	values := map[string]string{"host": host}
	for key, vals := range header {
		lower := strings.ToLower(key)
		if lower == "authorization" {
			continue
		}
		values[lower] = strings.Join(vals, ",")
		keys = append(keys, lower)
	}
	keys = append(keys, "host")
	sort.Strings(keys)
	dedup := keys[:0]
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		dedup = append(dedup, key)
	}
	var b strings.Builder
	for _, key := range dedup {
		b.WriteString(key)
		b.WriteString(":")
		b.WriteString(canonicalHeaderValue(values[key]))
		b.WriteString("\n")
	}
	return strings.Join(dedup, ";"), b.String()
}

func canonicalHeaderValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func canonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		vals := append([]string(nil), values[key]...)
		sort.Strings(vals)
		for _, value := range vals {
			parts = append(parts, awsQueryEscape(key)+"="+awsQueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func awsQueryEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func signingKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, value string) []byte {
	h := hmac.New(func() hash.Hash { return sha256.New() }, key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sha256BytesHex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
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
