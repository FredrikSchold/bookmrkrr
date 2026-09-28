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

	// Problem is set when Load could not fully use a config file that exists -
	// it was unreadable, it was not valid TOML, or it declared a key bkmr does
	// not recognize - and is empty otherwise. Load cannot return an error, but a
	// kill switch that stopped working in silence is its own kind of failure:
	// whoever asks for the config is expected to put this in front of the user,
	// whether or not they were going to use the network. A missing file is not a
	// problem - that is the ordinary case, and warning about it on every add
	// would be noise.
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
	// Decode rather than Unmarshal, for the metadata: a misspelled key is valid
	// TOML, so netwrok = "off" would otherwise parse cleanly, leave Network at
	// its default and hand the user a kill switch that does nothing, silently.
	// Anything the file declares that nothing here consumed is reported.
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return withProblem(fmt.Sprintf("%s is not valid config (%v); using defaults, so the network is on", path, err))
	}
	if cfg.Network == "" {
		cfg.Network = Default.Network
	}
	// A reported problem, not an error: whatever else the file said still
	// applies, so a stray key does not cost someone the settings they got right.
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		cfg.Problem = fmt.Sprintf("config %s has %s %s that bkmr does not recognize, so %s no effect - check the spelling",
			path, keyWord(len(undecoded)), quoteKeys(undecoded), hasWord(len(undecoded)))
	}
	return cfg
}

func quoteKeys(keys []toml.Key) string {
	quoted := make([]string, 0, len(keys))
	for _, k := range keys {
		quoted = append(quoted, fmt.Sprintf("%q", k.String()))
	}
	return strings.Join(quoted, ", ")
}

func keyWord(n int) string {
	if n == 1 {
		return "a key"
	}
	return "keys"
}

func hasWord(n int) string {
	if n == 1 {
		return "it has"
	}
	return "they have"
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
