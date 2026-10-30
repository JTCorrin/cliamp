package library

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

var (
	_ provider.ArtistBrowser       = (*Provider)(nil)
	_ provider.AlbumBrowser        = (*Provider)(nil)
	_ provider.AlbumTrackLoader    = (*Provider)(nil)
	_ provider.TrackArtistResolver = (*Provider)(nil)
	_ provider.BrowseEntryProvider = (*Provider)(nil)
)

const (
	unknownArtist  = "Unknown Artist"
	unknownAlbum   = "Unknown Album"
	variousArtists = "Various Artists"

	// The album sort types. The IDs match the ones that Navidrome uses, so a
	// saved sort of the album browser stays valid.
	sortByName   = "alphabeticalByName"
	sortByArtist = "alphabeticalByArtist"
	sortNewest   = "newest"
	sortByYear   = "byYear"
)

// BrowseEntry IDs of the routes that the provider pane shows.
const (
	entryArtists = "library:artists"
	entryAlbums  = "library:albums"
	entryStatus  = "library:status"
)

type fileRef struct{ root, file int32 }

type artistRec struct {
	id, name, sortKey string
	albums            []int // indexes into browseIndex.albums
	spellings         map[string]int
}

type albumRec struct {
	id, name, sortKey string
	artist, artistID  string
	artistSort        string
	year              int
	genre             string
	added             int64 // the newest modification time of its files
	files             []fileRef
	spellings         map[string]int
}

// browseIndex groups the files of an Index into artists and albums.
type browseIndex struct {
	ix         *Index
	artists    []artistRec
	artistByID map[string]int
	albums     []albumRec
	albumByID  map[string]int
	albumOf    [][]int32 // for each file of each root, the index of its album

	orderMu sync.Mutex
	orders  map[string][]int // album indexes in each sort order, built on first use
}

// groupID returns a stable ID for a folded key.
func groupID(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// sortKey orders names without case, accents and a leading "the ".
func sortKey(name string) string {
	k := fold(name)
	return strings.TrimPrefix(k, "the ")
}

func buildBrowseIndex(ix *Index) *browseIndex {
	// An album whose files name several artists and no album artist is a
	// compilation. Count the artists of each album folder first.
	type folder struct {
		root  int
		dir   string
		album string
	}
	artistsOf := map[folder]map[string]struct{}{}
	for ri, r := range ix.Roots {
		for _, f := range r.Files {
			if f.AlbumArtist != "" || f.Album == "" {
				continue
			}
			k := folder{ri, path.Dir(f.Rel), fold(f.Album)}
			if artistsOf[k] == nil {
				artistsOf[k] = map[string]struct{}{}
			}
			artistsOf[k][fold(f.Artist)] = struct{}{}
		}
	}

	bi := &browseIndex{ix: ix, artistByID: map[string]int{}, albumByID: map[string]int{}, albumOf: make([][]int32, len(ix.Roots))}
	for ri, r := range ix.Roots {
		bi.albumOf[ri] = make([]int32, len(r.Files))
		for fi, f := range r.Files {
			artist := f.AlbumArtist
			if artist == "" {
				artist = f.Artist
				if len(artistsOf[folder{ri, path.Dir(f.Rel), fold(f.Album)}]) > 1 {
					artist = variousArtists
				}
			}
			if strings.TrimSpace(artist) == "" {
				artist = unknownArtist
			}
			album := f.Album
			if strings.TrimSpace(album) == "" {
				album = unknownAlbum
			}
			artistKey, albumKey := fold(artist), fold(album)
			artistID := groupID(artistKey)
			albumID := groupID(artistKey, albumKey)

			ai, ok := bi.artistByID[artistID]
			if !ok {
				ai = len(bi.artists)
				bi.artistByID[artistID] = ai
				bi.artists = append(bi.artists, artistRec{id: artistID, name: artist, sortKey: sortKey(artist), spellings: map[string]int{}})
			}
			bi.artists[ai].spellings[artist]++
			li, ok := bi.albumByID[albumID]
			if !ok {
				li = len(bi.albums)
				bi.albumByID[albumID] = li
				bi.albums = append(bi.albums, albumRec{
					id: albumID, name: album, sortKey: sortKey(album),
					artistID: artistID, artistSort: bi.artists[ai].sortKey, spellings: map[string]int{},
				})
				bi.artists[ai].albums = append(bi.artists[ai].albums, li)
			}
			bi.albumOf[ri][fi] = int32(li)
			al := &bi.albums[li]
			al.spellings[album]++
			al.files = append(al.files, fileRef{int32(ri), int32(fi)})
			if f.Year > 0 && (al.year == 0 || f.Year < al.year) {
				al.year = f.Year
			}
			if al.genre == "" {
				al.genre = f.Genre
			}
			if f.ModUnix > al.added {
				al.added = f.ModUnix
			}
		}
	}
	// Files spell a name in different ways: the most used spelling is shown.
	for i := range bi.artists {
		bi.artists[i].name = commonSpelling(bi.artists[i].spellings)
		bi.artists[i].spellings = nil
	}
	for i := range bi.albums {
		al := &bi.albums[i]
		al.name = commonSpelling(al.spellings)
		al.spellings = nil
		al.artist = bi.artists[bi.artistByID[al.artistID]].name
	}
	sort.Slice(bi.artists, func(i, j int) bool { return bi.artists[i].sortKey < bi.artists[j].sortKey })
	// Sorting moved the artists, so the lookup must follow them.
	for i := range bi.artists {
		bi.artistByID[bi.artists[i].id] = i
	}
	return bi
}

// commonSpelling returns the most used spelling. A tie goes to the spelling
// that sorts first, so the choice does not depend on the order of the files.
func commonSpelling(spellings map[string]int) string {
	best, n := "", 0
	for s, c := range spellings {
		if c > n || (c == n && s < best) {
			best, n = s, c
		}
	}
	return best
}

// browseIndexFor returns the browse form of the current index. It builds the
// form once for each index, outside the lock.
func (p *Provider) browseIndexFor() *browseIndex {
	ix := p.index()
	p.mu.RLock()
	bi := p.browse
	p.mu.RUnlock()
	if bi != nil && bi.ix == ix {
		return bi
	}
	bi = buildBrowseIndex(ix)
	p.mu.Lock()
	if p.idx == ix {
		p.browse = bi
	}
	p.mu.Unlock()
	return bi
}

func (bi *browseIndex) albumInfo(al *albumRec) provider.AlbumInfo {
	return provider.AlbumInfo{
		ID:         al.id,
		Name:       al.name,
		Artist:     al.artist,
		ArtistID:   al.artistID,
		Year:       al.year,
		TrackCount: len(al.files),
		Genre:      al.genre,
	}
}

// Artists returns every artist of the library, sorted by name without a
// leading "The".
func (p *Provider) Artists() ([]provider.ArtistInfo, error) {
	bi := p.browseIndexFor()
	out := make([]provider.ArtistInfo, len(bi.artists))
	for i, a := range bi.artists {
		out[i] = provider.ArtistInfo{ID: a.id, Name: a.name, AlbumCount: len(a.albums)}
	}
	return out, nil
}

// ArtistAlbums returns the albums of an artist, oldest first.
func (p *Provider) ArtistAlbums(artistID string) ([]provider.AlbumInfo, error) {
	bi := p.browseIndexFor()
	ai, ok := bi.artistByID[artistID]
	if !ok {
		return nil, fmt.Errorf("library: unknown artist %q", artistID)
	}
	idx := slices.Clone(bi.artists[ai].albums)
	slices.SortFunc(idx, func(a, b int) int {
		x, y := &bi.albums[a], &bi.albums[b]
		// An album without a year goes last.
		if (x.year == 0) != (y.year == 0) {
			if x.year == 0 {
				return 1
			}
			return -1
		}
		if x.year != y.year {
			return x.year - y.year
		}
		return strings.Compare(x.sortKey, y.sortKey)
	})
	out := make([]provider.AlbumInfo, len(idx))
	for i, li := range idx {
		out[i] = bi.albumInfo(&bi.albums[li])
	}
	return out, nil
}

// AlbumSortTypes returns the sort orders of the album list.
func (p *Provider) AlbumSortTypes() []provider.SortType {
	return []provider.SortType{
		{ID: sortByName, Label: "By Name"},
		{ID: sortByArtist, Label: "By Artist"},
		{ID: sortNewest, Label: "Recently Added"},
		{ID: sortByYear, Label: "By Year"},
	}
}

// DefaultAlbumSort returns the sort order that the album list opens with.
func (p *Provider) DefaultAlbumSort() string { return sortByName }

// albumOrder returns the indexes of the albums in the sort order. An unknown
// sort order sorts by name. The result is built once for each order and must
// not be changed.
func (bi *browseIndex) albumOrder(sortType string) []int {
	switch sortType {
	case sortByArtist, sortNewest, sortByYear:
	default:
		sortType = sortByName
	}
	bi.orderMu.Lock()
	defer bi.orderMu.Unlock()
	if idx, ok := bi.orders[sortType]; ok {
		return idx
	}
	idx := make([]int, len(bi.albums))
	for i := range idx {
		idx[i] = i
	}
	cmpName := func(x, y *albumRec) int {
		if c := strings.Compare(x.sortKey, y.sortKey); c != 0 {
			return c
		}
		return strings.Compare(x.artistSort, y.artistSort)
	}
	slices.SortFunc(idx, func(a, b int) int {
		x, y := &bi.albums[a], &bi.albums[b]
		switch sortType {
		case sortByArtist:
			if c := strings.Compare(x.artistSort, y.artistSort); c != 0 {
				return c
			}
			if x.year != y.year {
				return x.year - y.year
			}
		case sortNewest:
			if x.added != y.added {
				if x.added > y.added {
					return -1
				}
				return 1
			}
		case sortByYear:
			if x.year != y.year {
				return x.year - y.year
			}
		}
		return cmpName(x, y)
	})
	if bi.orders == nil {
		bi.orders = map[string][]int{}
	}
	bi.orders[sortType] = idx
	return idx
}

// AlbumList returns one page of the albums in the sort order. An unknown
// sort order sorts by name.
func (p *Provider) AlbumList(sortType string, offset, size int) ([]provider.AlbumInfo, error) {
	bi := p.browseIndexFor()
	idx := bi.albumOrder(sortType)
	if offset < 0 {
		offset = 0
	}
	if offset >= len(idx) {
		return nil, nil
	}
	idx = idx[offset:]
	if size > 0 && len(idx) > size {
		idx = idx[:size]
	}
	out := make([]provider.AlbumInfo, len(idx))
	for i, li := range idx {
		out[i] = bi.albumInfo(&bi.albums[li])
	}
	return out, nil
}

// AlbumTracks returns the tracks of an album in disc and track order.
func (p *Provider) AlbumTracks(albumID string) ([]playlist.Track, error) {
	bi := p.browseIndexFor()
	li, ok := bi.albumByID[albumID]
	if !ok {
		return nil, fmt.Errorf("library: unknown album %q", albumID)
	}
	refs := slices.Clone(bi.albums[li].files)
	file := func(r fileRef) *File { return &bi.ix.Roots[r.root].Files[r.file] }
	slices.SortStableFunc(refs, func(a, b fileRef) int {
		x, y := file(a), file(b)
		if x.Disc != y.Disc {
			return x.Disc - y.Disc
		}
		if x.TrackNumber != y.TrackNumber {
			return x.TrackNumber - y.TrackNumber
		}
		return strings.Compare(x.Rel, y.Rel)
	})
	out := make([]playlist.Track, len(refs))
	for i, r := range refs {
		out[i] = file(r).Track(bi.ix.Roots[r.root].Path)
	}
	return out, nil
}

// ArtistForTrack returns the artist that holds a track of the library.
func (p *Provider) ArtistForTrack(track playlist.Track) (provider.ArtistInfo, bool) {
	bi := p.browseIndexFor()
	ri, fi, ok := bi.ix.find(track.Path)
	if !ok {
		return provider.ArtistInfo{}, false
	}
	ai := bi.artistByID[bi.albums[bi.albumOf[ri][fi]].artistID]
	a := &bi.artists[ai]
	return provider.ArtistInfo{ID: a.id, Name: a.name, AlbumCount: len(a.albums)}, true
}

// BrowseEntries returns the routes that the provider pane lists next to the
// playlists, and a last line that says what the index holds or what the scan
// is doing: a first scan of a large share takes minutes, and an unmounted
// share otherwise looks like a library that is merely out of date.
//
// The set of IDs never changes. The pane resolves a selected line by asking
// for the entries again, so a line that came and went with the scan would be
// opened as a playlist when the scan ended before the keypress.
func (p *Provider) BrowseEntries() []provider.BrowseEntry {
	return []provider.BrowseEntry{
		{ID: entryArtists, Name: "Artists", Mode: provider.BrowseArtistAlbums},
		{ID: entryAlbums, Name: "Albums", Mode: provider.BrowseAlbums},
		{ID: entryStatus, Name: p.statusLine(), Mode: provider.BrowseAlbums},
	}
}

// statusLine describes the state of the library in one line.
func (p *Provider) statusLine() string {
	st := p.Status()
	switch {
	case st.Scanning && st.Progress.Total > 0:
		return fmt.Sprintf("Scanning: %d of %d new files", st.Progress.Done, st.Progress.Total)
	case st.Scanning:
		return "Scanning for new files"
	case st.Err != "":
		return "Scan failed: " + st.Err
	case len(st.Last.Unavailable) > 0:
		return "Not readable, showing the saved index: " + strings.Join(st.Last.Unavailable, ", ")
	}
	switch n := p.index().Len(); n {
	case 0:
		return "No tracks indexed. Press Ctrl+R to scan"
	case 1:
		return "1 track indexed"
	default:
		return fmt.Sprintf("%d tracks indexed", n)
	}
}

// find returns the position of the file at an absolute path.
func (ix *Index) find(p string) (root, file int, ok bool) {
	for ri := range ix.Roots {
		r := &ix.Roots[ri]
		rel, err := filepath.Rel(r.Path, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		rel = filepath.ToSlash(rel)
		i, found := slices.BinarySearchFunc(r.Files, rel, func(f File, rel string) int { return strings.Compare(f.Rel, rel) })
		if found {
			return ri, i, true
		}
	}
	return 0, 0, false
}
