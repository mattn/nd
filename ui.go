package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type viewMode int

const (
	viewArtists viewMode = iota
	viewAlbums
	viewSongs
	viewPlaylists
	viewPlaylistSongs
	viewSearch
	viewLyrics
)

type model struct {
	client *SubsonicClient
	player *Player

	mode   viewMode
	width  int
	height int

	// data
	artists   []Artist
	albums    []Album
	songs     []Song
	playlists []Playlist

	// navigation
	cursor      int
	offset      int
	curArtist   *Artist
	curAlbum    *Album
	curPlaylist *Playlist

	// now playing
	nowPlaying *Song
	queue      []Song
	queueIdx   int
	playGen    uint64 // generation counter to ignore stale playDoneMsg

	// search
	searchInput string
	searching   bool

	// lyrics
	lyrics     []LyricLine
	lyricsMode viewMode // mode to return to when leaving lyrics

	// error
	err error
}

type playDoneMsg struct{ gen uint64 }
type lyricsMsg struct{ lines []LyricLine }
type errMsg struct{ err error }
type artistsMsg struct{ artists []Artist }
type albumsMsg struct{ albums []Album }
type songsMsg struct{ songs []Song }
type playlistsMsg struct{ playlists []Playlist }
type playlistSongsMsg struct{ songs []Song }
type searchMsg struct {
	artists []Artist
	albums  []Album
	songs   []Song
}

func newModel(client *SubsonicClient, player *Player) model {
	return model{
		client: client,
		player: player,
		mode:   viewArtists,
	}
}

func (m model) Init() tea.Cmd {
	return m.fetchArtists()
}

func (m model) fetchArtists() tea.Cmd {
	return func() tea.Msg {
		artists, err := m.client.GetArtists()
		if err != nil {
			return errMsg{err}
		}
		return artistsMsg{artists}
	}
}

func (m model) fetchAlbums(id string) tea.Cmd {
	return func() tea.Msg {
		albums, err := m.client.GetArtist(id)
		if err != nil {
			return errMsg{err}
		}
		return albumsMsg{albums}
	}
}

func (m model) fetchSongs(id string) tea.Cmd {
	return func() tea.Msg {
		songs, err := m.client.GetAlbum(id)
		if err != nil {
			return errMsg{err}
		}
		return songsMsg{songs}
	}
}

func (m model) fetchLyrics(songID string) tea.Cmd {
	return func() tea.Msg {
		lyrics, err := m.client.GetLyrics(songID)
		if err != nil {
			return errMsg{err}
		}
		if len(lyrics) == 0 {
			return errMsg{fmt.Errorf("no lyrics available")}
		}
		// Prefer synced lyrics, fallback to unsynced
		var best *StructuredLyrics
		for i := range lyrics {
			if best == nil || lyrics[i].Synced {
				best = &lyrics[i]
			}
		}
		return lyricsMsg{lines: best.Line}
	}
}

func (m model) fetchPlaylists() tea.Cmd {
	return func() tea.Msg {
		pls, err := m.client.GetPlaylists()
		if err != nil {
			return errMsg{err}
		}
		return playlistsMsg{pls}
	}
}

func (m model) fetchPlaylistSongs(id string) tea.Cmd {
	return func() tea.Msg {
		songs, err := m.client.GetPlaylist(id)
		if err != nil {
			return errMsg{err}
		}
		return playlistSongsMsg{songs}
	}
}

func (m model) doSearch(query string) tea.Cmd {
	return func() tea.Msg {
		res, err := m.client.Search(query)
		if err != nil {
			return errMsg{err}
		}
		if res == nil {
			return searchMsg{}
		}
		return searchMsg{artists: res.Artist, albums: res.Album, songs: res.Song}
	}
}

func (m model) listLen() int {
	switch m.mode {
	case viewArtists:
		return len(m.artists)
	case viewAlbums:
		return len(m.albums)
	case viewSongs, viewPlaylistSongs:
		return len(m.songs)
	case viewPlaylists:
		return len(m.playlists)
	case viewSearch:
		return len(m.songs)
	case viewLyrics:
		return len(m.lyrics)
	}
	return 0
}

func (m model) visibleLines() int {
	h := m.height - 5 // header + now playing(2) + error + help
	if h < 1 {
		h = 20
	}
	return h
}

func (m *model) clampCursor() {
	n := m.listLen()
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	vis := m.visibleLines()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateNormal(msg)

	case errMsg:
		m.err = msg.err
		return m, nil

	case playDoneMsg:
		// Ignore stale playDoneMsg from previous songs
		if msg.gen != m.playGen {
			return m, nil
		}
		// Auto-advance to next song in queue
		if m.nowPlaying != nil && len(m.queue) > 0 {
			m.queueIdx++
			if m.queueIdx < len(m.queue) {
				return m.startPlay(m.queue[m.queueIdx])
			}
			// End of queue
			m.nowPlaying = nil
		}
		return m, nil

	case lyricsMsg:
		m.lyrics = msg.lines
		m.lyricsMode = m.mode
		m.mode = viewLyrics
		m.cursor = 0
		m.offset = 0
		return m, nil

	case artistsMsg:
		m.artists = msg.artists
		m.mode = viewArtists
		m.cursor = 0
		m.offset = 0
		return m, nil

	case albumsMsg:
		m.albums = msg.albums
		m.mode = viewAlbums
		m.cursor = 0
		m.offset = 0
		return m, nil

	case songsMsg:
		m.songs = msg.songs
		m.mode = viewSongs
		m.cursor = 0
		m.offset = 0
		return m, nil

	case playlistsMsg:
		m.playlists = msg.playlists
		m.mode = viewPlaylists
		m.cursor = 0
		m.offset = 0
		return m, nil

	case playlistSongsMsg:
		m.songs = msg.songs
		m.mode = viewPlaylistSongs
		m.cursor = 0
		m.offset = 0
		return m, nil

	case searchMsg:
		m.songs = msg.songs
		m.mode = viewSearch
		m.cursor = 0
		m.offset = 0
		return m, nil
	}
	return m, nil
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.searching = false
		m.searchInput = ""
		return m, nil
	case tea.KeyEnter:
		m.searching = false
		q := m.searchInput
		m.searchInput = ""
		if q != "" {
			return m, m.doSearch(q)
		}
		return m, nil
	case tea.KeyBackspace:
		if len(m.searchInput) > 0 {
			m.searchInput = m.searchInput[:len(m.searchInput)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.searchInput += string(msg.Runes)
		}
		return m, nil
	}
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = nil
	switch msg.String() {
	case "q", "ctrl+c":
		m.playGen++
		m.nowPlaying = nil
		m.player.Stop()
		return m, tea.Quit

	case "up", "k":
		m.cursor--
		m.clampCursor()
		return m, nil

	case "down", "j":
		m.cursor++
		m.clampCursor()
		return m, nil

	case "home", "g":
		m.cursor = 0
		m.clampCursor()
		return m, nil

	case "end", "G":
		m.cursor = m.listLen() - 1
		m.clampCursor()
		return m, nil

	case "enter", "l":
		return m.handleSelect()

	case "backspace", "h", "esc":
		return m.handleBack()

	case " ":
		// play selected song directly
		return m.handlePlayCurrent()

	case "p":
		// playlists
		return m, m.fetchPlaylists()

	case "a":
		// back to artists
		return m, m.fetchArtists()

	case "/":
		m.searching = true
		m.searchInput = ""
		return m, nil

	case "n":
		return m.playNext()

	case "N":
		return m.playPrev()

	case "s":
		m.player.Stop()
		m.nowPlaying = nil
		return m, nil

	case "L":
		// show lyrics for now playing song
		if m.nowPlaying != nil {
			return m, m.fetchLyrics(m.nowPlaying.ID)
		}
		return m, nil
	}
	return m, nil
}

func (m model) handleSelect() (tea.Model, tea.Cmd) {
	switch m.mode {
	case viewArtists:
		if m.cursor < len(m.artists) {
			art := m.artists[m.cursor]
			m.curArtist = &art
			return m, m.fetchAlbums(art.ID)
		}
	case viewAlbums:
		if m.cursor < len(m.albums) {
			alb := m.albums[m.cursor]
			m.curAlbum = &alb
			return m, m.fetchSongs(alb.ID)
		}
	case viewSongs, viewPlaylistSongs, viewSearch:
		if m.cursor < len(m.songs) {
			m.queue = m.songs
			m.queueIdx = m.cursor
			return m.playSong(m.cursor)
		}
	case viewPlaylists:
		if m.cursor < len(m.playlists) {
			pl := m.playlists[m.cursor]
			m.curPlaylist = &pl
			return m, m.fetchPlaylistSongs(pl.ID)
		}
	}
	return m, nil
}

func (m model) handleBack() (tea.Model, tea.Cmd) {
	switch m.mode {
	case viewAlbums:
		return m, m.fetchArtists()
	case viewSongs:
		if m.curArtist != nil {
			return m, m.fetchAlbums(m.curArtist.ID)
		}
		return m, m.fetchArtists()
	case viewPlaylistSongs:
		return m, m.fetchPlaylists()
	case viewSearch:
		return m, m.fetchArtists()
	case viewLyrics:
		m.mode = m.lyricsMode
		m.cursor = 0
		m.offset = 0
		return m, nil
	}
	return m, nil
}

func (m model) handlePlayCurrent() (tea.Model, tea.Cmd) {
	if m.mode == viewSongs || m.mode == viewPlaylistSongs || m.mode == viewSearch {
		if m.cursor < len(m.songs) {
			m.queue = m.songs
			m.queueIdx = m.cursor
			return m.playSong(m.cursor)
		}
	}
	return m, nil
}

func (m model) startPlay(song Song) (model, tea.Cmd) {
	m.playGen++
	m.nowPlaying = &song
	m.err = nil
	streamURL := m.client.StreamURL(song.ID)
	if err := m.player.Play(streamURL); err != nil {
		m.err = err
		return m, nil
	}
	gen := m.playGen
	return m, waitForPlayDone(m.player, gen)
}

func waitForPlayDone(player *Player, gen uint64) tea.Cmd {
	return func() tea.Msg {
		<-player.Done()
		return playDoneMsg{gen: gen}
	}
}

func (m model) playSong(idx int) (model, tea.Cmd) {
	if idx < 0 || idx >= len(m.songs) {
		return m, nil
	}
	return m.startPlay(m.songs[idx])
}

func (m model) playNext() (model, tea.Cmd) {
	if len(m.queue) == 0 {
		return m, nil
	}
	m.queueIdx++
	if m.queueIdx >= len(m.queue) {
		m.queueIdx = 0
	}
	return m.startPlay(m.queue[m.queueIdx])
}

func (m model) playPrev() (model, tea.Cmd) {
	if len(m.queue) == 0 {
		return m, nil
	}
	m.queueIdx--
	if m.queueIdx < 0 {
		m.queueIdx = len(m.queue) - 1
	}
	return m.startPlay(m.queue[m.queueIdx])
}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	cursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	normalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	playingStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("82"))
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func (m model) View() string {
	var b strings.Builder

	// header
	switch m.mode {
	case viewArtists:
		b.WriteString(headerStyle.Render("  Artists"))
	case viewAlbums:
		name := ""
		if m.curArtist != nil {
			name = m.curArtist.Name
		}
		b.WriteString(headerStyle.Render(fmt.Sprintf("  %s - Albums", name)))
	case viewSongs:
		name := ""
		if m.curAlbum != nil {
			name = m.curAlbum.Name
		}
		b.WriteString(headerStyle.Render(fmt.Sprintf("  %s", name)))
	case viewPlaylists:
		b.WriteString(headerStyle.Render("  Playlists"))
	case viewPlaylistSongs:
		name := ""
		if m.curPlaylist != nil {
			name = m.curPlaylist.Name
		}
		b.WriteString(headerStyle.Render(fmt.Sprintf("  %s", name)))
	case viewSearch:
		b.WriteString(headerStyle.Render("  Search Results"))
	case viewLyrics:
		title := ""
		if m.nowPlaying != nil {
			title = fmt.Sprintf("%s - %s", m.nowPlaying.Title, m.nowPlaying.Artist)
		}
		b.WriteString(headerStyle.Render(fmt.Sprintf("  Lyrics: %s", title)))
	}
	b.WriteString("\n")

	if m.searching {
		b.WriteString(fmt.Sprintf("  / %s_\n", m.searchInput))
	}

	// list
	vis := m.visibleLines()
	n := m.listLen()
	for i := m.offset; i < m.offset+vis && i < n; i++ {
		prefix := "  "
		style := normalStyle
		if i == m.cursor {
			prefix = "> "
			style = cursorStyle
		}

		var line string
		switch m.mode {
		case viewArtists:
			a := m.artists[i]
			line = fmt.Sprintf("%s (%d albums)", a.Name, a.AlbumCount)
		case viewAlbums:
			a := m.albums[i]
			if a.Year > 0 {
				line = fmt.Sprintf("%s (%d)", a.Name, a.Year)
			} else {
				line = a.Name
			}
		case viewSongs, viewPlaylistSongs, viewSearch:
			s := m.songs[i]
			dur := fmt.Sprintf("%d:%02d", s.Duration/60, s.Duration%60)
			line = fmt.Sprintf("%2d. %s  %s  %s", s.Track, s.Title, dimStyle.Render(s.Artist), dimStyle.Render(dur))
			if m.nowPlaying != nil && s.ID == m.nowPlaying.ID {
				style = playingStyle
			}
		case viewPlaylists:
			p := m.playlists[i]
			line = fmt.Sprintf("%s (%d songs)", p.Name, p.SongCount)
		case viewLyrics:
			l := m.lyrics[i]
			line = l.Value
			if line == "" {
				line = " "
			}
		}
		b.WriteString(style.Render(prefix+line) + "\n")
	}

	// pad remaining lines
	for i := n; i < m.offset+vis; i++ {
		b.WriteString("\n")
	}

	// now playing
	b.WriteString("\n")
	if m.nowPlaying != nil {
		b.WriteString(playingStyle.Render(fmt.Sprintf("  ♪ %s - %s", m.nowPlaying.Title, m.nowPlaying.Artist)))
	} else {
		b.WriteString(dimStyle.Render("  ♪ Not playing"))
	}
	b.WriteString("\n")

	// error (always reserve one line to keep layout stable)
	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("  Error: %v", m.err)))
	}
	b.WriteString("\n")

	// help
	b.WriteString(helpStyle.Render("  j/k:move  enter/l:select  h/esc:back  space:play  n/N:next/prev  s:stop  /:search  L:lyrics  p:playlists  a:artists  q:quit"))
	b.WriteString("\n")

	return b.String()
}
