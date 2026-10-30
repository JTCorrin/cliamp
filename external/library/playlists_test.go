package library

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bjarneo/cliamp/external/local"
	"github.com/bjarneo/cliamp/playlist"
)

// storeProvider returns a provider over real music files, with the local
// provider as the store of its playlists, in a temporary config directory.
func storeProvider(t *testing.T) *Provider {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"CLIAMP_CONFIG_DIR", "XDG_CONFIG_HOME"} {
		t.Setenv(name, "")
	}
	root := t.TempDir()
	writeMP3(t, filepath.Join(root, "Band", "Album", "01.mp3"), tagsFor("First Song", "The Band", "The Album", 1))
	writeMP3(t, filepath.Join(root, "Band", "Album", "02.mp3"), tagsFor("Second Song", "The Band", "The Album", 2))
	p := testProvider(t, nil)
	ix, _, err := Scan(context.Background(), []string{root}, nil, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.idx = ix
	store := local.New(nil, nil)
	if store == nil {
		t.Fatal("local.New returned nil")
	}
	p.SetPlaylists(store)
	return p
}

func TestPlaylistsRoundTrip(t *testing.T) {
	p := storeProvider(t)
	ctx := context.Background()

	if lists, err := p.Playlists(); err != nil || len(lists) != 0 {
		t.Fatalf("Playlists before = %v, %v; want none", lists, err)
	}
	found, err := p.SearchTracks(ctx, "song", 10)
	if err != nil || len(found) != 2 {
		t.Fatalf("SearchTracks = %v, %v", found, err)
	}

	id, err := p.CreatePlaylist(ctx, "Road trip")
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}
	if err := p.AddTrackToPlaylist(ctx, id, found[0]); err != nil {
		t.Fatalf("AddTrackToPlaylist: %v", err)
	}
	added, skipped, err := p.AddTracksToPlaylist(ctx, id, found)
	if err != nil || added != 1 || skipped != 1 {
		t.Fatalf("AddTracksToPlaylist = %d, %d, %v; want 1 added, 1 skipped (a duplicate)", added, skipped, err)
	}

	lists, err := p.Playlists()
	if err != nil || len(lists) != 1 || lists[0].Name != "Road trip" || lists[0].TrackCount != 2 {
		t.Fatalf("Playlists = %+v, %v", lists, err)
	}
	if !p.CanAddToPlaylist(lists[0]) {
		t.Error("CanAddToPlaylist = false for a saved playlist")
	}

	tracks, err := p.Tracks(id)
	if err != nil || len(tracks) != 2 {
		t.Fatalf("Tracks = %v, %v", tracks, err)
	}
	for i, tr := range tracks {
		if tr.Path != found[i].Path || tr.Title != found[i].Title {
			t.Errorf("track %d = %+v, want %+v", i, tr, found[i])
		}
		if tr.ProviderMeta["library.id"] != found[i].ProviderMeta["library.id"] || tr.ProviderMeta["library.id"] == "" {
			t.Errorf("track %d lost its library ID: %v", i, tr.ProviderMeta)
		}
	}

	if err := p.RemoveTrack(id, 0); err != nil {
		t.Fatalf("RemoveTrack: %v", err)
	}
	if err := p.RenamePlaylist(id, "Highway"); err != nil {
		t.Fatalf("RenamePlaylist: %v", err)
	}
	tracks, err = p.Tracks("Highway")
	if err != nil || len(tracks) != 1 || tracks[0].Title != found[1].Title {
		t.Fatalf("Tracks after remove and rename = %v, %v", tracks, err)
	}
	if err := p.SavePlaylist("Highway", found); err != nil {
		t.Fatalf("SavePlaylist: %v", err)
	}
	if _, _, _, err := p.PrependTracksToPlaylist(ctx, "Highway", found[1:]); err != nil {
		t.Fatalf("PrependTracksToPlaylist: %v", err)
	}
	doc, err := p.PlaylistDocument("Highway")
	if err != nil || len(doc) == 0 {
		t.Fatalf("PlaylistDocument = %d bytes, %v", len(doc), err)
	}
	if err := p.DeletePlaylist("Highway"); err != nil {
		t.Fatalf("DeletePlaylist: %v", err)
	}
	if err := p.RestorePlaylistDocument("Highway", doc); err != nil {
		t.Fatalf("RestorePlaylistDocument: %v", err)
	}
	if got, _ := p.Tracks("Highway"); len(got) != 2 {
		t.Errorf("restored playlist holds %d tracks, want 2", len(got))
	}
}

// A playlist of library tracks plays from the local provider alone: the file
// holds the path and the tags.
func TestPlaylistFileIsSelfContained(t *testing.T) {
	p := storeProvider(t)
	ctx := context.Background()
	found, _ := p.SearchTracks(ctx, "first", 1)
	id, _ := p.CreatePlaylist(ctx, "Solo")
	if err := p.AddTrackToPlaylist(ctx, id, found[0]); err != nil {
		t.Fatal(err)
	}
	other := local.New(nil, nil)
	tracks, err := other.Tracks("Solo")
	if err != nil || len(tracks) != 1 || tracks[0].Path != found[0].Path {
		t.Errorf("local provider read %v, %v", tracks, err)
	}
}

func TestPlaylistsWithoutStore(t *testing.T) {
	p := testProvider(t, nil)
	ctx := context.Background()
	if lists, err := p.Playlists(); err != nil || lists != nil {
		t.Errorf("Playlists = %v, %v; want none", lists, err)
	}
	if p.CanAddToPlaylist(playlist.PlaylistInfo{ID: "x"}) {
		t.Error("CanAddToPlaylist = true with no store")
	}
	errs := map[string]error{}
	_, errs["Tracks"] = p.Tracks("x")
	_, errs["CreatePlaylist"] = p.CreatePlaylist(ctx, "x")
	errs["AddTrackToPlaylist"] = p.AddTrackToPlaylist(ctx, "x", playlist.Track{})
	_, _, errs["AddTracksToPlaylist"] = p.AddTracksToPlaylist(ctx, "x", nil)
	errs["DeletePlaylist"] = p.DeletePlaylist("x")
	errs["RemoveTrack"] = p.RemoveTrack("x", 0)
	errs["RenamePlaylist"] = p.RenamePlaylist("x", "y")
	errs["SavePlaylist"] = p.SavePlaylist("x", nil)
	_, _, _, errs["PrependTracksToPlaylist"] = p.PrependTracksToPlaylist(ctx, "x", nil)
	_, errs["PlaylistDocument"] = p.PlaylistDocument("x")
	errs["RestorePlaylistDocument"] = p.RestorePlaylistDocument("x", nil)
	for name, err := range errs {
		if err != errNoStore {
			t.Errorf("%s error = %v, want errNoStore", name, err)
		}
	}
	if len(errs) != 11 {
		t.Errorf("checked %d methods, want all 11 store methods", len(errs))
	}
}

func TestTracksOfPaneLinesAreEmpty(t *testing.T) {
	p := storeProvider(t)
	for _, id := range []string{entryArtists, entryAlbums, entryStatus} {
		if tracks, err := p.Tracks(id); err != nil || len(tracks) != 0 {
			t.Errorf("Tracks(%q) = %v, %v; want none and no error", id, tracks, err)
		}
	}
}
