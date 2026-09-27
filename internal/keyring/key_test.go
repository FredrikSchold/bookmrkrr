package keyring

import (
	"bytes"
	"errors"
	"testing"

	kr "github.com/zalando/go-keyring"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
)

func TestPutGetDeleteRoundTrip(t *testing.T) {
	kr.MockInit()
	key := bytes.Repeat([]byte{7}, crypto.KeyLen)

	if err := Put(key); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	got, err := Get()
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Errorf("Get() = %x, want %x", got, key)
	}
	if err := Delete(); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := Get(); !errors.Is(err, ErrNoKey) {
		t.Errorf("Get() after Delete() error = %v, want ErrNoKey", err)
	}
}

func TestGetWithNothingStoredReportsErrNoKey(t *testing.T) {
	kr.MockInit()

	if _, err := Get(); !errors.Is(err, ErrNoKey) {
		t.Errorf("Get() error = %v, want ErrNoKey", err)
	}
}

func TestDeleteWithNothingStoredIsNotAnError(t *testing.T) {
	kr.MockInit()

	if err := Delete(); err != nil {
		t.Errorf("Delete() error = %v, want nil when no key is cached", err)
	}
}

func TestPutRejectsAWrongLengthKey(t *testing.T) {
	kr.MockInit()

	if err := Put([]byte("short")); err == nil {
		t.Error("Put() error = nil, want an error for a wrong-length key")
	}
}
