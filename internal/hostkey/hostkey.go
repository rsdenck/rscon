// Package hostkey verifies SSH destination host keys using known_hosts,
// trusted host CAs and explicit SHA-256 fingerprints. Unverified connections
// are never trusted by default.
package hostkey

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Verifier builds per-destination host key callbacks.
type Verifier struct {
	checker    *ssh.CertChecker
	fallback   ssh.HostKeyCallback
	require    bool
	hasSources bool
}

// New builds a verifier. knownHostsFile may be empty; hostCAFiles are OpenSSH
// public keys used as @cert-authority anchors.
func New(knownHostsFile string, hostCAFiles []string, require bool) (*Verifier, error) {
	v := &Verifier{require: require}
	var authorities []ssh.PublicKey
	for _, path := range hostCAFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("hostkey: read host CA %s: %w", path, err)
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey(bytes.TrimSpace(data))
		if err != nil {
			return nil, fmt.Errorf("hostkey: parse host CA %s: %w", path, err)
		}
		authorities = append(authorities, key)
	}
	if knownHostsFile != "" {
		if _, err := os.Stat(knownHostsFile); err == nil {
			cb, err := knownhosts.New(knownHostsFile)
			if err != nil {
				return nil, fmt.Errorf("hostkey: load known_hosts: %w", err)
			}
			v.fallback = cb
			v.hasSources = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("hostkey: stat known_hosts: %w", err)
		}
	}
	if len(authorities) > 0 {
		v.hasSources = true
	}
	if require && !v.hasSources {
		return nil, errors.New("hostkey: verification is required but no known_hosts file or host CA is available")
	}
	v.checker = &ssh.CertChecker{
		IsHostAuthority: func(auth ssh.PublicKey, address string) bool {
			for _, ca := range authorities {
				if bytes.Equal(ca.Marshal(), auth.Marshal()) {
					return true
				}
			}
			return false
		},
		HostKeyFallback: v.fallback,
	}
	return v, nil
}

// Callback returns a host key callback. A non-empty pin is enforced before any
// certificate or known_hosts evaluation.
func (v *Verifier) Callback(pin string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if pin != "" {
			plain := key
			if cert, ok := key.(*ssh.Certificate); ok {
				plain = cert.Key
			}
			if ssh.FingerprintSHA256(plain) != pin {
				return fmt.Errorf("hostkey: fingerprint mismatch for %s", hostname)
			}
			if _, ok := key.(*ssh.Certificate); ok {
				return v.checker.CheckHostKey(hostname, remote, key)
			}
			return nil
		}
		err := v.checker.CheckHostKey(hostname, remote, key)
		if err == nil {
			return nil
		}
		if !v.require {
			var keyErr *knownhosts.KeyError
			if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
				return nil // unknown host accepted only in explicit non-production mode
			}
		}
		return fmt.Errorf("hostkey: cannot verify %s: %w", hostname, err)
	}
}

// RequireVerification reports whether the verifier is mandatory.
func (v *Verifier) RequireVerification() bool { return v.require }
