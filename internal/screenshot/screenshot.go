// Package screenshot converts an ANSI-colored terminal string to a
// self-contained HTML snippet suitable for pasting into emails or documents.
package screenshot

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

type state struct {
	fg        string // hex color or "" for default
	bg        string // hex color or "" for default
	bold      bool
	italic    bool
	underline bool
}

func (s state) isDefault() bool {
	return s.fg == "" && s.bg == "" && !s.bold && !s.italic && !s.underline
}

func (s state) style() string {
	var parts []string
	if s.fg != "" {
		parts = append(parts, "color:"+s.fg)
	}
	if s.bg != "" {
		parts = append(parts, "background-color:"+s.bg)
	}
	if s.bold {
		parts = append(parts, "font-weight:bold")
	}
	if s.italic {
		parts = append(parts, "font-style:italic")
	}
	if s.underline {
		parts = append(parts, "text-decoration:underline")
	}
	return strings.Join(parts, ";")
}

// ToDocument wraps ToHTML in a full HTML document suitable for saving to a
// .html file and opening in a browser.
func ToDocument(ansi string) string {
	body := ToHTML(ansi)
	return `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>sql screenshot</title>
<style>body{background:#121212;margin:24px;}</style>
</head>
<body>` + body + `</body>
</html>`
}

// ToHTML converts an ANSI-colored string to a self-contained HTML <pre> block.
// Colors use inline CSS so it renders correctly when pasted into emails or
// HTML documents without external stylesheets.
func ToHTML(ansi string) string {
	const (
		bgDefault = "#1e1e1e"
		fgDefault = "#d4d4d4"
		fontStack = "'Cascadia Code','Consolas','Courier New',monospace"
	)

	var sb strings.Builder
	fmt.Fprintf(&sb,
		`<pre style="background:%s;color:%s;font-family:%s;font-size:13px;line-height:1.4;padding:12px 16px;border-radius:6px;white-space:pre;overflow:auto;margin:0;">`,
		bgDefault, fgDefault, fontStack,
	)

	cur := state{}
	spanOpen := false

	closeSpan := func() {
		if spanOpen {
			sb.WriteString("</span>")
			spanOpen = false
		}
	}
	openSpan := func() {
		if cur.isDefault() || spanOpen {
			return
		}
		fmt.Fprintf(&sb, `<span style="%s">`, cur.style())
		spanOpen = true
	}

	i := 0
	for i < len(ansi) {
		// ESC character
		if ansi[i] != '\033' {
			// Regular byte — emit as HTML-escaped UTF-8.
			// Skip bare CR; let LF stand.
			if ansi[i] == '\r' {
				i++
				continue
			}
			if !spanOpen && !cur.isDefault() {
				openSpan()
			}
			// Emit one UTF-8 rune.
			r, size := nextRune(ansi, i)
			sb.WriteString(html.EscapeString(string(r)))
			i += size
			continue
		}

		// ESC — look for CSI ([) or skip other sequences.
		i++ // consume ESC
		if i >= len(ansi) {
			break
		}
		if ansi[i] != '[' {
			// Non-CSI escape sequence (e.g. ESC= or ESC>) — skip one byte.
			i++
			continue
		}
		i++ // consume '['

		// Scan to the final byte of the CSI sequence.
		start := i
		for i < len(ansi) && (ansi[i] < 0x40 || ansi[i] > 0x7E) {
			i++
		}
		if i >= len(ansi) {
			break
		}
		final := ansi[i]
		seq := ansi[start:i]
		i++ // consume final byte

		if final != 'm' {
			// Not an SGR sequence (cursor movement, erase, etc.) — ignore.
			continue
		}

		closeSpan()
		cur = applyCSI(cur, seq)
		openSpan()
	}

	closeSpan()
	sb.WriteString("</pre>")
	return sb.String()
}

// applyCSI applies an SGR parameter string (the part between ESC[ and m) to
// the current state and returns the updated state.
func applyCSI(cur state, seq string) state {
	if seq == "" || seq == "0" {
		return state{}
	}
	params := strings.Split(seq, ";")
	i := 0
	for i < len(params) {
		switch params[i] {
		case "0":
			cur = state{}
		case "1":
			cur.bold = true
		case "2":
			cur.bold = false // faint/normal
		case "3":
			cur.italic = true
		case "4":
			cur.underline = true
		case "22":
			cur.bold = false
		case "23":
			cur.italic = false
		case "24":
			cur.underline = false
		case "39":
			cur.fg = ""
		case "49":
			cur.bg = ""
		case "38":
			if i+1 < len(params) && params[i+1] == "2" && i+4 < len(params) {
				r, _ := strconv.Atoi(params[i+2])
				g, _ := strconv.Atoi(params[i+3])
				b, _ := strconv.Atoi(params[i+4])
				cur.fg = fmt.Sprintf("#%02x%02x%02x", r, g, b)
				i += 4
			} else if i+1 < len(params) && params[i+1] == "5" && i+2 < len(params) {
				n, _ := strconv.Atoi(params[i+2])
				cur.fg = color256(n)
				i += 2
			}
		case "48":
			if i+1 < len(params) && params[i+1] == "2" && i+4 < len(params) {
				r, _ := strconv.Atoi(params[i+2])
				g, _ := strconv.Atoi(params[i+3])
				b, _ := strconv.Atoi(params[i+4])
				cur.bg = fmt.Sprintf("#%02x%02x%02x", r, g, b)
				i += 4
			} else if i+1 < len(params) && params[i+1] == "5" && i+2 < len(params) {
				n, _ := strconv.Atoi(params[i+2])
				cur.bg = color256(n)
				i += 2
			}
		}
		i++
	}
	return cur
}

// nextRune decodes one UTF-8 rune from s starting at pos.
func nextRune(s string, pos int) (rune, int) {
	b := s[pos]
	if b < 0x80 {
		return rune(b), 1
	}
	// Multi-byte: determine length from leading byte.
	var size int
	switch {
	case b < 0xE0:
		size = 2
	case b < 0xF0:
		size = 3
	default:
		size = 4
	}
	if pos+size > len(s) {
		return rune(b), 1
	}
	var r rune
	switch size {
	case 2:
		r = rune(b&0x1F)<<6 | rune(s[pos+1]&0x3F)
	case 3:
		r = rune(b&0x0F)<<12 | rune(s[pos+1]&0x3F)<<6 | rune(s[pos+2]&0x3F)
	case 4:
		r = rune(b&0x07)<<18 | rune(s[pos+1]&0x3F)<<12 | rune(s[pos+2]&0x3F)<<6 | rune(s[pos+3]&0x3F)
	}
	return r, size
}

// color256 maps an xterm-256 color index to a hex string.
// lipgloss uses true-color, so this is only a fallback.
func color256(n int) string {
	if n < 0 || n > 255 {
		return ""
	}
	// Standard 16 colors
	std := []string{
		"#000000", "#800000", "#008000", "#808000",
		"#000080", "#800080", "#008080", "#c0c0c0",
		"#808080", "#ff0000", "#00ff00", "#ffff00",
		"#0000ff", "#ff00ff", "#00ffff", "#ffffff",
	}
	if n < 16 {
		return std[n]
	}
	// 216-color cube
	if n < 232 {
		n -= 16
		b := n % 6
		g := (n / 6) % 6
		r := n / 36
		val := func(v int) int {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return fmt.Sprintf("#%02x%02x%02x", val(r), val(g), val(b))
	}
	// Grayscale ramp
	v := 8 + (n-232)*10
	return fmt.Sprintf("#%02x%02x%02x", v, v, v)
}
