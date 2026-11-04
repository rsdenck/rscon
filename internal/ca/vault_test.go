package ca

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rsdenck/rscon/internal/vault"
	"golang.org/x/crypto/ssh"
)

func newTestCA(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return signer, sshPub
}

func signTestCert(t *testing.T, authority ssh.Signer, req map[string]any) string {
	t.Helper()
	pubLine, _ := req["public_key"].(string)
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubLine))
	if err != nil {
		t.Fatalf("parse user key: %v", err)
	}
	principals := []string{}
	for _, p := range req["valid_principals"].([]any) {
		principals = append(principals, p.(string))
	}
	ttl := 10 * time.Minute
	cert := &ssh.Certificate{
		Key:             key,
		CertType:        ssh.UserCert,
		KeyId:           req["key_id"].(string),
		ValidPrincipals: principals,
		ValidAfter:      uint64(time.Now().Add(-time.Minute).Unix()),
		ValidBefore:     uint64(time.Now().Add(ttl).Unix()),
		Serial:          424242,
		Permissions:     ssh.Permissions{Extensions: map[string]string{"rscon-role": "user"}},
	}
	if err := cert.SignCert(rand.Reader, authority); err != nil {
		t.Fatalf("sign test cert: %v", err)
	}
	return string(ssh.MarshalAuthorizedKey(cert))
}

func TestVaultSigner(t *testing.T) {
	authority, authorityPub := newTestCA(t)
	var logins int
	var signedRequest map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		logins++
		writeJSON(t, w, map[string]any{"auth": map[string]any{"client_token": "test-token", "lease_duration": 3600}})
	})
	mux.HandleFunc("/v1/auth/token/renew-self", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"auth": map[string]any{"client_token": "test-token", "lease_duration": 3600}})
	})
	mux.HandleFunc("/v1/ssh-client-signer/public_key", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"public_key": string(ssh.MarshalAuthorizedKey(authorityPub))}})
	})
	mux.HandleFunc("/v1/ssh-client-signer/sign/rscon-users", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&signedRequest); err != nil {
			t.Errorf("decode sign request: %v", err)
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"signed_key": signTestCert(t, authority, signedRequest)}})
	})
	mux.HandleFunc("/v1/sys/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"initialized": true, "sealed": false, "version": "1.18.0"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := vault.New(vault.Config{
		Address: server.URL, Mount: "ssh-client-signer", Role: "rscon-users",
		RoleID: "role-id", SecretID: "secret-id", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	signer := NewVaultSigner(client, "ssh-client-signer", time.Minute)

	userPub, userPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = userPriv
	userKey, _ := ssh.NewPublicKey(userPub)
	cert, err := signer.Sign(context.Background(), SignRequest{
		PublicKey:  userKey,
		Principals: []string{"alice"},
		CertType:   "user",
		TTL:        10 * time.Minute,
		KeyID:      "alice",
		Extensions: map[string]string{"rscon-role": "user"},
	})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if cert.Certificate.Serial != 424242 {
		t.Fatalf("serial = %d", cert.Certificate.Serial)
	}
	if cert.Certificate.KeyId != "alice" {
		t.Fatalf("key id = %q", cert.Certificate.KeyId)
	}
	if logins != 1 {
		t.Fatalf("expected one AppRole login, got %d", logins)
	}
	if err := signer.Health(context.Background()); err != nil {
		t.Fatalf("Health() = %v", err)
	}

	// The CA public key must be cached: a second call does not hit the API.
	before := logins
	if _, err := signer.CA(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logins != before {
		t.Fatal("CA() should not re-login when cached")
	}
}

func TestVaultSignerRejectsUnconfigured(t *testing.T) {
	client, err := vault.New(vault.Config{Address: "https://127.0.0.1:1", Mount: "ssh", Role: "r"})
	if err != nil {
		t.Fatal(err)
	}
	signer := NewVaultSigner(client, "ssh", time.Minute)
	_, err = signer.CA(context.Background())
	if err == nil {
		t.Fatal("expected error contacting unreachable Vault")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode: %v", err)
	}
}
