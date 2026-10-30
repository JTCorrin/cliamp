package playlist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileTags(t *testing.T) {
	dir := t.TempDir()
	untagged := filepath.Join(dir, "Some Artist - Some Song.mp3")
	if err := os.WriteFile(untagged, []byte("not audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(dir, "bare.flac")
	if err := os.WriteFile(bare, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		path       string
		wantErr    bool
		wantTitle  string
		wantArtist string
	}{
		{"file name with artist", untagged, false, "Some Song", "Some Artist"},
		{"file name only", bare, false, "bare", ""},
		{"missing file", filepath.Join(dir, "missing.mp3"), true, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadFileTags(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want error %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Track.Title != tt.wantTitle || got.Track.Artist != tt.wantArtist {
				t.Errorf("title, artist = %q, %q; want %q, %q", got.Track.Title, got.Track.Artist, tt.wantTitle, tt.wantArtist)
			}
			if got.Track.Path != tt.path {
				t.Errorf("Path = %q, want %q", got.Track.Path, tt.path)
			}
		})
	}
}
