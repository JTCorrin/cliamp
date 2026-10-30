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
