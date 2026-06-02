package storage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jsw-teams/myfiles/internal/config"
)

func TestTGBotsUploaderUsesDocumentForAllMedia(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "upload-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.WriteString("test file content"); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendDocument") {
			t.Errorf("expected sendDocument, got %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			return jsonResponse(http.StatusBadRequest, `{"ok":false,"description":"bad multipart"}`), nil
		}
		if _, _, err := r.FormFile("document"); err != nil {
			t.Errorf("expected document file field: %v", err)
		}
		for _, field := range []string{"photo", "video", "audio", "voice", "animation"} {
			if _, _, err := r.FormFile(field); err == nil {
				t.Errorf("unexpected Telegram media field %q", field)
			}
		}
		return jsonResponse(http.StatusOK, `{"ok":true,"result":{"document":{"file_id":"doc_file_id"}}}`), nil
	})}

	uploader := &TGBotsUploader{
		cfg: config.StorageConfig{
			UploadURL:     "https://tgbots.example/api",
			APIKey:        "123456:test-token",
			ChatID:        "42",
			PublicBaseURL: "https://files.example",
		},
		client: client,
	}

	for _, tc := range []struct {
		name string
		mime string
	}{
		{name: "audio", mime: "audio/mpeg"},
		{name: "image", mime: "image/png"},
		{name: "video", mime: "video/mp4"},
		{name: "generic", mime: "application/octet-stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := uploader.Upload(context.Background(), UploadInput{
				TempPath: tmp.Name(),
				FileID:   "fil_test",
				Filename: tc.name + ".bin",
				MIME:     tc.mime,
				SHA256:   "abc123",
				Size:     17,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.FileID != "doc_file_id" {
				t.Fatalf("FileID = %q, want doc_file_id", result.FileID)
			}
		})
	}

}

func TestTGBotsUploaderAcceptsAudioResponseFileID(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "upload-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.WriteString("audio content"); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body strings.Builder
		_ = json.NewEncoder(&body).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"audio": map[string]any{"file_id": "audio_file_id"},
			},
		})
		return jsonResponse(http.StatusOK, body.String()), nil
	})}

	uploader := &TGBotsUploader{
		cfg: config.StorageConfig{
			UploadURL:     "https://tgbots.example/api",
			APIKey:        "123456:test-token",
			ChatID:        "42",
			PublicBaseURL: "https://files.example",
		},
		client: client,
	}
	result, err := uploader.Upload(context.Background(), UploadInput{
		TempPath: tmp.Name(),
		FileID:   "fil_audio",
		Filename: "sample.mp3",
		MIME:     "audio/mpeg",
		SHA256:   "abc123",
		Size:     13,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileID != "audio_file_id" {
		t.Fatalf("FileID = %q, want audio_file_id", result.FileID)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
