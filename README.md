# nd

Terminal-based music player for [Navidrome](https://www.navidrome.org/) (and other Subsonic-compatible servers).

![](https://img.shields.io/badge/Go-1.26+-blue)

## Features

- Browse artists, albums, and songs
- Playlist support
- Search (artists, albums, songs)
- Lyrics display (synced/unsynced)
- Queue with auto-advance to next track
- Keyboard-driven TUI (powered by [Bubble Tea](https://github.com/charmbracelet/bubbletea))

## Requirements

- Go 1.26+
- [mpv](https://mpv.io/) for audio playback

## Install

```
go install github.com/mattn/nd@latest
```

## Configuration

Create `nd/config.json` in your OS config directory (e.g. `~/.config/nd/config.json` on Linux, `%APPDATA%\nd\config.json` on Windows):

```json
{
  "server": "http://localhost:4533",
  "user": "admin",
  "password": "your-password",
  "mpv": "/usr/bin/mpv"
}
```

Or pass flags directly:

```
nd -server http://localhost:4533 -user admin -password yourpass
```

The `mpv` field is optional and defaults to `mpv` in your `PATH`.

## Key Bindings

| Key | Action |
|-----|--------|
| `j` / `k` | Move down / up |
| `g` / `G` | Go to top / bottom |
| `Enter` / `l` | Select / enter |
| `h` / `Esc` / `Backspace` | Go back |
| `Space` | Play selected song |
| `n` / `N` | Next / previous track |
| `s` | Stop playback |
| `/` | Search |
| `L` | Show lyrics |
| `p` | Playlists |
| `a` | Artists |
| `q` | Quit |

## License

MIT

## Author

Yasuhiro Matsumoto (a.k.a. mattn)
