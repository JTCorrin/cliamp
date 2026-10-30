# Library

The library provider indexes folders of music files, such as a share of a NAS
that is mounted on your computer, so that cliamp can search them, browse them
by artist and album, and play them without a media server.

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
`cliamp --provider library`. While cliamp runs, press `Tab` to focus **Source**,
choose **Library** with `Left` and `Right`, and press `Enter`.

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

## Playlists

The playlists of the Library are your local playlists, the TOML files in
`~/.config/cliamp/playlists/`. See [playlists.md](playlists.md). Creating,
renaming, deleting and reordering a playlist work as they do for the Local
provider.

To add a track from a search, press `Ctrl+F`, pick a result, press `p`, and
choose a playlist or **New Playlist**.

To save an album, open it in **Albums** or **Artists**, press `a` to append it to
the queue, then press `p` for the playlist screen and `w` to save the queue with
the playlist picker. The same works for any selection of tracks.

A playlist file holds the full path of each track and its tags, so it plays
without the Library and without a scan. It stops playing when the share is not
mounted at the same path. Keep the mount point stable, such as `/mnt/nas/music`.

## Scanning

cliamp scans the folders at start and saves what it reads in
`~/.local/share/cliamp/library/index.gob`. Every later start loads that index at
once, so search and browse work before the scan ends, and a scan reads only new
and changed files. A file counts as changed when its size or modification time
differs.

While a scan runs, the provider pane shows a **Scanning** line. Press `Ctrl+R`
in the provider pane to scan again, for example after you add albums. The
first scan of a large share can take minutes, because cliamp reads the tags of
every file. Raise `workers` for a share with high latency.

The scan skips dot files, such as the `._` files that macOS leaves on a share,
and the `@eaDir` and `#recycle` folders of Synology. A path in `paths` may
be a symlink, such as `~/Music` that points at the share, but the scan does not
follow symlinked folders inside it. A relative path is taken from the folder
where you start cliamp, so use absolute paths. Do not list a folder and one of
its subfolders: each file would appear twice.

If the share is not mounted, or a folder holds no audio file while the index
holds some, cliamp keeps the saved index for that folder and the provider pane
shows **Not readable, showing the saved index**. A folder that is empty is never
taken as proof that you deleted the music. In the same way, a subfolder that
cannot be read during a scan, for example after a network error, keeps its
saved files. Remove the folder from `paths` to
drop its files from the index.

Tags come from the file: ID3, MP4, Vorbis and FLAC tags, the WAV INFO list, and
then the file name. The duration of a track shows once it plays. Playing an
`.m4a`, `.opus`, `.aac` or `.wma` file needs `ffmpeg`.
