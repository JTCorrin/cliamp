package library

import (
	"testing"

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
			p := NewFromConfig(tt.cfg)
			if (p == nil) != tt.wantNil {
				t.Fatalf("NewFromConfig nil = %v, want %v", p == nil, tt.wantNil)
			}
			if p == nil {
				return
			}
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
	paths := []string{"/a"}
	p := NewFromConfig(config.LibraryConfig{Paths: paths})
	paths[0] = "/changed"
	if p.roots[0] != "/a" {
		t.Errorf("roots[0] = %q, want /a", p.roots[0])
	}
}
