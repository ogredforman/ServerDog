package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// EnvPrefix is prepended to the upper-cased setting key to form its environment variable,
// e.g. burst_rate → SERVERDOG_BURST_RATE.
const EnvPrefix = "SERVERDOG_"

// Sources accepted by the source setting.
const (
	SourceWardogServers = "wardogservers"
	SourceFake          = "fake"
)

// Config holds every setting from SPEC §8 Configuration.
type Config struct {
	Source      string // wardogservers or fake
	UpstreamURL string // empty: the provider adapter's built-in URL
	Listen      string
	TLSCert     string // with TLSKey: serve HTTPS; both empty: plain HTTP
	TLSKey      string
	DataDir     string
	CacheFile   string // defaults to <DataDir>/tracker.json

	BurstRate   float64 // detail requests per second during the burst
	BurstBudget int     // max detail requests per burst
	SweepRate   float64 // detail requests per second during the sweep
	Concurrency int     // parallel detail requests during the burst

	PollDelay      time.Duration // wait after FetchedAt + RefreshEvery before polling
	StartingWindow time.Duration // how long after a change a server stays P1
	DropPct        int           // sharp player drop: percent of previous players
	DropMin        int           // sharp player drop: absolute players

	UserAgentContact string // optional contact appended to the User-Agent
}

// setting describes one configuration key. Every layer (default, file, env, flag) hands its
// value to set as a string, so parsing and validation live in one place.
type setting struct {
	key   string
	def   string
	usage string
	set   func(c *Config, v string) error
}

// settings lists every key in SPEC §8 Configuration with its default.
func settings() []setting {
	return []setting{
		{"source", SourceWardogServers, "server-list provider: wardogservers or fake", func(c *Config, v string) error {
			if v != SourceWardogServers && v != SourceFake {
				return fmt.Errorf("must be %q or %q, got %q", SourceWardogServers, SourceFake, v)
			}
			c.Source = v
			return nil
		}},
		{"upstream_url", "", "provider base URL (empty: provider default)", func(c *Config, v string) error {
			if v != "" {
				u, err := url.Parse(v)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					return fmt.Errorf("must be an absolute http(s) URL, got %q", v)
				}
			}
			c.UpstreamURL = v
			return nil
		}},
		{"listen", ":8080", "HTTP listen address", func(c *Config, v string) error {
			if v == "" {
				return errors.New("must not be empty")
			}
			c.Listen = v
			return nil
		}},
		{"tls_cert", "", "TLS certificate file (with tls_key)", func(c *Config, v string) error { c.TLSCert = v; return nil }},
		{"tls_key", "", "TLS key file (with tls_cert)", func(c *Config, v string) error { c.TLSKey = v; return nil }},
		{"data_dir", "./data", "directory for servers.json", func(c *Config, v string) error {
			if v == "" {
				return errors.New("must not be empty")
			}
			c.DataDir = v
			return nil
		}},
		{"cache_file", "", "tracker cache file (default <data_dir>/tracker.json)", func(c *Config, v string) error { c.CacheFile = v; return nil }},
		{"burst_rate", "4", "burst detail requests per second", func(c *Config, v string) error { return parseRate(&c.BurstRate, v) }},
		{"burst_budget", "200", "max detail requests per burst", func(c *Config, v string) error { return parseInt(&c.BurstBudget, v, 1, math.MaxInt32) }},
		{"sweep_rate", "1.5", "sweep detail requests per second", func(c *Config, v string) error { return parseRate(&c.SweepRate, v) }},
		{"concurrency", "3", "parallel burst detail requests", func(c *Config, v string) error { return parseInt(&c.Concurrency, v, 1, 64) }},
		{"poll_delay", "10s", "delay after the provider's next refresh before polling", func(c *Config, v string) error { return parseDuration(&c.PollDelay, v) }},
		{"starting_window", "20m", "how long after a change to keep rechecking a server", func(c *Config, v string) error { return parseDuration(&c.StartingWindow, v) }},
		{"drop_pct", "30", "sharp player drop: percent (1-100)", func(c *Config, v string) error { return parseInt(&c.DropPct, v, 1, 100) }},
		// 0 is allowed: it disables the absolute floor and leaves only drop_pct.
		{"drop_min", "25", "sharp player drop: minimum players", func(c *Config, v string) error { return parseInt(&c.DropMin, v, 0, math.MaxInt32) }},
		{"user_agent_contact", "", "contact added to the User-Agent", func(c *Config, v string) error {
			// It ends up in an HTTP header, so control characters (CR/LF injection) are rejected.
			if strings.IndexFunc(v, unicode.IsControl) >= 0 {
				return errors.New("must not contain control characters")
			}
			c.UserAgentContact = v
			return nil
		}},
	}
}

func parseRate(dst *float64, v string) error {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("must be a number, got %q", v)
	}
	if f <= 0 {
		return fmt.Errorf("must be > 0, got %v", f)
	}
	*dst = f
	return nil
}

func parseInt(dst *int, v string, lo, hi int) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("must be an integer, got %q", v)
	}
	if n < lo || n > hi {
		return fmt.Errorf("must be between %d and %d, got %d", lo, hi, n)
	}
	*dst = n
	return nil
}

func parseDuration(dst *time.Duration, v string) error {
	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("must be a duration like 10s or 20m, got %q", v)
	}
	if d <= 0 {
		return fmt.Errorf("must be > 0, got %v", d)
	}
	*dst = d
	return nil
}

// flagName turns a setting key into its flag name: burst_rate → burst-rate.
func flagName(key string) string { return strings.ReplaceAll(key, "_", "-") }

// envName turns a setting key into its environment variable: burst_rate → SERVERDOG_BURST_RATE.
func envName(key string) string { return EnvPrefix + strings.ToUpper(key) }

// Load builds the Config from command-line args (without the program name), environment
// variables read through getenv, an optional JSON file named by --config or SERVERDOG_CONFIG,
// and defaults, highest priority first. Unknown file keys and invalid values are errors.
// A -h/--help flag returns an error wrapping flag.ErrHelp.
func Load(args []string, getenv func(string) string) (Config, error) {
	settings := settings()
	fs := flag.NewFlagSet("serverdog", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "optional JSON config file")
	flagVals := make(map[string]*string, len(settings))
	for _, s := range settings {
		flagVals[s.key] = fs.String(flagName(s.key), "", s.usage)
	}
	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("config: unexpected argument %q", fs.Arg(0))
	}
	flagSet := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { flagSet[f.Name] = true })

	var c Config
	for _, s := range settings {
		if err := s.set(&c, s.def); err != nil {
			return Config{}, fmt.Errorf("config: %s (default): %w", s.key, err)
		}
	}

	// An explicit --config (even --config=) overrides SERVERDOG_CONFIG.
	path := *configPath
	if !flagSet["config"] {
		path = getenv(EnvPrefix + "CONFIG")
	}
	if path != "" {
		if err := applyFile(&c, settings, path); err != nil {
			return Config{}, err
		}
	}

	for _, s := range settings {
		name := envName(s.key)
		if v, ok := lookup(getenv, name); ok {
			if err := s.set(&c, v); err != nil {
				return Config{}, fmt.Errorf("config: %s (env %s): %w", s.key, name, err)
			}
		}
	}

	for _, s := range settings {
		if flagSet[flagName(s.key)] {
			if err := s.set(&c, *flagVals[s.key]); err != nil {
				return Config{}, fmt.Errorf("config: %s (flag --%s): %w", s.key, flagName(s.key), err)
			}
		}
	}

	if (c.TLSCert == "") != (c.TLSKey == "") {
		return Config{}, errors.New("config: tls_cert and tls_key must be set together")
	}
	if c.CacheFile == "" {
		c.CacheFile = filepath.Join(c.DataDir, "tracker.json")
	}
	return c, nil
}

// lookup treats an empty environment variable as unset, so SERVERDOG_X= falls through to
// the file and defaults.
func lookup(getenv func(string) string, name string) (string, bool) {
	v := getenv(name)
	return v, v != ""
}

// applyFile reads a JSON object of setting keys. Values may be JSON strings or numbers.
func applyFile(c *Config, settings []setting, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read config file: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("config: parse config file %s: %w", path, err)
	}
	known := make(map[string]setting, len(settings))
	for _, s := range settings {
		known[s.key] = s
	}
	// Report unknown keys before any value errors, and in a stable order.
	keys := slices.Sorted(maps.Keys(raw))
	for _, key := range keys {
		if _, ok := known[key]; !ok {
			return fmt.Errorf("config: unknown key %q in config file %s", key, path)
		}
	}
	for _, key := range keys {
		v, err := jsonScalar(raw[key])
		if err == nil {
			err = known[key].set(c, v)
		}
		if err != nil {
			return fmt.Errorf("config: %s (file %s): %w", key, path, err)
		}
	}
	return nil
}

// jsonScalar returns a JSON string's contents or a JSON number's literal text.
func jsonScalar(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String(), nil
	}
	return "", fmt.Errorf("must be a string or number, got %s", raw)
}
