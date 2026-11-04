package ca

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

// FileSigner is a development-only CA that keeps the private key on the local
// filesystem. Production configuration validation rejects it.
type FileSigner struct {
	signer ssh.Signer
	name   string
}

// NewFileSigner loads an OpenSSH private key.
func NewFileSigner(keyFile string) (*FileSigner, error) {
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("ca: read file CA key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("ca: parse file CA key: %w", err)
	}
	return &FileSigner{signer: signer, name: "file:" + keyFile}, nil
}

// Name implements Signer.
func (f *FileSigner) Name() string { return f.name }

// CA implements Signer.
func (f *FileSigner) CA(context.Context) (ssh.PublicKey, error) { return f.signer.PublicKey(), nil }

// Health implements Signer.
func (f *FileSigner) Health(context.Context) error { return nil }

// Sign implements Signer.
func (f *FileSigner) Sign(_ context.Context, req SignRequest) (*SignedCertificate, error) {
	if err := ValidateSignRequest(req); err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	cert := &ssh.Certificate{
		Key:             req.PublicKey,
		CertType:        ssh.UserCert,
		KeyId:           req.KeyID,
		ValidPrincipals: req.Principals,
		ValidAfter:      uint64(now.Add(-30 * time.Second).Unix()),
		ValidBefore:     uint64(now.Add(req.TTL).Unix()),
		Serial:          serial,
		Permissions: ssh.Permissions{
			CriticalOptions: req.CriticalOptions,
			Extensions:      req.Extensions,
		},
	}
	if cert.Permissions.CriticalOptions == nil {
		cert.Permissions.CriticalOptions = map[string]string{}
	}
	if cert.Permissions.Extensions == nil {
		cert.Permissions.Extensions = map[string]string{}
	}
	if err := cert.SignCert(rand.Reader, f.signer); err != nil {
		return nil, fmt.Errorf("ca: sign certificate: %w", err)
	}
	if err := CheckSigned(f.signer.PublicKey(), req.Principals[0], cert); err != nil {
		return nil, err
	}
	return &SignedCertificate{
		Certificate: cert,
		PEM:         CertPEM(cert),
		CA:          f.signer.PublicKey(),
		Serial:      cert.Serial,
	}, nil
}

func randomSerial() (uint64, error) {
	var b [8]byte
	for i := 0; i < 4; i++ {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("ca: generate serial: %w", err)
		}
		v := binary.BigEndian.Uint64(b[:])
		if v != 0 {
			return v, nil
		}
	}
	return 0, errors.New("ca: failed to generate a non-zero serial")
}
