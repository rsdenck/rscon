// Command rscond is the RSCON SSH bastion and control plane daemon.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/rsdenck/rscon/internal/api"
	"github.com/rsdenck/rscon/internal/audit"
	"github.com/rsdenck/rscon/internal/authz"
	"github.com/rsdenck/rscon/internal/ca"
	"github.com/rsdenck/rscon/internal/config"
	"github.com/rsdenck/rscon/internal/database"
	"github.com/rsdenck/rscon/internal/gateway"
	"github.com/rsdenck/rscon/internal/hostkey"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/observability"
	"github.com/rsdenck/rscon/internal/recording"
	"github.com/rsdenck/rscon/internal/store"
	"github.com/rsdenck/rscon/internal/vault"
	"github.com/rsdenck/rscon/internal/web"
	"github.com/rsdenck/rscon/pkg/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rscond:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath  = flag.String("config", "/etc/rscon/config.yaml", "path to the YAML configuration file")
		check       = flag.Bool("check", false, "validate configuration and exit")
		migrateOnly = flag.Bool("migrate", false, "apply database migrations and exit")
		rollback    = flag.Int("migrate-down", 0, "roll back N database migrations and exit")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println(version.String())
		return nil
	}
	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		return err
	}
	if *check {
		fmt.Printf("configuration is valid: %s\n", cfg.Source)
		return nil
	}
	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.Database.URL, cfg.Database.MaxConnections,
		cfg.Database.MinConnections, cfg.Database.ConnectTimeout.Duration())
	if err != nil {
		return err
	}
	defer db.Close()
	if *migrateOnly {
		if err := database.Migrate(ctx, db, logger); err != nil {
			return err
		}
		fmt.Println("database migrations applied")
		return nil
	}
	if *rollback > 0 {
		if err := database.Rollback(ctx, db, *rollback, logger); err != nil {
			return err
		}
		fmt.Printf("rolled back %d migration(s)\n", *rollback)
		return nil
	}
	if err := database.Migrate(ctx, db, logger); err != nil {
		return err
	}

	st := store.New(db)
	metrics := observability.New()
	auditSvc, err := audit.New(audit.Config{
		Store: st, Logger: logger, Metrics: metrics, RequirePersist: cfg.Audit.RequirePersist,
		Host: cfg.Server.Name,
		Syslog: audit.SyslogConfig{
			Enabled: cfg.Audit.Syslog.Enabled, Network: cfg.Audit.Syslog.Network,
			Address: cfg.Audit.Syslog.Address, Format: cfg.Audit.Syslog.Format,
		},
	})
	if err != nil {
		return err
	}
	defer auditSvc.Close()

	if err := bootstrapAdmin(ctx, cfg, st, auditSvc, logger); err != nil {
		return err
	}

	signer, err := buildSigner(ctx, cfg, logger)
	if err != nil {
		return err
	}
	if err := signer.Health(ctx); err != nil {
		return fmt.Errorf("certificate authority health check failed: %w", err)
	}
	caKey, err := signer.CA(ctx)
	if err != nil {
		return fmt.Errorf("read certificate authority public key: %w", err)
	}
	trustedCAs, _ := ca.LoadAuthorizedKeys(cfg.SSH.TrustedUserCAFiles)
	if len(trustedCAs) == 0 {
		trustedCAs = []ssh.PublicKey{caKey}
	}

	gatewayKey, err := loadOrCreateGatewayKey(cfg.SSH.GatewayKeyFile)
	if err != nil {
		return err
	}
	hostSigners, err := loadHostSigners(cfg, logger)
	if err != nil {
		return err
	}
	hostVerifier, err := hostkey.New(cfg.HostKeys.KnownHostsFile, cfg.HostKeys.TrustedHostCAFiles, cfg.HostKeys.RequireVerification)
	if err != nil {
		return err
	}
	var recordings recording.Store
	if cfg.Recording.Enabled {
		recordings, err = recording.NewFilesystem(cfg.Recording.Directory, cfg.Recording.MaxFileBytes)
		if err != nil {
			return err
		}
	}

	policy := authz.New(st, nil)
	gw, err := gateway.New(gateway.Options{
		Config: gateway.Config{
			Name: cfg.Server.Name, ListenAddress: cfg.Server.SSHAddress,
			HostSigners: hostSigners, GatewaySigner: gatewayKey, TrustedUserCAs: trustedCAs,
			CertificateTTL:     cfg.SSH.CertificateTTL.Duration(),
			DestinationCertTTL: cfg.SSH.DestinationCertTTL.Duration(),
			MaxSessionDuration: cfg.SSH.MaxSessionDuration.Duration(),
			IdleTimeout:        cfg.SSH.IdleTimeout.Duration(), DialTimeout: cfg.SSH.DialTimeout.Duration(),
			HandshakeTimeout:      cfg.SSH.HandshakeTimeout.Duration(),
			MaxConcurrentSessions: cfg.SSH.MaxConcurrentSessions, MaxAuthTries: cfg.SSH.MaxAuthTries,
			AllowLocalKeyAuth: cfg.SSH.AllowLocalKeyAuth, AllowTCPForwarding: cfg.SSH.AllowTCPForwarding,
			AllowSFTP: cfg.SSH.AllowSFTP, RecordingFailClosed: cfg.Recording.FailClosed,
		},
		Store: st, Authorizer: policy, Signer: signer, Recordings: recordings,
		Audit: auditSvc, Metrics: metrics, Logger: logger, HostKeys: hostVerifier,
	})
	if err != nil {
		return err
	}

	webHandler, err := web.Handler()
	if err != nil {
		return fmt.Errorf("web console: %w", err)
	}
	apiSrv, err := api.New(api.Options{
		Config: api.Config{
			ListenAddress: cfg.Server.APIAddress, PublicURL: cfg.Server.PublicURL,
			TLSCertFile: cfg.Server.TLSCertFile, TLSKeyFile: cfg.Server.TLSKeyFile,
			SessionTTL:     cfg.Auth.SessionTTL.Duration(),
			LoginRateLimit: cfg.Auth.LoginRateLimit, LoginRateWindow: cfg.Auth.LoginRateWindow.Duration(),
			LockoutThreshold: cfg.Auth.LockoutThreshold, LockoutDuration: cfg.Auth.LockoutDuration.Duration(),
			DefaultCertTTL: cfg.SSH.CertificateTTL.Duration(), MaxCertTTL: cfg.SSH.MaxCertificateTTL.Duration(),
			MaxBodyBytes: cfg.API.MaxBodyBytes, RateLimitPerMinute: cfg.API.RateLimitPerMinute,
			TrustedProxyCIDRs: cfg.API.TrustedProxyCIDRs, CORSOrigins: cfg.API.CORSAllowedOrigins,
			AllowSelfApproval: cfg.Security.AllowSelfApproval,
		},
		Store: st, Authz: policy, Signer: signer, Recordings: recordings, Audit: auditSvc,
		Metrics: metrics, Logger: logger, Gateway: gw, Web: webHandler, Version: version.Version,
	})
	if err != nil {
		return err
	}

	go housekeeping(ctx, st, logger)

	errCh := make(chan error, 2)
	go func() { errCh <- gw.Run(ctx) }()
	go func() { errCh <- apiSrv.Run(ctx) }()
	logger.Info("RSCON daemon started", "version", version.Version,
		"environment", cfg.Server.Environment, "gateway", cfg.Server.SSHAddress,
		"api", cfg.Server.APIAddress, "recording", cfg.Recording.Enabled)

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			stop()
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout.Duration())
	defer cancel()
	_ = shutdownCtx
	// Run returns after its listener closes and in-flight connections finish.
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "Server closed") {
			return err
		}
	case <-time.After(cfg.Server.ShutdownTimeout.Duration()):
		logger.Warn("shutdown timeout reached")
	}
	logger.Info("RSCON daemon stopped")
	return nil
}

func newLogger(cfg config.LogConfig) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func bootstrapAdmin(ctx context.Context, cfg config.Config, st *store.Store, auditSvc *audit.Service, logger *slog.Logger) error {
	count, err := st.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}
	password := os.Getenv(cfg.Auth.Bootstrap.PasswordEnv)
	if password == "" {
		return fmt.Errorf("no users exist; set %s to bootstrap the first administrator", cfg.Auth.Bootstrap.PasswordEnv)
	}
	if len(password) < 12 {
		return errors.New("bootstrap administrator password must be at least 12 characters")
	}
	hash, err := identity.HashPassword(password)
	if err != nil {
		return err
	}
	user := &model.User{
		ID: identity.NewID(), Username: cfg.Auth.Bootstrap.Username,
		DisplayName: "RSCON Administrator", PasswordHash: hash, Status: model.StatusActive,
	}
	if err := st.CreateUser(ctx, user); err != nil {
		return fmt.Errorf("create bootstrap administrator: %w", err)
	}
	role, err := st.GetRoleByName(ctx, "super_administrator")
	if err != nil {
		return fmt.Errorf("load super_administrator role: %w", err)
	}
	if err := st.BindRole(ctx, &model.RoleBinding{
		ID: identity.NewID(), RoleID: role.ID, SubjectType: "user", SubjectID: user.ID,
	}); err != nil {
		return fmt.Errorf("bind bootstrap administrator role: %w", err)
	}
	_ = auditSvc.Write(ctx, audit.Event{
		Category: audit.CategoryAdmin, Action: "bootstrap.administrator_created",
		ActorID: user.ID, ActorName: user.Username, Result: audit.ResultSuccess,
		Message: "first administrator created from environment bootstrap password",
	})
	logger.Warn("bootstrap administrator created; remove the bootstrap password from the environment",
		"username", user.Username, "env", cfg.Auth.Bootstrap.PasswordEnv)
	return nil
}

func buildSigner(ctx context.Context, cfg config.Config, logger *slog.Logger) (ca.Signer, error) {
	if !cfg.Vault.Enabled {
		logger.Warn("DEVELOPMENT FILE CA ENABLED: the CA private key is on local disk and must never be used in production")
		return ca.NewFileSigner(cfg.Dev.FileCAKeyFile)
	}
	roleID, err := readSecret(cfg.Vault.Auth.RoleIDFile, cfg.Vault.Auth.RoleIDEnv, "Vault role_id")
	if err != nil {
		return nil, err
	}
	secretID, err := readSecret(cfg.Vault.Auth.SecretIDFile, cfg.Vault.Auth.SecretIDEnv, "Vault secret_id")
	if err != nil {
		return nil, err
	}
	tlsConfig, err := vault.TLSConfig(cfg.Vault.TLS.CAFile, cfg.Vault.TLS.ServerName)
	if err != nil {
		return nil, err
	}
	client, err := vault.New(vault.Config{
		Address: cfg.Vault.Address, Namespace: cfg.Vault.Namespace,
		Mount: cfg.Vault.SSHMount, Role: cfg.Vault.SigningRole,
		RoleID: roleID, SecretID: secretID,
		HTTPClient: &http.Client{
			Timeout: cfg.Vault.Timeout.Duration(),
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig, Proxy: http.ProxyFromEnvironment,
				MaxIdleConns: 20, IdleConnTimeout: 90 * time.Second,
			},
		},
		Timeout: cfg.Vault.Timeout.Duration(), Logger: logger,
	})
	if err != nil {
		return nil, err
	}
	if err := client.Login(ctx); err != nil {
		return nil, fmt.Errorf("Vault AppRole login failed: %w", err)
	}
	return ca.NewVaultSigner(client, cfg.Vault.SSHMount, cfg.Vault.CACacheTTL.Duration()), nil
}

func readSecret(file, env, label string) (string, error) {
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %s file: %w", label, err)
		}
		value := strings.TrimSpace(string(data))
		if value == "" {
			return "", fmt.Errorf("%s file %s is empty", label, file)
		}
		return value, nil
	}
	if env == "" {
		return "", fmt.Errorf("%s is not configured", label)
	}
	value := os.Getenv(env)
	if value == "" {
		return "", fmt.Errorf("%s environment variable %s is empty", label, env)
	}
	return value, nil
}

func loadOrCreateGatewayKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return ssh.ParsePrivateKey(data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read gateway key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create gateway key directory: %w", err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "rscon-gateway")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, fmt.Errorf("write gateway key: %w", err)
	}
	return ssh.NewSignerFromKey(privateKey)
}

func loadHostSigners(cfg config.Config, logger *slog.Logger) ([]ssh.Signer, error) {
	var signers []ssh.Signer
	for _, path := range cfg.SSH.HostKeyFiles {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read SSH host key %s: %w", path, err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse SSH host key %s: %w", path, err)
		}
		signers = append(signers, signer)
	}
	if len(signers) > 0 {
		return signers, nil
	}
	if cfg.Server.Environment == "production" {
		return nil, errors.New("no SSH host keys found; generate them before starting rscond in production")
	}
	path := cfg.SSH.HostKeyFiles[0]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "rscon-host")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, err
	}
	logger.Warn("generated a development SSH host key", "path", path)
	return []ssh.Signer{signer}, nil
}

func housekeeping(ctx context.Context, st *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := st.ExpireAccessRequests(ctx); err == nil && n > 0 {
				logger.Info("expired access grants", "count", n)
			}
			if n, err := st.DeleteExpiredTokens(ctx, time.Now().Add(-24*time.Hour)); err == nil && n > 0 {
				logger.Info("deleted expired tokens", "count", n)
			}
		}
	}
}
