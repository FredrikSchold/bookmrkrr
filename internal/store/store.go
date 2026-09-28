// Package store reads and writes the encrypted vault file. It knows nothing
// about bookmarks beyond the fact that they serialize to JSON.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// renameFile is a seam so a test can fail the one rename that publishes a new
// vault. It is never reassigned outside tests, and a test that swaps it must
// not call t.Parallel() or run alongside anything else in this package: it is a
// plain package-level variable with no synchronisation, so concurrent use would
// be a genuine data race the moment CI runs with -race.
var renameFile = os.Rename

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
//
// Only a genuine "not found" counts as absent. A permission problem or an I/O
// fault means we cannot prove there is no vault, and answering false would let
// bkmr init - which consults nothing else - create a fresh empty vault on top of
// a real one.
func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, FileName))
	return !errors.Is(err, fs.ErrNotExist)
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
	plain, err := json.Marshal(&model.Collection{Version: model.Version, Bookmarks: []model.Bookmark{}})
	if err != nil {
		return err
	}
	blob, err := crypto.Seal(key, plain, crypto.Default, salt)
	if err != nil {
		return err
	}

	// O_EXCL, not Stat-then-write. The refusal has to be the same syscall as the
	// create: with a separate check, a vault that came into existence in the gap
	// would be truncated to a fresh empty collection, and the user's bookmarks
	// would be gone with no error to show for it. Sealing first also means a
	// failure there leaves no file behind at all.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("a vault already exists at %s", path)
		}
		return err
	}
	if _, err := f.Write(blob); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
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
//
// fn must not call Save or Mutate itself. The lock is a plain file-creation
// lock with no reentrancy, so a nested writer would spin against a lock this
// same goroutine is holding and return ErrBusy a second later.
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

// write seals c and replaces the vault file. Save and Mutate both end here, so
// this is the one point every writer passes through - which is why c.Clean() is
// called here rather than left to each command.
//
// model.Collection.Add cleans control characters out of a bookmark it inserts,
// but an insert is not the only way text reaches the vault: Find returns a
// writable *Bookmark and 'bkmr edit' assigns straight through it. Asking every
// present and future writer to remember model.CleanTitle is a rule that lapses;
// cleaning at the gate is a rule that cannot. This does not make store know
// anything new about bookmarks - what "clean" means stays entirely inside
// model - it only declines to encrypt a collection that has not been asked to
// tidy itself first.
//
// Titles, notes and tags, then - and not URLs. Clean deliberately does not
// touch Bookmark.URL, because a URL cannot be cleaned: dropping a byte out of
// one changes where it points, so model.NormalizeURL refuses a bad URL instead
// of repairing it, and refusal is not something this gate can do. It would have
// to either discard the user's bookmark on the way to disk - silent data loss -
// or fail the write, which would leave a vault that already held one such entry
// permanently unsaveable. URLs are validated where they enter instead:
// model.Collection.add calls NormalizeURL on every insert and nothing else in
// the tree assigns Bookmark.URL. Any future path that writes a URL outside add
// must call model.NormalizeURL itself; this gate will not catch it.
func (v *Vault) write(c *model.Collection) error {
	c.Clean()

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
	// Copy the current vault to .bak rather than renaming it there. A rename
	// would unlink vault.bkmr, and if the rename below then failed the vault
	// would be absent from disk entirely: the next command would report "no
	// vault found", and bkmr init - which only checks Exists() - would create a
	// fresh empty vault on top of the user's bookmarks.
	if err := writeBackup(v.Path()+".bak", blob); err != nil {
		os.Remove(tmp)
		return err
	}
	// The one publishing step. Rename replaces the existing vault atomically on
	// every supported platform, so a reader sees either the old file or the new
	// one and never a partial write.
	if err := renameFile(tmp, v.Path()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// writeBackup replaces path with data atomically, via a sibling temp file that
// is fsynced and then renamed into place.
//
// os.WriteFile would truncate the existing backup before writing, so a crash
// partway through would destroy the backup already on disk rather than merely
// fail to produce a new one. That is the one property the old rename-based
// protocol had, and this restores it without reintroducing the window where no
// vault file exists.
//
// This calls os.Rename directly rather than the renameFile seam on purpose. The
// seam exists so a test can fail the rename that publishes a new vault; routing
// this one through it as well would make that test abort here instead, before it
// ever reached the step it means to exercise.
func writeBackup(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
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
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
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
