package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nickschuch/pinchy/internal/config"
)

// makeDir is a test helper that creates a directory and returns its path.
func makeDir(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatalf("makeDir: %v", err)
	}
	return p
}

// makeFile is a test helper that creates a plain file and returns its path.
func makeFile(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatalf("makeFile: %v", err)
	}
	return p
}

// --------------------------------------------------------------------------
// expandAndAbs
// --------------------------------------------------------------------------

func TestExpandAndAbs_TildeSlash(t *testing.T) {
	home := t.TempDir()
	got, err := expandAndAbs("~/foo/bar", home)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, "foo", "bar")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpandAndAbs_BareHome(t *testing.T) {
	home := t.TempDir()
	got, err := expandAndAbs("~", home)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != home {
		t.Errorf("got %q, want %q", got, home)
	}
}

func TestExpandAndAbs_AbsolutePassThrough(t *testing.T) {
	home := t.TempDir()
	got, err := expandAndAbs("/absolute/path", home)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/absolute/path" {
		t.Errorf("got %q, want /absolute/path", got)
	}
}

// --------------------------------------------------------------------------
// resolveMounts
// --------------------------------------------------------------------------

func TestResolveMounts_EmptyList(t *testing.T) {
	got, err := resolveMounts(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestResolveMounts_ReadOnlyDefault(t *testing.T) {
	base := t.TempDir()
	src := makeDir(t, base, "mydir")

	mounts := []config.Mount{
		{Source: src, Target: "/home/skpr/mydir"},
	}
	resolved, err := resolveMounts(mounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved mount, got %d", len(resolved))
	}
	if !resolved[0].mount.ReadOnly {
		t.Error("expected ReadOnly=true for empty mode")
	}
}

func TestResolveMounts_ReadOnlyExplicit(t *testing.T) {
	base := t.TempDir()
	src := makeDir(t, base, "mydir")

	mounts := []config.Mount{
		{Source: src, Target: "/home/skpr/mydir", Mode: "ro"},
	}
	resolved, err := resolveMounts(mounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resolved[0].mount.ReadOnly {
		t.Error("expected ReadOnly=true for mode ro")
	}
}

func TestResolveMounts_ReadWrite(t *testing.T) {
	base := t.TempDir()
	src := makeDir(t, base, "mydir")

	mounts := []config.Mount{
		{Source: src, Target: "/home/skpr/mydir", Mode: "rw"},
	}
	resolved, err := resolveMounts(mounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].mount.ReadOnly {
		t.Error("expected ReadOnly=false for mode rw")
	}
}

func TestResolveMounts_TildeExpansion(t *testing.T) {
	// Override HOME to a temp dir that contains the expected subdirectory.
	home := t.TempDir()
	makeDir(t, home, ".aws")
	t.Setenv("HOME", home)

	mounts := []config.Mount{
		{Source: "~/.aws", Target: "/home/skpr/.aws"},
	}
	resolved, err := resolveMounts(mounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantSource := filepath.Join(home, ".aws")
	if resolved[0].mount.Source != wantSource {
		t.Errorf("Source = %q, want %q", resolved[0].mount.Source, wantSource)
	}
	if resolved[0].origSource != "~/.aws" {
		t.Errorf("origSource = %q, want ~/.aws", resolved[0].origSource)
	}
}

func TestResolveMounts_TargetPreserved(t *testing.T) {
	base := t.TempDir()
	src := makeDir(t, base, "mydir")

	mounts := []config.Mount{
		{Source: src, Target: "/home/skpr/mydir"},
	}
	resolved, err := resolveMounts(mounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].mount.Target != "/home/skpr/mydir" {
		t.Errorf("Target = %q, want /home/skpr/mydir", resolved[0].mount.Target)
	}
}

func TestResolveMounts_NonExistentSource(t *testing.T) {
	mounts := []config.Mount{
		{Source: "/nonexistent/path/that/does/not/exist", Target: "/home/skpr/foo"},
	}
	_, err := resolveMounts(mounts)
	if err == nil {
		t.Fatal("expected error for non-existent source, got nil")
	}
}

func TestResolveMounts_SourceIsFile(t *testing.T) {
	base := t.TempDir()
	src := makeFile(t, base, "notadir.txt")

	mounts := []config.Mount{
		{Source: src, Target: "/home/skpr/notadir"},
	}
	_, err := resolveMounts(mounts)
	if err == nil {
		t.Fatal("expected error for file source (not a directory), got nil")
	}
}

func TestResolveMounts_MultipleEntries(t *testing.T) {
	base := t.TempDir()
	aws := makeDir(t, base, "aws")
	skpr := makeDir(t, base, "skpr")

	mounts := []config.Mount{
		{Source: aws, Target: "/home/skpr/.aws"},
		{Source: skpr, Target: "/home/skpr/.skpr", Mode: "rw"},
	}
	resolved, err := resolveMounts(mounts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved mounts, got %d", len(resolved))
	}
	if !resolved[0].mount.ReadOnly {
		t.Error("first mount should be read-only")
	}
	if resolved[1].mount.ReadOnly {
		t.Error("second mount should be read-write")
	}
}
