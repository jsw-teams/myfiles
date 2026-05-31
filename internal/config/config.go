package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type Config struct {
	SourcePath string         `json:"-"`
	App        AppConfig      `json:"app"`
	Database   DatabaseConfig `json:"database"`
	Account    AccountConfig  `json:"account"`
	Storage    StorageConfig  `json:"storage"`
	Upload     UploadConfig   `json:"upload"`
	File       FileConfig     `json:"file"`
	Security   SecurityConfig `json:"security"`
	Audit      AuditConfig    `json:"audit"`
}

type AppConfig struct {
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	ListenAddr string `json:"listen_addr"`
	PublicDir  string `json:"public_dir"`
	DataDir    string `json:"data_dir"`
	TempDir    string `json:"temp_dir"`
}

type DatabaseConfig struct {
	Path string `json:"path"`
}

type AccountConfig struct {
	ClientName     string   `json:"client_name"`
	ClientID       string   `json:"client_id"`
	ClientSecret   string   `json:"client_secret"`
	LoginURL       string   `json:"login_url"`
	AccountBaseURL string   `json:"account_base_url"`
	MeURL          string   `json:"me_url"`
	RedirectURI    string   `json:"redirect_uri"`
	Scopes         []string `json:"scopes"`
}

type StorageConfig struct {
	Mode           string `json:"mode"`
	UploadURL      string `json:"upload_url"`
	PublicBaseURL  string `json:"public_base_url"`
	APIKey         string `json:"api_key"`
	ChatID         string `json:"chat_id"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	LocalDir       string `json:"local_dir"`
}

type UploadConfig struct {
	MaxBytes         int64    `json:"max_bytes"`
	AllowedMIMETypes []string `json:"allowed_mime_types"`
	AllowAnonymous   bool     `json:"allow_anonymous"`
}

type FileConfig struct {
	DefaultPublic         bool   `json:"default_public"`
	DefaultRequireConfirm bool   `json:"default_require_confirm"`
	DefaultRegionPolicy   string `json:"default_region_policy"`
	DefaultHotlinkPolicy  string `json:"default_hotlink_policy"`
}

type SecurityConfig struct {
	SessionCookieName string `json:"session_cookie_name"`
	SessionTTLHours   int    `json:"session_ttl_hours"`
	CookieSecure      bool   `json:"cookie_secure"`
}

type AuditConfig struct {
	RetentionDays int `json:"retention_days"`
}

const DefaultUploadMaxBytes int64 = 2 * 1024 * 1024 * 1024

func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("config file not found: %s", path)
		}
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	cfg.SourcePath = path
	normalize(&cfg)
	return cfg, validate(cfg)
}

func Save(path string, cfg Config) error {
	cfg.SourcePath = ""
	normalize(&cfg)
	if err := validate(cfg); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0600)
}

func Default() Config {
	cfg := Config{}
	cfg.App = AppConfig{Name: "myfiles", BaseURL: "http://127.0.0.1:19110", ListenAddr: "127.0.0.1:19110", PublicDir: "./frontend/dist", DataDir: "./data", TempDir: "./data/tmp"}
	cfg.Database.Path = "./data/myfiles.sqlite3"
	cfg.Account = AccountConfig{ClientName: "myfiles", ClientID: "myfiles", LoginURL: "https://account.js.gripe/login", AccountBaseURL: "https://gateway.js.gripe/api/v1/myaccount", MeURL: "https://gateway.js.gripe/api/v1/myaccount/me", RedirectURI: "http://127.0.0.1:19110/auth/account/callback", Scopes: []string{"accounts:read", "identities:resolve"}}
	cfg.Storage = StorageConfig{Mode: "local", PublicBaseURL: cfg.App.BaseURL, TimeoutSeconds: 120, LocalDir: "./data/storage"}
	cfg.Upload = UploadConfig{MaxBytes: DefaultUploadMaxBytes, AllowedMIMETypes: []string{"*/*"}, AllowAnonymous: true}
	cfg.File = FileConfig{DefaultPublic: true, DefaultRequireConfirm: false, DefaultRegionPolicy: "global", DefaultHotlinkPolicy: "allow"}
	cfg.Security = SecurityConfig{SessionCookieName: "myfiles_session", SessionTTLHours: 168, CookieSecure: false}
	cfg.Audit = AuditConfig{RetentionDays: 180}
	return cfg
}

func normalize(cfg *Config) {
	if cfg.App.Name == "" {
		cfg.App.Name = "myfiles"
	}
	if cfg.App.ListenAddr == "" {
		cfg.App.ListenAddr = "127.0.0.1:19110"
	}
	if cfg.App.DataDir == "" {
		cfg.App.DataDir = "./data"
	}
	if cfg.App.TempDir == "" {
		cfg.App.TempDir = cfg.App.DataDir + "/tmp"
	}
	if cfg.App.PublicDir == "" {
		cfg.App.PublicDir = "./frontend/dist"
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = cfg.App.DataDir + "/myfiles.sqlite3"
	}
	if cfg.Account.ClientName == "" {
		cfg.Account.ClientName = "myfiles"
	}
	if cfg.Account.MeURL == "" && cfg.Account.AccountBaseURL != "" {
		cfg.Account.MeURL = cfg.Account.AccountBaseURL + "/me"
	}
	if len(cfg.Account.Scopes) == 0 {
		cfg.Account.Scopes = []string{"accounts:read", "identities:resolve"}
	}
	if cfg.Storage.Mode == "" {
		cfg.Storage.Mode = "local"
	}
	if cfg.Storage.TimeoutSeconds <= 0 {
		cfg.Storage.TimeoutSeconds = 120
	}
	if cfg.Storage.PublicBaseURL == "" {
		cfg.Storage.PublicBaseURL = cfg.App.BaseURL
	}
	if cfg.Storage.LocalDir == "" {
		cfg.Storage.LocalDir = cfg.App.DataDir + "/storage"
	}
	if cfg.Upload.MaxBytes <= 0 {
		cfg.Upload.MaxBytes = DefaultUploadMaxBytes
	}
	if len(cfg.Upload.AllowedMIMETypes) == 0 {
		cfg.Upload.AllowedMIMETypes = []string{"*/*"}
	}
	if cfg.File.DefaultRegionPolicy == "" {
		cfg.File.DefaultRegionPolicy = "global"
	}
	if cfg.File.DefaultRegionPolicy == "cn" || cfg.File.DefaultRegionPolicy == "overseas" {
		cfg.File.DefaultRegionPolicy = "global"
	}
	if cfg.File.DefaultHotlinkPolicy == "" {
		cfg.File.DefaultHotlinkPolicy = "allow"
	}
	if cfg.Security.SessionCookieName == "" {
		cfg.Security.SessionCookieName = "myfiles_session"
	}
	if cfg.Security.SessionTTLHours <= 0 {
		cfg.Security.SessionTTLHours = int((168 * time.Hour).Hours())
	}
	if cfg.Audit.RetentionDays <= 0 {
		cfg.Audit.RetentionDays = 180
	}
}

func validate(cfg Config) error {
	if cfg.App.BaseURL == "" {
		return errors.New("app.base_url is required")
	}
	if cfg.Account.LoginURL == "" {
		return errors.New("account.login_url is required")
	}
	if cfg.Account.RedirectURI == "" {
		return errors.New("account.redirect_uri is required")
	}
	if cfg.Storage.Mode == "tgbots" && cfg.Storage.UploadURL == "" {
		return errors.New("storage.upload_url is required when storage.mode=tgbots")
	}
	return nil
}
