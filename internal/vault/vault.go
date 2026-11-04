// Package vault implements the subset of the HashiCorp Vault HTTP API needed
// by RSCON: AppRole login, token renewal, SSH secrets engine signing, CA
// public key retrieval and health checks. TLS verification is always enabled.
package vault

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Error is a Vault API error.
type Error struct {
	StatusCode int
	Message    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("vault: HTTP %d: %s", e.StatusCode, e.Message)
}

// TLSConfig builds a verified TLS configuration. An optional CA bundle adds to
// the system roots; verification can never be disabled.
func TLSConfig(caFile, serverName string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if caFile == "" {
		return cfg, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read vault CA file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("vault CA file contains no valid certificates")
	}
	cfg.RootCAs = pool
	return cfg, nil
}

// Config configures the client.
type Config struct {
	Address    string
	Namespace  string
	Mount      string
	Role       string
	RoleID     string
	SecretID   string
	HTTPClient *http.Client
	Timeout    time.Duration
	Logger     *slog.Logger
}

// Client is a concurrency-safe Vault client.
type Client struct {
	base      *url.URL
	http      *http.Client
	namespace string
	mount     string
	role      string
	roleID    string
	secretID  string
	timeout   time.Duration
	log       *slog.Logger
	mu        sync.Mutex
	token     string
	tokenExp  time.Time
}

// New creates a client. It does not contact Vault.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.Address)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("vault: invalid address %q", cfg.Address)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	return &Client{
		base:      u,
		http:      hc,
		namespace: cfg.Namespace,
		mount:     strings.Trim(cfg.Mount, "/"),
		role:      cfg.Role,
		roleID:    cfg.RoleID,
		secretID:  cfg.SecretID,
		timeout:   timeout,
		log:       cfg.Logger,
	}, nil
}

func (c *Client) logDebug(msg string, args ...any) {
	if c.log != nil {
		c.log.Debug(msg, args...)
	}
}

// Login authenticates with AppRole.
func (c *Client) Login(ctx context.Context) error {
	if c.roleID == "" || c.secretID == "" {
		return errors.New("vault: AppRole role_id and secret_id are required")
	}
	body := map[string]string{"role_id": c.roleID, "secret_id": c.secretID}
	var out struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/auth/approle/login", body, &out, false); err != nil {
		return fmt.Errorf("vault approle login: %w", err)
	}
	if out.Auth.ClientToken == "" {
		return errors.New("vault: AppRole login returned an empty token")
	}
	c.mu.Lock()
	c.token = out.Auth.ClientToken
	lease := time.Duration(out.Auth.LeaseDuration) * time.Second
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	c.tokenExp = time.Now().Add(lease)
	c.mu.Unlock()
	c.logDebug("vault token acquired", "lease", lease.String())
	return nil
}

func (c *Client) currentToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	token, exp := c.token, c.tokenExp
	c.mu.Unlock()
	if token != "" && time.Until(exp) > 30*time.Second {
		return token, nil
	}
	if token != "" {
		if err := c.renew(ctx, token); err == nil {
			c.mu.Lock()
			token = c.token
			c.mu.Unlock()
			return token, nil
		}
	}
	if err := c.Login(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token, nil
}

func (c *Client) renew(ctx context.Context, token string) error {
	var out struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	err := c.doWithToken(ctx, http.MethodPost, "/v1/auth/token/renew-self", map[string]any{}, &out, token)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if out.Auth.ClientToken != "" {
		c.token = out.Auth.ClientToken
	}
	lease := time.Duration(out.Auth.LeaseDuration) * time.Second
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	c.tokenExp = time.Now().Add(lease)
	return nil
}

// SignSSH asks the SSH secrets engine to sign a public key.
func (c *Client) SignSSH(ctx context.Context, req SignRequest) (string, error) {
	body := map[string]any{
		"public_key":       req.PublicKey,
		"valid_principals": req.Principals,
		"cert_type":        req.CertType,
		"key_id":           req.KeyID,
	}
	if req.TTL > 0 {
		body["ttl"] = req.TTL.String()
	}
	if len(req.CriticalOptions) > 0 {
		body["critical_options"] = req.CriticalOptions
	}
	if len(req.Extensions) > 0 {
		body["extensions"] = req.Extensions
	}
	path := fmt.Sprintf("/v1/%s/sign/%s", url.PathEscape(c.mount), url.PathEscape(c.role))
	var out struct {
		Data struct {
			SignedKey string `json:"signed_key"`
		} `json:"data"`
	}
	if err := c.doTokened(ctx, http.MethodPost, path, body, &out); err != nil {
		return "", fmt.Errorf("vault sign ssh key: %w", err)
	}
	if out.Data.SignedKey == "" {
		return "", errors.New("vault: SSH signing returned an empty certificate")
	}
	return out.Data.SignedKey, nil
}

// PublicKey reads the CA public key from the SSH secrets engine.
func (c *Client) PublicKey(ctx context.Context) (string, error) {
	path := fmt.Sprintf("/v1/%s/public_key", url.PathEscape(c.mount))
	var out struct {
		Data struct {
			PublicKey string `json:"public_key"`
		} `json:"data"`
	}
	if err := c.doTokened(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", fmt.Errorf("vault read SSH CA public key: %w", err)
	}
	if out.Data.PublicKey == "" {
		return "", errors.New("vault: SSH CA public key is empty")
	}
	return out.Data.PublicKey, nil
}

// Health describes Vault availability.
type Health struct {
	Initialized bool   `json:"initialized"`
	Sealed      bool   `json:"sealed"`
	Standby     bool   `json:"standby"`
	Version     string `json:"version"`
	StatusCode  int    `json:"-"`
}

// Health checks Vault. Sealed or uninitialised Vault is reported as an error.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	status, raw, err := c.raw(ctx, http.MethodGet, "/v1/sys/health?standbyok=true&perfstandbyok=true", nil, "")
	if err != nil {
		return h, err
	}
	h.StatusCode = status
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &h)
	}
	switch status {
	case http.StatusOK, http.StatusTooManyRequests, 472, 473:
		return h, nil
	default:
		return h, &Error{StatusCode: status, Message: "Vault is not healthy"}
	}
}

// SignRequest is an SSH signing request.
type SignRequest struct {
	PublicKey       string
	Principals      []string
	CertType        string
	KeyID           string
	TTL             time.Duration
	CriticalOptions map[string]string
	Extensions      map[string]string
}

// doTokened performs a request with automatic token acquisition and one retry
// after an authentication failure.
func (c *Client) doTokened(ctx context.Context, method, path string, body, out any) error {
	token, err := c.currentToken(ctx)
	if err != nil {
		return err
	}
	err = c.doWithToken(ctx, method, path, body, out, token)
	var ve *Error
	if errors.As(err, &ve) && ve.StatusCode == http.StatusForbidden {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		token, loginErr := c.currentToken(ctx)
		if loginErr != nil {
			return loginErr
		}
		return c.doWithToken(ctx, method, path, body, out, token)
	}
	return err
}

func (c *Client) do(ctx context.Context, method, path string, body, out any, withToken bool) error {
	token := ""
	if withToken {
		var err error
		token, err = c.currentToken(ctx)
		if err != nil {
			return err
		}
	}
	return c.doWithToken(ctx, method, path, body, out, token)
}

func (c *Client) doWithToken(ctx context.Context, method, path string, body, out any, token string) error {
	status, raw, err := c.raw(ctx, method, path, body, token)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return decodeError(status, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("vault: decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) raw(ctx context.Context, method, path string, body any, token string) (int, []byte, error) {
	u := *c.base
	if i := strings.IndexByte(path, '?'); i >= 0 {
		u.RawQuery = path[i+1:]
		path = path[:i]
	}
	u.Path = strings.TrimRight(c.base.Path, "/") + path
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if c.namespace != "" {
		req.Header.Set("X-Vault-Namespace", c.namespace)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return 0, nil, err
		}
		return 0, nil, fmt.Errorf("vault: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("vault: read response: %w", err)
	}
	return resp.StatusCode, raw, nil
}

func decodeError(status int, raw []byte) error {
	var payload struct {
		Errors []string `json:"errors"`
	}
	msg := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &payload) == nil && len(payload.Errors) > 0 {
		msg = strings.Join(payload.Errors, "; ")
	}
	if len(msg) > 512 {
		msg = msg[:512]
	}
	return &Error{StatusCode: status, Message: msg}
}

// TokenRemaining is exposed for diagnostics (never returns the token).
func (c *Client) TokenRemaining() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" {
		return 0
	}
	return time.Until(c.tokenExp)
}
