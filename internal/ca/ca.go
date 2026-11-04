// Package ca defines the certificate authority boundary. The SSH gateway and
// API only ever call Signer; when Vault is enabled the CA private key never
// enters this process.
package ca

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

// SignRequest describes a certificate to issue.
type SignRequest struct {
	PublicKey       ssh.PublicKey
	Principals      []string
	CertType        string // user (host certificates are out of scope in v0.1)
	TTL             time.Duration
	KeyID           string
	CriticalOptions map[string]string
	Extensions      map[string]string
}

// SignedCertificate is the result of signing.
type SignedCertificate struct {
	Certificate *ssh.Certificate
	PEM         []byte
	CA          ssh.PublicKey
	Serial      uint64
}

// Signer is a certificate authority backend.
type Signer interface {
	// Name identifies the backend (for example "vault:ssh-client-signer").
	Name() string
	// CA returns the authority public key.
	CA(ctx context.Context) (ssh.PublicKey, error)
	// Sign signs a public key. Private key material is never accepted.
	Sign(ctx context.Context, req SignRequest) (*SignedCertificate, error)
	// Health reports backend availability.
	Health(ctx context.Context) error
}

// ValidateSignRequest performs backend-independent checks.
func ValidateSignRequest(req SignRequest) error {
	if req.PublicKey == nil {
		return errors.New("ca: public key is required")
	}
	if len(req.Principals) == 0 {
		return errors.New("ca: at least one principal is required")
	}
	if req.TTL < time.Minute || req.TTL > 24*time.Hour {
		return fmt.Errorf("ca: TTL must be between 1m and 24h, got %s", req.TTL)
	}
	if req.CertType == "" {
		return errors.New("ca: certificate type is required")
	}
	if req.CertType != "user" {
		return fmt.Errorf("ca: unsupported certificate type %q", req.CertType)
	}
	return nil
}

// CertPEM serialises a certificate in OpenSSH authorized_keys format.
func CertPEM(cert *ssh.Certificate) []byte {
	return ssh.MarshalAuthorizedKey(cert)
}

// CheckSigned verifies that cert is a valid user certificate from authority.
func CheckSigned(authority ssh.PublicKey, principal string, cert *ssh.Certificate) error {
	checker := &ssh.CertChecker{
		IsUserAuthority: func(key ssh.PublicKey) bool {
			return ssh.FingerprintSHA256(key) == ssh.FingerprintSHA256(authority)
		},
	}
	if err := checker.CheckCert(principal, cert); err != nil {
		return fmt.Errorf("ca: signed certificate is invalid: %w", err)
	}
	return nil
}
