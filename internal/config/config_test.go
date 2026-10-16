package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "serverdog.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(nil, env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Source:         SourceWardogServers,
		Listen:         ":8080",
		DataDir:        "./data",
		CacheFile:      filepath.Join("./data", "tracker.json"),
		BurstRate:      4,
		BurstBudget:    200,
		SweepRate:      1.5,
		Concurrency:    3,
		PollDelay:      10 * time.Second,
		StartingWindow: 20 * time.Minute,
		DropPct:        30,
		DropMin:        25,
	}
	if c != want {
		t.Errorf("defaults:\n got %+v\nwant %+v", c, want)
	}
}

func TestLoadPrecedence(t *testing.T) {
	file := writeFile(t, `{"listen": ":1", "burst_rate": 1, "poll_delay": "1s", "data_dir": "/file"}`)
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want func(Config) bool
	}{
		{"file beats default", []string{"--config", file}, nil,
			func(c Config) bool { return c.Listen == ":1" && c.BurstRate == 1 && c.PollDelay == time.Second }},
		{"env beats file", []string{"--config", file}, map[string]string{"SERVERDOG_LISTEN": ":2", "SERVERDOG_BURST_RATE": "2"},
			func(c Config) bool { return c.Listen == ":2" && c.BurstRate == 2 && c.PollDelay == time.Second }},
		{"flag beats env", []string{"--config", file, "--listen", ":3", "--poll-delay=3s"}, map[string]string{"SERVERDOG_LISTEN": ":2", "SERVERDOG_POLL_DELAY": "2s"},
			func(c Config) bool { return c.Listen == ":3" && c.PollDelay == 3*time.Second && c.BurstRate == 1 }},
		{"env beats default without file", nil, map[string]string{"SERVERDOG_SOURCE": "fake"},
			func(c Config) bool { return c.Source == SourceFake }},
		{"empty env falls through", []string{"--config", file}, map[string]string{"SERVERDOG_LISTEN": ""},
			func(c Config) bool { return c.Listen == ":1" }},
		{"config path from env", nil, map[string]string{"SERVERDOG_CONFIG": file},
			func(c Config) bool { return c.Listen == ":1" }},
		{"config flag beats config env", []string{"--config", file}, map[string]string{"SERVERDOG_CONFIG": "/missing.json"},
			func(c Config) bool { return c.Listen == ":1" }},
		{"cache_file follows data_dir", []string{"--config", file}, nil,
			func(c Config) bool { return c.CacheFile == filepath.Join("/file", "tracker.json") }},
		{"explicit cache_file wins", []string{"--config", file, "--cache-file", "/x/t.json"}, nil,
			func(c Config) bool { return c.CacheFile == "/x/t.json" }},
		{"every flag", []string{
			"--source", "fake", "--upstream-url", "https://example.test", "--listen", ":443",
			"--tls-cert", "c.pem", "--tls-key", "k.pem", "--data-dir", "/d", "--cache-file", "/d/c.json",
			"--burst-rate", "5", "--burst-budget", "50", "--sweep-rate", "0.5", "--concurrency", "2",
			"--poll-delay", "5s", "--starting-window", "15m", "--drop-pct", "40", "--drop-min", "10",
			"--user-agent-contact", "me@example.test",
		}, nil, func(c Config) bool {
			return c == Config{
				Source: "fake", UpstreamURL: "https://example.test", Listen: ":443", TLSCert: "c.pem", TLSKey: "k.pem",
				DataDir: "/d", CacheFile: "/d/c.json", BurstRate: 5, BurstBudget: 50, SweepRate: 0.5, Concurrency: 2,
				PollDelay: 5 * time.Second, StartingWindow: 15 * time.Minute, DropPct: 40, DropMin: 10,
				UserAgentContact: "me@example.test",
			}
		}},
		{"every env var", nil, map[string]string{
			"SERVERDOG_SOURCE": "fake", "SERVERDOG_UPSTREAM_URL": "http://127.0.0.1:9", "SERVERDOG_TLS_CERT": "c", "SERVERDOG_TLS_KEY": "k",
			"SERVERDOG_DATA_DIR": "/e", "SERVERDOG_BURST_BUDGET": "7", "SERVERDOG_SWEEP_RATE": "2", "SERVERDOG_CONCURRENCY": "4",
			"SERVERDOG_STARTING_WINDOW": "1h", "SERVERDOG_DROP_PCT": "50", "SERVERDOG_DROP_MIN": "0", "SERVERDOG_USER_AGENT_CONTACT": "x",
		}, func(c Config) bool {
			return c.Source == "fake" && c.UpstreamURL == "http://127.0.0.1:9" && c.TLSCert == "c" && c.TLSKey == "k" &&
				c.DataDir == "/e" && c.CacheFile == filepath.Join("/e", "tracker.json") && c.BurstBudget == 7 && c.SweepRate == 2 &&
				c.Concurrency == 4 && c.StartingWindow == time.Hour && c.DropPct == 50 && c.DropMin == 0 && c.UserAgentContact == "x"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(tt.args, env(tt.env))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !tt.want(c) {
				t.Errorf("unexpected config: %+v", c)
			}
		})
	}
}

func TestLoadFileEverySetting(t *testing.T) {
	file := writeFile(t, `{
		"source": "fake", "upstream_url": "https://example.test/api", "listen": ":9000",
		"tls_cert": "c", "tls_key": "k", "data_dir": "/f", "cache_file": "/f/x.json",
		"burst_rate": "2.5", "burst_budget": 10, "sweep_rate": 0.25, "concurrency": 1,
		"poll_delay": "30s", "starting_window": "10m", "drop_pct": 25, "drop_min": "5",
		"user_agent_contact": "ops"
	}`)
	c, err := Load([]string{"--config", file}, env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Source: "fake", UpstreamURL: "https://example.test/api", Listen: ":9000", TLSCert: "c", TLSKey: "k",
		DataDir: "/f", CacheFile: "/f/x.json", BurstRate: 2.5, BurstBudget: 10, SweepRate: 0.25, Concurrency: 1,
		PollDelay: 30 * time.Second, StartingWindow: 10 * time.Minute, DropPct: 25, DropMin: 5, UserAgentContact: "ops",
	}
	if c != want {
		t.Errorf("file:\n got %+v\nwant %+v", c, want)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		file    string // written and passed via --config when non-empty
		wantErr string
	}{
		{"unknown file key", nil, nil, `{"listen": ":1", "burst_rat": 4}`, `unknown key "burst_rat"`},
		{"file not an object", nil, nil, `[1, 2]`, "parse config file"},
		{"file malformed", nil, nil, `{"listen":`, "parse config file"},
		{"file bool value", nil, nil, `{"burst_rate": true}`, "burst_rate (file"},
		{"file null value", nil, nil, `{"listen": null}`, "listen (file"},
		{"file bad duration", nil, nil, `{"poll_delay": 10}`, "poll_delay (file"},
		{"missing config file", []string{"--config", filepath.Join(os.TempDir(), "serverdog-missing", "x.json")}, nil, "", "read config file"},
		{"unknown flag", []string{"--bogus", "1"}, nil, "", "bogus"},
		{"positional argument", []string{"extra"}, nil, "", `unexpected argument "extra"`},
		{"bad source", []string{"--source", "steam"}, nil, "", "source (flag --source)"},
		{"bad source env", nil, map[string]string{"SERVERDOG_SOURCE": "x"}, "", "source (env SERVERDOG_SOURCE)"},
		{"zero burst rate", []string{"--burst-rate", "0"}, nil, "", "burst_rate (flag --burst-rate): must be > 0"},
		{"nan sweep rate", []string{"--sweep-rate", "NaN"}, nil, "", "sweep_rate"},
		{"non-numeric rate", nil, map[string]string{"SERVERDOG_BURST_RATE": "fast"}, "", "burst_rate (env SERVERDOG_BURST_RATE)"},
		{"zero concurrency", []string{"--concurrency", "0"}, nil, "", "concurrency"},
		{"zero budget", []string{"--burst-budget", "0"}, nil, "", "burst_budget"},
		{"fractional budget", []string{"--burst-budget", "1.5"}, nil, "", "must be an integer"},
		{"drop_pct over 100", []string{"--drop-pct", "101"}, nil, "", "drop_pct"},
		{"drop_pct zero", []string{"--drop-pct", "0"}, nil, "", "drop_pct"},
		{"negative drop_min", []string{"--drop-min", "-1"}, nil, "", "drop_min"},
		{"bare number duration", []string{"--poll-delay", "10"}, nil, "", "poll_delay"},
		{"negative duration", []string{"--starting-window", "-5m"}, nil, "", "must be > 0"},
		{"relative upstream url", []string{"--upstream-url", "api.example.test"}, nil, "", "upstream_url"},
		{"ftp upstream url", []string{"--upstream-url", "ftp://example.test"}, nil, "", "upstream_url"},
		{"empty listen", []string{"--listen", ""}, nil, "", "listen"},
		{"empty data_dir in file", nil, nil, `{"data_dir": ""}`, "data_dir"},
		{"control chars in contact", nil, map[string]string{"SERVERDOG_USER_AGENT_CONTACT": "a\x00b"}, "", "user_agent_contact"},
		{"tls cert without key", []string{"--tls-cert", "c.pem"}, nil, "", "tls_cert and tls_key"},
		{"tls key without cert", nil, map[string]string{"SERVERDOG_TLS_KEY": "k"}, "", "tls_cert and tls_key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args
			if tt.file != "" {
				args = append([]string{"--config", writeFile(t, tt.file)}, args...)
			}
			_, err := Load(args, env(tt.env))
			if err == nil {
				t.Fatalf("Load succeeded, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadHelp(t *testing.T) {
	_, err := Load([]string{"-h"}, env(nil))
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("Load(-h) error = %v, want flag.ErrHelp", err)
	}
}

// TestDeployExampleConfig keeps deploy/serverdog.example.json loadable as the config keys change.
func TestDeployExampleConfig(t *testing.T) {
	c, err := Load([]string{"--config", filepath.Join("..", "..", "deploy", "serverdog.example.json")}, env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Listen != "127.0.0.1:8080" {
		t.Errorf("Listen = %q, want loopback 127.0.0.1:8080 (Apache proxies to it)", c.Listen)
	}
	if c.TLSCert != "" || c.TLSKey != "" {
		t.Errorf("TLS set (%q, %q), want unset: Apache terminates TLS", c.TLSCert, c.TLSKey)
	}
	if c.DataDir != "/var/lib/serverdog" {
		t.Errorf("DataDir = %q, want the unit's StateDirectory /var/lib/serverdog", c.DataDir)
	}
}
