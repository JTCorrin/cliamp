package library

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// id3Frame returns one ID3v2.3 text frame.
func id3Frame(id, text string) []byte {
	body := append([]byte{0}, text...) // encoding 0: ISO-8859-1
	frame := append([]byte(id), make([]byte, 4)...)
	binary.BigEndian.PutUint32(frame[4:], uint32(len(body)))
	frame = append(frame, 0, 0)
	return append(frame, body...)
}

// writeMP3 writes a file that holds only an ID3v2.3 tag, which is all that
// the tag reader needs. Empty fields are left out of the tag.
func writeMP3(t *testing.T, path string, fields map[string]string) {
	t.Helper()
	var frames []byte
	for id, v := range fields {
		frames = append(frames, id3Frame(id, v)...)
	}
	n := len(frames)
	size := []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	data := append([]byte("ID3\x03\x00\x00"), size...)
	data = append(data, frames...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func tagsFor(title, artist, album string, track int) map[string]string {
	return map[string]string{
		"TIT2": title, "TPE1": artist, "TALB": album,
		"TRCK": itoa(track), "TPOS": "1/2", "TPE2": artist, "TYER": "1999",
	}
}

func itoa(n int) string {
	if n == 0 {
		return ""
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func rels(r Root) []string {
	var out []string
	for _, f := range r.Files {
		out = append(out, f.Rel)
	}
	return out
}

func TestScanReadsTags(t *testing.T) {
	root := t.TempDir()
	writeMP3(t, filepath.Join(root, "Artist", "Album", "01.mp3"), tagsFor("Song One", "The Artist", "The Album", 1))
	writeMP3(t, filepath.Join(root, "Artist", "Album", "02.mp3"), tagsFor("Song Two", "The Artist", "The Album", 2))
	if err := os.WriteFile(filepath.Join(root, "cover.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ix, res, err := Scan(context.Background(), []string{root}, nil, 2, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.Added != 2 || ix.Len() != 2 {
		t.Fatalf("Added, Len = %d, %d; want 2, 2 (%+v)", res.Added, ix.Len(), res)
	}
	f := ix.Roots[0].Files[0]
	if f.Rel != "Artist/Album/01.mp3" {
		t.Errorf("Rel = %q", f.Rel)
	}
	if f.Title != "Song One" || f.Artist != "The Artist" || f.Album != "The Album" || f.AlbumArtist != "The Artist" {
		t.Errorf("tags = %+v", f)
	}
	if f.TrackNumber != 1 || f.Disc != 1 || f.Year != 1999 {
		t.Errorf("numbers = track %d disc %d year %d", f.TrackNumber, f.Disc, f.Year)
	}
	tr := f.Track(root)
	if tr.Path != filepath.Join(root, "Artist", "Album", "01.mp3") || tr.ProviderMeta["library.id"] != TrackID(root, f.Rel) {
		t.Errorf("track = %+v", tr)
	}
}

func TestScanSkipsMetadataNames(t *testing.T) {
	root := t.TempDir()
	writeMP3(t, filepath.Join(root, "keep.mp3"), tagsFor("Keep", "A", "B", 1))
	writeMP3(t, filepath.Join(root, "._keep.mp3"), tagsFor("Fork", "A", "B", 1))
	writeMP3(t, filepath.Join(root, "@eaDir", "x.mp3"), tagsFor("Thumb", "A", "B", 1))
	writeMP3(t, filepath.Join(root, ".hidden", "y.mp3"), tagsFor("Hidden", "A", "B", 1))
	ix, _, err := Scan(context.Background(), []string{root}, nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := rels(ix.Roots[0]); !slices.Equal(got, []string{"keep.mp3"}) {
		t.Errorf("files = %v", got)
	}
}

func TestScanIncremental(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.mp3")
	b := filepath.Join(root, "b.mp3")
	writeMP3(t, a, tagsFor("A", "X", "Y", 1))
	writeMP3(t, b, tagsFor("B", "X", "Y", 2))
	first, _, err := Scan(context.Background(), []string{root}, nil, 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Change b, remove a, add c.
	writeMP3(t, b, tagsFor("B changed", "X", "Y", 2))
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(b, later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	writeMP3(t, filepath.Join(root, "c.mp3"), tagsFor("C", "X", "Y", 3))

	var reads atomic.Int32
	second, res, err := Scan(context.Background(), []string{root}, first, 2, func(Progress) { reads.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || res.Updated != 1 || res.Removed != 1 || res.Unchanged != 0 {
		t.Errorf("result = %+v", res)
	}
	if reads.Load() != 2 {
		t.Errorf("progress calls = %d, want 2 (only b and c are read)", reads.Load())
	}
	if got := rels(second.Roots[0]); !slices.Equal(got, []string{"b.mp3", "c.mp3"}) {
		t.Errorf("files = %v", got)
	}
	if second.Roots[0].Files[0].Title != "B changed" {
		t.Errorf("b.mp3 title = %q", second.Roots[0].Files[0].Title)
	}
	if len(first.Roots[0].Files) != 2 || first.Roots[0].Files[0].Rel != "a.mp3" {
		t.Error("Scan changed the old index")
	}

	third, res, err := Scan(context.Background(), []string{root}, second, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Unchanged != 2 || res.Added+res.Updated+res.Removed != 0 || third.Len() != 2 {
		t.Errorf("unchanged rescan = %+v", res)
	}
}

func TestScanKeepsCacheWhenRootIsUnavailable(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "share")
	writeMP3(t, filepath.Join(root, "a.mp3"), tagsFor("A", "X", "Y", 1))
	first, _, err := Scan(context.Background(), []string{root}, nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"unmounted share is an empty folder", func(t *testing.T) {
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"folder is gone", func(t *testing.T) {
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			got, res, err := Scan(context.Background(), []string{root}, first, 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.Len() != 1 || res.Removed != 0 || !slices.Equal(res.Unavailable, []string{root}) {
				t.Errorf("Len = %d, result = %+v; want the cached file kept", got.Len(), res)
			}
		})
	}
}

func TestScanNewEmptyRootHasNoFiles(t *testing.T) {
	root := t.TempDir()
	ix, res, err := Scan(context.Background(), []string{root}, nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ix.Len() != 0 || len(res.Unavailable) != 0 || len(ix.Roots) != 1 {
		t.Errorf("Len = %d, roots = %d, result = %+v", ix.Len(), len(ix.Roots), res)
	}
}

func TestScanDropsRemovedRoot(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	writeMP3(t, filepath.Join(one, "a.mp3"), tagsFor("A", "X", "Y", 1))
	writeMP3(t, filepath.Join(two, "b.mp3"), tagsFor("B", "X", "Y", 1))
	first, _, err := Scan(context.Background(), []string{one, two}, nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, res, err := Scan(context.Background(), []string{one}, first, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 1 || len(got.Roots) != 1 || res.Removed != 1 {
		t.Errorf("Len = %d, roots = %d, result = %+v", got.Len(), len(got.Roots), res)
	}
}

func TestScanCancel(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		writeMP3(t, filepath.Join(root, string(rune('a'+i))+".mp3"), tagsFor("T", "X", "Y", 1))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ix, _, err := Scan(ctx, []string{root}, nil, 2, nil)
	if err != context.Canceled || ix != nil {
		t.Errorf("Scan = %v, %v; want nil, context.Canceled", ix, err)
	}
}

func TestExpandRoot(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	tests := []struct{ in, want string }{
		{"~", "/home/u"},
		{"~/Music", "/home/u/Music"},
		{"/mnt/nas//music/", "/mnt/nas/music"},
	}
	for _, tt := range tests {
		if got := ExpandRoot(tt.in); got != tt.want {
			t.Errorf("ExpandRoot(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	// A relative path does not depend on the working directory afterwards.
	if got := ExpandRoot("music"); !filepath.IsAbs(got) || filepath.Base(got) != "music" {
		t.Errorf("ExpandRoot(music) = %q, want an absolute path", got)
	}
}

func TestIndexRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeMP3(t, filepath.Join(root, "a.mp3"), tagsFor("A", "X", "Y", 1))
	ix, _, err := Scan(context.Background(), []string{root}, nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sub", "index.gob")
	if err := SaveIndex(path, ix); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 1 || got.Roots[0].Files[0].Title != "A" || got.Roots[0].Path != root {
		t.Errorf("loaded = %+v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want only the index", len(entries))
	}
}

func TestLoadIndexRecovers(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "corrupt.gob")
	if err := os.WriteFile(corrupt, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(dir, "none.gob"), "corrupt": corrupt} {
		t.Run(name, func(t *testing.T) {
			ix, err := LoadIndex(path)
			if err != nil || ix == nil || ix.Len() != 0 || ix.Version != indexVersion {
				t.Errorf("LoadIndex = %+v, %v; want an empty index", ix, err)
			}
		})
	}
}

// WalkDir does not follow a root that is a symlink, such as ~/Music that
// points at the share.
func TestScanFollowsSymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	writeMP3(t, filepath.Join(real, "sub", "a.mp3"), tagsFor("A", "X", "Y", 1))
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ix, res, err := Scan(context.Background(), []string{link}, nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ix.Len() != 1 || len(res.Unavailable) != 0 {
		t.Fatalf("Len = %d, result = %+v; want the file below the link", ix.Len(), res)
	}
	// The root keeps the configured path, so IDs and track paths use it.
	if ix.Roots[0].Path != link {
		t.Errorf("root path = %q, want %q", ix.Roots[0].Path, link)
	}
	// A rescan through the link sees the file as unchanged.
	again, res, err := Scan(context.Background(), []string{link}, ix, 1, nil)
	if err != nil || again.Len() != 1 || res.Unchanged != 1 {
		t.Errorf("rescan: Len = %d, result = %+v, err = %v", again.Len(), res, err)
	}
}

// A folder that the walk could not read is not proof that its files are gone:
// the cached entries below it stay.
func TestReadRootKeepsFilesBelowSkippedPaths(t *testing.T) {
	root := t.TempDir()
	writeMP3(t, filepath.Join(root, "ok", "a.mp3"), tagsFor("A", "X", "Y", 1))
	prev := &Root{Path: root, Files: []File{
		{Rel: "gone/b.mp3", Title: "B"},
		{Rel: "gone/deep/c.mp3", Title: "C"},
		{Rel: "gone2/d.mp3", Title: "D"},
		{Rel: "ok/a.mp3", Title: "stale"},
	}}
	cands := []candidate{{rel: "ok/a.mp3", modUnix: 1, size: 1}}

	files, counts, err := readRoot(context.Background(), root, cands, []string{"gone"}, prev, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Rel)
	}
	// gone/ is kept, gone2/ is a different folder and is removed, and a.mp3 is re-read.
	if want := []string{"gone/b.mp3", "gone/deep/c.mp3", "ok/a.mp3"}; !slices.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	if counts.Removed != 1 || counts.Updated != 1 || counts.Unchanged != 2 {
		t.Errorf("counts = %+v", counts)
	}
	if !underSkipped("gone/x.mp3", []string{"gone"}) || underSkipped("gone2/x.mp3", []string{"gone"}) || !underSkipped("gone", []string{"gone"}) {
		t.Error("underSkipped matches a folder by name prefix, not by path")
	}
}

// A file with artist and album tags but no title keeps those tags.
func TestScanKeepsTagsWithoutTitle(t *testing.T) {
	root := t.TempDir()
	writeMP3(t, filepath.Join(root, "track7.mp3"), map[string]string{"TPE1": "Real Artist", "TALB": "Real Album", "TPE2": "Real Artist", "TRCK": "07"})
	ix, _, err := Scan(context.Background(), []string{root}, nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := ix.Roots[0].Files[0]
	if f.Title != "track7" || f.Artist != "Real Artist" || f.Album != "Real Album" || f.AlbumArtist != "Real Artist" || f.TrackNumber != 7 {
		t.Errorf("file = %+v; want the tags kept and the title from the file name", f)
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
