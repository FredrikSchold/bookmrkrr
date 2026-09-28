package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/FredrikSchold/bookmrkrr/internal/config"
	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/keyring"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
)

// readPassword is a seam so tests can supply a password without a terminal.
//
// keyring.PromptPassword writes its prompt straight to standard error rather
// than through errOut, because internal/keyring knows nothing about this
// package's streams. That is tolerable only because it is the one prompt path:
// a test that replaces this variable never reaches it. Do not add a second one.
var readPassword = keyring.PromptPassword

// openVault resolves the data directory and returns a vault handle whose key
// comes from the keychain, or from a password prompt when nothing is cached.
func openVault() (*store.Vault, error) {
	dir, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	if !store.Exists(dir) {
		return nil, errNoVault(dir)
	}
	key, err := keyFor(dir)
	if err != nil {
		return nil, err
	}
	return store.New(dir, key), nil
}

// errNoVault is the one place the "there is no vault yet" refusal is worded, so
// the commands that check for one cannot drift apart.
func errNoVault(dir string) error {
	return fmt.Errorf("no vault found in %s - run 'bkmr init' to create one", dir)
}

// keyFor returns the cached key, or derives one from a prompted password. It
// does not cache what it derives; only 'bkmr unlock' does that.
func keyFor(dir string) ([]byte, error) {
	if key, err := keyring.Get(); err == nil {
		return key, nil
	}
	return deriveFromPrompt(dir)
}

func deriveFromPrompt(dir string) ([]byte, error) {
	blob, err := os.ReadFile(store.New(dir, nil).Path())
	if err != nil {
		return nil, err
	}
	salt, params, err := crypto.SaltOf(blob)
	if err != nil {
		return nil, err
	}
	pw, err := readPassword("Vault password: ")
	if err != nil {
		return nil, err
	}
	return crypto.DeriveKey(pw, salt, params), nil
}

// explainVaultError rewrites the vault errors whose own text would leave a
// person stuck. Presenting them is the CLI's job, not internal/store's, so
// every command that reads or writes the vault routes its error through here
// and the advice lives in one place instead of at each call site. Anything it
// does not recognise is passed through untouched, nil included.
func explainVaultError(err error) error {
	if errors.Is(err, store.ErrBusy) {
		return fmt.Errorf("%w - wait for it to finish and try again", store.ErrBusy)
	}
	// A sharing violation on the rename that publishes a new vault arrives as
	// *os.LinkError, not ErrBusy: bkmr's own lock file was free, so something
	// outside bkmr - a backup agent, an editor, a virus scanner - is holding
	// the file open. This is routine on Windows. The default text names a .tmp
	// file the user never asked about and offers no way forward, so keep the
	// platform's reason and drop the rest. %w, not %v: the underlying error is
	// what a caller would match against - fs.ErrPermission, say - and there is
	// no reason to break the chain to reword the wrapper.
	var le *os.LinkError
	if errors.As(err, &le) {
		return fmt.Errorf("could not replace %s: %w - something else has the file open; close it and try again", le.New, le.Err)
	}
	// A vault that will not open is the one failure a user can meet with no idea
	// what to do next, and bkmr has been keeping the previous contents in
	// vault.bkmr.bak the whole time without ever mentioning it. This is where they
	// meet the failure, so this is where the pointer belongs.
	//
	// Conditional on nothing, and phrased as an "if". crypto.ErrBadVault names
	// both causes in one sentence on purpose - a wrong password and a damaged file
	// are told apart by nothing bkmr prints - because a message that distinguished
	// them would confirm to whoever holds the file that a given password was
	// merely wrong. Appending advice only when the file is genuinely corrupt would
	// rebuild exactly that oracle, so the advice is appended always and the
	// judgement about which case this is stays with the user.
	if errors.Is(err, crypto.ErrBadVault) {
		return fmt.Errorf("%w; if you believe the file is damaged, %s.bak holds the previous contents - copy it over %s and try again",
			err, store.FileName, store.FileName)
	}
	return err
}
