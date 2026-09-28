package main

import (
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
		return errNoVault(dir)
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
	// A failed delete is a failed lock, and nothing here can soften that.
	// Asking keyring.Get whether a key survived would be worse than useless:
	// Get reports ErrNoKey for a keychain that is absent and for one that is
	// merely locked alike, so a locked macOS Keychain still holding the key
	// would answer "nothing cached" and we would claim a lock that never
	// happened - and the key would be live again the moment the keychain
	// opened. So say only what is known, and keep "Vault locked." for a delete
	// that actually returned.
	if err := keyring.Delete(); err != nil {
		return fmt.Errorf("could not reach the system keychain (%w); if a key is cached there it may still be present, and bkmr will prompt until it can be dropped", err)
	}
	fmt.Fprintln(out, "Vault locked.")
	return nil
}
