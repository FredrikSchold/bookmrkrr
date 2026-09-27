package main

import (
	"errors"
	"fmt"

	"github.com/FredrikSchold/bookmrkrr/internal/config"
	"github.com/FredrikSchold/bookmrkrr/internal/keyring"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
)

func init() {
	register(command{
		Name:    "unlock",
		Summary: "derive the key from your password and cache it",
		Usage:   "bkmr unlock",
		Run:     runUnlock,
	})
	register(command{
		Name:    "lock",
		Summary: "drop the cached key",
		Usage:   "bkmr lock",
		Run:     runLock,
	})
}

func runUnlock([]string) error {
	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	if !store.Exists(dir) {
		return fmt.Errorf("no vault found in %s - run 'bkmr init' to create one", dir)
	}
	key, err := deriveFromPrompt(dir)
	if err != nil {
		return err
	}
	// Verify before caching, so a typo never gets stored.
	if _, err := store.New(dir, key).Load(); err != nil {
		return explainVaultError(err)
	}
	// Caching is the whole point of unlock, so a failure here is a failure of
	// the command - but say so in full. keyring.Get reports ErrNoKey for every
	// failure alike, so a silently discarded Put would leave the user reprompted
	// on every command forever with nothing to explain it.
	if err := keyring.Put(key); err != nil {
		return fmt.Errorf("could not cache the key in the system keychain: %w - the password was right, but bkmr will prompt for it on every command", err)
	}
	fmt.Fprintln(out, "Vault unlocked.")
	return nil
}

func runLock([]string) error {
	if err := keyring.Delete(); err != nil {
		// The keychain refused the delete. lock's promise is that no cached key
		// is left for bkmr to use, so ask whether one is still readable rather
		// than dumping a dbus or wincred error and calling it a failure:
		// keyring.Get reports ErrNoKey for every platform failure too, so a box
		// with no credential store at all answers "nothing cached" here, which
		// is the truth. Explain why the delete itself did not run, and leave
		// "there was nothing to lock" unsaid - it might not be true.
		if _, getErr := keyring.Get(); !errors.Is(getErr, keyring.ErrNoKey) {
			return fmt.Errorf("could not drop the cached key from the system keychain: %w", err)
		}
		fmt.Fprintf(errOut, "bkmr: the system keychain is unavailable (%v); no cached key was reachable.\n", err)
	}
	fmt.Fprintln(out, "Vault locked.")
	return nil
}
