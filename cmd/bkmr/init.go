package main

import (
	"bytes"
	"fmt"

	"github.com/FredrikSchold/bookmrkrr/internal/config"
	"github.com/FredrikSchold/bookmrkrr/internal/crypto"
	"github.com/FredrikSchold/bookmrkrr/internal/keyring"
	"github.com/FredrikSchold/bookmrkrr/internal/store"
)

func init() {
	register(command{
		Name:    "init",
		Summary: "create the vault and set its password",
		Usage:   "bkmr init",
		Run:     runInit,
	})
}

func runInit([]string) error {
	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	if store.Exists(dir) {
		return fmt.Errorf("a vault already exists in %s", dir)
	}

	pw, err := readPassword("New vault password: ")
	if err != nil {
		return err
	}
	again, err := readPassword("Confirm password: ")
	if err != nil {
		return err
	}
	if !bytes.Equal(pw, again) {
		return fmt.Errorf("the passwords do not match")
	}

	salt, err := crypto.NewSalt()
	if err != nil {
		return err
	}
	// One crypto.Params value, used for the derivation and handed to Create for
	// the header, so the two cannot drift: every later unlock derives from the
	// parameters it reads back out of that header, and a header that disagrees
	// with the derivation would leave the vault unopenable by its own password.
	params := crypto.Default
	key := crypto.DeriveKey(pw, salt, params)
	if err := store.Create(dir, key, salt, params); err != nil {
		return err
	}
	// A keychain that will not take the key is not a reason to throw the vault
	// away - it exists and the password opens it. Report both halves of what
	// happened, the platform's own error included, so the user knows why every
	// command from here on asks for a password. The halves go to different
	// streams: what init achieved is output, why caching failed is a
	// diagnostic, and nothing in this package puts a diagnostic in the pipe.
	if err := keyring.Put(key); err != nil {
		fmt.Fprintf(out, "Vault created in %s.\n", dir)
		fmt.Fprintf(errOut, "bkmr: could not cache the key (%v); you will be prompted each time.\n", err)
		return nil
	}
	fmt.Fprintf(out, "Vault created in %s and unlocked.\nAdd your first bookmark with 'bkmr add <url>'.\n", dir)
	return nil
}
