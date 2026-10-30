// Package library implements a provider over folders of music files, such as
// a share of a NAS that is mounted on this machine. A scan reads the tags of
// the files into an index, which serves search and browse without touching
// the share again.
package library

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/config"
	"github.com/bjarneo/cliamp/internal/appdir"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

var (
	_ playlist.Refresher = (*Provider)(nil)
	_ provider.Closer    = (*Provider)(nil)
)

// Status describes the scans of a Provider.
type Status struct {
	Scanning bool
	Progress Progress
	// Last is the result of the last finished scan.
	Last ScanResult
	// Err is the error of the last scan, or empty.
	Err string
}

// Provider implements playlist.Provider for the indexed folders.
type Provider struct {
	roots     []string
	workers   int
	scan      bool
	indexPath string // empty keeps the index in memory only

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.RWMutex
	idx    *Index       // never changed after it is stored: a scan builds a new one
	search *searchIndex // the searchable form of idx, built on first use
	browse *browseIndex // the artists and albums of idx, built on first use
	status Status
}

// NewFromConfig returns a Provider from a LibraryConfig, or nil if the
// section holds no folder. It loads the saved index, and starts a scan in the
// background unless the section turns the scan off.
func NewFromConfig(cfg config.LibraryConfig) *Provider {
	if !cfg.IsSet() {
		return nil
	}
	indexPath := ""
	if dir, err := appdir.DataDir(); err == nil {
		indexPath = filepath.Join(dir, "library", "index.gob")
	} else {
		applog.Warn("library: the index is not saved: %v", err)
	}
	p := newProvider(cfg, indexPath)
	if p.scan {
		p.startScan()
	}
	return p
}

func newProvider(cfg config.LibraryConfig, indexPath string) *Provider {
	workers := cfg.Workers
	if workers <= 0 {
		workers = config.DefaultLibraryWorkers
	}
	p := &Provider{
		roots:     append([]string(nil), cfg.Paths...),
		workers:   workers,
		scan:      !cfg.ScanDisabled,
		indexPath: indexPath,
		idx:       &Index{Version: indexVersion},
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	if indexPath != "" {
		ix, err := LoadIndex(indexPath)
		if err != nil {
			applog.Warn("library: %v", err)
		} else {
			p.idx = ix
		}
	}
	return p
}

// Name returns the display name used in the provider selector.
func (p *Provider) Name() string { return "Library" }

// Playlists returns the playlists of the library. It has none until the
// browse and playlist views are in place.
func (p *Provider) Playlists() ([]playlist.PlaylistInfo, error) {
	return nil, nil
}

// Tracks returns the tracks of a playlist of the library.
func (p *Provider) Tracks(playlistID string) ([]playlist.Track, error) {
	return nil, fmt.Errorf("library: unknown playlist %q", playlistID)
}

// Refresh starts a scan for new, changed and removed files. It does nothing
// while a scan runs.
func (p *Provider) Refresh() { p.startScan() }

// Close stops a running scan and waits for it.
func (p *Provider) Close() {
	p.cancel()
	p.wg.Wait()
}

// Status returns the state of the scans.
func (p *Provider) Status() Status {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

// index returns the current index. The caller must not change it.
func (p *Provider) index() *Index {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.idx
}

// startScan runs one scan in the background. It returns false when a scan
// already runs or the provider is closed.
func (p *Provider) startScan() bool {
	p.mu.Lock()
	if p.status.Scanning || p.ctx.Err() != nil {
		p.mu.Unlock()
		return false
	}
	p.status.Scanning = true
	p.status.Progress = Progress{}
	p.status.Err = ""
	old := p.idx
	p.wg.Add(1)
	p.mu.Unlock()

	go func() {
		defer p.wg.Done()
		next, res, err := Scan(p.ctx, p.roots, old, p.workers, func(pr Progress) {
			p.mu.Lock()
			p.status.Progress = pr
			p.mu.Unlock()
		})
		if err == nil && p.indexPath != "" {
			if serr := SaveIndex(p.indexPath, next); serr != nil {
				applog.Warn("library: %v", serr)
			}
		}
		p.mu.Lock()
		p.status.Scanning = false
		if err != nil {
			p.status.Err = err.Error()
		} else {
			p.idx = next
			p.status.Last = res
		}
		p.mu.Unlock()
		if err == nil {
			applog.Info("library: scan found %d added, %d updated, %d removed, %d unchanged, %d skipped; %d unavailable roots",
				res.Added, res.Updated, res.Removed, res.Unchanged, res.Skipped, len(res.Unavailable))
		}
	}()
	return true
}
