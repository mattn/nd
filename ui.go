package main

import (
	"fmt"
	"strings"
	"time"

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
	viewMenu      // browse menu (album lists, starred, random)
	viewListSongs // songs from the browse menu
	viewInfo      // details of an item, or help
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
	menuCursor  int    // last selected browse menu item
	albumList   string // album list type when albums came from the menu
	listTitle   string // title of the album list or song list

	// now playing
	nowPlaying *Song
	queue      []Song
	queueIdx   int
	playGen    uint64 // generation counter to ignore stale playDoneMsg
	playStart  time.Time
	scrobble   bool

	// search
	searchInput string
	searching   bool
	searchSeq   uint64 // incremented on each edit to debounce queries
	searchQuery string // query whose results are currently wanted
	searchPrev  searchState

	// cover art
	cover     coverConfig
	covers    map[string]string // sixel data by coverKey; "" when unavailable
	coverWant string            // coverKey of the cover that should be shown
	coverSeq  uint64            // incremented on each change to debounce fetches

	// lyrics
	lyrics     []LyricLine
	lyricsMode viewMode // mode to return to when leaving lyrics

	// info view
	info      []infoLine
	infoTitle string
	infoCover string
	infoKind  itemKind
	infoID    string
	infoExtra []infoLine // lines fetched from the server
	infoPrev  listState

	// error, or status of the last action
	err    error
	status string
}

type playDoneMsg struct{ gen uint64 }
type lyricsMsg struct{ lines []LyricLine }
type errMsg struct{ err error }
type artistsMsg struct{ artists []Artist }
type albumsMsg struct{ albums []Album }
type songsMsg struct{ songs []Song }
type playlistsMsg struct{ playlists []Playlist }
type playlistSongsMsg struct{ songs []Song }
type searchState struct {
	mode   viewMode
	cursor int
	offset int
	songs  []Song
}

type searchTickMsg struct{ seq uint64 }

type searchMsg struct {
	query   string
	artists []Artist
	albums  []Album
	songs   []Song
}

func newModel(client *SubsonicClient, player *Player, cover coverConfig, scrobble bool) model {
	return model{
		client:   client,
		player:   player,
		mode:     viewArtists,
		cover:    cover,
		covers:   map[string]string{},
		scrobble: scrobble,
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
			return searchMsg{query: query}
		}
		return searchMsg{query: query, artists: res.Artist, albums: res.Album, songs: res.Song}
	}
}

func (m model) listLen() int {
	switch m.mode {
	case viewArtists:
		return len(m.artists)
	case viewAlbums:
		return len(m.albums)
	case viewSongs, viewPlaylistSongs, viewSearch, viewListSongs:
		return len(m.songs)
	case viewPlaylists:
		return len(m.playlists)
	case viewLyrics:
		return len(m.lyrics)
	case viewMenu:
		return len(menuItems)
	case viewInfo:
		return len(m.info)
	}
	return 0
}

func (m model) visibleLines() int {
	h := m.height - 5 // header + now playing(2) + error + help
	if m.searching {
		h-- // search input line
	}
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
	nm, cmd := m.update(msg)
	return nm.(model).syncCover(cmd)
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			cmd := m.scrobblePlayed()
			m.nowPlaying = nil
			return m, cmd
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
		if m.curArtist != nil {
			for i, a := range m.artists {
				if a.ID == m.curArtist.ID {
					m.cursor = i
					break
				}
			}
		}
		m.clampCursor()
		return m, nil

	case albumsMsg:
		m.albums = msg.albums
		m.albumList = ""
		m.showAlbums()
		return m, nil

	case albumListMsg:
		m.albums = msg.albums
		m.albumList = msg.listType
		m.listTitle = msg.title
		m.showAlbums()
		return m, nil

	case listSongsMsg:
		m.songs = msg.songs
		m.listTitle = msg.title
		m.mode = viewListSongs
		m.cursor = 0
		m.offset = 0
		if msg.play && len(m.songs) > 0 {
			m.queue = m.songs
			m.queueIdx = 0
			return m.startPlay(m.songs[0])
		}
		return m, nil

	case infoExtraMsg:
		if msg.kind == m.infoKind && msg.id == m.infoID {
			m.infoExtra = msg.lines
			m.refreshInfo()
			m.clampCursor()
		}
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
		if m.curPlaylist != nil {
			for i, p := range m.playlists {
				if p.ID == m.curPlaylist.ID {
					m.cursor = i
					break
				}
			}
		}
		m.clampCursor()
		return m, nil

	case playlistSongsMsg:
		m.songs = msg.songs
		m.mode = viewPlaylistSongs
		m.cursor = 0
		m.offset = 0
		return m, nil

	case coverTickMsg:
		if msg.seq != m.coverSeq || m.coverWant == "" {
			return m, nil
		}
		if _, ok := m.covers[m.coverWant]; ok {
			return m, nil
		}
		return m, m.fetchCover(m.coverWant)

	case coverMsg:
		m.covers[msg.key] = msg.sixel
		return m, nil

	case searchTickMsg:
		if msg.seq != m.searchSeq || m.searchInput == "" {
			return m, nil
		}
		m.searchQuery = m.searchInput
		return m, m.doSearch(m.searchQuery)

	case searchMsg:
		// Ignore results for queries that are no longer current
		if msg.query != m.searchQuery {
			return m, nil
		}
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
		// Cancel: restore the view shown before searching
		m.searching = false
		m.searchInput = ""
		m.searchQuery = ""
		m.searchSeq++
		m.mode = m.searchPrev.mode
		m.cursor = m.searchPrev.cursor
		m.offset = m.searchPrev.offset
		m.songs = m.searchPrev.songs
		m.clampCursor()
		return m, nil
	case tea.KeyEnter:
		// Confirm: leave input mode and let the user pick from the results
		m.searching = false
		m.clampCursor()
		if m.searchInput != "" && m.searchInput != m.searchQuery {
			m.searchSeq++
			m.searchQuery = m.searchInput
			return m, m.doSearch(m.searchQuery)
		}
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		m.cursor--
		m.clampCursor()
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		m.cursor++
		m.clampCursor()
		return m, nil
	case tea.KeyBackspace:
		if r := []rune(m.searchInput); len(r) > 0 {
			m.searchInput = string(r[:len(r)-1])
		}
		return m.searchChanged()
	case tea.KeyRunes, tea.KeySpace:
		m.searchInput += string(msg.Runes)
		return m.searchChanged()
	}
	return m, nil
}

// searchChanged schedules a debounced search for the current input.
func (m model) searchChanged() (tea.Model, tea.Cmd) {
	m.searchSeq++
	if m.searchInput == "" {
		m.searchQuery = ""
		m.mode = m.searchPrev.mode
		m.cursor = m.searchPrev.cursor
		m.offset = m.searchPrev.offset
		m.songs = m.searchPrev.songs
		m.clampCursor()
		return m, nil
	}
	seq := m.searchSeq
	return m, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return searchTickMsg{seq: seq}
	})
}

// showAlbums switches to the album list, keeping the cursor on the album
// that was last opened.
func (m *model) showAlbums() {
	m.mode = viewAlbums
	m.cursor = 0
	m.offset = 0
	if m.curAlbum != nil {
		for i, a := range m.albums {
			if a.ID == m.curAlbum.ID {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = nil
	m.status = ""
	switch msg.String() {
	case "q", "ctrl+c":
		// Submit the current song before exiting
		if cmd := m.scrobblePlayed(); cmd != nil {
			cmd()
		}
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
		m.searchQuery = ""
		if m.mode != viewSearch {
			m.searchPrev = searchState{mode: m.mode, cursor: m.cursor, offset: m.offset, songs: m.songs}
		}
		m.clampCursor()
		return m, nil

	case "n":
		return m.playNext()

	case "N":
		return m.playPrev()

	case "s":
		cmd := m.scrobblePlayed()
		m.player.Stop()
		m.nowPlaying = nil
		return m, cmd

	case "b":
		return m.openMenu()

	case "r":
		return m.playRandom()

	case "i":
		return m.openInfo()

	case "f":
		return m.toggleStar()

	case "0", "1", "2", "3", "4", "5":
		return m.setRating(int(msg.String()[0] - '0'))

	case "?":
		return m.openHelp()

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
	case viewSongs, viewPlaylistSongs, viewSearch, viewListSongs:
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
	case viewMenu:
		return m.selectMenu()
	case viewInfo:
		// Open a similar artist
		if m.cursor < len(m.info) && m.info[m.cursor].artist != nil {
			art := *m.info[m.cursor].artist
			m.curArtist = &art
			m.infoKind = itemNone
			return m, m.fetchAlbums(art.ID)
		}
	}
	return m, nil
}

func (m model) handleBack() (tea.Model, tea.Cmd) {
	switch m.mode {
	case viewAlbums:
		if m.albumList != "" {
			return m.openMenu()
		}
		return m, m.fetchArtists()
	case viewSongs:
		if m.albumList != "" {
			// Albums from the menu are still loaded; refetching random
			// albums would give a different list.
			m.showAlbums()
			return m, nil
		}
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
	case viewListSongs:
		return m.openMenu()
	case viewMenu:
		return m, m.fetchArtists()
	case viewInfo:
		m.infoKind = itemNone
		m.restoreList(m.infoPrev)
		return m, nil
	}
	return m, nil
}

func (m model) handlePlayCurrent() (tea.Model, tea.Cmd) {
	if m.mode == viewSongs || m.mode == viewPlaylistSongs || m.mode == viewSearch || m.mode == viewListSongs {
		if m.cursor < len(m.songs) {
			m.queue = m.songs
			m.queueIdx = m.cursor
			return m.playSong(m.cursor)
		}
	}
	return m, nil
}

func (m model) startPlay(song Song) (model, tea.Cmd) {
	played := m.scrobblePlayed()
	m.playGen++
	m.nowPlaying = &song
	m.playStart = time.Now()
	m.err = nil
	streamURL := m.client.StreamURL(song.ID)
	if err := m.player.Play(streamURL); err != nil {
		m.err = err
		m.nowPlaying = nil
		return m, played
	}
	gen := m.playGen
	return m, tea.Batch(waitForPlayDone(m.player, gen), played, m.scrobbleNowPlaying())
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

// Styles are set by applyTheme.
var (
	cursorStyle  lipgloss.Style
	normalStyle  lipgloss.Style
	dimStyle     lipgloss.Style
	playingStyle lipgloss.Style
	headerStyle  lipgloss.Style
	helpStyle    lipgloss.Style
	errorStyle   lipgloss.Style
)

func (m model) View() string {
	var b strings.Builder

	// header
	switch m.mode {
	case viewArtists:
		b.WriteString(headerStyle.Render("  Artists"))
	case viewAlbums:
		if m.albumList != "" {
			b.WriteString(headerStyle.Render("  " + m.listTitle))
			break
		}
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
		b.WriteString(headerStyle.Render(fmt.Sprintf("  Search: %s", m.searchQuery)))
	case viewLyrics:
		title := ""
		if m.nowPlaying != nil {
			title = fmt.Sprintf("%s - %s", m.nowPlaying.Title, m.nowPlaying.Artist)
		}
		b.WriteString(headerStyle.Render(fmt.Sprintf("  Lyrics: %s", title)))
	case viewMenu:
		b.WriteString(headerStyle.Render("  Browse"))
	case viewListSongs:
		b.WriteString(headerStyle.Render("  " + m.listTitle))
	case viewInfo:
		title := m.infoTitle
		if m.infoKind != itemNone {
			title = "Info: " + title
		}
		b.WriteString(headerStyle.Render("  " + title))
	}
	b.WriteString("\n")

	if m.searching {
		b.WriteString(normalStyle.Render(fmt.Sprintf("  / %s_", m.searchInput)) + "\n")
	}

	// list
	vis := m.visibleLines()
	n := m.listLen()
	rows := make([]string, 0, vis)
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
			line = fmt.Sprintf("%s (%d albums)%s", a.Name, a.AlbumCount, starMark(a.Starred))
		case viewAlbums:
			a := m.albums[i]
			line = a.Name
			if m.albumList != "" && a.Artist != "" {
				line += " - " + a.Artist
			}
			if a.Year > 0 {
				line += fmt.Sprintf(" (%d)", a.Year)
			}
			line += starMark(a.Starred)
		case viewSongs, viewPlaylistSongs, viewSearch, viewListSongs:
			s := m.songs[i]
			dur := formatDuration(s.Duration)
			num := s.Track
			if m.mode == viewListSongs {
				num = i + 1
			}
			title := s.Title + starMark(s.Starred)
			if i == m.cursor {
				// Nested styles would reset the cursor background
				line = fmt.Sprintf("%2d. %s  %s  %s", num, title, s.Artist, dur)
			} else {
				line = fmt.Sprintf("%2d. %s  %s  %s", num, title, dimStyle.Render(s.Artist), dimStyle.Render(dur))
			}
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
		case viewMenu:
			line = menuItems[i].title
		case viewInfo:
			line = m.info[i].text
		}
		rows = append(rows, style.Render(prefix+line))
	}
	for len(rows) < vis {
		rows = append(rows, "")
	}
	b.WriteString(m.renderListWithCover(rows))

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
		b.WriteString(errorStyle.Render(fmt.Sprintf("  Error: %v", m.err)))
	} else if m.status != "" {
		b.WriteString(dimStyle.Render("  " + m.status))
	}
	b.WriteString("\n")

	// help
	b.WriteString(helpStyle.Render("  j/k:move  enter:select  esc:back  space:play  /:search  b:browse  i:info  f:star  ?:help  q:quit"))

	return fillBackground(b.String(), m.width, screenSeq)
}
