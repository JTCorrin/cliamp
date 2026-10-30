# Library

The library provider indexes folders of music files, such as a share of a NAS
that is mounted on your computer, so that cliamp can search them, browse them
by artist and album, and play them without a media server.

This provider is under development. Setup and features are listed here as they
land.

## Setup

Mount the share, then add the folders to `config.toml`:

```toml
[library]
paths = ["/mnt/nas/music"]
# workers = 4
# scan_on_start = true
```

| Key | Default | Meaning |
| --- | --- | --- |
| `paths` | none | The music folders to index. The provider turns on when this is set. |
| `workers` | `4` | The number of files whose tags are read at the same time during a scan. Raise it for a share with high latency. |
| `scan_on_start` | `true` | Rescan for new and changed files in the background at start. |

Set `provider = "library"` to open cliamp on the library, or start it with
`cliamp --provider library`.

## Search

Press `Ctrl+F` while the Library is the active provider. The query is split
into words. A track matches when every word appears in its title, artist or
album, in any order and in any case, and accents do not count: `beyonce` finds
`Beyoncé`. A match at the start of a field or of a word ranks higher, and a
title match ranks higher than an artist match, which ranks higher than an album
match. When no track holds every word, the search falls back to the letters of
the query in order, so `mny` finds `Money`.

Search reads the index in memory, so it does not touch the share.

## Browse

The provider pane lists **Artists** and **Albums** next to your playlists.

- **Artists** opens artist, then album, then tracks. Albums are oldest first.
- **Albums** lists every album. Press `s` to sort it by name, artist, recently
  added or year.

Tracks group by the album artist tag, and by the artist tag when a file has no
album artist. A folder whose files share an album name but name several artists,
with no album artist tag, shows as one album under **Various Artists**. Names
that differ only in case, such as `Pink Floyd` and `pink floyd`, are one
artist, and the spelling that most files use is shown. Sorting ignores a
leading `The`. Files with no artist or album tag appear under **Unknown Artist**
and **Unknown Album**.
