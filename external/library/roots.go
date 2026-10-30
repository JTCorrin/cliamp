package library

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/provider"
)

// LibraryRoots lists the folders in the order they were added.
func (p *Provider) LibraryRoots() []provider.LibraryRoot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	unavailable := map[string]bool{}
	for _, path := range p.status.Last.Unavailable {
		unavailable[path] = true
	}
	roots := make([]provider.LibraryRoot, 0, len(p.roots))
	for _, configured := range p.roots {
		path := ExpandRoot(configured)
		root := provider.LibraryRoot{Path: path, Available: !unavailable[path]}
		if r := p.idx.root(path); r != nil {
			root.Tracks = len(r.Files)
		}
		roots = append(roots, root)
	}
	return roots
}

// LibraryScanning reports whether a scan runs now.
func (p *Provider) LibraryScanning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status.Scanning
}

// within reports whether path is dir or lies below it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolved returns path with its symlinks followed, or path itself when it
// cannot be resolved, such as a share that is not mounted.
func resolved(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}

// CheckNewRoot validates a folder that is about to join the folders in
// existing. It returns the folder as it is stored: absolute and cleaned. The
// folder must exist, must be a folder, and must not be one of the folders, hold
// one, or lie inside one, because the files below it would be indexed twice. A
// link to a folder of the library counts as that folder.
func CheckNewRoot(existing []string, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("library: the folder is empty")
	}
	root := ExpandRoot(path)
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("library: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("library: %s is not a folder", root)
	}
	realRoot := resolved(root)
	for _, have := range existing {
		have = ExpandRoot(have)
		realHave := resolved(have)
		switch {
		case have == root || realHave == realRoot:
			return "", fmt.Errorf("library: %s is already in the library", root)
		case within(have, root) || within(realHave, realRoot):
			return "", fmt.Errorf("library: %s is inside %s, which is already in the library", root, have)
		case within(root, have) || within(realRoot, realHave):
			return "", fmt.Errorf("library: %s holds %s, which is already in the library; remove that folder first", root, have)
		}
	}
	return root, nil
}

// AddLibraryRoot adds a folder, saves the list and starts a scan.
func (p *Provider) AddLibraryRoot(path string) (string, error) {
	// One change of the folder list at a time. The folder check can wait on a
	// slow share and the save writes the config, so neither holds mu, which
	// readers need.
	p.rootsMu.Lock()
	defer p.rootsMu.Unlock()

	p.mu.RLock()
	existing := slices.Clone(p.roots)
	closed := p.ctx.Err() != nil
	save := p.saveRoots
	p.mu.RUnlock()
	if closed {
		return "", fmt.Errorf("library: closed")
	}
	root, err := CheckNewRoot(existing, path)
	if err != nil {
		return "", err
	}
	next := append(existing, root)
	if save != nil {
		if err := save(next); err != nil {
			return "", fmt.Errorf("library: save the folders: %w", err)
		}
	}
	p.mu.Lock()
	p.roots = next
	p.mu.Unlock()
	applog.Info("library: added folder %s", root)
	p.RescanLibrary()
	return root, nil
}

// RemoveLibraryRoot drops a folder and the files of the index below it. It
// never touches the music files.
func (p *Provider) RemoveLibraryRoot(path string) error {
	p.rootsMu.Lock()
	defer p.rootsMu.Unlock()

	root := ExpandRoot(path)
	p.mu.RLock()
	existing := slices.Clone(p.roots)
	save := p.saveRoots
	p.mu.RUnlock()
	index := slices.IndexFunc(existing, func(r string) bool { return ExpandRoot(r) == root })
	if index < 0 {
		return fmt.Errorf("library: %s is not in the library", root)
	}
	next := slices.Delete(existing, index, index+1)
	if save != nil {
		if err := save(next); err != nil {
			return fmt.Errorf("library: save the folders: %w", err)
		}
	}
	p.mu.Lock()
	p.roots = next
	p.idx = pruneRoots(p.idx, next)
	p.status.Last.Unavailable = keepRoots(p.status.Last.Unavailable, next)
	p.mu.Unlock()
	p.saveIndex()
	applog.Info("library: removed folder %s", root)
	return nil
}

// RescanLibrary scans the folders. A scan that already runs is followed by
// one more, so a folder added meanwhile is not missed.
func (p *Provider) RescanLibrary() {
	for {
		if p.startScan() {
			return
		}
		p.mu.Lock()
		if p.ctx.Err() != nil {
			p.mu.Unlock()
			return
		}
		if p.status.Scanning {
			p.rescan = true
			p.mu.Unlock()
			return
		}
		// The scan ended between the two looks: start one now.
		p.mu.Unlock()
	}
}
