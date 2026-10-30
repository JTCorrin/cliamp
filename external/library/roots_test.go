package library

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bjarneo/cliamp/config"
	"github.com/bjarneo/cliamp/provider"
)

// emptyProvider returns a provider with no folder that saves the folder list
// into saved.
func emptyProvider(t *testing.T, saved *[][]string) *Provider {
	t.Helper()
	p := newProvider(config.LibraryConfig{}, filepath.Join(t.TempDir(), "index.gob"))
	t.Cleanup(p.Close)
	p.SetSaveRoots(func(paths []string) error {
		*saved = append(*saved, append([]string(nil), paths...))
		return nil
	})
	return p
}

// settle waits for the scans of p to end.
func settle(t *testing.T, p *Provider) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.RLock()
		busy := p.status.Scanning
		p.mu.RUnlock()
		if !busy {
			p.wg.Wait()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the scan did not end")
}

func TestAddLibraryRootScansAndSaves(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	root := t.TempDir()
	writeMP3(t, filepath.Join(root, "Band", "a.mp3"), tagsFor("A", "The Band", "The Album", 1))

	if got := p.LibraryRoots(); len(got) != 0 {
		t.Fatalf("roots before = %v, want none", got)
	}
	stored, err := p.AddLibraryRoot(root)
	if err != nil || stored != root {
		t.Fatalf("AddLibraryRoot = %q, %v; want %q", stored, err, root)
	}
	settle(t, p)

	if !reflect.DeepEqual(saved, [][]string{{root}}) {
		t.Errorf("saved = %v, want the one folder", saved)
	}
	roots := p.LibraryRoots()
	want := []provider.LibraryRoot{{Path: root, Tracks: 1, Available: true}}
	if !reflect.DeepEqual(roots, want) {
		t.Errorf("roots = %+v, want %+v", roots, want)
	}
	if got := titles(t, p, "a", 5); len(got) != 1 {
		t.Errorf("search found %v after the folder was added", got)
	}
}

func TestAddLibraryRootRejectsBadFolders(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddLibraryRoot(root); err != nil {
		t.Fatal(err)
	}
	settle(t, p)
	saved = nil

	tests := []struct {
		name, path, want string
	}{
		{"empty", "  ", "empty"},
		{"missing", filepath.Join(root, "nope"), "no such file"},
		{"a file", file, "not a folder"},
		{"the same folder", root + "/", "already in the library"},
		{"inside a folder", child, "is inside"},
		{"holding a folder", filepath.Dir(root), "holds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.AddLibraryRoot(tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
	if len(saved) != 0 {
		t.Errorf("a rejected folder was saved: %v", saved)
	}
	if got := p.LibraryRoots(); len(got) != 1 {
		t.Errorf("roots = %v, want only the first folder", got)
	}
}

func TestAddLibraryRootDoesNotKeepAFolderThatFailedToSave(t *testing.T) {
	p := newProvider(config.LibraryConfig{}, "")
	t.Cleanup(p.Close)
	p.SetSaveRoots(func([]string) error { return errors.New("disk full") })
	if _, err := p.AddLibraryRoot(t.TempDir()); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v, want the save error", err)
	}
	if got := p.LibraryRoots(); len(got) != 0 {
		t.Errorf("roots = %v, want none after a failed save", got)
	}
}

func TestRemoveLibraryRootDropsItsFiles(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	one, two := t.TempDir(), t.TempDir()
	writeMP3(t, filepath.Join(one, "a.mp3"), tagsFor("First", "X", "Y", 1))
	writeMP3(t, filepath.Join(two, "b.mp3"), tagsFor("Second", "X", "Y", 2))
	for _, r := range []string{one, two} {
		if _, err := p.AddLibraryRoot(r); err != nil {
			t.Fatal(err)
		}
		settle(t, p)
	}
	if p.index().Len() != 2 {
		t.Fatalf("index holds %d files, want 2", p.index().Len())
	}
	saved = nil

	if err := p.RemoveLibraryRoot(one); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, [][]string{{two}}) {
		t.Errorf("saved = %v, want the remaining folder", saved)
	}
	if got := titles(t, p, "first", 5); len(got) != 0 {
		t.Errorf("a removed folder still serves %v", got)
	}
	if got := titles(t, p, "second", 5); len(got) != 1 {
		t.Errorf("the other folder lost its file: %v", got)
	}
	// The saved index no longer holds the folder either.
	reloaded := newProvider(config.LibraryConfig{Paths: []string{two}, ScanDisabled: true}, p.indexPath)
	t.Cleanup(reloaded.Close)
	if reloaded.index().Len() != 1 {
		t.Errorf("reloaded index holds %d files, want 1", reloaded.index().Len())
	}

	if err := p.RemoveLibraryRoot(one); err == nil || !strings.Contains(err.Error(), "not in the library") {
		t.Errorf("removing it again: %v, want a not-in-the-library error", err)
	}
}

// A folder removed while a scan reads it must not come back when the scan ends.
func TestRemoveLibraryRootDuringAScanStaysRemoved(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		writeMP3(t, filepath.Join(root, "a"+itoa(i%90+10)+".mp3"), tagsFor("T", "X", "Y", 1))
	}
	if _, err := p.AddLibraryRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := p.RemoveLibraryRoot(root); err != nil {
		t.Fatal(err)
	}
	settle(t, p)
	if p.index().Len() != 0 || len(p.index().Roots) != 0 {
		t.Errorf("the removed folder came back: %d files, %d roots", p.index().Len(), len(p.index().Roots))
	}
}

func TestAddLibraryRootDuringAScanIsScannedNext(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	one, two := t.TempDir(), t.TempDir()
	for i := 0; i < 30; i++ {
		writeMP3(t, filepath.Join(one, "a"+itoa(i%90+10)+".mp3"), tagsFor("T", "X", "Y", 1))
	}
	writeMP3(t, filepath.Join(two, "late.mp3"), tagsFor("Late", "X", "Y", 1))
	if _, err := p.AddLibraryRoot(one); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddLibraryRoot(two); err != nil {
		t.Fatal(err)
	}
	settle(t, p)
	if got := titles(t, p, "late", 5); len(got) != 1 {
		t.Errorf("a folder added during a scan was not scanned: %v", got)
	}
}

func TestLoadedIndexIsPrunedToTheConfiguredFolders(t *testing.T) {
	root, gone := t.TempDir(), t.TempDir()
	writeMP3(t, filepath.Join(root, "a.mp3"), tagsFor("Kept", "X", "Y", 1))
	writeMP3(t, filepath.Join(gone, "b.mp3"), tagsFor("Dropped", "X", "Y", 1))
	ix, _, err := Scan(testContext(t), []string{root, gone}, nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "index.gob")
	if err := SaveIndex(path, ix); err != nil {
		t.Fatal(err)
	}
	p := newProvider(config.LibraryConfig{Paths: []string{root}, ScanDisabled: true}, path)
	t.Cleanup(p.Close)
	if p.index().Len() != 1 || len(p.index().Roots) != 1 {
		t.Errorf("index holds %d files in %d roots, want only the configured folder", p.index().Len(), len(p.index().Roots))
	}
}

func TestStatusLineWithoutFolders(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	if got := p.statusLine(); !strings.Contains(got, "No music folder") {
		t.Errorf("status line = %q, want a prompt to add a folder", got)
	}
}

func TestRescanLibraryAfterClose(t *testing.T) {
	p := newProvider(config.LibraryConfig{Paths: []string{t.TempDir()}}, "")
	p.Close()
	done := make(chan struct{})
	go func() { p.RescanLibrary(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RescanLibrary on a closed provider did not return")
	}
}

// Folders added at the same moment must all be kept, and the last saved list
// must hold every one of them.
func TestConcurrentAddLibraryRootKeepsEveryFolder(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	const n = 8
	dirs := make([]string, n)
	for i := range dirs {
		dirs[i] = t.TempDir()
	}
	errs := make(chan error, n)
	for _, dir := range dirs {
		go func() {
			_, err := p.AddLibraryRoot(dir)
			errs <- err
		}()
	}
	for range dirs {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	settle(t, p)
	if got := len(p.LibraryRoots()); got != n {
		t.Fatalf("roots = %d, want %d", got, n)
	}
	if last := saved[len(saved)-1]; len(last) != n {
		t.Errorf("the last saved list holds %d folders, want %d", len(last), n)
	}
}

// A link to a folder of the library is that folder: adding it would index
// every file twice under different track IDs.
func TestAddLibraryRootRefusesALinkToAFolderOfTheLibrary(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	root := t.TempDir()
	if _, err := p.AddLibraryRoot(root); err != nil {
		t.Fatal(err)
	}
	settle(t, p)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := p.AddLibraryRoot(link); err == nil || !strings.Contains(err.Error(), "already in the library") {
		t.Errorf("adding a link to %s: %v, want an already-in-the-library error", root, err)
	}
	// A link to a folder inside it is inside it.
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	childLink := filepath.Join(t.TempDir(), "childlink")
	if err := os.Symlink(child, childLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := p.AddLibraryRoot(childLink); err == nil || !strings.Contains(err.Error(), "is inside") {
		t.Errorf("adding a link into the library: %v, want an inside error", err)
	}
	if got := len(p.LibraryRoots()); got != 1 {
		t.Errorf("roots = %d, want only the first", got)
	}
}

// A folder that was unreadable when removed must not leave its warning behind.
func TestRemoveLibraryRootDropsItsUnavailableMark(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	root := t.TempDir()
	if _, err := p.AddLibraryRoot(root); err != nil {
		t.Fatal(err)
	}
	settle(t, p)
	p.mu.Lock()
	p.status.Last.Unavailable = []string{root}
	p.mu.Unlock()
	if got := p.statusLine(); !strings.Contains(got, "Not readable") {
		t.Fatalf("status line = %q, want the unreadable notice", got)
	}
	if err := p.RemoveLibraryRoot(root); err != nil {
		t.Fatal(err)
	}
	if got := p.statusLine(); !strings.Contains(got, "No music folder") {
		t.Errorf("status line after removal = %q, want the empty prompt", got)
	}
}

func TestLibraryScanningFollowsTheScan(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	if p.LibraryScanning() {
		t.Fatal("a provider with no folder is scanning")
	}
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		writeMP3(t, filepath.Join(root, "a"+itoa(i%90+10)+".mp3"), tagsFor("T", "X", "Y", 1))
	}
	if _, err := p.AddLibraryRoot(root); err != nil {
		t.Fatal(err)
	}
	// The scan starts before AddLibraryRoot returns, so a client that asks
	// next sees it, unless a tiny folder is already done.
	settle(t, p)
	if p.LibraryScanning() {
		t.Error("still scanning after the scan ended")
	}
}

// The file on disk must end up with the newest index even when a scan and a
// removal both write it.
func TestIndexFileHoldsTheNewestIndexAfterARemovalDuringAScan(t *testing.T) {
	var saved [][]string
	p := emptyProvider(t, &saved)
	keep, drop := t.TempDir(), t.TempDir()
	for i := 0; i < 30; i++ {
		writeMP3(t, filepath.Join(drop, "d"+itoa(i%90+10)+".mp3"), tagsFor("Drop", "X", "Y", 1))
	}
	writeMP3(t, filepath.Join(keep, "k.mp3"), tagsFor("Keep", "X", "Y", 1))
	for _, r := range []string{keep, drop} {
		if _, err := p.AddLibraryRoot(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.RemoveLibraryRoot(drop); err != nil {
		t.Fatal(err)
	}
	settle(t, p)
	onDisk, err := LoadIndex(p.indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.root(drop) != nil {
		t.Errorf("the index file still holds the removed folder %s", drop)
	}
	if r := onDisk.root(keep); r == nil || len(r.Files) != 1 {
		t.Errorf("the index file lost the kept folder: %+v", r)
	}
}

func TestCloseDuringRescansDoesNotRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := newProvider(config.LibraryConfig{Paths: []string{t.TempDir()}}, "")
		done := make(chan struct{})
		go func() {
			for j := 0; j < 5; j++ {
				p.RescanLibrary()
			}
			close(done)
		}()
		p.Close()
		<-done
	}
}
