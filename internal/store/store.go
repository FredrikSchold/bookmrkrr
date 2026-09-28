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

// renameFile is a seam so a test can fail a rename that publishes a vault -
// Create's and write's both, which is how the tests simulate an interrupted
// write. It is never reassigned outside tests, and a test that swaps it must
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
//
// A zero-byte file is the one other thing that counts as absent, and it is not
// an exception to that rule so much as an application of it: crypto.Seal cannot
// produce zero bytes - a sealed vault is a header, a nonce and a tag before it
// holds anything at all - so an empty vault.bkmr is not a vault whose contents
// we might destroy. It is the reservation Create makes before it publishes,
// left behind by an init that was killed in the one rename between the two. See
// emptyFile.
func Exists(dir string) bool {
	path := filepath.Join(dir, FileName)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err == nil && emptyFile(info) {
		return false
	}
	return true
}

// emptyFile reports whether info describes a regular file of no bytes. It is the
// one predicate for "a reservation Create left behind rather than a vault", so
// that Exists and Create cannot come to disagree about what they are looking at:
// a file Exists calls absent is a file Create is entitled to replace, and the
// two answers have to be the same answer.
func emptyFile(info fs.FileInfo) bool { return info.Mode().IsRegular() && info.Size() == 0 }

// Create writes a new empty vault. It refuses to overwrite an existing one.
//
// params goes into the file header and must be the parameters key was derived
// with. Passing it rather than documenting "derive with crypto.Default" is the
// point: a header that disagrees with its own key derivation produces a vault
// that no later unlock can open - every unlock reads the parameters back out of
// the header - and a defect that severe should not be left to a caller
// remembering a comment. The same reasoning put one bounds predicate behind both
// Seal and Open.
func Create(dir string, key, salt []byte, params crypto.Params) error {
	path := filepath.Join(dir, FileName)
	plain, err := json.Marshal(&model.Collection{Version: model.Version, Bookmarks: []model.Bookmark{}})
	if err != nil {
		return err
	}
	blob, err := crypto.Seal(key, plain, params, salt)
	if err != nil {
		return err
	}

	// Sealed and written in full before anything claims the vault's own name, then
	// renamed into place, exactly as write does. The old protocol wrote the blob
	// straight into the file that O_EXCL had just created, so an init killed
	// partway through - during the fsync, most likely, which is the slow part -
	// left a short vault.bkmr behind. Every command would then call that file
	// corrupt, and bkmr init would refuse to replace it, because Exists() was the
	// only thing it consulted: a mistyped Ctrl-C and the user is stuck deleting a
	// file by hand.
	tmp := path + ".tmp"
	if err := writeFileSynced(tmp, blob); err != nil {
		return err
	}

	// The refusal stays a single syscall rather than becoming a Stat and a
	// rename. O_EXCL is what makes it atomic: with a separate check, a vault that
	// came into existence in the gap would be replaced by a fresh empty
	// collection, and the user's bookmarks would be gone with no error to show
	// for it. What the O_EXCL creates is an empty reservation, and the rename
	// below replaces it with the real thing.
	//
	// An existing empty file is that same reservation from an init that died in
	// the window between these two steps. It is not a vault - see emptyFile - so
	// it is reclaimed rather than treated as somebody's bookmarks.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	switch {
	case err == nil:
		if err := f.Close(); err != nil {
			os.Remove(tmp)
			os.Remove(path)
			return err
		}
	case os.IsExist(err):
		info, statErr := os.Stat(path)
		if statErr != nil || !emptyFile(info) {
			os.Remove(tmp)
			return fmt.Errorf("a vault already exists at %s", path)
		}
	default:
		os.Remove(tmp)
		return err
	}

	if err := renameFile(tmp, path); err != nil {
		os.Remove(tmp)
		// The reservation goes too. Leaving it would mean Exists() reports a
		// vault that cannot be opened, which is the state this whole protocol
		// exists to avoid.
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

// Mutate reloads the vault under the write lock, applies fn, and saves the
// result. It is the only way to write the vault, deliberately: there is no
// Load-then-Save pair, because a concurrent add made in between would be
// silently discarded, and because the Load here is what keeps a wrong key from
// destroying the vault.
//
// That second property is not obvious and is worth stating. keyFor in cmd/bkmr
// derives a key from a typed password without verifying it - only 'bkmr unlock'
// verifies - so the key this Vault holds may be wrong, and nothing in the write
// path can tell. Load is what finds out: it fails with crypto.ErrBadVault before
// fn runs and before anything is written. A writer that skipped the Load would
// reseal a collection under the wrong key and rename it over the user's vault,
// turning one mistyped password into total data loss. So there is exactly one
// way in.
//
// fn must not call Mutate itself. The lock is a plain file-creation lock with no
// reentrancy, so a nested writer would spin against a lock this same goroutine
// is holding and return ErrBusy a second later.
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

// write seals c and replaces the vault file. Mutate is its only caller, so this
// is the one point every writer passes through - which is why c.Clean() is
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
	if err := writeFileSynced(tmp, sealed); err != nil {
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
	if err := writeFileSynced(tmp, data); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// writeFileSynced writes data to path in full, fsyncs it, and removes the file
// again if any step fails, so a caller that is about to rename it into place can
// treat success as "the bytes are on the disk" and failure as "nothing is".
//
// One copy rather than three: Create, write and writeBackup all need exactly
// this, and the sequence is easy to get subtly wrong - an fsync that is skipped,
// a handle that is not closed on the error path, a temp file left behind for the
// next run to trip over. It does not rename anything; each caller publishes its
// own way, which is the one part of the protocol that genuinely differs between
// them.
func writeFileSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
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
