// Package crypto is the only package in BookMrkr that performs cryptographic
// operations. The vault is one authenticated envelope: a cleartext header
// carrying the KDF parameters, salt and nonce, followed by XChaCha20-Poly1305
// ciphertext. The header is authenticated as associated data, so the KDF
// parameters cannot be weakened without invalidating the file.
package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	magic = "BMRK1"
	// Version is the on-disk format version.
	Version = 1
	// SaltLen is the Argon2id salt length in bytes.
	SaltLen = 16
	// KeyLen is the derived key length in bytes.
	KeyLen = 32

	headerLen = 5 + 1 + 4 + 4 + 1 + SaltLen + chacha20poly1305.NonceSizeX // 55
)

// ErrBadVault covers a wrong password and a corrupt file alike. Callers must
// not distinguish the two: doing so would hand an attacker an oracle.
var ErrBadVault = errors.New("could not decrypt vault - wrong password or corrupt file")

// Params holds the Argon2id cost parameters, stored in the file header.
type Params struct {
	Time      uint32
	MemoryKiB uint32
	Threads   uint8
}

// Default costs roughly 100ms on a laptop. Paid only on unlock, never on
// routine commands, because the derived key is cached in the OS keychain.
var Default = Params{Time: 3, MemoryKiB: 64 * 1024, Threads: 4}

// NewSalt returns a fresh random salt.
func NewSalt() ([]byte, error) {
	s := make([]byte, SaltLen)
	if _, err := rand.Read(s); err != nil {
		return nil, err
	}
	return s, nil
}

// DeriveKey stretches a password into a KeyLen key with Argon2id.
func DeriveKey(password, salt []byte, p Params) []byte {
	return argon2.IDKey(password, salt, p.Time, p.MemoryKiB, p.Threads, KeyLen)
}

func header(p Params, salt, nonce []byte) []byte {
	h := make([]byte, 0, headerLen)
	h = append(h, magic...)
	h = append(h, Version)
	h = binary.BigEndian.AppendUint32(h, p.Time)
	h = binary.BigEndian.AppendUint32(h, p.MemoryKiB)
	h = append(h, p.Threads)
	h = append(h, salt...)
	h = append(h, nonce...)
	return h
}

// Seal encrypts plaintext into a complete vault blob.
func Seal(key, plaintext []byte, p Params, salt []byte) ([]byte, error) {
	if len(key) != KeyLen {
		return nil, fmt.Errorf("key must be %d bytes, got %d", KeyLen, len(key))
	}
	if len(salt) != SaltLen {
		return nil, fmt.Errorf("salt must be %d bytes, got %d", SaltLen, len(salt))
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	h := header(p, salt, nonce)
	return append(h, aead.Seal(nil, nonce, plaintext, h)...), nil
}

// SaltOf reads the salt and KDF parameters from a vault blob so a caller can
// derive the key from a password without first decrypting anything.
func SaltOf(blob []byte) ([]byte, Params, error) {
	if len(blob) < headerLen || string(blob[:len(magic)]) != magic || blob[5] != Version {
		return nil, Params{}, ErrBadVault
	}
	return blob[15:31], Params{
		Time:      binary.BigEndian.Uint32(blob[6:10]),
		MemoryKiB: binary.BigEndian.Uint32(blob[10:14]),
		Threads:   blob[14],
	}, nil
}

// Open authenticates and decrypts a vault blob.
func Open(key, blob []byte) ([]byte, error) {
	if _, _, err := SaltOf(blob); err != nil {
		return nil, err
	}
	if len(key) != KeyLen {
		return nil, ErrBadVault
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, ErrBadVault
	}
	out, err := aead.Open(nil, blob[31:headerLen], blob[headerLen:], blob[:headerLen])
	if err != nil {
		return nil, ErrBadVault
	}
	return out, nil
}
