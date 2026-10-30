package library

import (
	"slices"
	"testing"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

func browseFixture(t *testing.T) *Provider {
	t.Helper()
	files := []File{
		{Rel: "Floyd/Wall/101.mp3", Title: "In the Flesh", Artist: "Pink Floyd", AlbumArtist: "Pink Floyd", Album: "The Wall", Year: 1979, Disc: 1, TrackNumber: 1, ModUnix: 100},
		{Rel: "Floyd/Wall/201.mp3", Title: "Hey You", Artist: "Pink Floyd", AlbumArtist: "Pink Floyd", Album: "The Wall", Year: 1979, Disc: 2, TrackNumber: 1, ModUnix: 100},
		{Rel: "Floyd/Wall/102.mp3", Title: "The Thin Ice", Artist: "Pink Floyd", AlbumArtist: "Pink Floyd", Album: "The Wall", Year: 1979, Disc: 1, TrackNumber: 2, ModUnix: 100},
		{Rel: "Floyd/Dark/01.mp3", Title: "Speak to Me", Artist: "pink floyd", AlbumArtist: "pink floyd", Album: "The Dark Side of the Moon", Year: 1973, TrackNumber: 1, ModUnix: 300},
		{Rel: "Abba/Gold/01.mp3", Title: "Dancing Queen", Artist: "ABBA", Album: "Gold", Year: 1992, TrackNumber: 1, ModUnix: 200},
		// A compilation without an album artist tag.
		{Rel: "Hits/01.mp3", Title: "One", Artist: "Band A", Album: "Hits 1", TrackNumber: 1, ModUnix: 50},
		{Rel: "Hits/02.mp3", Title: "Two", Artist: "Band B", Album: "Hits 1", TrackNumber: 2, ModUnix: 50},
		{Rel: "Loose/song.mp3", Title: "Loose", ModUnix: 10},
		{Rel: "Loose/song2.mp3", Title: "Loose 2", Artist: "Solo", ModUnix: 10},
	}
	slices.SortFunc(files, func(a, b File) int {
		switch {
		case a.Rel < b.Rel:
			return -1
		case a.Rel > b.Rel:
			return 1
		}
		return 0
	})
	return testProvider(t, files)
}

func artistNames(t *testing.T, p *Provider) []string {
	t.Helper()
	artists, err := p.Artists()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range artists {
		out = append(out, a.Name)
	}
	return out
}

func TestArtists(t *testing.T) {
	p := browseFixture(t)
	// Case variants of a name merge, and the most used spelling is shown.
	want := []string{"ABBA", "Pink Floyd", "Solo", "Unknown Artist", "Various Artists"}
	if got := artistNames(t, p); !slices.Equal(got, want) {
		t.Errorf("Artists = %v, want %v", got, want)
	}
	artists, _ := p.Artists()
	for _, a := range artists {
		if a.Name == "Pink Floyd" && a.AlbumCount != 2 {
			t.Errorf("Pink Floyd AlbumCount = %d, want 2", a.AlbumCount)
		}
	}
}

func TestArtistAlbums(t *testing.T) {
	p := browseFixture(t)
	artists, _ := p.Artists()
	var floyd provider.ArtistInfo
	for _, a := range artists {
		if a.Name == "Pink Floyd" {
			floyd = a
		}
	}
	albums, err := p.ArtistAlbums(floyd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 2 || albums[0].Name != "The Dark Side of the Moon" || albums[1].Name != "The Wall" {
		t.Fatalf("albums = %+v, want oldest first", albums)
	}
	if albums[1].TrackCount != 3 || albums[1].Year != 1979 || albums[1].Artist != "Pink Floyd" || albums[1].ArtistID != floyd.ID {
		t.Errorf("The Wall = %+v", albums[1])
	}
	if _, err := p.ArtistAlbums("nope"); err == nil {
		t.Error("an unknown artist must be an error")
	}
}

func TestAlbumTracksOrder(t *testing.T) {
	p := browseFixture(t)
	albums, _ := p.AlbumList(sortByName, 0, 100)
	var wall provider.AlbumInfo
	for _, a := range albums {
		if a.Name == "The Wall" {
			wall = a
		}
	}
	tracks, err := p.AlbumTracks(wall.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tr := range tracks {
		got = append(got, tr.Title)
	}
	if want := []string{"In the Flesh", "The Thin Ice", "Hey You"}; !slices.Equal(got, want) {
		t.Errorf("tracks = %v, want disc then track order %v", got, want)
	}
	if tracks[0].Path != "/music/Floyd/Wall/101.mp3" {
		t.Errorf("Path = %q", tracks[0].Path)
	}
	if _, err := p.AlbumTracks("nope"); err == nil {
		t.Error("an unknown album must be an error")
	}
}

func TestCompilationGroupsUnderVariousArtists(t *testing.T) {
	p := browseFixture(t)
	albums, _ := p.AlbumList(sortByName, 0, 100)
	for _, a := range albums {
		if a.Name == "Hits 1" {
			if a.Artist != "Various Artists" || a.TrackCount != 2 {
				t.Errorf("Hits 1 = %+v", a)
			}
			return
		}
	}
	t.Error("Hits 1 not listed")
}

func TestAlbumListSorts(t *testing.T) {
	p := browseFixture(t)
	names := func(sortType string, offset, size int) []string {
		albums, err := p.AlbumList(sortType, offset, size)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, a := range albums {
			out = append(out, a.Name)
		}
		return out
	}
	tests := []struct {
		name string
		sort string
		want []string
	}{
		{"name", sortByName, []string{"The Dark Side of the Moon", "Gold", "Hits 1", "Unknown Album", "Unknown Album", "The Wall"}},
		{"artist", sortByArtist, []string{"Gold", "The Dark Side of the Moon", "The Wall", "Unknown Album", "Unknown Album", "Hits 1"}},
		{"newest", sortNewest, []string{"The Dark Side of the Moon", "Gold", "The Wall", "Hits 1", "Unknown Album", "Unknown Album"}},
		{"year", sortByYear, []string{"Hits 1", "Unknown Album", "Unknown Album", "The Dark Side of the Moon", "The Wall", "Gold"}},
		{"unknown sorts by name", "bogus", []string{"The Dark Side of the Moon", "Gold", "Hits 1", "Unknown Album", "Unknown Album", "The Wall"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := names(tt.sort, 0, 100); !slices.Equal(got, tt.want) {
				t.Errorf("AlbumList(%q) = %v, want %v", tt.sort, got, tt.want)
			}
		})
	}
	t.Run("paging", func(t *testing.T) {
		all := names(sortByName, 0, 100)
		var paged []string
		for off := 0; ; off += 4 {
			page := names(sortByName, off, 4)
			if len(page) == 0 {
				break
			}
			paged = append(paged, page...)
		}
		if !slices.Equal(paged, all) {
			t.Errorf("pages = %v, want %v", paged, all)
		}
		if got := names(sortByName, 100, 4); got != nil {
			t.Errorf("past the end = %v, want none", got)
		}
		if got := names(sortByName, -5, 2); len(got) != 2 {
			t.Errorf("negative offset gave %d albums, want 2", len(got))
		}
	})
}

func TestIDsSurviveRescan(t *testing.T) {
	a := browseFixture(t)
	b := browseFixture(t)
	ids := func(p *Provider) []string {
		albums, _ := p.AlbumList(sortByName, 0, 100)
		var out []string
		for _, al := range albums {
			out = append(out, al.ID)
		}
		return out
	}
	if !slices.Equal(ids(a), ids(b)) {
		t.Error("album IDs differ between two builds of the same index")
	}
	seen := map[string]bool{}
	for _, id := range ids(a) {
		if seen[id] {
			t.Errorf("duplicate album ID %q", id)
		}
		seen[id] = true
	}
}

func TestArtistForTrack(t *testing.T) {
	p := browseFixture(t)
	tr := playlist.Track{Path: "/music/Abba/Gold/01.mp3"}
	a, ok := p.ArtistForTrack(tr)
	if !ok || a.Name != "ABBA" {
		t.Errorf("ArtistForTrack = %+v, %v", a, ok)
	}
	for _, path := range []string{"/music/Abba/Gold/missing.mp3", "/elsewhere/x.mp3", "/music/../etc/passwd"} {
		if _, ok := p.ArtistForTrack(playlist.Track{Path: path}); ok {
			t.Errorf("ArtistForTrack(%q) = true, want false", path)
		}
	}
}

func TestBrowseSeesNewIndex(t *testing.T) {
	p := testProvider(t, []File{{Rel: "a.mp3", Title: "A", Artist: "One", Album: "X"}})
	if got := artistNames(t, p); !slices.Equal(got, []string{"One"}) {
		t.Fatalf("before = %v", got)
	}
	p.mu.Lock()
	p.idx = &Index{Version: indexVersion, Roots: []Root{{Path: "/music", Files: []File{{Rel: "b.mp3", Title: "B", Artist: "Two", Album: "Y"}}}}}
	p.mu.Unlock()
	if got := artistNames(t, p); !slices.Equal(got, []string{"Two"}) {
		t.Errorf("after = %v", got)
	}
}

func TestBrowseEntries(t *testing.T) {
	p := browseFixture(t)
	entries := p.BrowseEntries()
	if len(entries) != 2 || entries[0].Mode != provider.BrowseArtistAlbums || entries[1].Mode != provider.BrowseAlbums {
		t.Errorf("entries = %+v", entries)
	}
	for _, e := range entries {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entry %+v needs an ID and a name", e)
		}
	}
}

func TestBrowseEntriesShowScanState(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   string // the name of the third entry, or empty for none
	}{
		{"idle", Status{}, ""},
		{"scanning, count unknown", Status{Scanning: true}, "Scanning for new files"},
		{"scanning", Status{Scanning: true, Progress: Progress{Done: 12, Total: 50}}, "Scanning: 12 of 50 new files"},
		{"failed", Status{Err: "boom"}, "Scan failed: boom"},
		{"unmounted", Status{Last: ScanResult{Unavailable: []string{"/mnt/a", "/mnt/b"}}}, "Not readable, showing the saved index: /mnt/a, /mnt/b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := browseFixture(t)
			p.mu.Lock()
			p.status = tt.status
			p.mu.Unlock()
			entries := p.BrowseEntries()
			if tt.want == "" {
				if len(entries) != 2 {
					t.Fatalf("entries = %+v, want only Artists and Albums", entries)
				}
				return
			}
			if len(entries) != 3 || entries[2].Name != tt.want || entries[2].ID != entryStatus {
				t.Errorf("entries = %+v, want a status entry %q", entries, tt.want)
			}
		})
	}
}

func BenchmarkBuildBrowseIndex100k(b *testing.B) {
	p := testProvider(b, syntheticFiles(100_000))
	ix := p.index()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildBrowseIndex(ix)
	}
}
