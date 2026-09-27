package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the user's config.toml. Every field has a working default, so a
// missing or malformed file is never fatal.
type Config struct {
	// Network is "on" (the default) or "off". Off disables title fetching
	// entirely, which leaves the binary making no outbound connections.
	Network string `toml:"network"`
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
// the tool does by default, not about what a damaged file happens to say.
func Load() Config {
	path, err := ConfigPath()
	if err != nil {
		return Default
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Default
	}
	cfg := Default
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Default
	}
	if cfg.Network == "" {
		cfg.Network = Default.Network
	}
	return cfg
}

// NetworkEnabled reports whether outbound requests are permitted.
func (c Config) NetworkEnabled() bool { return c.Network != "off" }
