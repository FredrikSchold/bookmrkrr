// Package store reads and writes the encrypted vault file. It knows nothing
// about bookmarks beyond the fact that they serialize to JSON.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/model"
)

// FileName is the vault's name inside the data directory.
const FileName = "vault.bkmr"

// ErrBusy means another bkmr process holds the write lock.
var ErrBusy = errors.New("vault is busy - another bkmr is writing to it")

// Vault is an encrypted bookmark collection on disk.
type Vault struct {
	dir string
	key []byte
}

// New returns a Vault handle. It performs no I/O.
func New(dir string, key []byte) *Vault { return &Vault{dir: dir, key: key} }

// Path is the vault file's full path.
func (v *Vault) Path() string { return filepath.Join(v.dir, FileName) }

// Exists reports whether a vault file is present in dir.
func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil
}

// Create writes a new empty vault. It refuses to overwrite an existing one.
//
// The new vault's header records crypto.Default, so the caller must have
// derived key with crypto.Default and the same salt. Nothing in this signature
// enforces that: a caller who derives with different parameters writes a file
// whose header disagrees with its own key derivation, and every later unlock -
// which reads the parameters back out of the header - would derive a different
// key and be told the vault is corrupt.
func Create(dir string, key, salt []byte) error {
	path := filepath.Join(dir, FileName)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("a vault already exists at %s", path)
	}
	plain, err := json.Marshal(&model.Collection{Version: model.Version, Bookmarks: []model.Bookmark{}})
	if err != nil {
		return err
	}
	blob, err := crypto.Seal(key, plain, crypto.Default, salt)
	if err != nil {
		return err
	}
	return os.WriteFile(path, blob, 0o600)
}

// Load decrypts and parses the vault.
func (v *Vault) Load() (*model.Collection, error) {
	blob, err := os.ReadFile(v.Path())
	if err != nil {
		return nil, err
	}
	plain, err := crypto.Open(v.key, blob)
	if err != nil {
		return nil, err
	}
	var c model.Collection
	if err := json.Unmarshal(plain, &c); err != nil {
		return nil, crypto.ErrBadVault
	}
	return &c, nil
}

// Save encrypts and atomically replaces the vault, keeping one backup. It
// reuses the salt and KDF parameters already in the file so a cached key
// stays valid.
func (v *Vault) Save(c *model.Collection) error {
	release, err := v.lock()
	if err != nil {
		return err
	}
	defer release()
	return v.write(c)
}

// Mutate reloads the vault under the write lock, applies fn, and saves the
// result. Every writer must use this rather than Load-then-Save, or a
// concurrent add made in between would be silently discarded.
func (v *Vault) Mutate(fn func(*model.Collection) error) error {
	release, err := v.lock()
	if err != nil {
		return err
	}
	defer release()

	c, err := v.Load()
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	return v.write(c)
}

func (v *Vault) write(c *model.Collection) error {
	blob, err := os.ReadFile(v.Path())
	if err != nil {
		return err
	}
	salt, params, err := crypto.SaltOf(blob)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return err
	}
	sealed, err := crypto.Seal(v.key, plain, params, salt)
	if err != nil {
		return err
	}

	tmp := v.Path() + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(sealed); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(v.Path(), v.Path()+".bak"); err != nil && !os.IsNotExist(err) {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, v.Path())
}

// lock takes an advisory lock via exclusive file creation.
//
// ponytail: a crash leaves vault.lock behind and the user must delete it.
// Upgrade to a PID-stamped lock with staleness detection only if that
// actually bites someone.
func (v *Vault) lock() (func(), error) {
	path := filepath.Join(v.dir, "vault.lock")
	deadline := time.Now().Add(time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, ErrBusy
		}
		time.Sleep(20 * time.Millisecond)
	}
}
