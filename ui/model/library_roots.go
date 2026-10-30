package model

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/provider"
)

// libraryRootAddedMsg reports the end of adding a music folder to a library.
type libraryRootAddedMsg struct {
	path string // the folder as stored, or as chosen when err is set
	err  error
}

// addLibraryRootCmd adds path to the folders of manager. Checking the folder
// can wait on a slow share, so it runs off the update loop.
func addLibraryRootCmd(manager provider.LibraryManager, path string) tea.Cmd {
	return func() tea.Msg {
		stored, err := manager.AddLibraryRoot(path)
		if err != nil {
			return libraryRootAddedMsg{path: path, err: err}
		}
		return libraryRootAddedMsg{path: stored}
	}
}

// handleLibraryRootAdded tells the user how adding the folder ended and
// refreshes the pane, so its status line shows the scan.
func (m *Model) handleLibraryRootAdded(msg libraryRootAddedMsg) tea.Cmd {
	if msg.err != nil {
		m.status.Warning(msg.err.Error(), statusTTLLong)
		return nil
	}
	m.status.Show(fmt.Sprintf("Added %s. Scanning in the background…", msg.path), statusTTLLong)
	return m.fetchProviderPlaylists()
}
