// Package library implements a provider over folders of music files, such as
// a share of a NAS that is mounted on this machine. A scan reads the tags of
// the files into an index, which serves search and browse without touching
// the share again.
package library

import (
	"fmt"

	"github.com/bjarneo/cliamp/config"
	"github.com/bjarneo/cliamp/playlist"
)

// Provider implements playlist.Provider for the indexed folders.
type Provider struct {
	roots   []string
	workers int
	scan    bool
}

// NewFromConfig returns a Provider from a LibraryConfig, or nil if the
// section holds no folder.
func NewFromConfig(cfg config.LibraryConfig) *Provider {
	if !cfg.IsSet() {
		return nil
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = config.DefaultLibraryWorkers
	}
	return &Provider{
		roots:   append([]string(nil), cfg.Paths...),
		workers: workers,
		scan:    !cfg.ScanDisabled,
	}
}

// Name returns the display name used in the provider selector.
func (p *Provider) Name() string { return "Library" }

// Playlists returns the playlists of the library. It has none until the
// index exists.
func (p *Provider) Playlists() ([]playlist.PlaylistInfo, error) {
	return nil, nil
}

// Tracks returns the tracks of a playlist of the library.
func (p *Provider) Tracks(playlistID string) ([]playlist.Track, error) {
	return nil, fmt.Errorf("library: unknown playlist %q", playlistID)
}
