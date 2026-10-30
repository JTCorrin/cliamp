package model

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

// fakeLibrary is a provider that manages music folders in memory.
type fakeLibrary struct {
	roots    []provider.LibraryRoot
	rescans  int
	addErr   error
	scanning bool
}

func (f *fakeLibrary) Name() string                                { return "Library" }
func (f *fakeLibrary) Playlists() ([]playlist.PlaylistInfo, error) { return nil, nil }
func (f *fakeLibrary) Tracks(string) ([]playlist.Track, error)     { return nil, nil }
func (f *fakeLibrary) LibraryRoots() []provider.LibraryRoot        { return f.roots }
func (f *fakeLibrary) RescanLibrary()                              { f.rescans++ }
func (f *fakeLibrary) LibraryScanning() bool                       { return f.scanning }

func (f *fakeLibrary) AddLibraryRoot(path string) (string, error) {
	if f.addErr != nil {
		return "", f.addErr
	}
	stored := "/abs" + path
	f.roots = append(f.roots, provider.LibraryRoot{Path: stored, Available: true})
	return stored, nil
}

func (f *fakeLibrary) RemoveLibraryRoot(path string) error {
	for i, r := range f.roots {
		if r.Path == path {
			f.roots = append(f.roots[:i], f.roots[i+1:]...)
			return nil
		}
	}
	return errors.New("not in the library")
}

func libraryModel(t *testing.T, lib playlist.Provider) Model {
	t.Helper()
	m := newHeadlessModel(t, &headlessEngine{}, nil)
	m.providers = []provider.Entry{{Key: "library", Name: "Library", Provider: lib}}
	return m
}

func TestIPCLibraryRootsOperations(t *testing.T) {
	lib := &fakeLibrary{roots: []provider.LibraryRoot{{Path: "/music", Tracks: 7, Available: true}}}
	m := libraryModel(t, lib)

	response := runV2(t, &m, "library.roots", ipc.Request{})
	if !response.OK || !reflect.DeepEqual(response.Roots, []ipc.LibraryRootInfo{{Path: "/music", Tracks: 7, Available: true}}) {
		t.Fatalf("library.roots = %+v", response)
	}

	response = runV2(t, &m, "library.add_root", ipc.Request{Path: "/nas"})
	if !response.OK || response.Output != "/abs/nas" || len(response.Roots) != 2 {
		t.Fatalf("library.add_root = %+v, want the stored path and both folders", response)
	}

	response = runV2(t, &m, "library.remove_root", ipc.Request{Path: "/music"})
	if !response.OK || len(response.Roots) != 1 || response.Roots[0].Path != "/abs/nas" {
		t.Fatalf("library.remove_root = %+v, want the remaining folder", response)
	}

	lib.scanning = true
	response = runV2(t, &m, "library.scan", ipc.Request{})
	if !response.OK || lib.rescans != 1 || !response.Scanning || len(response.Roots) != 1 {
		t.Fatalf("library.scan = %+v with %d rescans, want 1 rescan, scanning and the folders", response, lib.rescans)
	}
}

func TestIPCLibraryRootsReportErrors(t *testing.T) {
	lib := &fakeLibrary{addErr: errors.New("library: /x is not a folder")}
	m := libraryModel(t, lib)

	response := runV2(t, &m, "library.add_root", ipc.Request{Path: "/x"})
	if response.OK || !strings.Contains(response.Error, "is not a folder") {
		t.Errorf("add_root of a bad folder = %+v, want the provider's error", response)
	}
	response = runV2(t, &m, "library.remove_root", ipc.Request{Path: "/missing"})
	if response.OK || !strings.Contains(response.Error, "not in the library") {
		t.Errorf("remove_root of an unknown folder = %+v", response)
	}
}

func TestIPCLibraryRootsNeedAManagingProvider(t *testing.T) {
	// A provider that does not manage folders, and a key that is not there.
	m := libraryModel(t, plainProvider{})
	response := runV2(t, &m, "library.roots", ipc.Request{})
	if response.OK || !strings.Contains(response.Error, "does not manage music folders") {
		t.Errorf("library.roots on a plain provider = %+v", response)
	}
	response = runV2(t, &m, "library.roots", ipc.Request{Provider: "nope"})
	if response.OK || !strings.Contains(response.Error, "unknown provider") {
		t.Errorf("library.roots with an unknown key = %+v", response)
	}
}

type plainProvider struct{}

func (plainProvider) Name() string                                { return "Plain" }
func (plainProvider) Playlists() ([]playlist.PlaylistInfo, error) { return nil, nil }
func (plainProvider) Tracks(string) ([]playlist.Track, error)     { return nil, nil }
