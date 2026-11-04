// Package config implements strict YAML configuration loading and validation.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a YAML-friendly time.Duration ("10m", "8h").
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string such as 10m: %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// Duration returns the standard library duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Config is the complete daemon configuration.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Vault     VaultConfig     `yaml:"vault"`
	Auth      AuthConfig      `yaml:"auth"`
	SSH       SSHConfig       `yaml:"ssh"`
	HostKeys  HostKeyConfig   `yaml:"host_keys"`
	Recording RecordingConfig `yaml:"recording"`
	Audit     AuditConfig     `yaml:"audit"`
	API       APIConfig       `yaml:"api"`
	Security  SecurityConfig  `yaml:"security"`
	Log       LogConfig       `yaml:"log"`
	Dev       DevConfig       `yaml:"dev"`
	Source    string          `yaml:"-"`
}

// ServerConfig describes process-level networking.
type ServerConfig struct {
	Name            string   `yaml:"name"`
	Environment     string   `yaml:"environment"`
	SSHAddress      string   `yaml:"ssh_address"`
	APIAddress      string   `yaml:"api_address"`
	PublicURL       string   `yaml:"public_url"`
	TLSCertFile     string   `yaml:"tls_cert_file"`
	TLSKeyFile      string   `yaml:"tls_key_file"`
	ShutdownTimeout Duration `yaml:"shutdown_timeout"`
}

// DatabaseConfig configures PostgreSQL. The URL should come from the
// environment in production (RSCON_DATABASE_URL).
type DatabaseConfig struct {
	URL            string   `yaml:"url"`
	URLEnv         string   `yaml:"url_env"`
	MaxConnections int32    `yaml:"max_connections"`
	MinConnections int32    `yaml:"min_connections"`
	ConnectTimeout Duration `yaml:"connect_timeout"`
}

// VaultConfig configures the Vault SSH secrets engine.
type VaultConfig struct {
	Enabled           bool      `yaml:"enabled"`
	Address           string    `yaml:"address"`
	Namespace         string    `yaml:"namespace"`
	SSHMount          string    `yaml:"ssh_mount"`
	SigningRole       string    `yaml:"signing_role"`
	CACacheTTL        Duration  `yaml:"ca_cache_ttl"`
	Timeout           Duration  `yaml:"timeout"`
	AllowInsecureHTTP bool      `yaml:"allow_insecure_http"`
	TLS               VaultTLS  `yaml:"tls"`
	Auth              VaultAuth `yaml:"auth"`
}

// VaultTLS configures verification of the Vault listener.
type VaultTLS struct {
	CAFile     string `yaml:"ca_file"`
	ServerName string `yaml:"server_name"`
}

// VaultAuth configures AppRole credentials. Values are read from a file or an
// environment variable; they are never taken from the YAML document.
type VaultAuth struct {
	Method       string `yaml:"method"`
	RoleIDFile   string `yaml:"role_id_file"`
	SecretIDFile string `yaml:"secret_id_file"`
	RoleIDEnv    string `yaml:"role_id_env"`
	SecretIDEnv  string `yaml:"secret_id_env"`
}

// AuthConfig configures API authentication.
type AuthConfig struct {
	SessionTTL       Duration      `yaml:"session_ttl"`
	LoginRateLimit   int           `yaml:"login_rate_limit"`
	LoginRateWindow  Duration      `yaml:"login_rate_window"`
	LockoutThreshold int           `yaml:"lockout_threshold"`
	LockoutDuration  Duration      `yaml:"lockout_duration"`
	Bootstrap        BootstrapAuth `yaml:"bootstrap"`
}

// BootstrapAuth controls creation of the first local administrator.
type BootstrapAuth struct {
	Username    string `yaml:"username"`
	PasswordEnv string `yaml:"password_env"`
}

// SSHConfig configures the data plane.
type SSHConfig struct {
	HostKeyFiles          []string `yaml:"host_key_files"`
	TrustedUserCAFiles    []string `yaml:"trusted_user_ca_files"`
	GatewayKeyFile        string   `yaml:"gateway_key_file"`
	CertificateTTL        Duration `yaml:"certificate_ttl"`
	MaxCertificateTTL     Duration `yaml:"max_certificate_ttl"`
	DestinationCertTTL    Duration `yaml:"destination_certificate_ttl"`
	MaxSessionDuration    Duration `yaml:"max_session_duration"`
	IdleTimeout           Duration `yaml:"idle_timeout"`
	DialTimeout           Duration `yaml:"dial_timeout"`
	HandshakeTimeout      Duration `yaml:"handshake_timeout"`
	MaxConcurrentSessions int      `yaml:"max_concurrent_sessions"`
	MaxAuthTries          int      `yaml:"max_auth_tries"`
	AllowLocalKeyAuth     bool     `yaml:"allow_local_key_auth"`
	AllowTCPForwarding    bool     `yaml:"allow_tcp_forwarding"`
	AllowSFTP             bool     `yaml:"allow_sftp"`
	AllowAgentForwarding  bool     `yaml:"allow_agent_forwarding"`
	AllowX11Forwarding    bool     `yaml:"allow_x11_forwarding"`
}

// HostKeyConfig configures destination identity verification.
type HostKeyConfig struct {
	KnownHostsFile      string   `yaml:"known_hosts_file"`
	TrustedHostCAFiles  []string `yaml:"trusted_host_ca_files"`
	RequireVerification bool     `yaml:"require_verification"`
}

// RecordingConfig configures terminal capture.
type RecordingConfig struct {
	Enabled      bool     `yaml:"enabled"`
	FailClosed   bool     `yaml:"fail_closed"`
	Backend      string   `yaml:"backend"`
	Directory    string   `yaml:"directory"`
	Retention    Duration `yaml:"retention"`
	MaxFileBytes int64    `yaml:"max_file_bytes"`
}

// AuditConfig configures audit fan-out.
type AuditConfig struct {
	RequirePersist bool         `yaml:"require_persist"`
	Syslog         SyslogConfig `yaml:"syslog"`
}

// SyslogConfig supports RFC3164-style syslog or GELF over UDP/TCP.
type SyslogConfig struct {
	Enabled bool   `yaml:"enabled"`
	Network string `yaml:"network"`
	Address string `yaml:"address"`
	Format  string `yaml:"format"`
}

// APIConfig configures the HTTP control plane.
type APIConfig struct {
	CORSAllowedOrigins []string `yaml:"cors_allowed_origins"`
	RateLimitPerMinute int      `yaml:"rate_limit_per_minute"`
	TrustedProxyCIDRs  []string `yaml:"trusted_proxy_cidrs"`
	WebDirectory       string   `yaml:"web_directory"`
	MaxBodyBytes       int64    `yaml:"max_body_bytes"`
}

// SecurityConfig holds global security switches that must not be weakened.
type SecurityConfig struct {
	DefaultDeny       bool `yaml:"default_deny"`
	AllowSelfApproval bool `yaml:"allow_self_approval"`
}

// LogConfig configures structured logging.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// DevConfig contains explicitly unsafe development-only switches.
type DevConfig struct {
	InsecureFileCA bool   `yaml:"insecure_file_ca"`
	FileCAKeyFile  string `yaml:"file_ca_key_file"`
}

// Default returns a configuration with safe defaults applied.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Name:            "rscond-01",
			Environment:     "production",
			SSHAddress:      "0.0.0.0:2222",
			APIAddress:      "127.0.0.1:8080",
			PublicURL:       "https://bastion.example.com",
			ShutdownTimeout: Duration(30 * time.Second),
		},
		Database: DatabaseConfig{
			URLEnv:         "RSCON_DATABASE_URL",
			MaxConnections: 20,
			MinConnections: 2,
			ConnectTimeout: Duration(10 * time.Second),
		},
		Vault: VaultConfig{
			SSHMount:    "ssh-client-signer",
			SigningRole: "rscon-users",
			CACacheTTL:  Duration(5 * time.Minute),
			Timeout:     Duration(10 * time.Second),
			Auth:        VaultAuth{Method: "approle"},
		},
		Auth: AuthConfig{
			SessionTTL:       Duration(8 * time.Hour),
			LoginRateLimit:   10,
			LoginRateWindow:  Duration(time.Minute),
			LockoutThreshold: 5,
			LockoutDuration:  Duration(15 * time.Minute),
			Bootstrap: BootstrapAuth{
				Username:    "admin",
				PasswordEnv: "RSCON_BOOTSTRAP_ADMIN_PASSWORD",
			},
		},
		SSH: SSHConfig{
			HostKeyFiles:          []string{"/etc/rscon/ssh_host_ed25519_key"},
			GatewayKeyFile:        "/var/lib/rscon/gateway_ed25519",
			CertificateTTL:        Duration(10 * time.Minute),
			MaxCertificateTTL:     Duration(1 * time.Hour),
			DestinationCertTTL:    Duration(15 * time.Minute),
			MaxSessionDuration:    Duration(8 * time.Hour),
			IdleTimeout:           Duration(30 * time.Minute),
			DialTimeout:           Duration(10 * time.Second),
			HandshakeTimeout:      Duration(30 * time.Second),
			MaxConcurrentSessions: 5,
			MaxAuthTries:          3,
		},
		HostKeys: HostKeyConfig{
			KnownHostsFile:      "/etc/rscon/known_hosts",
			RequireVerification: true,
		},
		Recording: RecordingConfig{
			Enabled:      true,
			FailClosed:   true,
			Backend:      "filesystem",
			Directory:    "/var/lib/rscon/recordings",
			Retention:    Duration(90 * 24 * time.Hour),
			MaxFileBytes: 256 << 20,
		},
		Audit: AuditConfig{
			RequirePersist: true,
			Syslog:         SyslogConfig{Network: "udp", Address: "127.0.0.1:514", Format: "json"},
		},
		API: APIConfig{
			RateLimitPerMinute: 600,
			MaxBodyBytes:       1 << 20,
		},
		Security: SecurityConfig{DefaultDeny: true},
		Log:      LogConfig{Level: "info", Format: "json"},
	}
}

// LoadFile reads and validates a YAML configuration file.
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return Config{}, err
	}
	cfg.Source = path
	return cfg, nil
}

// Parse decodes and validates a YAML document. Unknown keys are rejected.
func Parse(data []byte) (Config, error) {
	cfg := Default()
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	// Reject trailing documents to avoid silently ignored configuration.
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return Config{}, errors.New("config: multiple YAML documents are not supported")
	}
	if err := cfg.applyEnv(); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyEnv() error {
	if c.Database.URLEnv == "" {
		c.Database.URLEnv = "RSCON_DATABASE_URL"
	}
	if v, ok := os.LookupEnv(c.Database.URLEnv); ok && v != "" {
		c.Database.URL = v
	}
	return nil
}

// Validate performs strict semantic validation.
func (c *Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if strings.TrimSpace(c.Server.Name) == "" {
		add("server.name is required")
	}
	switch c.Server.Environment {
	case "production", "staging", "development", "test":
	default:
		add("server.environment must be one of production, staging, development, test")
	}
	if err := validateAddr(c.Server.SSHAddress); err != nil {
		add("server.ssh_address: %v", err)
	}
	if err := validateAddr(c.Server.APIAddress); err != nil {
		add("server.api_address: %v", err)
	}
	if c.Server.PublicURL == "" {
		add("server.public_url is required")
	} else if u, err := url.Parse(c.Server.PublicURL); err != nil || u.Scheme == "" || u.Host == "" {
		add("server.public_url must be an absolute URL")
	}
	if (c.Server.TLSCertFile == "") != (c.Server.TLSKeyFile == "") {
		add("server.tls_cert_file and server.tls_key_file must be set together")
	}
	if c.Server.ShutdownTimeout.Duration() <= 0 {
		add("server.shutdown_timeout must be positive")
	}

	if strings.TrimSpace(c.Database.URL) == "" {
		add("database URL is required (set database.url or %s)", c.Database.URLEnv)
	} else if !strings.HasPrefix(c.Database.URL, "postgres://") && !strings.HasPrefix(c.Database.URL, "postgresql://") {
		add("database.url must use the postgres:// or postgresql:// scheme")
	}
	if c.Database.MaxConnections < 1 {
		add("database.max_connections must be >= 1")
	}
	if c.Database.MinConnections < 0 || c.Database.MinConnections > c.Database.MaxConnections {
		add("database.min_connections must be between 0 and max_connections")
	}
	if c.Database.ConnectTimeout.Duration() <= 0 {
		add("database.connect_timeout must be positive")
	}

	if c.Vault.Enabled {
		u, err := url.Parse(c.Vault.Address)
		if err != nil || u.Host == "" {
			add("vault.address must be an absolute URL")
		} else if u.Scheme != "https" && !(u.Scheme == "http" && c.Vault.AllowInsecureHTTP) {
			add("vault.address must use https (set vault.allow_insecure_http only for local testing)")
		}
		if c.Vault.SSHMount == "" {
			add("vault.ssh_mount is required")
		}
		if c.Vault.SigningRole == "" {
			add("vault.signing_role is required")
		}
		if c.Vault.Auth.Method != "approle" {
			add("vault.auth.method must be approle (the only implemented method)")
		}
		hasRole := c.Vault.Auth.RoleIDFile != "" || c.Vault.Auth.RoleIDEnv != ""
		hasSecret := c.Vault.Auth.SecretIDFile != "" || c.Vault.Auth.SecretIDEnv != ""
		if !hasRole {
			add("vault.auth.role_id_file or role_id_env is required")
		}
		if !hasSecret {
			add("vault.auth.secret_id_file or secret_id_env is required")
		}
		if c.Vault.Timeout.Duration() <= 0 {
			add("vault.timeout must be positive")
		}
		if c.Vault.CACacheTTL.Duration() <= 0 {
			add("vault.ca_cache_ttl must be positive")
		}
	}

	if c.Auth.SessionTTL.Duration() <= 0 {
		add("auth.session_ttl must be positive")
	}
	if c.Auth.LoginRateLimit < 1 {
		add("auth.login_rate_limit must be >= 1")
	}
	if c.Auth.LoginRateWindow.Duration() <= 0 {
		add("auth.login_rate_window must be positive")
	}
	if c.Auth.LockoutThreshold < 1 {
		add("auth.lockout_threshold must be >= 1")
	}
	if c.Auth.LockoutDuration.Duration() <= 0 {
		add("auth.lockout_duration must be positive")
	}
	if c.Auth.Bootstrap.Username == "" || c.Auth.Bootstrap.PasswordEnv == "" {
		add("auth.bootstrap.username and password_env are required")
	}

	if len(c.SSH.HostKeyFiles) == 0 {
		add("ssh.host_key_files must contain at least one key")
	}
	if c.SSH.GatewayKeyFile == "" {
		add("ssh.gateway_key_file is required")
	}
	if c.SSH.CertificateTTL.Duration() <= 0 || c.SSH.CertificateTTL.Duration() > c.SSH.MaxCertificateTTL.Duration() {
		add("ssh.certificate_ttl must be positive and <= ssh.max_certificate_ttl")
	}
	if c.SSH.MaxCertificateTTL.Duration() <= 0 {
		add("ssh.max_certificate_ttl must be positive")
	}
	if c.SSH.DestinationCertTTL.Duration() <= 0 {
		add("ssh.destination_certificate_ttl must be positive")
	}
	if c.SSH.MaxSessionDuration.Duration() <= 0 {
		add("ssh.max_session_duration must be positive")
	}
	if c.SSH.IdleTimeout.Duration() <= 0 {
		add("ssh.idle_timeout must be positive")
	}
	if c.SSH.DialTimeout.Duration() <= 0 || c.SSH.HandshakeTimeout.Duration() <= 0 {
		add("ssh.dial_timeout and ssh.handshake_timeout must be positive")
	}
	if c.SSH.MaxConcurrentSessions < 1 {
		add("ssh.max_concurrent_sessions must be >= 1")
	}
	if c.SSH.MaxAuthTries < 1 || c.SSH.MaxAuthTries > 10 {
		add("ssh.max_auth_tries must be between 1 and 10")
	}
	if c.SSH.AllowAgentForwarding || c.SSH.AllowX11Forwarding {
		add("ssh.allow_agent_forwarding and ssh.allow_x11_forwarding cannot be enabled: these channels are not supported")
	}

	if c.HostKeys.RequireVerification {
		if c.HostKeys.KnownHostsFile == "" && len(c.HostKeys.TrustedHostCAFiles) == 0 {
			add("host key verification requires host_keys.known_hosts_file or trusted_host_ca_files")
		}
	} else if c.Server.Environment == "production" {
		add("host_keys.require_verification cannot be disabled in production")
	}

	if c.Recording.Enabled {
		if c.Recording.Backend != "filesystem" {
			add("recording.backend must be filesystem (the only implemented backend)")
		}
		if c.Recording.Directory == "" {
			add("recording.directory is required when recording is enabled")
		}
		if c.Recording.MaxFileBytes < 1<<20 {
			add("recording.max_file_bytes must be >= 1 MiB")
		}
	}
	switch c.Audit.Syslog.Format {
	case "json", "gelf":
	default:
		add("audit.syslog.format must be json or gelf")
	}
	switch c.Audit.Syslog.Network {
	case "udp", "tcp":
	default:
		add("audit.syslog.network must be udp or tcp")
	}
	if c.API.RateLimitPerMinute < 1 {
		add("api.rate_limit_per_minute must be >= 1")
	}
	if c.API.MaxBodyBytes < 1024 {
		add("api.max_body_bytes must be >= 1024")
	}
	for _, cidr := range c.API.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			add("api.trusted_proxy_cidrs contains invalid CIDR %q", cidr)
		}
	}
	if !c.Security.DefaultDeny {
		add("security.default_deny must be true; disabling it is not supported")
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		add("log.level must be debug, info, warn or error")
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		add("log.format must be json or text")
	}

	// Certificate authority selection.
	if c.Vault.Enabled {
		if c.Dev.InsecureFileCA {
			add("dev.insecure_file_ca cannot be enabled when Vault is enabled")
		}
	} else {
		if !c.Dev.InsecureFileCA {
			add("no certificate authority configured: enable vault or set dev.insecure_file_ca (development only)")
		}
		if c.Dev.FileCAKeyFile == "" {
			add("dev.file_ca_key_file is required when vault is disabled")
		}
		if c.Server.Environment == "production" {
			add("development file CA is refused in production")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func validateAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must be host:port: %w", err)
	}
	if host == "" {
		return errors.New("host must not be empty")
	}
	if port == "" {
		return errors.New("port must not be empty")
	}
	return nil
}
