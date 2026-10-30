package library

import (
	"context"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bjarneo/cliamp/internal/fuzzy"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/provider"
)

var _ provider.Searcher = (*Provider)(nil)

// Scores of one query word against one field, best first.
const (
	scoreExact      = 100
	scorePrefix     = 60
	scoreWordPrefix = 40
	scoreSubstring  = 20

	// Added to the score of a word by the field that it matched.
	bonusTitle  = 10
	bonusArtist = 6
	bonusAlbum  = 0

	// fuzzyDivisor scales a subsequence score below every word score.
	fuzzyDivisor = 4

	// cancelCheckEvery is how many records a search handles between looks at
	// the context.
	cancelCheckEvery = 4096
)

// searchRec holds the folded text of one file, so a search folds only the
// query.
type searchRec struct {
	title, artist, album string
	root, file           int32
}

// searchIndex is the searchable form of an Index.
type searchIndex struct {
	ix   *Index
	recs []searchRec
}

func buildSearchIndex(ix *Index) *searchIndex {
	si := &searchIndex{ix: ix, recs: make([]searchRec, 0, ix.Len())}
	for ri, r := range ix.Roots {
		for fi, f := range r.Files {
			si.recs = append(si.recs, searchRec{
				title:  fold(f.Title),
				artist: fold(f.Artist),
				album:  fold(f.Album),
				root:   int32(ri),
				file:   int32(fi),
			})
		}
	}
	return si
}

// searchIndexFor returns the searchable form of the current index. It builds
// the form once for each index, outside the lock, so a build does not stall
// the calls that only read the status.
func (p *Provider) searchIndexFor() *searchIndex {
	ix := p.index()
	p.mu.RLock()
	si := p.search
	p.mu.RUnlock()
	if si != nil && si.ix == ix {
		return si
	}
	si = buildSearchIndex(ix)
	p.mu.Lock()
	if p.idx == ix {
		p.search = si
	}
	p.mu.Unlock()
	return si
}

// fold lowercases s and replaces the common accented Latin letters with their
// base letter, so "Beyoncé" matches "beyonce".
func fold(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf || ('A' <= s[i] && s[i] <= 'Z') {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		if r < 0xC0 {
			return r
		}
		if b, ok := baseLetter[r]; ok {
			return b
		}
		return r
	}, s)
}

// baseLetter maps accented Latin letters to their base letter. Letters that
// have no one-letter base, such as ß and æ, stay as they are.
var baseLetter = func() map[rune]rune {
	m := map[rune]rune{}
	for base, accented := range map[rune]string{
		'a': "àáâãäåāăą",
		'c': "çćĉċč",
		'd': "ďđ",
		'e': "èéêëēĕėęě",
		'g': "ĝğġģ",
		'h': "ĥħ",
		'i': "ìíîïĩīĭįı",
		'j': "ĵ",
		'k': "ķ",
		'l': "ĺļľŀł",
		'n': "ñńņňŉ",
		'o': "òóôõöøōŏő",
		'r': "ŕŗř",
		's': "śŝşš",
		't': "ţťŧ",
		'u': "ùúûüũūŭůűų",
		'w': "ŵ",
		'y': "ýÿŷ",
		'z': "źżž",
	} {
		for _, r := range accented {
			m[r] = base
		}
	}
	return m
}()

// wordScore returns how well one query word matches one folded field, or 0.
func wordScore(word, field string) int {
	if field == "" {
		return 0
	}
	if field == word {
		return scoreExact
	}
	i := strings.Index(field, word)
	if i < 0 {
		return 0
	}
	if i == 0 {
		return scorePrefix
	}
	// The first hit may sit inside a word while a later one starts a word.
	for ; i >= 0; i = nextIndex(field, word, i+1) {
		prev, _ := utf8.DecodeLastRuneInString(field[:i])
		if !unicode.IsLetter(prev) && !unicode.IsDigit(prev) {
			return scoreWordPrefix
		}
	}
	return scoreSubstring
}

// nextIndex returns the index of word in s at or after from, or -1.
func nextIndex(s, word string, from int) int {
	if from >= len(s) {
		return -1
	}
	if i := strings.Index(s[from:], word); i >= 0 {
		return from + i
	}
	return -1
}

// recScore returns the score of a record for the query words and whether
// every word matched in the title, the artist or the album.
func recScore(words []string, r *searchRec) (int, bool) {
	total := 0
	for _, w := range words {
		best := 0
		if s := wordScore(w, r.title); s > 0 {
			best = s + bonusTitle
		}
		if s := wordScore(w, r.artist); s > 0 && s+bonusArtist > best {
			best = s + bonusArtist
		}
		if s := wordScore(w, r.album); s > 0 && s+bonusAlbum > best {
			best = s + bonusAlbum
		}
		if best == 0 {
			return 0, false
		}
		total += best
	}
	return total, true
}

// fuzzyScore is the fallback when no record holds every word: the best
// subsequence match of the whole query in one field.
func fuzzyScore(query string, r *searchRec) (int, bool) {
	best, ok := 0, false
	for _, field := range [...]string{r.title, r.artist, r.album} {
		if s, m := fuzzy.Match(query, field); m && (!ok || s > best) {
			best, ok = s, true
		}
	}
	return best / fuzzyDivisor, ok
}

type hit struct {
	score int
	rec   *searchRec
}

// SearchTracks returns up to limit tracks whose title, artist or album hold
// every word of query, best match first. When no track holds every word, it
// returns the tracks that match the query as a subsequence. limit <= 0 means
// no cap. Implements provider.Searcher.
func (p *Provider) SearchTracks(ctx context.Context, query string, limit int) ([]playlist.Track, error) {
	words := strings.Fields(fold(query))
	if len(words) == 0 {
		return nil, nil
	}
	si := p.searchIndexFor()
	hits, err := collectHits(ctx, si, func(r *searchRec) (int, bool) { return recScore(words, r) })
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		joined := strings.Join(words, " ")
		hits, err = collectHits(ctx, si, func(r *searchRec) (int, bool) { return fuzzyScore(joined, r) })
		if err != nil {
			return nil, err
		}
	}
	slices.SortFunc(hits, func(a, b hit) int { return compareHits(si.ix, a, b) })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]playlist.Track, len(hits))
	for i, h := range hits {
		root := &si.ix.Roots[h.rec.root]
		out[i] = root.Files[h.rec.file].Track(root.Path)
	}
	return out, nil
}

func collectHits(ctx context.Context, si *searchIndex, score func(*searchRec) (int, bool)) ([]hit, error) {
	var hits []hit
	for i := range si.recs {
		if i%cancelCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		r := &si.recs[i]
		if s, ok := score(r); ok {
			hits = append(hits, hit{score: s, rec: r})
		}
	}
	return hits, nil
}

// compareHits orders by score, best first, and then by artist, album, disc,
// track number and title, so the order does not change between searches.
func compareHits(ix *Index, a, b hit) int {
	if a.score != b.score {
		return b.score - a.score
	}
	if c := strings.Compare(a.rec.artist, b.rec.artist); c != 0 {
		return c
	}
	if c := strings.Compare(a.rec.album, b.rec.album); c != 0 {
		return c
	}
	fa := &ix.Roots[a.rec.root].Files[a.rec.file]
	fb := &ix.Roots[b.rec.root].Files[b.rec.file]
	if fa.Disc != fb.Disc {
		return fa.Disc - fb.Disc
	}
	if fa.TrackNumber != fb.TrackNumber {
		return fa.TrackNumber - fb.TrackNumber
	}
	return strings.Compare(a.rec.title, b.rec.title)
}
