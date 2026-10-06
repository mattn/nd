package main

import (
	"fmt"
	"image/color"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colors holds the color of each UI element. Values are ANSI 256 color
// numbers (e.g. "212") or hex colors (e.g. "#ff79c6"). Empty means unset.
// Background "none" keeps the terminal's own background.
type Colors struct {
	Background string `json:"background,omitempty"`
	Header     string `json:"header,omitempty"`
	Normal     string `json:"normal,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	CursorBG   string `json:"cursor_bg,omitempty"`
	Dim        string `json:"dim,omitempty"`
	Playing    string `json:"playing,omitempty"`
	Help       string `json:"help,omitempty"`
	Error      string `json:"error,omitempty"`
}

var themes = map[string]Colors{
	"default": {
		Header:  "39",
		Normal:  "252",
		Cursor:  "212",
		Dim:     "240",
		Playing: "82",
		Help:    "241",
		Error:   "196",
	},
	"dracula": {
		Background: "#282a36",
		Header:     "#bd93f9",
		Normal:     "#f8f8f2",
		Cursor:     "#ff79c6",
		CursorBG:   "#44475a",
		Dim:        "#6272a4",
		Playing:    "#50fa7b",
		Help:       "#6272a4",
		Error:      "#ff5555",
	},
	"nord": {
		Background: "#2e3440",
		Header:     "#88c0d0",
		Normal:     "#d8dee9",
		Cursor:     "#eceff4",
		CursorBG:   "#434c5e",
		Dim:        "#616e88",
		Playing:    "#a3be8c",
		Help:       "#616e88",
		Error:      "#bf616a",
	},
	"gruvbox": {
		Background: "#282828",
		Header:     "#83a598",
		Normal:     "#ebdbb2",
		Cursor:     "#fabd2f",
		CursorBG:   "#3c3836",
		Dim:        "#928374",
		Playing:    "#b8bb26",
		Help:       "#928374",
		Error:      "#fb4934",
	},
	"gruvbox-light": {
		Background: "#fbf1c7",
		Header:     "#076678",
		Normal:     "#3c3836",
		Cursor:     "#b57614",
		CursorBG:   "#ebdbb2",
		Dim:        "#928374",
		Playing:    "#79740e",
		Help:       "#928374",
		Error:      "#9d0006",
	},
	"catppuccin": {
		Background: "#1e1e2e",
		Header:     "#89b4fa",
		Normal:     "#cdd6f4",
		Cursor:     "#f5c2e7",
		CursorBG:   "#313244",
		Dim:        "#6c7086",
		Playing:    "#a6e3a1",
		Help:       "#6c7086",
		Error:      "#f38ba8",
	},
	"catppuccin-latte": {
		Background: "#eff1f5",
		Header:     "#1e66f5",
		Normal:     "#4c4f69",
		Cursor:     "#ea76cb",
		CursorBG:   "#ccd0da",
		Dim:        "#8c8fa1",
		Playing:    "#40a02b",
		Help:       "#8c8fa1",
		Error:      "#d20f39",
	},
	"solarized-light": {
		Background: "#fdf6e3",
		Header:     "#268bd2",
		Normal:     "#586e75",
		Cursor:     "#d33682",
		CursorBG:   "#eee8d5",
		Dim:        "#93a1a1",
		Playing:    "#859900",
		Help:       "#93a1a1",
		Error:      "#dc322f",
	},
	"github-light": {
		Background: "#ffffff",
		Header:     "#0969da",
		Normal:     "#24292f",
		Cursor:     "#0550ae",
		CursorBG:   "#ddf4ff",
		Dim:        "#6e7781",
		Playing:    "#1a7f37",
		Help:       "#6e7781",
		Error:      "#cf222e",
	},
	"mono": {
		Header:  "15",
		Normal:  "250",
		Cursor:  "15",
		Dim:     "243",
		Playing: "15",
		Help:    "243",
		Error:   "15",
	},
}

// screenSeq is the escape sequence that sets the theme's background (and
// normal foreground); empty when the terminal background is used.
var screenSeq string

func sortedThemeNames() []string {
	names := make([]string, 0, len(themes))
	for name := range themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func themeNames() string {
	return strings.Join(sortedThemeNames(), ", ")
}

// printThemes lists the built-in themes with a color preview, marking current.
func printThemes(w io.Writer, current string) {
	if current == "" {
		current = "default"
	}
	fg := func(s string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(s))
	}
	for _, name := range sortedThemeNames() {
		c := themes[name]
		mark := "  "
		if name == current {
			mark = "* "
		}
		cursor := fg(c.Cursor)
		if c.CursorBG != "" {
			cursor = cursor.Background(lipgloss.Color(c.CursorBG))
		}
		sample := fmt.Sprintf(" %s %s %s %s %s ",
			fg(c.Header).Bold(true).Render("Header"),
			fg(c.Normal).Render("Normal"),
			cursor.Render("> Cursor"),
			fg(c.Playing).Bold(true).Render("Playing"),
			fg(c.Dim).Render("Dim"),
		)
		fmt.Fprintf(w, "%s%-17s %s\n", mark, name,
			fillBackground(sample, 0, colorSeq(c.Normal, c.Background)))
	}
}

// merge returns c with every empty field filled from base.
func (c Colors) merge(base Colors) Colors {
	pick := func(v, b string) string {
		if v != "" {
			return v
		}
		return b
	}
	return Colors{
		Background: pick(c.Background, base.Background),
		Header:     pick(c.Header, base.Header),
		Normal:     pick(c.Normal, base.Normal),
		Cursor:     pick(c.Cursor, base.Cursor),
		CursorBG:   pick(c.CursorBG, base.CursorBG),
		Dim:        pick(c.Dim, base.Dim),
		Playing:    pick(c.Playing, base.Playing),
		Help:       pick(c.Help, base.Help),
		Error:      pick(c.Error, base.Error),
	}
}

// colorSeq returns the escape sequence for the given foreground and
// background, or "" when there is no background.
func colorSeq(fg, bg string) string {
	if bg == "" || bg == "none" {
		return ""
	}
	st := lipgloss.NewStyle().Background(lipgloss.Color(bg))
	if fg != "" {
		st = st.Foreground(lipgloss.Color(fg))
	}
	out := st.Render("X")
	i := strings.Index(out, "X")
	if i <= 0 {
		return "" // terminal has no color support
	}
	return out[:i]
}

var cursorForwardRe = regexp.MustCompile(`\x1b\[(\d*)C`)

// lineWidth returns the number of columns l occupies, including the columns
// skipped over by cursor forward sequences (used to pass over cover art).
func lineWidth(l string) int {
	w := lipgloss.Width(l)
	for _, m := range cursorForwardRe.FindAllStringSubmatch(l, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			n = 1
		}
		w += n
	}
	return w
}

// fillBackground paints seq under every line of s, padding each line to
// width. Resets emitted by nested styles are followed by seq again so the
// background continues across them. The background is left on at the end
// of each line so that an erase to end of line fills with it too.
func fillBackground(s string, width int, seq string) string {
	if seq == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\x1b[0m", "\x1b[0m"+seq)
		if w := lineWidth(l); w < width {
			l += strings.Repeat(" ", width-w)
		}
		lines[i] = seq + l
	}
	lines[len(lines)-1] += "\x1b[0m"
	return strings.Join(lines, "\n")
}

// applyTheme sets the UI styles from the named theme with overrides.
func applyTheme(name string, overrides *Colors) error {
	if name == "" {
		name = "default"
	}
	base, ok := themes[name]
	if !ok {
		return fmt.Errorf("unknown theme %q (available: %s)", name, themeNames())
	}
	c := base
	if overrides != nil {
		c = overrides.merge(base)
	}

	fg := func(s string) lipgloss.Style {
		st := lipgloss.NewStyle()
		if s != "" {
			st = st.Foreground(lipgloss.Color(s))
		}
		return st
	}
	headerStyle = fg(c.Header).Bold(true)
	normalStyle = fg(c.Normal)
	cursorStyle = fg(c.Cursor)
	if c.CursorBG != "" {
		cursorStyle = cursorStyle.Background(lipgloss.Color(c.CursorBG))
	}
	dimStyle = fg(c.Dim)
	playingStyle = fg(c.Playing).Bold(true)
	helpStyle = fg(c.Help)
	errorStyle = fg(c.Error)
	screenSeq = colorSeq(c.Normal, c.Background)
	coverBG = color.Black
	if bg, ok := parseHexColor(c.Background); ok {
		coverBG = bg
	}
	return nil
}

// parseHexColor parses "#rrggbb".
func parseHexColor(s string) (color.Color, bool) {
	if len(s) != 7 || s[0] != '#' {
		return nil, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return nil, false
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}, true
}
