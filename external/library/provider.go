// Package library implements a provider over folders of music files, such as
// a share of a NAS that is mounted on this machine. A scan reads the tags of
// the files into an index, which serves search and browse without touching
// the share again.
package library

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/config"
	"github.com/bjarneo/cliamp/internal/appdir"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

var (
	_ playlist.Refresher      = (*Provider)(nil)
	_ provider.Closer         = (*Provider)(nil)
	_ provider.LibraryManager = (*Provider)(nil)
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
	workers   int
	scan      bool
	indexPath string // empty keeps the index in memory only

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.RWMutex
	idx    *Index        // never changed after it is stored: a scan builds a new one
	search *searchIndex  // the searchable form of idx, built on first use
	browse *browseIndex  // the artists and albums of idx, built on first use
	store  PlaylistStore // where the playlists are kept, or nil
	status Status

	roots     []string                   // the folders; replaced, never changed in place
	saveRoots func(paths []string) error // saves the folders, or nil
	rescan    bool                       // a scan follows the one that runs
}

// NewFromConfig returns a Provider from a LibraryConfig. The provider exists
// without a folder, so a folder can be added while cliamp runs. It loads the
// saved index, and starts a scan in the background unless the section turns
// the scan off or lists no folder.
func NewFromConfig(cfg config.LibraryConfig) *Provider {
	indexPath := ""
	if dir, err := appdir.DataDir(); err == nil {
		indexPath = filepath.Join(dir, "library", "index.gob")
	} else {
		applog.Warn("library: the index is not saved: %v", err)
	}
	p := newProvider(cfg, indexPath)
	if p.scan && len(p.roots) > 0 {
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
			// A folder that left the config must not keep serving its files.
			p.idx = pruneRoots(ix, p.roots)
		}
	}
	return p
}

// SetSaveRoots sets the function that saves the folders after AddLibraryRoot
// and RemoveLibraryRoot change them. Call it before the provider is used.
func (p *Provider) SetSaveRoots(save func(paths []string) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.saveRoots = save
}

// Name returns the display name used in the provider selector.
func (p *Provider) Name() string { return "Library" }

// Refresh starts a scan for new, changed and removed files. It does nothing
// while a scan runs.
func (p *Provider) Refresh() { p.startScan() }

// closeTimeout bounds how long Close waits for a scan. A scan checks for
// cancellation between files, so a read that hangs on a stalled share holds
// it up, and the application must still be able to exit.
const closeTimeout = 3 * time.Second

// Close stops a running scan and waits for it, up to closeTimeout.
func (p *Provider) Close() {
	p.cancel()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeTimeout):
		applog.Warn("library: a scan did not stop; the share may be stalled")
	}
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
	roots := slices.Clone(p.roots)
	p.wg.Add(1)
	p.mu.Unlock()

	go func() {
		defer p.wg.Done()
		next, res, err := Scan(p.ctx, roots, old, p.workers, func(pr Progress) {
			p.mu.Lock()
			p.status.Progress = pr
			p.mu.Unlock()
		})
		p.mu.Lock()
		p.status.Scanning = false
		if err != nil {
			p.status.Err = err.Error()
		} else {
			// A folder may have been removed while the scan ran.
			next = pruneRoots(next, p.roots)
			p.idx = next
			p.status.Last = res
		}
		again := p.rescan && p.ctx.Err() == nil
		p.rescan = false
		p.mu.Unlock()
		if err == nil && p.indexPath != "" {
			if serr := SaveIndex(p.indexPath, next); serr != nil {
				applog.Warn("library: %v", serr)
			}
		}
		if err == nil {
			applog.Info("library: scan found %d added, %d updated, %d removed, %d unchanged, %d skipped; %d unavailable roots",
				res.Added, res.Updated, res.Removed, res.Unchanged, res.Skipped, len(res.Unavailable))
		}
		if again {
			p.startScan()
		}
	}()
	return true
}

// pruneRoots returns ix without the roots that are not in roots. It returns
// ix itself when nothing is dropped.
func pruneRoots(ix *Index, roots []string) *Index {
	keep := make(map[string]bool, len(roots))
	for _, r := range roots {
		keep[ExpandRoot(r)] = true
	}
	kept := make([]Root, 0, len(ix.Roots))
	for _, r := range ix.Roots {
		if keep[r.Path] {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(ix.Roots) {
		return ix
	}
	return &Index{Version: ix.Version, Roots: kept}
}
