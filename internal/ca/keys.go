package ca

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// LoadAuthorizedKeys reads one or more files containing OpenSSH public keys or
// certificates. Blank lines and comments are ignored.
func LoadAuthorizedKeys(paths []string) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("ca: read public key file %s: %w", path, err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
			if err != nil {
				return nil, fmt.Errorf("ca: parse key in %s: %w", path, err)
			}
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("ca: no public keys found in %v", paths)
	}
	return keys, nil
}

// Fingerprint returns the SHA-256 fingerprint of a key.
func Fingerprint(key ssh.PublicKey) string {
	return ssh.FingerprintSHA256(key)
}
