package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/provider"
)

// pickModel returns a model whose file browser is open in a temporary folder
// with the folders "Albums" and "Rock" and one audio file, in pick mode. It
// records the folders that were chosen.
func pickModel(t *testing.T, chosen *[]string) (Model, string) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"Albums", "Rock"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := keybindingTestModel()
	m.fileBrowser.dir = root
	m.openFileBrowserPickDir(func(path string) tea.Cmd {
		*chosen = append(*chosen, path)
		return nil
	})
	return m, root
}

// cursorOn moves the file browser cursor to the entry called name.
func cursorOn(t *testing.T, m *Model, name string) {
	t.Helper()
	for i := 0; i < m.fbCount(); i++ {
		// The browser shows a folder as "name/".
		if n := m.fbEntry(i).name; n == name || n == name+"/" {
			m.fileBrowser.cursor = i
			return
		}
	}
	t.Fatalf("no entry %q in the file browser", name)
}

func pressFB(t *testing.T, m *Model, key string) {
	t.Helper()
	msg, ok := keyPressFor(key)
	if !ok {
		t.Fatalf("no key press for %q", key)
	}
	m.handleFileBrowserKey(msg)
}

func TestFileBrowserPicksTheFolderUnderTheCursor(t *testing.T) {
	var chosen []string
	m, root := pickModel(t, &chosen)
	cursorOn(t, &m, "Rock")
	pressFB(t, &m, "space")
	if len(chosen) != 1 || chosen[0] != filepath.Join(root, "Rock") {
		t.Fatalf("chosen = %v, want the Rock folder", chosen)
	}
	if m.fileBrowser.visible || m.fileBrowser.onPick != nil {
		t.Error("the browser stays open after a choice")
	}
}

func TestFileBrowserPicksTheOpenFolderFromTheParentRowOrAFile(t *testing.T) {
	for _, name := range []string{"..", "song.mp3"} {
		t.Run(name, func(t *testing.T) {
			var chosen []string
			m, root := pickModel(t, &chosen)
			cursorOn(t, &m, name)
			pressFB(t, &m, "space")
			if len(chosen) != 1 || chosen[0] != root {
				t.Errorf("chosen = %v, want the open folder %s", chosen, root)
			}
		})
	}
}

func TestFileBrowserPickModeOpensFoldersWithEnter(t *testing.T) {
	var chosen []string
	m, root := pickModel(t, &chosen)
	cursorOn(t, &m, "Albums")
	pressFB(t, &m, "enter")
	if m.fileBrowser.dir != filepath.Join(root, "Albums") {
		t.Fatalf("dir = %q, want it to open Albums", m.fileBrowser.dir)
	}
	if len(chosen) != 0 || !m.fileBrowser.visible {
		t.Error("Enter on a folder must open it, not choose it")
	}
	// Enter on an audio file does nothing: it must not queue or play it.
	m.fileBrowser.dir = root
	m.loadFBDir()
	cursorOn(t, &m, "song.mp3")
	pressFB(t, &m, "enter")
	if len(m.fileBrowser.selected) != 0 || !m.fileBrowser.visible || len(chosen) != 0 {
		t.Errorf("Enter on a file changed state: selected %v, visible %v", m.fileBrowser.selected, m.fileBrowser.visible)
	}
}

func TestFileBrowserPickModeBlocksAudioActions(t *testing.T) {
	var chosen []string
	m, _ := pickModel(t, &chosen)
	cursorOn(t, &m, "song.mp3")
	for _, key := range []string{"a", "R", "w", "D"} {
		pressFB(t, &m, key)
		if len(m.fileBrowser.selected) != 0 || m.fileBrowser.confirmReplace || !m.fileBrowser.visible || len(chosen) != 0 {
			t.Fatalf("%q acted in pick mode: selected %v, confirm %v, visible %v", key, m.fileBrowser.selected, m.fileBrowser.confirmReplace, m.fileBrowser.visible)
		}
	}
}

func TestFileBrowserPickModeCancels(t *testing.T) {
	for _, key := range []string{"esc", "q", "o"} {
		t.Run(key, func(t *testing.T) {
			var chosen []string
			m, _ := pickModel(t, &chosen)
			pressFB(t, &m, key)
			if m.fileBrowser.visible || m.fileBrowser.onPick != nil || len(chosen) != 0 {
				t.Errorf("visible %v, onPick set %v, chosen %v; want closed and nothing chosen", m.fileBrowser.visible, m.fileBrowser.onPick != nil, chosen)
			}
		})
	}
}

// Opening the browser the usual way must clear a pick left over from before.
func TestOpeningTheFileBrowserForAudioClearsPickMode(t *testing.T) {
	var chosen []string
	m, _ := pickModel(t, &chosen)
	m.fileBrowser.visible = false
	m.openFileBrowser()
	if m.fileBrowser.onPick != nil {
		t.Error("a stale pick would turn the audio browser into a folder picker")
	}
}

func TestFileBrowserHeaderNamesTheMode(t *testing.T) {
	var chosen []string
	m, _ := pickModel(t, &chosen)
	m.layout.panelWidth = 200
	if got := m.fbHeaderLine(); !strings.Contains(got, "Choose a music folder") {
		t.Errorf("header = %q, want it to say that a folder is chosen", got)
	}
	m.fileBrowser.onPick = nil
	if got := m.fbHeaderLine(); strings.Contains(got, "Choose a music folder") {
		t.Errorf("header = %q must not say that in the normal mode", got)
	}
}

func TestProviderPaneAddKeyOpensThePickerOnlyForALibrary(t *testing.T) {
	lib := &fakeLibrary{}
	m := keybindingTestModel()
	m.provider = lib
	m.focus = focusProvider
	msg, _ := keyPressFor("A")
	m.handleProviderPaneKey(msg)
	if !m.fileBrowser.visible || m.fileBrowser.onPick == nil {
		t.Fatal("A did not open the folder picker for a library provider")
	}

	// Choosing a folder adds it to the library.
	m.fileBrowser.dir = t.TempDir()
	m.loadFBDir()
	m.fileBrowser.cursor = 0
	space, _ := keyPressFor("space")
	cmd := m.handleFileBrowserKey(space)
	if cmd == nil {
		t.Fatal("choosing a folder returned no command")
	}
	added, ok := cmd().(libraryRootAddedMsg)
	if !ok || added.err != nil || !strings.HasPrefix(added.path, "/abs") {
		t.Fatalf("add result = %+v, ok %v", added, ok)
	}

	// A provider that does not manage folders ignores the key.
	other := keybindingTestModel()
	other.provider = plainProvider{}
	other.focus = focusProvider
	other.handleProviderPaneKey(msg)
	if other.fileBrowser.visible {
		t.Error("A opened a folder picker for a provider without library folders")
	}
}

func TestAddLibraryRootCmdReportsFailure(t *testing.T) {
	lib := &fakeLibrary{addErr: errors.New("library: not a folder")}
	msg, ok := addLibraryRootCmd(lib, "/x")().(libraryRootAddedMsg)
	if !ok || msg.err == nil {
		t.Fatalf("msg = %+v, want an error", msg)
	}
	m := keybindingTestModel()
	if cmd := m.handleLibraryRootAdded(msg); cmd != nil {
		t.Error("a failed add must not refresh the pane")
	}
}

var _ provider.LibraryManager = (*fakeLibrary)(nil)
