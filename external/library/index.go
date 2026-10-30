package library

import (
	"crypto/sha1"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// indexVersion changes when the stored layout of the index changes. An index
// of another version is dropped and rebuilt by the next scan.
const indexVersion = 1

// File is one audio file of the index. Rel is the path below the root of the
// File, with forward slashes.
type File struct {
	Rel         string
	ModUnix     int64
	Size        int64
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	Genre       string
	Year        int
	TrackNumber int
	Disc        int
}

// Root holds the files of one music folder.
type Root struct {
	Path  string
	Files []File
}

// Index is the set of files that the scans found. The zero value is an empty
// index.
type Index struct {
	Version int
	Roots   []Root
}

// Len returns the number of files in the index.
func (ix *Index) Len() int {
	n := 0
	for _, r := range ix.Roots {
		n += len(r.Files)
	}
	return n
}

// root returns the root with the path, or nil.
func (ix *Index) root(path string) *Root {
	for i := range ix.Roots {
		if ix.Roots[i].Path == path {
			return &ix.Roots[i]
		}
	}
	return nil
}

// TrackID returns the stable ID of a file: it depends only on the root and
// the relative path, so it survives rescans and tag edits.
func TrackID(root, rel string) string {
	sum := sha1.Sum([]byte(root + "\x00" + rel))
	return hex.EncodeToString(sum[:8])
}

// Track returns the playlist track of the file below root.
func (f File) Track(root string) playlist.Track {
	return playlist.Track{
		Path:        filepath.Join(root, filepath.FromSlash(f.Rel)),
		Title:       f.Title,
		Artist:      f.Artist,
		Album:       f.Album,
		Genre:       f.Genre,
		Year:        f.Year,
		TrackNumber: f.TrackNumber,
		ProviderMeta: map[string]string{
			provider.MetaLibraryID: TrackID(root, f.Rel),
		},
	}
}

// LoadIndex reads the index file at path. A missing file, a file of another
// version and a file that does not decode all return an empty index and no
// error, because the next scan rebuilds the index. Any other error is
// returned.
func LoadIndex(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Index{Version: indexVersion}, nil
		}
		return nil, fmt.Errorf("open library index: %w", err)
	}
	defer f.Close()
	var ix Index
	if err := gob.NewDecoder(f).Decode(&ix); err != nil || ix.Version != indexVersion {
		return &Index{Version: indexVersion}, nil
	}
	return &ix, nil
}

// SaveIndex writes the index to path through a temporary file and a rename,
// so a crash never leaves half an index.
func SaveIndex(path string, ix *Index) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create library index directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".index-*.tmp")
	if err != nil {
		return fmt.Errorf("create library index: %w", err)
	}
	defer os.Remove(tmp.Name())
	ix.Version = indexVersion
	if err := gob.NewEncoder(tmp).Encode(ix); err != nil {
		tmp.Close()
		return fmt.Errorf("write library index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write library index: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save library index: %w", err)
	}
	return nil
}
