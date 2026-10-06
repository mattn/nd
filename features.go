package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// itemKind is the kind of item an action applies to.
type itemKind int

const (
	itemNone itemKind = iota
	itemArtist
	itemAlbum
	itemSong
)

// listState is a view to return to.
type listState struct {
	mode   viewMode
	cursor int
	offset int
}

func (m model) saveList() listState {
	return listState{mode: m.mode, cursor: m.cursor, offset: m.offset}
}

func (m *model) restoreList(s listState) {
	m.mode = s.mode
	m.cursor = s.cursor
	m.offset = s.offset
	m.clampCursor()
}

// ---------------------------------------------------------------------------
// Scrobbling

// scrobbleThreshold is how long a song must play to count as played: half
// its length or four minutes, whichever comes first (as Last.fm does).
func scrobbleThreshold(duration int) time.Duration {
	d := time.Duration(duration) * time.Second / 2
	if d > 4*time.Minute {
		d = 4 * time.Minute
	}
	return d
}

// scrobbleNowPlaying reports the song that just started playing.
func (m model) scrobbleNowPlaying() tea.Cmd {
	if !m.scrobble || m.nowPlaying == nil {
		return nil
	}
	id, at := m.nowPlaying.ID, m.playStart
	return func() tea.Msg {
		_ = m.client.Scrobble(id, at, false)
		return nil
	}
}

// scrobblePlayed submits the current song as played if it played long
// enough. Call it before the current song is replaced or stopped.
func (m model) scrobblePlayed() tea.Cmd {
	if !m.scrobble || m.nowPlaying == nil {
		return nil
	}
	th := scrobbleThreshold(m.nowPlaying.Duration)
	if th <= 0 || time.Since(m.playStart) < th {
		return nil
	}
	id, at := m.nowPlaying.ID, m.playStart
	return func() tea.Msg {
		if err := m.client.Scrobble(id, at, true); err != nil {
			return errMsg{fmt.Errorf("scrobble: %w", err)}
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// Favorites and ratings

// target returns the item an action applies to: the item under the cursor,
// or the song playing when the view has no such item.
func (m model) target() (itemKind, string) {
	switch m.mode {
	case viewArtists:
		if m.cursor < len(m.artists) {
			return itemArtist, m.artists[m.cursor].ID
		}
	case viewAlbums:
		if m.cursor < len(m.albums) {
			return itemAlbum, m.albums[m.cursor].ID
		}
	case viewSongs, viewPlaylistSongs, viewSearch, viewListSongs:
		if m.cursor < len(m.songs) {
			return itemSong, m.songs[m.cursor].ID
		}
	case viewInfo:
		if m.infoKind != itemNone {
			return m.infoKind, m.infoID
		}
	}
	if m.nowPlaying != nil {
		return itemSong, m.nowPlaying.ID
	}
	return itemNone, ""
}

// forEachItem calls fn for the starred and rating fields of every loaded
// copy of the item.
func (m *model) forEachItem(kind itemKind, id string, fn func(starred *string, rating *int)) {
	switch kind {
	case itemArtist:
		for i := range m.artists {
			if m.artists[i].ID == id {
				fn(&m.artists[i].Starred, &m.artists[i].UserRating)
			}
		}
		if m.curArtist != nil && m.curArtist.ID == id {
			fn(&m.curArtist.Starred, &m.curArtist.UserRating)
		}
	case itemAlbum:
		for i := range m.albums {
			if m.albums[i].ID == id {
				fn(&m.albums[i].Starred, &m.albums[i].UserRating)
			}
		}
	case itemSong:
		for _, list := range [][]Song{m.songs, m.queue, m.searchPrev.songs} {
			for i := range list {
				if list[i].ID == id {
					fn(&list[i].Starred, &list[i].UserRating)
				}
			}
		}
		if m.nowPlaying != nil && m.nowPlaying.ID == id {
			fn(&m.nowPlaying.Starred, &m.nowPlaying.UserRating)
		}
	}
}

func (m model) isStarred(kind itemKind, id string) bool {
	starred := false
	m.forEachItem(kind, id, func(s *string, _ *int) {
		if *s != "" {
			starred = true
		}
	})
	return starred
}

func (m model) toggleStar() (tea.Model, tea.Cmd) {
	kind, id := m.target()
	if kind == itemNone {
		return m, nil
	}
	star := !m.isStarred(kind, id)
	val := ""
	m.status = "☆ Unstarred"
	if star {
		val = time.Now().UTC().Format(time.RFC3339)
		m.status = "★ Starred"
	}
	m.forEachItem(kind, id, func(s *string, _ *int) { *s = val })
	m.refreshInfo()
	return m, func() tea.Msg {
		if err := m.client.SetStarred(kind, id, star); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m model) setRating(rating int) (tea.Model, tea.Cmd) {
	kind, id := m.target()
	if kind == itemNone {
		return m, nil
	}
	m.forEachItem(kind, id, func(_ *string, r *int) { *r = rating })
	if rating == 0 {
		m.status = "Rating cleared"
	} else {
		m.status = "Rated " + stars(rating)
	}
	m.refreshInfo()
	return m, func() tea.Msg {
		if err := m.client.SetRating(id, rating); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func stars(rating int) string {
	if rating <= 0 {
		return "-"
	}
	return strings.Repeat("★", rating) + strings.Repeat("☆", 5-rating)
}

// starMark is appended to list items that are starred.
func starMark(starred string) string {
	if starred != "" {
		return " ★"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Browse menu

type menuItem struct {
	title    string
	listType string // getAlbumList2 type; empty for song lists
	songs    func(c *SubsonicClient) ([]Song, error)
}

var menuItems = []menuItem{
	{title: "Recently added", listType: "newest"},
	{title: "Recently played", listType: "recent"},
	{title: "Most played", listType: "frequent"},
	{title: "Top rated", listType: "highest"},
	{title: "Random albums", listType: "random"},
	{title: "Starred albums", listType: "starred"},
	{title: "Starred songs", songs: (*SubsonicClient).GetStarredSongs},
	{title: "Random songs", songs: randomSongs},
}

func randomSongs(c *SubsonicClient) ([]Song, error) {
	return c.GetRandomSongs(100)
}

type albumListMsg struct {
	albums   []Album
	listType string
	title    string
}

type listSongsMsg struct {
	songs []Song
	title string
	play  bool
}

func (m model) fetchAlbumList(listType, title string) tea.Cmd {
	return func() tea.Msg {
		albums, err := m.client.GetAlbumList(listType, 100)
		if err != nil {
			return errMsg{err}
		}
		return albumListMsg{albums: albums, listType: listType, title: title}
	}
}

func (m model) fetchListSongs(item menuItem, play bool) tea.Cmd {
	return func() tea.Msg {
		songs, err := item.songs(m.client)
		if err != nil {
			return errMsg{err}
		}
		return listSongsMsg{songs: songs, title: item.title, play: play}
	}
}

func (m model) openMenu() (tea.Model, tea.Cmd) {
	m.mode = viewMenu
	m.cursor = m.menuCursor
	m.offset = 0
	m.clampCursor()
	return m, nil
}

func (m model) selectMenu() (tea.Model, tea.Cmd) {
	if m.cursor >= len(menuItems) {
		return m, nil
	}
	m.menuCursor = m.cursor
	item := menuItems[m.cursor]
	if item.listType != "" {
		m.curAlbum = nil
		return m, m.fetchAlbumList(item.listType, item.title)
	}
	return m, m.fetchListSongs(item, false)
}

// playRandom loads random songs and starts playing them.
func (m model) playRandom() (tea.Model, tea.Cmd) {
	for _, item := range menuItems {
		if item.title == "Random songs" {
			return m, m.fetchListSongs(item, true)
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Info view

// infoLine is a line of the info view. Lines with an artist open that
// artist when selected.
type infoLine struct {
	text   string
	artist *Artist
}

type infoExtraMsg struct {
	kind  itemKind
	id    string
	lines []infoLine
}

// infoWrapWidth is the width text in the info view is wrapped to.
func (m model) infoWrapWidth() int {
	w := m.width - 4
	if cols, _, _ := m.coverBox(); cols > 0 {
		w = m.width - cols - coverGap - 1 - 4
	}
	if w < 20 {
		w = 20
	}
	return w
}

func (m model) wrapText(text string) []infoLine {
	var lines []infoLine
	for _, para := range strings.Split(text, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			lines = append(lines, infoLine{})
			continue
		}
		for _, l := range strings.Split(ansi.Wrap(para, m.infoWrapWidth(), ""), "\n") {
			lines = append(lines, infoLine{text: l})
		}
	}
	return lines
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// plainText converts the HTML in biographies and notes to plain text.
func plainText(s string) string {
	s = strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p>", "\n").Replace(s)
	return strings.TrimSpace(html.UnescapeString(htmlTagRe.ReplaceAllString(s, "")))
}

func field(label, value string) infoLine {
	return infoLine{text: fmt.Sprintf("%-9s %s", label+":", value)}
}

func formatDuration(sec int) string {
	if sec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

func formatDate(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Local().Format("2006-01-02")
	}
	return s
}

func starredText(s string) string {
	if s == "" {
		return "no"
	}
	return "yes (" + formatDate(s) + ")"
}

// openInfo shows details of the target item.
func (m model) openInfo() (tea.Model, tea.Cmd) {
	kind, id := m.target()
	if kind == itemNone {
		return m, nil
	}
	if m.mode != viewInfo {
		m.infoPrev = m.saveList()
	}
	m.infoKind, m.infoID = kind, id
	m.infoExtra = nil
	m.refreshInfo()
	m.mode = viewInfo
	m.cursor = 0
	m.offset = 0

	switch kind {
	case itemArtist:
		return m, func() tea.Msg {
			info, err := m.client.GetArtistInfo(id)
			if err != nil {
				return errMsg{err}
			}
			var lines []infoLine
			if bio := plainText(info.Biography); bio != "" {
				lines = append(lines, infoLine{})
				lines = append(lines, m.wrapText(bio)...)
			}
			if len(info.SimilarArtist) > 0 {
				lines = append(lines, infoLine{}, infoLine{text: "Similar artists:"})
				for i := range info.SimilarArtist {
					a := info.SimilarArtist[i]
					l := infoLine{text: "  " + a.Name}
					if a.ID != "" {
						l.artist = &a
					}
					lines = append(lines, l)
				}
			}
			return infoExtraMsg{kind: kind, id: id, lines: lines}
		}
	case itemAlbum:
		return m, func() tea.Msg {
			info, err := m.client.GetAlbumInfo(id)
			if err != nil {
				return nil // notes are optional
			}
			var lines []infoLine
			if notes := plainText(info.Notes); notes != "" {
				lines = append(lines, infoLine{})
				lines = append(lines, m.wrapText(notes)...)
			}
			return infoExtraMsg{kind: kind, id: id, lines: lines}
		}
	}
	return m, nil
}

// refreshInfo rebuilds the info view from the loaded data, e.g. after the
// item was starred or rated.
func (m *model) refreshInfo() {
	if m.infoKind == itemNone {
		return
	}
	var lines []infoLine
	m.infoCover = ""
	switch m.infoKind {
	case itemArtist:
		a := m.findArtist(m.infoID)
		if a == nil {
			return
		}
		m.infoTitle = a.Name
		m.infoCover = a.CoverArt
		lines = append(lines,
			field("Artist", a.Name),
			field("Albums", fmt.Sprint(a.AlbumCount)),
			field("Rating", stars(a.UserRating)),
			field("Starred", starredText(a.Starred)),
		)
	case itemAlbum:
		a := m.findAlbum(m.infoID)
		if a == nil {
			return
		}
		m.infoTitle = a.Name
		m.infoCover = a.CoverArt
		lines = append(lines, field("Album", a.Name), field("Artist", a.Artist))
		if a.Year > 0 {
			lines = append(lines, field("Year", fmt.Sprint(a.Year)))
		}
		if a.Genre != "" {
			lines = append(lines, field("Genre", a.Genre))
		}
		lines = append(lines,
			field("Songs", fmt.Sprint(a.SongCount)),
			field("Length", formatDuration(a.Duration)),
			field("Plays", fmt.Sprint(a.PlayCount)),
			field("Rating", stars(a.UserRating)),
			field("Starred", starredText(a.Starred)),
		)
		if a.Created != "" {
			lines = append(lines, field("Added", formatDate(a.Created)))
		}
	case itemSong:
		s := m.findSong(m.infoID)
		if s == nil {
			return
		}
		m.infoTitle = s.Title
		m.infoCover = s.CoverArt
		lines = append(lines, field("Title", s.Title), field("Artist", s.Artist), field("Album", s.Album))
		if s.Track > 0 {
			track := fmt.Sprint(s.Track)
			if s.Disc > 0 {
				track = fmt.Sprintf("%d-%d", s.Disc, s.Track)
			}
			lines = append(lines, field("Track", track))
		}
		if s.Year > 0 {
			lines = append(lines, field("Year", fmt.Sprint(s.Year)))
		}
		if s.Genre != "" {
			lines = append(lines, field("Genre", s.Genre))
		}
		lines = append(lines, field("Length", formatDuration(s.Duration)))
		if s.Suffix != "" {
			format := strings.ToUpper(s.Suffix)
			if s.BitRate > 0 {
				format += fmt.Sprintf(" %d kbps", s.BitRate)
			}
			lines = append(lines, field("Format", format))
		}
		if s.Size > 0 {
			lines = append(lines, field("Size", fmt.Sprintf("%.1f MB", float64(s.Size)/1024/1024)))
		}
		lines = append(lines,
			field("Plays", fmt.Sprint(s.PlayCount)),
			field("Rating", stars(s.UserRating)),
			field("Starred", starredText(s.Starred)),
		)
		if s.Path != "" {
			lines = append(lines, field("Path", s.Path))
		}
	}
	m.info = append(lines, m.infoExtra...)
}

func (m *model) findArtist(id string) *Artist {
	if m.curArtist != nil && m.curArtist.ID == id {
		return m.curArtist
	}
	for i := range m.artists {
		if m.artists[i].ID == id {
			return &m.artists[i]
		}
	}
	return nil
}

func (m *model) findAlbum(id string) *Album {
	for i := range m.albums {
		if m.albums[i].ID == id {
			return &m.albums[i]
		}
	}
	return nil
}

func (m *model) findSong(id string) *Song {
	for _, list := range [][]Song{m.songs, m.queue} {
		for i := range list {
			if list[i].ID == id {
				return &list[i]
			}
		}
	}
	if m.nowPlaying != nil && m.nowPlaying.ID == id {
		return m.nowPlaying
	}
	return nil
}

var helpLines = []string{
	"j / k, ↓ / ↑     Move down / up",
	"g / G           Go to top / bottom",
	"Enter / l       Select / enter",
	"h / Esc         Go back",
	"Space           Play selected song",
	"n / N           Next / previous track",
	"s               Stop playback",
	"/               Search",
	"a               Artists",
	"p               Playlists",
	"b               Browse (recent, most played, random, starred...)",
	"r               Play random songs",
	"i               Show details of the selected item",
	"f               Star / unstar the selected item",
	"1-5 / 0         Rate / clear rating of the selected item",
	"L               Show lyrics",
	"?               Show this help",
	"q               Quit",
}

func (m model) openHelp() (tea.Model, tea.Cmd) {
	if m.mode != viewInfo {
		m.infoPrev = m.saveList()
	}
	m.infoKind, m.infoID = itemNone, ""
	m.infoTitle = "Help"
	m.infoCover = ""
	m.info = nil
	for _, l := range helpLines {
		m.info = append(m.info, infoLine{text: l})
	}
	m.mode = viewInfo
	m.cursor = 0
	m.offset = 0
	return m, nil
}
