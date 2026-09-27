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
	//
	// 4096 is deliberately mid-range: it must stay inside the bounds SaltOf
	// enforces, so that the Poly1305 tag is the only thing that can reject this
	// blob. A value on or outside a bound would let SaltOf fail first and this
	// test would keep passing while no longer exercising the AD binding at all.
	binary.BigEndian.PutUint32(blob[10:14], 4096)

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

func TestSaltOfRejectsHostileKDFParams(t *testing.T) {
	salt, _ := NewSalt()
	key := DeriveKey([]byte("pw"), salt, fastParams)
	good, err := Seal(key, []byte("x"), fastParams, salt)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(b []byte)
	}{
		{"zero time cost panics argon2", func(b []byte) { binary.BigEndian.PutUint32(b[6:10], 0) }},
		{"zero threads panics argon2", func(b []byte) { b[14] = 0 }},
		{"absurd memory cost would exhaust RAM", func(b []byte) { binary.BigEndian.PutUint32(b[10:14], 1<<22) }},
		{"absurd time cost would hang", func(b []byte) { binary.BigEndian.PutUint32(b[6:10], 1<<20) }},
		{"memory cost below argon2 minimum", func(b []byte) { binary.BigEndian.PutUint32(b[10:14], 1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob := append([]byte(nil), good...)
			tt.mutate(blob)
			if _, _, err := SaltOf(blob); !errors.Is(err, ErrBadVault) {
				t.Errorf("SaltOf() error = %v, want ErrBadVault", err)
			}
			if _, err := Open(key, blob); !errors.Is(err, ErrBadVault) {
				t.Errorf("Open() error = %v, want ErrBadVault", err)
			}
		})
	}
}

// Seal and Open must be inverses over everything Seal accepts. If Seal writes a
// blob whose header sits outside the bounds SaltOf enforces, that file can never
// be opened again and the user is told their good vault is corrupt.
func TestSealRejectsOutOfRangeParams(t *testing.T) {
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	key := DeriveKey([]byte("pw"), salt, fastParams)

	tests := []struct {
		name string
		p    Params
	}{
		{"time above ceiling", Params{Time: 100, MemoryKiB: 64 * 1024, Threads: 4}},
		{"zero time", Params{Time: 0, MemoryKiB: 64 * 1024, Threads: 4}},
		{"threads above ceiling", Params{Time: 3, MemoryKiB: 64 * 1024, Threads: 65}},
		{"zero threads", Params{Time: 3, MemoryKiB: 64 * 1024, Threads: 0}},
		{"memory below floor", Params{Time: 3, MemoryKiB: 1, Threads: 4}},
		{"memory above ceiling", Params{Time: 3, MemoryKiB: 1 << 22, Threads: 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Seal(key, []byte("x"), tt.p, salt)
			if err == nil {
				t.Fatal("Seal() succeeded with out-of-range params; the blob it wrote could never be opened again")
			}
			// The write path takes programmer input, not attacker input, so it
			// should say what is wrong rather than hide behind ErrBadVault.
			if errors.Is(err, ErrBadVault) {
				t.Errorf("Seal() error = %v, want a descriptive error rather than ErrBadVault", err)
			}
		})
	}
}

const goldenPassword = "correct horse battery staple"
const goldenPlaintext = `{"version":1,"bookmarks":[{"id":"aaaaaaaa","url":"https://example.com/","added":"2026-09-27T00:00:00Z"}]}`

// TestGoldenVaultStillOpens pins format version 1 forever. Regenerate the
// fixture only when intentionally introducing a NEW version, never to make
// this test pass.
func TestGoldenVaultStillOpens(t *testing.T) {
	path := filepath.Join("testdata", "golden_v1.bkmr")

	// A regenerating run must never be able to report PASS. Re-sealing the
	// fixture and then asserting against it proves nothing - it would only show
	// that Seal and Open agree with each other right now, which is the one thing
	// this test is not for. Failing unconditionally means a stray export or a CI
	// job that inherits BKMR_WRITE_GOLDEN is loud instead of silently destroying
	// the artifact that pins the format.
	if os.Getenv("BKMR_WRITE_GOLDEN") == "1" {
		salt, err := NewSalt()
		if err != nil {
			t.Fatal(err)
		}
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
		t.Fatal("golden fixture regenerated; unset BKMR_WRITE_GOLDEN and re-run so this test actually verifies it")
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
