// Command hooks implements the Claude Code hooks for this repo.
//
// It is its own Go module so it never becomes part of the app build, and it is
// written in Go (not bash + jq) so it behaves the same on Linux, macOS and Windows.
//
// Usage (wired up in .claude/settings.json):
//
//	go -C "$CLAUDE_PROJECT_DIR/tools/hooks" run . <subcommand> [flags]
//
// Subcommands:
//
//	format      PostToolUse (Edit|Write|MultiEdit): gofmt Go files; prettier for files under
//	            --web-dir (default "web") when <web-dir>/node_modules has prettier installed.
//	guard-edit  PreToolUse  (Edit|Write|MultiEdit): deny edits to files that are committed
//	            under any --protect dir (repeatable), e.g. --protect internal/db/migrations/
//	guard-bash  PreToolUse  (Bash): ask before adding dependencies; with --no-cgo, deny CGO_ENABLED=1.
//	check       Stop: go build + go vet must pass before Claude finishes.
//
// This file is part of the devflow Go preset. Copy tools/hooks into a project and adjust
// the flags in .claude/settings.json rather than editing this program.
//
// Decisions are reported with Claude Code's JSON hook output on stdout and the
// process always exits 0. (Exit code 2 can't be used: `go run` reports any
// non-zero child exit as 1, which Claude Code treats as a non-blocking error.)
// Any internal failure is treated as "proceed" so a broken hook never wedges a session.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type hookInput struct {
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	StopHookActive bool            `json:"stop_hook_active"`
}

type toolInput struct {
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "hooks: missing subcommand")
		os.Exit(0)
	}

	raw, _ := io.ReadAll(os.Stdin)
	var in hookInput
	_ = json.Unmarshal(raw, &in)
	var ti toolInput
	_ = json.Unmarshal(in.ToolInput, &ti)

	root := projectDir(in.Cwd)

	fs := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	var protect multiFlag
	fs.Var(&protect, "protect", "directory whose committed files may not be edited (repeatable)")
	noCgo := fs.Bool("no-cgo", false, "deny commands that set CGO_ENABLED=1")
	webDir := fs.String("web-dir", "web", "frontend directory for prettier formatting")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return // bad flags: fail open
	}

	switch os.Args[1] {
	case "format":
		format(root, ti.FilePath, *webDir)
	case "guard-edit":
		guardEdit(root, ti.FilePath, protect)
	case "guard-bash":
		guardBash(ti.Command, *noCgo)
	case "check":
		check(root, in.StopHookActive)
	default:
		fmt.Fprintf(os.Stderr, "hooks: unknown subcommand %q\n", os.Args[1])
	}
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// preToolUse reports a permission decision ("deny" or "ask") for the pending tool call.
func preToolUse(decision, reason string) {
	emit(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       decision,
			"permissionDecisionReason": reason,
		},
	})
}

// block tells Claude to address reason before continuing (PostToolUse and Stop).
func block(reason string) {
	emit(map[string]any{"decision": "block", "reason": reason})
}

func emit(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }

func projectDir(cwd string) string {
	if d := os.Getenv("CLAUDE_PROJECT_DIR"); d != "" {
		return d
	}
	if cwd != "" {
		return cwd
	}
	d, _ := os.Getwd()
	return d
}

// relPath returns p relative to root using forward slashes, or "" if p is outside root.
func relPath(root, p string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// --- format -----------------------------------------------------------------

var webExts = map[string]bool{
	".ts": true, ".js": true, ".svelte": true, ".css": true, ".json": true, ".html": true,
}

func format(root, file, webDir string) {
	rel := relPath(root, file)
	if rel == "" {
		return
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	ext := strings.ToLower(filepath.Ext(rel))

	switch {
	case ext == ".go":
		if _, err := exec.LookPath("gofmt"); err != nil {
			return
		}
		out, err := run(root, 30*time.Second, "gofmt", "-w", abs)
		if err != nil {
			// Usually a syntax error. Tell Claude so it fixes it now.
			block(fmt.Sprintf("gofmt failed on %s (likely a syntax error):\n%s", rel, trim(out, 20)))
		}
	case webDir != "" && strings.HasPrefix(rel, strings.TrimSuffix(webDir, "/")+"/") && webExts[ext]:
		name := "prettier"
		if runtime.GOOS == "windows" {
			name = "prettier.cmd"
		}
		web := filepath.Join(root, filepath.FromSlash(webDir))
		prettier := filepath.Join(web, "node_modules", ".bin", name)
		if _, err := os.Stat(prettier); err != nil {
			return // frontend not set up yet
		}
		// Formatting problems in web files are not worth blocking on.
		_, _ = run(web, 30*time.Second, prettier, "--write", abs)
	}
}

// --- guard-edit -------------------------------------------------------------

func guardEdit(root, file string, protect []string) {
	rel := relPath(root, file)
	if rel == "" {
		return
	}
	protected := false
	for _, dir := range protect {
		dir = strings.TrimSuffix(filepath.ToSlash(dir), "/") + "/"
		if strings.HasPrefix(rel, dir) {
			protected = true
			break
		}
	}
	if !protected {
		return
	}
	// Is this exact file already committed? (HEAD may not exist in a fresh repo.)
	if _, err := run(root, 10*time.Second, "git", "cat-file", "-e", "HEAD:"+rel); err != nil {
		return // new or uncommitted migration: editing is fine
	}
	preToolUse("deny", fmt.Sprintf(
		"%s is committed in a protected directory (e.g. applied migrations) and must not be edited. "+
			"Create a new file instead. See .claude/rules/ for this project's conventions.", rel))
}

// --- guard-bash -------------------------------------------------------------

var (
	cgoOn  = regexp.MustCompile(`CGO_ENABLED\s*=\s*1`)
	addDep = regexp.MustCompile(`(^|[;&|]\s*|\s)(go\s+get\s|npm\s+(install|i|add)\s+[^-\s]|pnpm\s+add\s|yarn\s+add\s|bun\s+add\s)`)
)

func guardBash(cmd string, noCgo bool) {
	switch {
	case cmd == "":
	case noCgo && cgoOn.MatchString(cmd):
		preToolUse("deny", "This project must build without CGO. Find a pure-Go approach instead.")
	case addDep.MatchString(cmd):
		// Don't block: ask the user to approve, with the reason visible.
		preToolUse("ask", "Adds a dependency. This project prefers the standard library; confirm this dependency is needed.")
	}
}

// --- check (Stop) -----------------------------------------------------------

func check(root string, stopHookActive bool) {
	// If we already blocked once and Claude is continuing because of it,
	// don't loop forever: let it stop and let the human look.
	if stopHookActive {
		return
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return // Stage 0 hasn't created the module yet
	}
	if !goFilesChanged(root) {
		return
	}
	if out, err := run(root, 3*time.Minute, "go", "build", "./..."); err != nil {
		block("`go build ./...` failed. Fix this before finishing:\n" + trim(out, 40))
		return
	}
	if out, err := run(root, 3*time.Minute, "go", "vet", "./..."); err != nil {
		block("`go vet ./...` failed. Fix this before finishing:\n" + trim(out, 40))
	}
}

// goFilesChanged reports whether any Go-related files differ from HEAD.
// Outside a git repo it returns true, so the check still runs.
func goFilesChanged(root string) bool {
	out, err := run(root, 10*time.Second, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return true
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if i := strings.Index(p, " -> "); i >= 0 { // renames
			p = p[i+4:]
		}
		p = strings.Trim(p, `"`)
		if strings.HasPrefix(p, "tools/hooks/") {
			continue // separate module, not part of the app build
		}
		if strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "go.mod") ||
			strings.HasSuffix(p, "go.sum") || strings.HasSuffix(p, ".sql") {
			return true
		}
	}
	return false
}

// --- helpers ----------------------------------------------------------------

func run(dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func trim(s string, maxLines int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("... (%d more lines)", len(lines)-maxLines))
	}
	return strings.Join(lines, "\n")
}
