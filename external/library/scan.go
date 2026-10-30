package library

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bjarneo/cliamp/playlist"
)

// Progress reports how far the tag reads of a scan have come. Total counts
// the new and changed files of the scan so far.
type Progress struct {
	Done  int
	Total int
}

// ScanResult counts what a scan did.
type ScanResult struct {
	Added     int
	Updated   int
	Removed   int
	Unchanged int
	// Skipped counts files and folders that could not be read.
	Skipped int
	// Unavailable lists the roots that the scan left as they were, because
	// the folder is missing or holds no audio file while the index holds
	// some. An unmounted share looks like an empty folder.
	Unavailable []string
}

// candidate is a file that the walk found.
type candidate struct {
	rel     string
	modUnix int64
	size    int64
}

// ExpandRoot returns the folder of a configured root: a leading ~ becomes the
// home folder, a relative path becomes absolute, and the path is cleaned.
func ExpandRoot(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

// Scan reads the music folders of roots into a new index. Files that are
// unchanged in old, by size and modification time, keep their tags, so a
// rescan reads only new and changed files. workers is the number of files
// that are read at the same time. progress may be nil.
//
// Scan never changes old. When ctx ends it returns ctx.Err() and no index.
func Scan(ctx context.Context, roots []string, old *Index, workers int, progress func(Progress)) (*Index, ScanResult, error) {
	if old == nil {
		old = &Index{}
	}
	if workers < 1 {
		workers = 1
	}
	var res ScanResult
	next := &Index{Version: indexVersion}
	for _, configured := range roots {
		rootPath := ExpandRoot(configured)
		if next.root(rootPath) != nil {
			continue // the same folder twice
		}
		prev := old.root(rootPath)
		cands, skipped, err := walkRoot(ctx, rootPath)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, res, ctxErr
		}
		res.Skipped += len(skipped)
		if err != nil || (len(cands) == 0 && prev != nil && len(prev.Files) > 0) {
			res.Unavailable = append(res.Unavailable, rootPath)
			if prev != nil {
				next.Roots = append(next.Roots, *prev)
			} else {
				next.Roots = append(next.Roots, Root{Path: rootPath})
			}
			continue
		}
		files, counts, err := readRoot(ctx, rootPath, cands, skipped, prev, workers, progress)
		if err != nil {
			return nil, res, err
		}
		res.Added += counts.Added
		res.Updated += counts.Updated
		res.Removed += counts.Removed
		res.Unchanged += counts.Unchanged
		res.Skipped += counts.Skipped
		next.Roots = append(next.Roots, Root{Path: rootPath, Files: files})
	}
	// A root that left the config drops out of the index.
	for _, r := range old.Roots {
		if next.root(r.Path) == nil {
			res.Removed += len(r.Files)
		}
	}
	return next, res, nil
}

// walkRoot lists the audio files below root. A root that is a symlink is
// followed, because WalkDir does not follow one. A file or folder that cannot
// be read is not listed: skipped holds its path below root, so the caller can
// keep what the index knew about it. The error is not nil only when root
// itself cannot be read.
func walkRoot(ctx context.Context, root string) (cands []candidate, skipped []string, err error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a folder", root)
	}
	skip := func(path string) {
		if rel, err := filepath.Rel(real, path); err == nil {
			skipped = append(skipped, filepath.ToSlash(rel))
		}
	}
	err = filepath.WalkDir(real, func(path string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if path == real {
				return err
			}
			skip(path)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == real {
			return nil
		}
		name := d.Name()
		if skipName(name) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !playlist.IsAudioFile(name) {
			return nil
		}
		fi, err := os.Stat(path) // follows a symlink to a file
		if err != nil {
			skip(path)
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(real, path)
		if err != nil {
			skip(path)
			return nil
		}
		cands = append(cands, candidate{rel: filepath.ToSlash(rel), modUnix: fi.ModTime().Unix(), size: fi.Size()})
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, skipped, nil // the caller reports the cancellation
		}
		return nil, skipped, err
	}
	return cands, skipped, nil
}

// underSkipped reports whether rel is one of the skipped paths or below one.
func underSkipped(rel string, skipped []string) bool {
	for _, s := range skipped {
		if rel == s || strings.HasPrefix(rel, s+"/") {
			return true
		}
	}
	return false
}

// skipName reports whether a file or folder name is metadata of the file
// system or of the NAS, not music: dot files, such as the ._ files that macOS
// leaves on a share, and the @eaDir folders of Synology.
func skipName(name string) bool {
	return strings.HasPrefix(name, ".") || name == "@eaDir" || name == "#recycle"
}

// readRoot returns the files of one root: the unchanged files of prev, and
// the tags that workers read from the new and changed files. A file of prev
// below a path in skipped is kept as it was: the walk could not read it, which
// does not show that the file is gone.
func readRoot(ctx context.Context, root string, cands []candidate, skipped []string, prev *Root, workers int, progress func(Progress)) ([]File, ScanResult, error) {
	var counts ScanResult
	known := map[string]File{}
	if prev != nil {
		for _, f := range prev.Files {
			known[f.Rel] = f
		}
	}
	files := make([]File, 0, len(cands))
	var todo []candidate
	seen := make(map[string]bool, len(cands))
	for _, c := range cands {
		seen[c.rel] = true
		if f, ok := known[c.rel]; ok && f.ModUnix == c.modUnix && f.Size == c.size {
			files = append(files, f)
			counts.Unchanged++
			continue
		}
		todo = append(todo, c)
	}
	for rel, f := range known {
		switch {
		case seen[rel]:
		case underSkipped(rel, skipped):
			files = append(files, f)
			counts.Unchanged++
		default:
			counts.Removed++
		}
	}

	type result struct {
		file File
		err  error
		cand candidate
	}
	jobs := make(chan candidate)
	results := make(chan result)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				f, err := readFile(root, c)
				select {
				case results <- result{file: f, err: err, cand: c}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, c := range todo {
			select {
			case jobs <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	done := 0
	for r := range results {
		done++
		if progress != nil {
			progress(Progress{Done: done, Total: len(todo)})
		}
		_, existed := known[r.cand.rel]
		switch {
		case r.err == nil && existed:
			files = append(files, r.file)
			counts.Updated++
		case r.err == nil:
			files = append(files, r.file)
			counts.Added++
		default:
			counts.Skipped++
			if old, ok := known[r.cand.rel]; ok {
				files = append(files, old) // keep the last good tags
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, counts, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return files, counts, nil
}

// readFile reads the tags of one candidate.
func readFile(root string, c candidate) (File, error) {
	tags, err := playlist.ReadFileTags(filepath.Join(root, filepath.FromSlash(c.rel)))
	if err != nil {
		return File{}, err
	}
	t := tags.Track
	return File{
		Rel:         c.rel,
		ModUnix:     c.modUnix,
		Size:        c.size,
		Title:       t.Title,
		Artist:      t.Artist,
		AlbumArtist: tags.AlbumArtist,
		Album:       t.Album,
		Genre:       t.Genre,
		Year:        t.Year,
		TrackNumber: t.TrackNumber,
		Disc:        tags.Disc,
	}, nil
}
