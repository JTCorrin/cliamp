package library

import (
	"context"
	"errors"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// PlaylistStore is where the playlists of the library are kept. The local
// provider is the store, so a playlist of library tracks is a TOML file like
// any other playlist, and it plays without the library.
type PlaylistStore interface {
	playlist.Provider
	provider.PlaylistCreator
	provider.PlaylistWriter
	provider.PlaylistBatchWriter
	provider.PlaylistTargetFilter
	provider.PlaylistDeleter
	provider.PlaylistRenamer
	provider.PlaylistSaver
	provider.PlaylistPrepender
	provider.PlaylistDocumenter
}

var (
	_ provider.PlaylistCreator      = (*Provider)(nil)
	_ provider.PlaylistWriter       = (*Provider)(nil)
	_ provider.PlaylistBatchWriter  = (*Provider)(nil)
	_ provider.PlaylistTargetFilter = (*Provider)(nil)
	_ provider.PlaylistDeleter      = (*Provider)(nil)
	_ provider.PlaylistRenamer      = (*Provider)(nil)
	_ provider.PlaylistSaver        = (*Provider)(nil)
	_ provider.PlaylistPrepender    = (*Provider)(nil)
	_ provider.PlaylistDocumenter   = (*Provider)(nil)
)

// errNoStore is returned by the playlist methods of a provider that has no
// store.
var errNoStore = errors.New("library: no playlist storage")

// SetPlaylists sets the store of the playlists. Call it before the provider
// is used. A nil store leaves the library without playlists.
func (p *Provider) SetPlaylists(store PlaylistStore) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.store = store
}

func (p *Provider) playlists() PlaylistStore {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.store
}

// Playlists returns the playlists of the store.
func (p *Provider) Playlists() ([]playlist.PlaylistInfo, error) {
	if s := p.playlists(); s != nil {
		return s.Playlists()
	}
	return nil, nil
}

// Tracks returns the tracks of a playlist of the store.
func (p *Provider) Tracks(playlistID string) ([]playlist.Track, error) {
	switch playlistID {
	case entryArtists, entryAlbums, entryStatus:
		// A line of the provider pane, not a playlist: the store has no file
		// of that name.
		return nil, nil
	}
	s := p.playlists()
	if s == nil {
		return nil, errNoStore
	}
	return s.Tracks(playlistID)
}

// CreatePlaylist creates an empty playlist in the store.
func (p *Provider) CreatePlaylist(ctx context.Context, name string) (string, error) {
	s := p.playlists()
	if s == nil {
		return "", errNoStore
	}
	return s.CreatePlaylist(ctx, name)
}

// AddTrackToPlaylist appends a track to a playlist of the store.
func (p *Provider) AddTrackToPlaylist(ctx context.Context, playlistID string, track playlist.Track) error {
	s := p.playlists()
	if s == nil {
		return errNoStore
	}
	return s.AddTrackToPlaylist(ctx, playlistID, track)
}

// AddTracksToPlaylist appends tracks to a playlist of the store.
func (p *Provider) AddTracksToPlaylist(ctx context.Context, playlistID string, tracks []playlist.Track) (int, int, error) {
	s := p.playlists()
	if s == nil {
		return 0, 0, errNoStore
	}
	return s.AddTracksToPlaylist(ctx, playlistID, tracks)
}

// CanAddToPlaylist reports whether the store takes tracks in pl.
func (p *Provider) CanAddToPlaylist(pl playlist.PlaylistInfo) bool {
	s := p.playlists()
	return s != nil && s.CanAddToPlaylist(pl)
}

// DeletePlaylist removes a playlist of the store.
func (p *Provider) DeletePlaylist(name string) error {
	s := p.playlists()
	if s == nil {
		return errNoStore
	}
	return s.DeletePlaylist(name)
}

// RemoveTrack removes one track from a playlist of the store.
func (p *Provider) RemoveTrack(name string, index int) error {
	s := p.playlists()
	if s == nil {
		return errNoStore
	}
	return s.RemoveTrack(name, index)
}

// RenamePlaylist renames a playlist of the store.
func (p *Provider) RenamePlaylist(oldName, newName string) error {
	s := p.playlists()
	if s == nil {
		return errNoStore
	}
	return s.RenamePlaylist(oldName, newName)
}

// SavePlaylist overwrites the tracks of a playlist of the store.
func (p *Provider) SavePlaylist(name string, tracks []playlist.Track) error {
	s := p.playlists()
	if s == nil {
		return errNoStore
	}
	return s.SavePlaylist(name, tracks)
}

// PrependTracksToPlaylist puts tracks at the start of a playlist of the store.
func (p *Provider) PrependTracksToPlaylist(ctx context.Context, playlistID string, tracks []playlist.Track) (int, int, int, error) {
	s := p.playlists()
	if s == nil {
		return 0, 0, 0, errNoStore
	}
	return s.PrependTracksToPlaylist(ctx, playlistID, tracks)
}

// PlaylistDocument returns the TOML document of a playlist of the store.
func (p *Provider) PlaylistDocument(name string) ([]byte, error) {
	s := p.playlists()
	if s == nil {
		return nil, errNoStore
	}
	return s.PlaylistDocument(name)
}

// RestorePlaylistDocument restores the TOML document of a playlist of the
// store.
func (p *Provider) RestorePlaylistDocument(name string, data []byte) error {
	s := p.playlists()
	if s == nil {
		return errNoStore
	}
	return s.RestorePlaylistDocument(name, data)
}
