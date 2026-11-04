package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

const validYAML = `
server:
  name: test-01
  environment: development
  ssh_address: "127.0.0.1:2222"
  api_address: "127.0.0.1:8080"
  public_url: "http://127.0.0.1:8080"
database:
  url: "postgres://rscon@localhost/rscon?sslmode=disable"
auth:
  session_ttl: 1h
ssh:
  host_key_files: ["/tmp/host_key"]
  gateway_key_file: "/tmp/gateway_key"
  certificate_ttl: 10m
  max_certificate_ttl: 1h
  destination_certificate_ttl: 15m
  max_session_duration: 8h
  idle_timeout: 30m
  dial_timeout: 10s
  handshake_timeout: 30s
host_keys:
  known_hosts_file: "/tmp/known_hosts"
recording:
  directory: "/tmp/recordings"
dev:
  insecure_file_ca: true
  file_ca_key_file: "/tmp/ca"
`

func TestParseValid(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.SSH.CertificateTTL.Duration() != 10*time.Minute {
		t.Fatalf("certificate ttl = %v", cfg.SSH.CertificateTTL)
	}
	if cfg.Recording.Backend != "filesystem" {
		t.Fatalf("default backend = %q", cfg.Recording.Backend)
	}
	if !cfg.Security.DefaultDeny {
		t.Fatal("default deny must default to true")
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	_, err := Parse([]byte(validYAML + "\nunknown_key: true\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown_key") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestProductionRejectsFileCA(t *testing.T) {
	y := strings.Replace(validYAML, "environment: development", "environment: production", 1)
	_, err := Parse([]byte(y))
	if err == nil || !strings.Contains(err.Error(), "file CA is refused in production") {
		t.Fatalf("expected production CA rejection, got %v", err)
	}
}

func TestEnableAgentForwardingRejected(t *testing.T) {
	y := validYAML + "  allow_agent_forwarding: true\n"
	_, err := Parse([]byte(y))
	if err == nil || !strings.Contains(err.Error(), "allow_agent_forwarding") {
		t.Fatalf("expected forwarding rejection, got %v", err)
	}
}

func TestDefaultDenyCannotBeDisabled(t *testing.T) {
	y := strings.Replace(validYAML, "dev:", "security:\n  default_deny: false\ndev:", 1)
	_, err := Parse([]byte(y))
	if err == nil || !strings.Contains(err.Error(), "default_deny") {
		t.Fatalf("expected default_deny rejection, got %v", err)
	}
}

func TestDatabaseURLFromEnv(t *testing.T) {
	t.Setenv("RSCON_TEST_DB_URL", "postgres://fromenv@localhost/db")
	y := strings.Replace(validYAML, `url: "postgres://rscon@localhost/rscon?sslmode=disable"`, `url_env: RSCON_TEST_DB_URL`, 1)
	cfg, err := Parse([]byte(y))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Database.URL != "postgres://fromenv@localhost/db" {
		t.Fatalf("database url = %q", cfg.Database.URL)
	}
}

func TestMFAConfigDoesNotAcceptSecrets(t *testing.T) {
	// Ensure the strict decoder rejects attempts to inline passwords.
	dir := t.TempDir()
	path := dir + "/config.yaml"
	if err := os.WriteFile(path, []byte(validYAML+"\nauth:\n  password: hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected duplicate/unknown field rejection")
	}
}
