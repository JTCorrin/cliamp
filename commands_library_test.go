package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bjarneo/cliamp/config"
	"github.com/bjarneo/cliamp/ipc"
)

func TestUserIPCErrorKeepsTheNotRunningSentinel(t *testing.T) {
	err := userIPCError(ipc.ErrNotRunning)
	if !errors.Is(err, ipc.ErrNotRunning) {
		t.Fatalf("userIPCError does not unwrap to ipc.ErrNotRunning: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "cliamp is not running (no socket at ") {
		t.Errorf("message = %q", err.Error())
	}
	other := errors.New("boom")
	if got := userIPCError(other); got != other {
		t.Errorf("another error was changed: %v", got)
	}
}

// With no cliamp running, the library command edits the config file.
func TestLibraryCommandWorksOfflineOnTheConfig(t *testing.T) {
	isolateProviderEnv(t)
	a, b := t.TempDir(), t.TempDir()

	roots, err := libraryRoots("library.roots", "")
	if err != nil || len(roots) != 0 {
		t.Fatalf("list of a fresh config = %v, %v; want none", roots, err)
	}
	for _, dir := range []string{a, b} {
		if _, err := libraryRoots("library.add_root", dir); err != nil {
			t.Fatalf("add %s: %v", dir, err)
		}
	}
	cfg, err := config.Load()
	if err != nil || !reflect.DeepEqual(cfg.Library.Paths, []string{a, b}) {
		t.Fatalf("saved paths = %v, %v; want %v", cfg.Library.Paths, err, []string{a, b})
	}

	if _, err := libraryRoots("library.add_root", a); err == nil || !strings.Contains(err.Error(), "already in the library") {
		t.Errorf("adding a folder twice: %v", err)
	}
	if _, err := libraryRoots("library.add_root", filepath.Join(a, "missing")); err == nil {
		t.Error("adding a missing folder must fail")
	}

	roots, err = libraryRoots("library.remove_root", a)
	if err != nil || len(roots) != 1 || roots[0].Path != b {
		t.Fatalf("remove = %v, %v; want only %s", roots, err, b)
	}
	if _, err := libraryRoots("library.remove_root", a); err == nil || !strings.Contains(err.Error(), "not in the library") {
		t.Errorf("removing it again: %v", err)
	}

	if _, err := libraryRoots("library.scan", ""); err == nil || !strings.Contains(err.Error(), "needs a running cliamp") {
		t.Errorf("scan without cliamp: %v", err)
	}
}

func TestLibraryCommandOfflineKeepsOtherConfig(t *testing.T) {
	isolateProviderEnv(t)
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".config", "cliamp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	initial := "# my notes\nvolume = -3\n[library]\nworkers = 6\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := libraryRoots("library.add_root", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"# my notes", "volume = -3", "workers = 6"} {
		if !strings.Contains(string(data), keep) {
			t.Errorf("config lost %q:\n%s", keep, data)
		}
	}
}
