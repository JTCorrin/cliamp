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

// within reports whether path is dir or lies below it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CheckNewRoot validates a folder that is about to join the folders in
// existing. It returns the folder as it is stored: absolute and cleaned. The
// folder must exist, must be a folder, and must not be one of the folders, hold
// one, or lie inside one, because the files below it would be indexed twice.
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
	for _, have := range existing {
		have = ExpandRoot(have)
		switch {
		case have == root:
			return "", fmt.Errorf("library: %s is already in the library", root)
		case within(have, root):
			return "", fmt.Errorf("library: %s is inside %s, which is already in the library", root, have)
		case within(root, have):
			return "", fmt.Errorf("library: %s holds %s, which is already in the library; remove that folder first", root, have)
		}
	}
	return root, nil
}

// AddLibraryRoot adds a folder, saves the list and starts a scan.
func (p *Provider) AddLibraryRoot(path string) (string, error) {
	// The folder check can wait on a slow share, so it runs before the lock.
	p.mu.RLock()
	existing := slices.Clone(p.roots)
	p.mu.RUnlock()
	root, err := CheckNewRoot(existing, path)
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	if p.ctx.Err() != nil {
		p.mu.Unlock()
		return "", fmt.Errorf("library: closed")
	}
	// A folder may have joined while the check ran: check again, without I/O.
	for _, have := range p.roots {
		if have2 := ExpandRoot(have); have2 == root || within(have2, root) || within(root, have2) {
			p.mu.Unlock()
			return "", fmt.Errorf("library: %s overlaps a folder that was just added", root)
		}
	}
	next := append(slices.Clone(p.roots), root)
	// The list is saved under the lock: a second add must see this folder,
	// and saving the config is a short local write.
	if p.saveRoots != nil {
		if err := p.saveRoots(next); err != nil {
			p.mu.Unlock()
			return "", fmt.Errorf("library: save the folders: %w", err)
		}
	}
	p.roots = next
	p.mu.Unlock()
	applog.Info("library: added folder %s", root)
	p.RescanLibrary()
	return root, nil
}

// RemoveLibraryRoot drops a folder and the files of the index below it.
func (p *Provider) RemoveLibraryRoot(path string) error {
	root := ExpandRoot(path)
	p.mu.Lock()
	index := slices.IndexFunc(p.roots, func(r string) bool { return ExpandRoot(r) == root })
	if index < 0 {
		p.mu.Unlock()
		return fmt.Errorf("library: %s is not in the library", root)
	}
	next := slices.Delete(slices.Clone(p.roots), index, index+1)
	if p.saveRoots != nil {
		if err := p.saveRoots(next); err != nil {
			p.mu.Unlock()
			return fmt.Errorf("library: save the folders: %w", err)
		}
	}
	p.roots = next
	p.idx = pruneRoots(p.idx, next)
	idx := p.idx
	p.mu.Unlock()
	if p.indexPath != "" {
		if err := SaveIndex(p.indexPath, idx); err != nil {
			applog.Warn("library: %v", err)
		}
	}
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
