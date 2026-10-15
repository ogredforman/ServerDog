package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// capture runs fn and returns what it wrote to stdout.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}

func TestGuardBash(t *testing.T) {
	tests := []struct {
		name  string
		cmd   string
		noCgo bool
		want  string // substring of output; "" means no output
	}{
		{"plain command", "go test ./...", true, ""},
		{"cgo denied", "CGO_ENABLED=1 go build ./...", true, `"deny"`},
		{"cgo allowed without flag", "CGO_ENABLED=1 go build ./...", false, ""},
		{"go get asks", "go get github.com/go-chi/chi/v5", false, `"ask"`},
		{"chained go get asks", "cd x && go get example.com/y", false, `"ask"`},
		{"npm i pkg asks", "npm i svelte", false, `"ask"`},
		{"npm install bare is fine", "cd web && npm install", false, ""},
		{"npm ci is fine", "npm ci", false, ""},
		{"pnpm add asks", "pnpm add zod", false, `"ask"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := capture(t, func() { guardBash(tt.cmd, tt.noCgo) })
			if tt.want == "" && out != "" {
				t.Fatalf("expected no output, got %s", out)
			}
			if tt.want != "" && !strings.Contains(out, tt.want) {
				t.Fatalf("expected %s in output, got %q", tt.want, out)
			}
		})
	}
}

func TestGuardEdit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel string) string {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	git("init", "-q")
	committed := write("db/migrations/00001_init.sql")
	other := write("internal/app.go")
	git("add", "-A")
	git("commit", "-qm", "init")
	uncommitted := write("db/migrations/00002_next.sql")

	protect := []string{"db/migrations/"}
	tests := []struct {
		name    string
		file    string
		protect []string
		deny    bool
	}{
		{"committed protected file", committed, protect, true},
		{"uncommitted protected file", uncommitted, protect, false},
		{"unprotected file", other, protect, false},
		{"no protect flags", committed, nil, false},
		{"protect without trailing slash", committed, []string{"db/migrations"}, true},
		{"outside project", "/etc/hosts", protect, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := capture(t, func() { guardEdit(root, tt.file, tt.protect) })
			if got := strings.Contains(out, `"deny"`); got != tt.deny {
				t.Fatalf("deny = %v, want %v (output %q)", got, tt.deny, out)
			}
		})
	}
}

func TestRelPath(t *testing.T) {
	root := t.TempDir()
	if got := relPath(root, filepath.Join(root, "a", "b.go")); got != "a/b.go" {
		t.Fatalf("got %q", got)
	}
	if got := relPath(root, "a/b.go"); got != "a/b.go" {
		t.Fatalf("relative input: got %q", got)
	}
	if got := relPath(root, filepath.Dir(root)); got != "" {
		t.Fatalf("outside root: got %q", got)
	}
}
