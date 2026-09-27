package crypto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fastParams keeps unit tests quick. The golden test deliberately uses Default.
var fastParams = Params{Time: 1, MemoryKiB: 8 * 1024, Threads: 1}

func TestSealOpenRoundTrip(t *testing.T) {
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	key := DeriveKey([]byte("correct horse battery staple"), salt, fastParams)
	plain := []byte(`{"version":1,"bookmarks":[]}`)

	blob, err := Seal(key, plain, fastParams, salt)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Contains(blob, []byte("bookmarks")) {
		t.Fatal("Seal() left plaintext visible in the output")
	}

	got, err := Open(key, blob)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("Open() = %q, want %q", got, plain)
	}
}

func TestOpenWithWrongPasswordFails(t *testing.T) {
	salt, _ := NewSalt()
	blob, err := Seal(DeriveKey([]byte("right"), salt, fastParams), []byte("secret"), fastParams, salt)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Open(DeriveKey([]byte("wrong"), salt, fastParams), blob)
	if !errors.Is(err, ErrBadVault) {
		t.Errorf("Open() error = %v, want ErrBadVault", err)
	}
}

func TestOpenDetectsFlippedCiphertextByte(t *testing.T) {
	salt, _ := NewSalt()
	key := DeriveKey([]byte("pw"), salt, fastParams)
	blob, _ := Seal(key, []byte("secret payload"), fastParams, salt)

	blob[len(blob)-1] ^= 0x01

	if _, err := Open(key, blob); !errors.Is(err, ErrBadVault) {
		t.Errorf("Open() error = %v, want ErrBadVault", err)
	}
}

func TestOpenRejectsDowngradedKDFParams(t *testing.T) {
	salt, _ := NewSalt()
	key := DeriveKey([]byte("pw"), salt, fastParams)
	blob, _ := Seal(key, []byte("secret"), fastParams, salt)

	// Rewrite the memory cost in the header. It is authenticated as
	// associated data, so the vault must refuse to open.
	binary.BigEndian.PutUint32(blob[10:14], 8)

	if _, err := Open(key, blob); !errors.Is(err, ErrBadVault) {
		t.Errorf("Open() error = %v, want ErrBadVault", err)
	}
}

// Review Focus 1: a truncated or empty vault must not panic.
func TestOpenHandlesShortAndEmptyBlobs(t *testing.T) {
	key := make([]byte, KeyLen)
	for _, blob := range [][]byte{nil, {}, make([]byte, 30), make([]byte, headerLen)} {
		if _, err := Open(key, blob); !errors.Is(err, ErrBadVault) {
			t.Errorf("Open(%d bytes) error = %v, want ErrBadVault", len(blob), err)
		}
		if _, _, err := SaltOf(blob); !errors.Is(err, ErrBadVault) {
			t.Errorf("SaltOf(%d bytes) error = %v, want ErrBadVault", len(blob), err)
		}
	}
}

func TestSaltOfReturnsHeaderValues(t *testing.T) {
	salt, _ := NewSalt()
	key := DeriveKey([]byte("pw"), salt, fastParams)
	blob, _ := Seal(key, []byte("x"), fastParams, salt)

	gotSalt, gotParams, err := SaltOf(blob)
	if err != nil {
		t.Fatalf("SaltOf() error = %v", err)
	}
	if !bytes.Equal(gotSalt, salt) {
		t.Errorf("SaltOf() salt = %x, want %x", gotSalt, salt)
	}
	if gotParams != fastParams {
		t.Errorf("SaltOf() params = %+v, want %+v", gotParams, fastParams)
	}
}

const goldenPassword = "correct horse battery staple"
const goldenPlaintext = `{"version":1,"bookmarks":[{"id":"aaaaaaaa","url":"https://example.com/","added":"2026-09-27T00:00:00Z"}]}`

// TestGoldenVaultStillOpens pins format version 1 forever. Regenerate the
// fixture only when intentionally introducing a NEW version, never to make
// this test pass.
func TestGoldenVaultStillOpens(t *testing.T) {
	path := filepath.Join("testdata", "golden_v1.bkmr")

	if os.Getenv("BKMR_WRITE_GOLDEN") == "1" {
		salt, _ := NewSalt()
		blob, err := Seal(DeriveKey([]byte(goldenPassword), salt, Default), []byte(goldenPlaintext), Default, salt)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("golden fixture written")
	}

	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden fixture: %v (regenerate with BKMR_WRITE_GOLDEN=1)", err)
	}
	salt, params, err := SaltOf(blob)
	if err != nil {
		t.Fatalf("SaltOf(golden) error = %v", err)
	}
	got, err := Open(DeriveKey([]byte(goldenPassword), salt, params), blob)
	if err != nil {
		t.Fatalf("Open(golden) error = %v", err)
	}
	if string(got) != goldenPlaintext {
		t.Errorf("Open(golden) = %s, want %s", got, goldenPlaintext)
	}
}
