package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the user's config.toml. Every field has a working default, so a
// missing or malformed file is never fatal.
type Config struct {
	// Network is "on" (the default) or "off". Off disables title fetching
	// entirely, which leaves the binary making no outbound connections.
	Network networkMode `toml:"network"`

	// Problem is set when Load could not use a config file that exists, and is
	// empty otherwise. Load cannot return an error, but a kill switch that
	// stopped working in silence is its own kind of failure: whoever asks for
	// the config is expected to put this in front of the user. A missing file
	// is not a problem - that is the ordinary case, and warning about it on
	// every add would be noise.
	Problem string `toml:"-"`
}

// networkMode is the network setting as written in config.toml.
type networkMode string

// UnmarshalTOML accepts a TOML boolean as well as a string, because
// network = false is the natural thing to write and a type error against a
// string field would fall back to the default - silently turning fetching back
// on for someone who asked for it off.
func (n *networkMode) UnmarshalTOML(v interface{}) error {
	switch t := v.(type) {
	case string:
		*n = networkMode(t)
	case bool:
		if t {
			*n = "on"
		} else {
			*n = "off"
		}
	default:
		return fmt.Errorf("network must be a string or a boolean, got %T", v)
	}
	return nil
}

// Default is the configuration used when no file is present.
var Default = Config{Network: "on"}

// Load reads config.toml. Any problem - missing file, bad TOML, unreadable
// path - yields Default rather than an error, because a config problem must
// not stop someone saving a bookmark.
//
// The default is deliberately the permissive one. A typo in config.toml must
// not silently disable the network either: a user who turned fetching off and
// then broke the file gets it back on, and the README's promise is about what
// the tool does by default, not about what a damaged file happens to say. What
// stops that from being silent is Problem, which every caller is expected to
// report.
func Load() Config {
	path, err := ConfigPath()
	if err != nil {
		return withProblem(fmt.Sprintf("could not work out where config.toml lives (%v); using defaults", err))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default
		}
		return withProblem(fmt.Sprintf("could not read %s (%v); using defaults", path, err))
	}
	cfg := Default
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return withProblem(fmt.Sprintf("%s is not valid config (%v); using defaults, so the network is on", path, err))
	}
	if cfg.Network == "" {
		cfg.Network = Default.Network
	}
	return cfg
}

func withProblem(msg string) Config {
	cfg := Default
	cfg.Problem = msg
	return cfg
}

// NetworkEnabled reports whether outbound requests are permitted.
//
// The spellings someone reaching for a kill switch is likely to write all count
// as off. Matching only the exact string "off" would mean "Off" and "no" left
// the network on, which is the wrong way round for a mistake to fail.
func (c Config) NetworkEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(string(c.Network))) {
	case "off", "false", "no":
		return false
	}
	return true
}
