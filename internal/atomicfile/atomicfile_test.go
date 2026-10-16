package atomicfile

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWrite(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		old     string // existing content; "" means no file
		write   func(io.Writer) error
		want    string // content after Write
		wantErr bool
	}{
		{"new file", "", func(w io.Writer) error { _, err := io.WriteString(w, "new"); return err }, "new", false},
		{"replaces file", "old", func(w io.Writer) error { _, err := io.WriteString(w, "new"); return err }, "new", false},
		{"writer error keeps old file", "old", func(w io.Writer) error { io.WriteString(w, "partial"); return boom }, "old", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f.json")
			if tc.old != "" {
				if err := os.WriteFile(path, []byte(tc.old), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := Write(context.Background(), path, 0o644, tc.write)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Write error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, boom) {
				t.Errorf("Write error = %v, want it to wrap the writer's error", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("content = %q, want %q", got, tc.want)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Errorf("dir has %d entries, want only f.json (temp file left behind?)", len(entries))
			}
			if runtime.GOOS != "windows" && !tc.wantErr {
				fi, _ := os.Stat(path)
				if fi.Mode().Perm() != 0o644 {
					t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
				}
			}
		})
	}
}

func TestWriteCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Write(ctx, path, 0o644, func(io.Writer) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Write with a canceled ctx = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat = %v, want no file written", err)
	}
}

func TestWriteMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "f.json")
	if err := Write(context.Background(), path, 0o644, func(io.Writer) error { return nil }); err == nil {
		t.Fatal("Write into a missing dir succeeded, want error")
	}
}
