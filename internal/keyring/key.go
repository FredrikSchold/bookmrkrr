// Package keyring caches the derived vault key in the operating system's
// credential store so routine commands never prompt for a password.
//
// The DERIVED KEY is stored, never the passphrase: a compromised keychain
// then yields something usable only against this vault, not a secret the
// user may have reused elsewhere.
package keyring

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	kr "github.com/zalando/go-keyring"
	"golang.org/x/term"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
)

const (
	service = "bookmrkrr"
	user    = "vault-key"
)

// ErrNoKey means no key is cached, so the caller should prompt.
var ErrNoKey = errors.New("no cached key - run 'bkmr unlock'")

// Put caches the derived key.
func Put(key []byte) error {
	if len(key) != crypto.KeyLen {
		return fmt.Errorf("key must be %d bytes, got %d", crypto.KeyLen, len(key))
	}
	return kr.Set(service, user, base64.StdEncoding.EncodeToString(key))
}

// Get returns the cached key, or ErrNoKey. Any platform failure - no Secret
// Service on a headless Linux box, for instance - also reports ErrNoKey, so
// the caller falls back to prompting instead of crashing.
func Get() ([]byte, error) {
	s, err := kr.Get(service, user)
	if err != nil {
		return nil, ErrNoKey
	}
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(key) != crypto.KeyLen {
		return nil, ErrNoKey
	}
	return key, nil
}

// Delete drops the cached key. Deleting nothing is not an error.
func Delete() error {
	if err := kr.Delete(service, user); err != nil && !errors.Is(err, kr.ErrNotFound) {
		return err
	}
	return nil
}

// PromptPassword reads a password from the terminal without echoing it.
func PromptPassword(prompt string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("cannot prompt for a password: stdin is not a terminal")
	}
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	if len(pw) == 0 {
		return nil, errors.New("empty password")
	}
	return pw, nil
}
