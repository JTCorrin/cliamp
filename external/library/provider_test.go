package library

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bjarneo/cliamp/config"
)

func TestNewFromConfig(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.LibraryConfig
		wantNil     bool
		wantWorkers int
		wantScan    bool
	}{
		{name: "no paths", cfg: config.LibraryConfig{}, wantNil: true},
		{name: "defaults", cfg: config.LibraryConfig{Paths: []string{"/a"}}, wantWorkers: config.DefaultLibraryWorkers, wantScan: true},
		{name: "workers", cfg: config.LibraryConfig{Paths: []string{"/a"}, Workers: 9}, wantWorkers: 9, wantScan: true},
		{name: "scan off", cfg: config.LibraryConfig{Paths: []string{"/a"}, ScanDisabled: true}, wantWorkers: config.DefaultLibraryWorkers},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			p := NewFromConfig(tt.cfg)
			if (p == nil) != tt.wantNil {
				t.Fatalf("NewFromConfig nil = %v, want %v", p == nil, tt.wantNil)
			}
			if p == nil {
				return
			}
			t.Cleanup(p.Close)
			if p.workers != tt.wantWorkers || p.scan != tt.wantScan {
				t.Errorf("workers, scan = %d, %v; want %d, %v", p.workers, p.scan, tt.wantWorkers, tt.wantScan)
			}
			if p.Name() != "Library" {
				t.Errorf("Name() = %q", p.Name())
			}
		})
	}
}

func TestNewFromConfigCopiesPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths := []string{"/a"}
	p := NewFromConfig(config.LibraryConfig{Paths: paths, ScanDisabled: true})
	paths[0] = "/changed"
	if p.roots[0] != "/a" {
		t.Errorf("roots[0] = %q, want /a", p.roots[0])
	}
}

func TestProviderScansAndPersists(t *testing.T) {
	root := t.TempDir()
	writeMP3(t, filepath.Join(root, "a.mp3"), tagsFor("A", "X", "Y", 1))
	indexPath := filepath.Join(t.TempDir(), "index.gob")
	cfg := config.LibraryConfig{Paths: []string{root}}

	p := newProvider(cfg, indexPath)
	if p.index().Len() != 0 {
		t.Fatal("a new provider with no saved index must start empty")
	}
	if !p.startScan() {
		t.Fatal("startScan = false")
	}
	p.wg.Wait()
	if st := p.Status(); st.Scanning || st.Err != "" || st.Last.Added != 1 {
		t.Fatalf("status = %+v", st)
	}
	if p.index().Len() != 1 {
		t.Fatalf("index Len = %d, want 1", p.index().Len())
	}

	// A second provider serves the saved index before it scans.
	q := newProvider(config.LibraryConfig{Paths: []string{root}, ScanDisabled: true}, indexPath)
	defer q.Close()
	if q.index().Len() != 1 {
		t.Errorf("reloaded index Len = %d, want 1", q.index().Len())
	}

	// Refresh scans again and sees a new file.
	writeMP3(t, filepath.Join(root, "b.mp3"), tagsFor("B", "X", "Y", 2))
	p.Refresh()
	p.wg.Wait()
	if p.index().Len() != 2 {
		t.Errorf("index Len after Refresh = %d, want 2", p.index().Len())
	}
	p.Close()
	if p.startScan() {
		t.Error("startScan after Close = true, want false")
	}
}

func TestProviderClosedDoesNotScan(t *testing.T) {
	p := newProvider(config.LibraryConfig{Paths: []string{t.TempDir()}}, "")
	before := p.index()
	p.Close()
	if p.startScan() {
		t.Error("a closed provider started a scan")
	}
	if p.index() != before {
		t.Error("the index was replaced")
	}
}

func TestCloseReturnsPromptly(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 30; i++ {
		writeMP3(t, filepath.Join(root, string(rune('a'+i%26))+itoa(i%90+10)+".mp3"), tagsFor("T", "X", "Y", 1))
	}
	p := newProvider(config.LibraryConfig{Paths: []string{root}}, "")
	p.startScan()
	start := time.Now()
	p.Close()
	if d := time.Since(start); d > closeTimeout {
		t.Errorf("Close took %v", d)
	}
	if p.Status().Scanning {
		t.Error("a scan still runs after Close")
	}
}
