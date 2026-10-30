package library

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/bjarneo/cliamp/config"
)

// testProvider returns a provider that serves the given files of one root.
func testProvider(t testing.TB, files []File) *Provider {
	t.Helper()
	p := newProvider(config.LibraryConfig{Paths: []string{"/music"}, ScanDisabled: true}, "")
	t.Cleanup(p.Close)
	p.idx = &Index{Version: indexVersion, Roots: []Root{{Path: "/music", Files: files}}}
	return p
}

func titles(t testing.TB, p *Provider, query string, limit int) []string {
	t.Helper()
	tracks, err := p.SearchTracks(context.Background(), query, limit)
	if err != nil {
		t.Fatalf("SearchTracks(%q): %v", query, err)
	}
	var out []string
	for _, tr := range tracks {
		out = append(out, tr.Title)
	}
	return out
}

func TestSearchTracks(t *testing.T) {
	files := []File{
		{Rel: "a.mp3", Title: "Money", Artist: "Pink Floyd", Album: "The Dark Side of the Moon", TrackNumber: 6},
		{Rel: "b.mp3", Title: "Time", Artist: "Pink Floyd", Album: "The Dark Side of the Moon", TrackNumber: 4},
		{Rel: "c.mp3", Title: "Money for Nothing", Artist: "Dire Straits", Album: "Brothers in Arms"},
		{Rel: "d.mp3", Title: "Halo", Artist: "Beyoncé", Album: "I Am... Sasha Fierce"},
		{Rel: "e.mp3", Title: "Moneyball", Artist: "Someone", Album: "Other"},
		{Rel: "f.mp3", Title: "Easy Money", Artist: "Someone", Album: "Other"},
	}
	p := testProvider(t, files)
	tests := []struct {
		name  string
		query string
		limit int
		want  []string
	}{
		{"empty", "", 20, nil},
		{"blank", "   ", 20, nil},
		{"exact title first", "money", 20, []string{"Money", "Money for Nothing", "Moneyball", "Easy Money"}},
		{"words across fields", "floyd money", 20, []string{"Money"}},
		{"word order is free", "money floyd", 20, []string{"Money"}},
		{"album match", "dark side", 20, []string{"Time", "Money"}},
		{"accents fold both ways", "beyonce", 20, []string{"Halo"}},
		{"accented query", "beyoncé halo", 20, []string{"Halo"}},
		{"case", "PINK FLOYD", 20, []string{"Time", "Money"}},
		{"limit", "money", 2, []string{"Money", "Money for Nothing"}},
		{"no match", "zzzz", 20, nil},
		// Money and Money for Nothing tie, so the artist orders them.
		{"subsequence fallback", "mny", 20, []string{"Money for Nothing", "Money", "Moneyball", "Easy Money"}},
		{"every word must match", "money zzzz", 20, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := titles(t, p, tt.query, tt.limit)
			if !slices.Equal(got, tt.want) {
				t.Errorf("SearchTracks(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestSearchTracksTrack(t *testing.T) {
	p := testProvider(t, []File{{Rel: "x/y.mp3", Title: "Song", Artist: "A", Album: "B", Year: 2001, TrackNumber: 3}})
	tracks, err := p.SearchTracks(context.Background(), "song", 5)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks = %v, err = %v", tracks, err)
	}
	tr := tracks[0]
	if tr.Path != "/music/x/y.mp3" || tr.Year != 2001 || tr.TrackNumber != 3 || tr.ProviderMeta["library.id"] != TrackID("/music", "x/y.mp3") {
		t.Errorf("track = %+v", tr)
	}
}

func TestSearchTracksSeesNewIndex(t *testing.T) {
	p := testProvider(t, []File{{Rel: "a.mp3", Title: "Old Song"}})
	if got := titles(t, p, "song", 5); !slices.Equal(got, []string{"Old Song"}) {
		t.Fatalf("before = %v", got)
	}
	p.mu.Lock()
	p.idx = &Index{Version: indexVersion, Roots: []Root{{Path: "/music", Files: []File{{Rel: "b.mp3", Title: "New Song"}}}}}
	p.mu.Unlock()
	if got := titles(t, p, "song", 5); !slices.Equal(got, []string{"New Song"}) {
		t.Errorf("after = %v", got)
	}
}

func TestSearchTracksCancel(t *testing.T) {
	p := testProvider(t, []File{{Rel: "a.mp3", Title: "Song"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.SearchTracks(ctx, "song", 5); err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestFold(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"abc", "abc"},
		{"ABC", "abc"},
		{"Beyoncé", "beyonce"},
		{"Sigur Rós", "sigur ros"},
		{"Mötley Crüe", "motley crue"},
		{"Łódź", "lodz"},
		{"Ünal", "unal"},
		{"日本語", "日本語"},
	}
	for _, tt := range tests {
		if got := fold(tt.in); got != tt.want {
			t.Errorf("fold(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWordScore(t *testing.T) {
	tests := []struct {
		word, field string
		want        int
	}{
		{"money", "money", scoreExact},
		{"mon", "money", scorePrefix},
		{"floyd", "pink floyd", scoreWordPrefix},
		{"loyd", "pink floyd", scoreSubstring},
		{"floyd", "pink-floyd", scoreWordPrefix},
		{"ink", "pink floyd ink", scoreWordPrefix}, // the later hit starts a word
		{"zzz", "pink floyd", 0},
		{"a", "", 0},
	}
	for _, tt := range tests {
		if got := wordScore(tt.word, tt.field); got != tt.want {
			t.Errorf("wordScore(%q, %q) = %d, want %d", tt.word, tt.field, got, tt.want)
		}
	}
}

// syntheticFiles returns n files with varied artists, albums and titles.
func syntheticFiles(n int) []File {
	words := []string{"love", "night", "river", "fire", "dream", "stone", "light", "road", "heart", "wind", "gold", "blue", "ghost", "city", "rain"}
	files := make([]File, n)
	for i := range files {
		a, b, c := words[i%len(words)], words[(i/len(words))%len(words)], words[(i/(len(words)*len(words)))%len(words)]
		files[i] = File{
			Rel:         fmt.Sprintf("artist %d/album %d/%02d.mp3", i/100, i/10, i%10),
			Title:       fmt.Sprintf("%s of the %s %d", a, b, i),
			Artist:      fmt.Sprintf("The %s %s %d", c, a, i/100),
			Album:       fmt.Sprintf("%s %s %d", b, c, i/10),
			TrackNumber: i % 10,
		}
	}
	return files
}

// TestSearchTracksSpeed holds the search of a large library to the budget of
// the plan: under 50 ms for 100k tracks.
func TestSearchTracksSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("speed check")
	}
	p := testProvider(t, syntheticFiles(100_000))
	p.searchIndexFor() // the first search builds the folded index
	for _, q := range []string{"l", "love", "love night", "river of the", "zzzzz", "gold ghost 12"} {
		start := time.Now()
		if _, err := p.SearchTracks(context.Background(), q, 20); err != nil {
			t.Fatal(err)
		}
		// The budget is 50 ms. The race detector slows the loop, so this
		// guard is loose and the benchmark gives the real number.
		if d := time.Since(start); d > 500*time.Millisecond {
			t.Errorf("search %q took %v", q, d)
		}
	}
}

func BenchmarkSearchTracks100k(b *testing.B) {
	p := testProvider(b, syntheticFiles(100_000))
	p.searchIndexFor()
	for _, q := range []string{"l", "love night", "river of the", "zzzzz"} {
		b.Run(q, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := p.SearchTracks(context.Background(), q, 20); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
