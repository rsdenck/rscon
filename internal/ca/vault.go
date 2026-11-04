package ca

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rsdenck/rscon/internal/vault"
	"golang.org/x/crypto/ssh"
)

// VaultSigner delegates SSH certificate signing to Vault's SSH secrets engine.
type VaultSigner struct {
	client   *vault.Client
	mount    string
	cacheTTL time.Duration
	mu       sync.Mutex
	ca       ssh.PublicKey
	caExpiry time.Time
}

// NewVaultSigner returns a Vault-backed signer.
func NewVaultSigner(client *vault.Client, mount string, cacheTTL time.Duration) *VaultSigner {
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	return &VaultSigner{client: client, mount: mount, cacheTTL: cacheTTL}
}

// Name implements Signer.
func (v *VaultSigner) Name() string { return "vault:" + v.mount }

// CA implements Signer, caching the public key.
func (v *VaultSigner) CA(ctx context.Context) (ssh.PublicKey, error) {
	v.mu.Lock()
	if v.ca != nil && time.Now().Before(v.caExpiry) {
		key := v.ca
		v.mu.Unlock()
		return key, nil
	}
	v.mu.Unlock()

	raw, err := v.client.PublicKey(ctx)
	if err != nil {
		return nil, err
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("ca: parse Vault CA public key: %w", err)
	}
	v.mu.Lock()
	v.ca = key
	v.caExpiry = time.Now().Add(v.cacheTTL)
	v.mu.Unlock()
	return key, nil
}

// Sign implements Signer. Only the public key leaves the process.
func (v *VaultSigner) Sign(ctx context.Context, req SignRequest) (*SignedCertificate, error) {
	if err := ValidateSignRequest(req); err != nil {
		return nil, err
	}
	authority, err := v.CA(ctx)
	if err != nil {
		return nil, err
	}
	signed, err := v.client.SignSSH(ctx, vault.SignRequest{
		PublicKey:       string(ssh.MarshalAuthorizedKey(req.PublicKey)),
		Principals:      req.Principals,
		CertType:        req.CertType,
		KeyID:           req.KeyID,
		TTL:             req.TTL,
		CriticalOptions: req.CriticalOptions,
		Extensions:      req.Extensions,
	})
	if err != nil {
		return nil, err
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(signed))
	if err != nil {
		return nil, fmt.Errorf("ca: parse signed certificate: %w", err)
	}
	cert, ok := pub.(*ssh.Certificate)
	if !ok {
		return nil, fmt.Errorf("ca: Vault returned %T, expected an SSH certificate", pub)
	}
	if err := CheckSigned(authority, req.Principals[0], cert); err != nil {
		return nil, err
	}
	return &SignedCertificate{
		Certificate: cert,
		PEM:         CertPEM(cert),
		CA:          authority,
		Serial:      cert.Serial,
	}, nil
}

// Health implements Signer.
func (v *VaultSigner) Health(ctx context.Context) error {
	_, err := v.client.Health(ctx)
	return err
}
