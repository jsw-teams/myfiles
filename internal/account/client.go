package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jsw-teams/myfiles/internal/config"
)

type User struct {
	ID           string         `json:"id"`
	Email        string         `json:"email"`
	DisplayName  string         `json:"displayName"`
	Role         string         `json:"role"`
	UserType     string         `json:"userType"`
	Capabilities map[string]any `json:"capabilities"`
}

type APIError struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	StatusCode int            `json:"statusCode"`
	Detail     map[string]any `json:"detail,omitempty"`
}

func (e *APIError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

type Client struct {
	cfg  config.AccountConfig
	http *http.Client
}

func NewClient(cfg config.AccountConfig) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Me(ctx context.Context, accountSession string) (User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.MeURL, nil)
	if err != nil {
		return User{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accountSession)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "myfiles/account-client")

	resp, err := c.http.Do(req)
	if err != nil {
		return User{}, &APIError{Code: "account_unreachable", Message: "账户中心暂时不可用", StatusCode: 502}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return User{}, normalizeError(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}

	var envelope struct {
		User         *User          `json:"user"`
		ID           string         `json:"id"`
		Email        string         `json:"email"`
		DisplayName  string         `json:"displayName"`
		Role         string         `json:"role"`
		UserType     string         `json:"userType"`
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return User{}, &APIError{Code: "bad_account_response", Message: "账户中心返回格式不可识别", StatusCode: 502}
	}
	if envelope.User != nil {
		if envelope.User.Capabilities == nil {
			envelope.User.Capabilities = map[string]any{}
		}
		return *envelope.User, nil
	}
	u := User{ID: envelope.ID, Email: envelope.Email, DisplayName: envelope.DisplayName, Role: envelope.Role, UserType: envelope.UserType, Capabilities: envelope.Capabilities}
	if u.Capabilities == nil {
		u.Capabilities = map[string]any{}
	}
	if u.ID == "" {
		return User{}, &APIError{Code: "bad_account_response", Message: "账户中心未返回用户 ID", StatusCode: 502}
	}
	return u, nil
}

func normalizeError(status int, contentType string, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if strings.Contains(strings.ToLower(contentType), "text/html") && status == http.StatusForbidden {
		return &APIError{Code: "cloudflare_challenge", Message: "账户中心要求浏览器验证，请在登录窗口完成验证后重试", StatusCode: status}
	}
	var payload struct {
		Error  string         `json:"error"`
		Code   string         `json:"code"`
		Detail map[string]any `json:"detail"`
	}
	if json.Unmarshal(body, &payload) == nil && (payload.Code != "" || payload.Error != "") {
		code := payload.Code
		if code == "" {
			code = "account_error"
		}
		message := payload.Error
		if message == "" {
			message = code
		}
		switch code {
		case "account_disabled":
			message = "统一账户已停用，请联系支持团队处理"
		case "bad_credentials":
			message = "统一账户登录已失效，请重新登录"
		}
		return &APIError{Code: code, Message: message, StatusCode: status, Detail: payload.Detail}
	}
	switch status {
	case http.StatusUnauthorized:
		return &APIError{Code: "bad_credentials", Message: "统一账户登录已失效，请重新登录", StatusCode: status}
	case http.StatusForbidden:
		return &APIError{Code: "account_forbidden", Message: "统一账户无权访问该应用", StatusCode: status}
	default:
		if len(msg) > 160 {
			msg = msg[:160]
		}
		return &APIError{Code: "account_error", Message: "账户中心返回错误", StatusCode: status, Detail: map[string]any{"status": status, "body": msg}}
	}
}
