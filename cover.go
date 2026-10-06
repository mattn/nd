package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
)

// coverConfig describes whether cover art can be drawn and the terminal's
// cell size in pixels.
type coverConfig struct {
	enabled bool
	cellW   int
	cellH   int
}

// detectCover checks the terminal for sixel support and its cell size. It
// must run before the TUI starts since it talks to the terminal directly.
func detectCover() coverConfig {
	if !sixel.IsSupported() {
		return coverConfig{}
	}
	w, h, err := sixel.CellSize()
	if err != nil || w <= 0 || h <= 0 {
		return coverConfig{}
	}
	return coverConfig{enabled: true, cellW: w, cellH: h}
}

type coverTickMsg struct{ seq uint64 }

type coverMsg struct {
	key   string
	sixel string
}

const coverGap = 1 // columns between the list and the cover

// coverBox returns the size of the cover in cells and in pixels, or zero
// when there is no room for it.
func (m model) coverBox() (cols, rows, px int) {
	c := m.cover
	if !c.enabled || m.width < 60 {
		return 0, 0, 0
	}
	vis := m.visibleLines()
	px = (m.width * 2 / 5) * c.cellW
	if h := vis * c.cellH; px > h {
		px = h
	}
	// Sixel bands are 6 pixels tall; keep the image inside its rows.
	px = px / 6 * 6
	cols = (px + c.cellW - 1) / c.cellW
	rows = (px + c.cellH - 1) / c.cellH
	if cols < 8 || rows < 4 {
		return 0, 0, 0
	}
	return cols, rows, px
}

// coverID returns the cover art ID for what is focused on the screen.
func (m model) coverID() string {
	switch m.mode {
	case viewAlbums:
		if m.cursor < len(m.albums) {
			return m.albums[m.cursor].CoverArt
		}
	case viewSongs, viewPlaylistSongs, viewSearch:
		if m.cursor < len(m.songs) {
			return m.songs[m.cursor].CoverArt
		}
	}
	if m.nowPlaying != nil {
		return m.nowPlaying.CoverArt
	}
	return ""
}

func (m model) coverKey() string {
	id := m.coverID()
	_, _, px := m.coverBox()
	if id == "" || px == 0 {
		return ""
	}
	return fmt.Sprintf("%s@%d", id, px)
}

// syncCover schedules a fetch when the cover to show has changed. Fetches
// are debounced so scrolling through a list does not request every cover.
func (m model) syncCover(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	key := m.coverKey()
	if key == m.coverWant {
		return m, cmd
	}
	m.coverWant = key
	m.coverSeq++
	if key == "" {
		return m, cmd
	}
	if _, ok := m.covers[key]; ok {
		return m, cmd
	}
	seq := m.coverSeq
	return m, tea.Batch(cmd, tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return coverTickMsg{seq: seq}
	}))
}

func (m model) fetchCover(key string) tea.Cmd {
	id := m.coverID()
	_, _, px := m.coverBox()
	return func() tea.Msg {
		img, err := m.client.CoverArt(id, px)
		if err != nil {
			return coverMsg{key: key}
		}
		return coverMsg{key: key, sixel: encodeCover(img, px)}
	}
}

// encodeCover scales img to fit a px*px square, letterboxed with the theme
// background, and encodes it as sixel.
func encodeCover(img image.Image, px int) string {
	dst := image.NewRGBA(image.Rect(0, 0, px, px))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(coverBG), image.Point{}, draw.Src)
	b := img.Bounds()
	w, h := px, px
	if b.Dx() > b.Dy() {
		h = px * b.Dy() / b.Dx()
	} else if b.Dy() > b.Dx() {
		w = px * b.Dx() / b.Dy()
	}
	r := image.Rect((px-w)/2, (px-h)/2, (px-w)/2+w, (px-h)/2+h)
	draw.CatmullRom.Scale(dst, r, img, b, draw.Over, nil)

	var buf bytes.Buffer
	if err := sixel.NewEncoder(&buf).Encode(dst); err != nil {
		return ""
	}
	return buf.String()
}

// coverBG is the color around covers that are not square.
var coverBG color.Color = color.Black

// renderListWithCover lays out the list rows with the cover on their right.
// The cover is drawn from the first row; the other rows skip over it with a
// cursor movement so redrawing them leaves the image intact.
func (m model) renderListWithCover(rows []string) string {
	cols, imgRows, _ := m.coverBox()
	if cols == 0 || m.coverWant == "" {
		return strings.Join(rows, "\n") + "\n"
	}
	data := m.covers[m.coverWant]
	leftW := m.width - cols - coverGap - 1
	margin := strings.Repeat(" ", m.width-leftW-coverGap-cols)
	gap := strings.Repeat(" ", coverGap)
	blank := strings.Repeat(" ", cols)
	skip := ansi.CursorForward(cols)

	var b strings.Builder
	for i, row := range rows {
		row = ansi.Truncate(row, leftW, "")
		if w := ansi.StringWidth(row); w < leftW {
			row += strings.Repeat(" ", leftW-w)
		}
		b.WriteString(row)
		b.WriteString(gap)
		switch {
		case data == "" || i >= imgRows:
			b.WriteString(blank)
		case i == 0:
			b.WriteString(ansi.SaveCursor + data + ansi.RestoreCursor + skip)
		default:
			b.WriteString(skip)
		}
		b.WriteString(margin)
		b.WriteString("\n")
	}
	return b.String()
}
