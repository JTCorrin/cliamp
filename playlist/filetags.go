package playlist

import (
	"os"
	"strings"

	"github.com/dhowden/tag"
)

// FileTags is what ReadFileTags reads from an audio file: the Track fields,
// and the tag fields that a Track does not carry.
type FileTags struct {
	Track       Track
	AlbumArtist string
	Disc        int
}

// ReadFileTags reads the embedded tags of the local audio file at path with
// one pass over the file. It falls back to the WAV INFO list and then to the
// file name, as TrackFromPath does. It returns an error only when the file
// cannot be opened.
func ReadFileTags(path string) (FileTags, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileTags{}, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil || m == nil {
		if t, ok := readWAVInfo(f, path); ok {
			return FileTags{Track: t}, nil
		}
		return FileTags{Track: TrackFromFilename(path)}, nil
	}
	t := Track{
		Path:   path,
		Title:  sanitizeTag(strings.TrimSpace(m.Title())),
		Artist: sanitizeTag(strings.TrimSpace(m.Artist())),
		Album:  sanitizeTag(strings.TrimSpace(m.Album())),
		Genre:  sanitizeTag(strings.TrimSpace(m.Genre())),
		Year:   m.Year(),
	}
	if t.Title == "" {
		// Keep the tags that exist: only the title, and an artist that the
		// tags lack, come from the file name.
		fallback := TrackFromFilename(path)
		t.Title = fallback.Title
		if t.Artist == "" {
			t.Artist = fallback.Artist
		}
	}
	t.TrackNumber, _ = m.Track()
	disc, _ := m.Disc()
	return FileTags{
		Track:       t,
		AlbumArtist: sanitizeTag(strings.TrimSpace(m.AlbumArtist())),
		Disc:        disc,
	}, nil
}
