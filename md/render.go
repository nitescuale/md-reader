// Package md converts Markdown into RTF for display in the Windows RichEdit
// control (msftedit.dll), together with a plain-text mirror of the visible
// characters and a table of link spans (offsets into that mirror).
//
// The package is intentionally dependency free and platform independent so it
// can be unit tested on any OS.
package md

import (
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf16"
)

// ------------------------------------------------------------------ themes

// Theme is the colour palette used for a render.
type Theme struct {
	Name          string
	BG            string
	Text          string
	Muted         string
	Link          string
	CodeText      string
	CodeBG        string
	Border        string
	QuoteBar      string
	TableHeaderBG string
}

// LightTheme returns the light palette.
func LightTheme() Theme {
	return Theme{
		Name: "clair", BG: "#ffffff", Text: "#24292f", Muted: "#57606a", Link: "#0969da",
		CodeText: "#24292f", CodeBG: "#eff1f3", Border: "#d8dee4", QuoteBar: "#d0d7de",
		TableHeaderBG: "#f6f8fa",
	}
}

// DarkTheme returns the dark palette.
func DarkTheme() Theme {
	return Theme{
		Name: "sombre", BG: "#0d1117", Text: "#e6edf3", Muted: "#8b949e", Link: "#58a6ff",
		CodeText: "#e6edf3", CodeBG: "#1c2129", Border: "#30363d", QuoteBar: "#3d444d",
		TableHeaderBG: "#1c2129",
	}
}

// colour table slots (\cfN and \cbN both index the same table).
const (
	cfText        = 1
	cfMuted       = 2
	cfLink        = 3
	cfCodeText    = 4
	cbCode        = 5
	cfBorder      = 6
	cfQuoteBar    = 7
	cbTableHeader = 8
)

// font table slots.
const (
	fontUI     = 0
	fontMono   = 1
	fontSymbol = 2
)

// ------------------------------------------------------------ public types

// Options controls a render.
type Options struct {
	Theme Theme
	// Scale multiplies every font size (1.0 = 11pt body text).
	Scale float64
	// WidthTwips is the usable width of the RichEdit control, used to lay out
	// tables. 1440 twips = 1 inch.
	WidthTwips int
}

// LinkSpan is a clickable range of the rendered text.
type LinkSpan struct {
	Start int
	End   int
	// URL is the raw destination from the markdown source.
	URL string
	// Local is true when URL is a filesystem path (relative or absolute).
	Local bool
}

// Result is a rendered document.
type Result struct {
	RTF   string
	Text  string
	Links []LinkSpan
}

// ----------------------------------------------------------------- emitter

type emitter struct {
	sb    strings.Builder
	mir   []rune
	links []LinkSpan
	opt   Options
}

func (e *emitter) ctrl(s string) { e.sb.WriteString(s) }

// text writes visible text: RTF-escaped into the stream, raw into the mirror.
func (e *emitter) text(s string) {
	e.sb.WriteString(escapeRTF(s))
	e.mir = append(e.mir, []rune(s)...)
}

func (e *emitter) par() {
	e.ctrl(`\par` + "\n")
	e.mir = append(e.mir, '\r')
}

func (e *emitter) tab() {
	e.ctrl(`\tab `)
	e.mir = append(e.mir, '\t')
}

func (e *emitter) pos() int { return len(e.mir) }

func (e *emitter) sz(base int) int {
	return int(math.Round(float64(base) * e.opt.Scale))
}

// ------------------------------------------------------- rtf text escaping

func escapeRTF(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '{' || r == '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\tab `)
		case r == '\n':
			b.WriteString(`\par `)
		case r >= 32 && r < 127:
			b.WriteRune(r)
		default:
			for _, u := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&b, `\u%d?`, int32(int16(u)))
			}
		}
	}
	return b.String()
}

func hexToRTF(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return `\red0\green0\blue0;`
	}
	var r, g, bl int
	fmt.Sscanf(hex[0:2], "%02x", &r)
	fmt.Sscanf(hex[2:4], "%02x", &g)
	fmt.Sscanf(hex[4:6], "%02x", &bl)
	return fmt.Sprintf(`\red%d\green%d\blue%d;`, r, g, bl)
}

// ---------------------------------------------------------------- rendering

// Render converts markdown source into a Result.
func Render(src string, opt Options) Result {
	if opt.Scale <= 0 {
		opt.Scale = 1
	}
	if opt.WidthTwips <= 0 {
		opt.WidthTwips = 9360
	}
	if opt.Theme.Name == "" {
		opt.Theme = LightTheme()
	}
	e := &emitter{opt: opt}
	e.header()
	lines := splitLines(normalize(src))
	lines = stripFrontMatter(lines)
	e.blocks(lines, blockCtx{})
	e.ctrl("}")
	return Result{RTF: e.sb.String(), Text: string(e.mir), Links: e.links}
}

func (e *emitter) header() {
	t := e.opt.Theme
	e.ctrl(`{\rtf1\ansi\ansicpg1252\deff0\uc1\viewkind4` + "\n")
	e.ctrl(`{\fonttbl` +
		`{\f0\fnil\fcharset0 Segoe UI;}` +
		`{\f1\fmodern\fcharset0 Consolas;}` +
		`{\f2\fnil\fcharset0 Segoe UI Symbol;}` +
		`}` + "\n")
	e.ctrl(`{\colortbl ;` +
		hexToRTF(t.Text) + hexToRTF(t.Muted) + hexToRTF(t.Link) + hexToRTF(t.CodeText) +
		hexToRTF(t.CodeBG) + hexToRTF(t.Border) + hexToRTF(t.QuoteBar) + hexToRTF(t.TableHeaderBG) +
		`}` + "\n")
	e.ctrl(`\deftab720 `)
}

type blockCtx struct {
	li    int // accumulated left indent, twips
	ri    int
	quote bool
	pre   *string // marker emitted before the first paragraph of a list item
}

type pstyle struct {
	font   int
	size   int
	bold   bool
	italic bool
	strike bool
	color  int
	li, ri int
	fi     int
	sb, sa int
	sl     int
	bar    bool // left border bar
	bcolor int
}

func (e *emitter) openPar(p pstyle, ctx blockCtx) {
	var b strings.Builder
	b.WriteString(`\pard`)
	sl := p.sl
	if sl == 0 {
		sl = 276
	}
	fmt.Fprintf(&b, `\sl%d\slmult1`, sl)
	if li := p.li + ctx.li; li > 0 {
		fmt.Fprintf(&b, `\li%d`, li)
	}
	if ri := p.ri + ctx.ri; ri > 0 {
		fmt.Fprintf(&b, `\ri%d`, ri)
	}
	if p.fi != 0 {
		fmt.Fprintf(&b, `\fi%d`, p.fi)
	}
	if p.sb > 0 {
		fmt.Fprintf(&b, `\sb%d`, p.sb)
	}
	if p.sa > 0 {
		fmt.Fprintf(&b, `\sa%d`, p.sa)
	}
	if p.bar {
		c := p.bcolor
		if c == 0 {
			c = cfBorder
		}
		fmt.Fprintf(&b, `\brdrl\brdrs\brdrw15\brsp10\brdrcf%d`, c)
	} else {
		b.WriteString(`\brdrl\brdrnone`)
	}
	if p.bold {
		b.WriteString(`\b`)
	} else {
		b.WriteString(`\b0`)
	}
	if p.italic {
		b.WriteString(`\i`)
	} else {
		b.WriteString(`\i0`)
	}
	if p.strike {
		b.WriteString(`\strike`)
	} else {
		b.WriteString(`\strike0`)
	}
	fmt.Fprintf(&b, `\f%d\fs%d\cf%d`, p.font, p.size, p.color)
	b.WriteString(" ")
	e.ctrl(b.String())
	if ctx.pre != nil && *ctx.pre != "" {
		e.text(*ctx.pre)
		*ctx.pre = ""
	}
}

// ------------------------------------------------------------ block parsing

func normalize(src string) string {
	src = strings.TrimPrefix(src, "\ufeff")
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	src = strings.ReplaceAll(src, "\t", "    ")
	return src
}

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

func stripFrontMatter(lines []string) []string {
	if len(lines) < 3 {
		return lines
	}
	first := strings.TrimSpace(lines[0])
	if first != "---" && first != "+++" {
		return lines
	}
	for i := 1; i < len(lines) && i < 60; i++ {
		if strings.TrimSpace(lines[i]) == first {
			return lines[i+1:]
		}
	}
	return lines
}

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

func isHR(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 3 {
		return false
	}
	c := t[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	n := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case c:
			n++
		case ' ':
		default:
			return false
		}
	}
	return n >= 3
}

func atxHeading(line string) (int, string, bool) {
	t := strings.TrimLeft(line, " ")
	if leadingSpaces(line) > 3 {
		return 0, "", false
	}
	lvl := 0
	for lvl < len(t) && t[lvl] == '#' {
		lvl++
	}
	if lvl == 0 || lvl > 6 {
		return 0, "", false
	}
	rest := t[lvl:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimRight(rest, "#")
	rest = strings.TrimSpace(rest)
	return lvl, rest, true
}

func isSetext(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 2 {
		return false
	}
	for _, c := range t {
		if c != '=' && c != '-' {
			return false
		}
	}
	return true
}

func setextLevel(line string) int {
	if strings.Contains(strings.TrimSpace(line), "=") {
		return 1
	}
	return 2
}

// fenceInfo reports whether the line opens a fenced code block and returns the
// fence marker (e.g. "```") and the info string.
func fenceInfo(line string) (string, string, bool) {
	ind := leadingSpaces(line)
	if ind > 3 {
		return "", "", false
	}
	t := line[ind:]
	var c byte
	if strings.HasPrefix(t, "```") {
		c = '`'
	} else if strings.HasPrefix(t, "~~~") {
		c = '~'
	} else {
		return "", "", false
	}
	n := 0
	for n < len(t) && t[n] == c {
		n++
	}
	return t[:n], strings.TrimSpace(t[n:]), true
}

func isQuote(line string) bool {
	t := strings.TrimLeft(line, " ")
	return leadingSpaces(line) <= 3 && strings.HasPrefix(t, ">")
}

func isListMarker(line string) bool {
	_, _, _, _, ok := parseMarker(line)
	return ok
}

// parseMarker parses "  - item" / "1) item" returning indent, kind ('b' or
// 'o'), the number, and the item content.
func parseMarker(line string) (indent int, kind byte, num int, content string, ok bool) {
	indent = leadingSpaces(line)
	if indent > 8 {
		return 0, 0, 0, "", false
	}
	t := line[indent:]
	if t == "" {
		return 0, 0, 0, "", false
	}
	if c := t[0]; c == '-' || c == '*' || c == '+' {
		if len(t) < 2 || (t[1] != ' ' && t[1] != '\t') {
			return 0, 0, 0, "", false
		}
		return indent, 'b', 0, strings.TrimLeft(t[1:], " "), true
	}
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i == 0 || i > 9 || i >= len(t) {
		return 0, 0, 0, "", false
	}
	if t[i] != '.' && t[i] != ')' {
		return 0, 0, 0, "", false
	}
	if i+1 >= len(t) {
		return 0, 0, 0, "", false
	}
	if t[i+1] != ' ' && t[i+1] != '\t' {
		return 0, 0, 0, "", false
	}
	n := 0
	for j := 0; j < i; j++ {
		n = n*10 + int(t[j]-'0')
	}
	return indent, 'o', n, strings.TrimLeft(t[i+1:], " "), true
}

func isBlockStart(line string) bool {
	if strings.TrimSpace(line) == "" {
		return true
	}
	if _, _, ok := fenceInfo(line); ok {
		return true
	}
	if _, _, ok := atxHeading(line); ok {
		return true
	}
	if isHR(line) || isQuote(line) || isListMarker(line) {
		return true
	}
	return false
}

func (e *emitter) blocks(lines []string, ctx blockCtx) {
	i := 0
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if marker, lang, ok := fenceInfo(line); ok {
			i = e.fenced(lines, i, marker, lang, ctx)
			continue
		}
		if isHR(line) {
			e.hr(ctx)
			i++
			continue
		}
		if lvl, txt, ok := atxHeading(line); ok {
			e.heading(lvl, txt, ctx)
			i++
			continue
		}
		if i+1 < len(lines) && isTableStart(line, lines[i+1]) {
			i = e.table(lines, i, ctx)
			continue
		}
		if isListMarker(line) {
			i = e.list(lines, i, ctx)
			continue
		}
		if isQuote(line) {
			i = e.quote(lines, i, ctx)
			continue
		}
		if strings.HasPrefix(line, "    ") {
			i = e.indentedCode(lines, i, ctx)
			continue
		}
		if i+1 < len(lines) && isSetext(lines[i+1]) && strings.TrimSpace(line) != "" {
			e.heading(setextLevel(lines[i+1]), line, ctx)
			i += 2
			continue
		}
		// paragraph (possibly with hard line breaks)
		j := i
		var segment []string
		for j < len(lines) {
			l := lines[j]
			if strings.TrimSpace(l) == "" {
				break
			}
			if j > i && isBlockStart(l) {
				break
			}
			segment = append(segment, l)
			hard := strings.HasSuffix(l, "  ") || strings.HasSuffix(l, "\\")
			j++
			if hard {
				e.paragraph(strings.TrimSpace(strings.Join(segment, " ")), ctx)
				segment = segment[:0]
			}
		}
		if len(segment) > 0 {
			e.paragraph(strings.TrimSpace(strings.Join(segment, " ")), ctx)
		}
		i = j
	}
}

func (e *emitter) heading(level int, text string, ctx blockCtx) {
	type hs struct {
		size, sb, sa int
		muted        bool
	}
	table := map[int]hs{
		1: {34, 260, 120, false},
		2: {28, 220, 100, false},
		3: {24, 190, 80, false},
		4: {22, 170, 60, false},
		5: {21, 150, 60, true},
		6: {20, 150, 60, true},
	}
	h, ok := table[level]
	if !ok {
		h = table[6]
	}
	color := cfText
	if h.muted {
		color = cfMuted
	}
	p := pstyle{font: fontUI, size: e.sz(h.size), bold: true, color: color, sb: h.sb, sa: h.sa}
	if ctx.quote {
		p.bar = true
		p.bcolor = cfQuoteBar
		p.li = 0
	}
	e.openPar(p, ctx)
	e.inline(text, istyle{bold: true})
	e.par()
}

func (e *emitter) paragraph(text string, ctx blockCtx) {
	p := pstyle{font: fontUI, size: e.sz(22), color: cfText, sa: 140}
	if ctx.quote {
		p.bar = true
		p.bcolor = cfQuoteBar
		p.color = cfMuted
	}
	e.openPar(p, ctx)
	e.inline(text, istyle{})
	e.par()
}

func (e *emitter) codeBlock(lines []string, ctx blockCtx) {
	for _, l := range lines {
		p := pstyle{font: fontMono, size: e.sz(19), color: cfCodeText, sl: 240, li: 240, ri: 240}
		if ctx.quote {
			p.bar = true
			p.bcolor = cfQuoteBar
		}
		e.openPar(p, ctx)
		e.ctrl(`{\cb` + itoa(cbCode) + `\chshdng0\chcbpat` + itoa(cbCode) + `\f` + itoa(fontMono) + `\fs` + itoa(e.sz(19)) + ` `)
		if l != "" {
			e.text(l)
		} else {
			e.ctrl(`\ `)
		}
		e.ctrl(`}`)
		e.par()
	}
	if len(lines) == 0 {
		p := pstyle{font: fontMono, size: e.sz(19), color: cfCodeText, sl: 240, li: 240, ri: 240}
		e.openPar(p, ctx)
		e.par()
	}
}

func (e *emitter) fenced(lines []string, start int, marker, lang string, ctx blockCtx) int {
	i := start + 1
	var body []string
	for i < len(lines) {
		t := strings.TrimLeft(lines[i], " ")
		if strings.HasPrefix(t, marker) {
			i++
			break
		}
		body = append(body, lines[i])
		i++
	}
	e.codeBlock(body, ctx)
	return i
}

func (e *emitter) indentedCode(lines []string, start int, ctx blockCtx) int {
	i := start
	var body []string
	for i < len(lines) {
		l := lines[i]
		if strings.HasPrefix(l, "    ") {
			body = append(body, l[4:])
			i++
			continue
		}
		if strings.TrimSpace(l) == "" {
			// keep a blank line only if the code continues
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    ") {
				body = append(body, "")
				i++
				continue
			}
		}
		break
	}
	e.codeBlock(body, ctx)
	return i
}

func (e *emitter) hr(ctx blockCtx) {
	p := pstyle{font: fontUI, size: e.sz(10), color: cfBorder, sb: 120, sa: 120, sl: 240}
	p.bar = false
	e.openPar(p, ctx)
	e.ctrl(`\brdrb\brdrs\brdrw10\brdrcf` + itoa(cfBorder) + ` `)
	e.par()
}

func (e *emitter) quote(lines []string, start int, ctx blockCtx) int {
	var inner []string
	i := start
	for i < len(lines) {
		l := lines[i]
		t := strings.TrimLeft(l, " ")
		if leadingSpaces(l) <= 3 && strings.HasPrefix(t, ">") {
			t = t[1:]
			t = strings.TrimPrefix(t, " ")
			inner = append(inner, t)
			i++
			continue
		}
		if strings.TrimSpace(l) != "" && !isBlockStart(l) {
			inner = append(inner, t)
			i++
			continue
		}
		break
	}
	e.blocks(inner, blockCtx{li: ctx.li + 300, ri: ctx.ri + 200, quote: true, pre: ctx.pre})
	return i
}

func (e *emitter) list(lines []string, start int, ctx blockCtx) int {
	base, kind, _, _, _ := parseMarker(lines[start])
	i := start
	counter := 0
	for i < len(lines) {
		indent, k, num, content, ok := parseMarker(lines[i])
		if !ok || indent != base || k != kind {
			break
		}
		counter++
		body := []string{content}
		i++
		for i < len(lines) {
			l := lines[i]
			if strings.TrimSpace(l) == "" {
				if i+1 < len(lines) && leadingSpaces(lines[i+1]) >= base+2 && strings.TrimSpace(lines[i+1]) != "" {
					body = append(body, "")
					i++
					continue
				}
				break
			}
			if leadingSpaces(l) < base {
				break
			}
			if ind2, _, _, _, ok2 := parseMarker(l); ok2 && ind2 == base {
				break
			}
			body = append(body, unindent(l, base+2))
			i++
		}
		marker := "•"
		if kind == 'o' {
			n := counter
			if num > 0 && num != counter {
				n = num
			}
			marker = itoa(n) + "."
		}
		// task list
		if len(body) > 0 {
			first := body[0]
			if t := strings.TrimSpace(first); len(t) >= 3 && t[0] == '[' && t[2] == ']' && (t[1] == ' ' || t[1] == 'x' || t[1] == 'X') {
				box := "☐"
				if t[1] == 'x' || t[1] == 'X' {
					box = "☑"
				}
				body[0] = strings.TrimSpace(t[3:])
				marker = box
			}
		}
		prefix := marker + "\t"
		sub := blockCtx{li: 720, ri: 0, quote: ctx.quote, pre: &prefix}
		e.blocks(body, sub)
	}
	return i
}

func unindent(s string, n int) string {
	i := 0
	for i < n && i < len(s) && s[i] == ' ' {
		i++
	}
	return s[i:]
}

// ------------------------------------------------------------------ tables

func isTableStart(header, delim string) bool {
	if !strings.Contains(header, "|") {
		return false
	}
	cells := splitRow(delim)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		c = strings.Trim(c, ":")
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		for i := 0; i < len(c); i++ {
			if c[i] != '-' {
				return false
			}
		}
	}
	return true
}

func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	var cells []string
	var cur strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\':
			cur.WriteRune(r)
			esc = true
		case r == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

func alignRow(delim string) []byte {
	cells := splitRow(delim)
	out := make([]byte, len(cells))
	for i, c := range cells {
		c = strings.TrimSpace(c)
		left := strings.HasPrefix(c, ":")
		right := strings.HasSuffix(c, ":")
		switch {
		case left && right:
			out[i] = 'c'
		case right:
			out[i] = 'r'
		default:
			out[i] = 'l'
		}
	}
	return out
}

func (e *emitter) table(lines []string, start int, ctx blockCtx) int {
	header := splitRow(lines[start])
	aligns := alignRow(lines[start+1])
	i := start + 2
	var rows [][]string
	for i < len(lines) {
		l := lines[i]
		if strings.TrimSpace(l) == "" || !strings.Contains(l, "|") {
			break
		}
		rows = append(rows, splitRow(l))
		i++
	}
	n := len(header)
	for _, r := range rows {
		if len(r) > n {
			n = len(r)
		}
	}
	if n == 0 {
		return i
	}
	// column weights from the longest cell text
	weights := make([]int, n)
	for c := 0; c < n; c++ {
		setWeight := func(cells []string, cols []string) {
			if c < len(cols) {
				if l := len([]rune(cols[c])); l > weights[c] {
					weights[c] = l
				}
			}
		}
		setWeight(header, header)
		for _, r := range rows {
			setWeight(nil, r)
		}
		if weights[c] < 6 {
			weights[c] = 6
		}
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	width := e.opt.WidthTwips
	if width < 2000 {
		width = 2000
	}
	if li := ctx.li; li > 0 {
		width -= li
	}
	if width < 1500 {
		width = 1500
	}
	xs := make([]int, n)
	acc := 0
	for c := 0; c < n; c++ {
		w := width * weights[c] / total
		if w < 700 {
			w = 700
		}
		acc += w
		xs[c] = acc
	}

	cellBorder := `\clbrdrt\brdrs\brdrw5\brdrcf` + itoa(cfBorder) +
		`\clbrdrl\brdrs\brdrw5\brdrcf` + itoa(cfBorder) +
		`\clbrdrb\brdrs\brdrw5\brdrcf` + itoa(cfBorder) +
		`\clbrdrr\brdrs\brdrw5\brdrcf` + itoa(cfBorder)

	emitRow := func(cells []string, isHeader bool) {
		e.ctrl(`\trowd\trgaph60\trleft` + itoa(ctx.li) + `\trbrdrt\brdrs\brdrw5\brdrcf` + itoa(cfBorder) +
			`\trbrdrl\brdrs\brdrw5\brdrcf` + itoa(cfBorder) +
			`\trbrdrb\brdrs\brdrw5\brdrcf` + itoa(cfBorder) +
			`\trbrdrr\brdrs\brdrw5\brdrcf` + itoa(cfBorder) + "\n")
		for c := 0; c < n; c++ {
			e.ctrl(`\clvertalt` + cellBorder)
			if isHeader {
				e.ctrl(`\clcbpat` + itoa(cbTableHeader))
			}
			e.ctrl(`\cellx` + itoa(xs[c]))
		}
		e.ctrl("\n")
		for c := 0; c < n; c++ {
			var txt string
			if c < len(cells) {
				txt = cells[c]
			}
			al := byte('l')
			if c < len(aligns) {
				al = aligns[c]
			}
			align := `\ql`
			switch al {
			case 'c':
				align = `\qc`
			case 'r':
				align = `\qr`
			}
			p := pstyle{font: fontUI, size: e.sz(20), color: cfText, sa: 60, sb: 60, bold: isHeader}
			e.openPar(p, ctx)
			e.ctrl(`\intbl` + align + ` `)
			if txt != "" {
				e.inline(txt, istyle{bold: isHeader})
			}
			e.ctrl(`\cell`)
		}
		e.ctrl(`\row` + "\n")
	}
	emitRow(header, true)
	for _, r := range rows {
		emitRow(r, false)
	}
	e.ctrl(`\pard\sa140 `)
	e.par()
	return i
}

// ------------------------------------------------------------------ inline

type istyle struct {
	bold   bool
	italic bool
	strike bool
}

func (e *emitter) inline(s string, st istyle) {
	runes := []rune(s)
	var lit []rune
	flush := func() {
		if len(lit) > 0 {
			e.text(string(lit))
			lit = lit[:0]
		}
	}
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == '\\' && i+1 < len(runes) && isPunct(runes[i+1]):
			lit = append(lit, runes[i+1])
			i += 2
		case r == '`':
			n := countRun(runes, i, '`')
			if j, ok := findClosing(runes, i+n, '`', n); ok {
				flush()
				content := strings.Trim(string(runes[i+n:j]), " ")
				e.styled(content, st, true)
				i = j + n
				continue
			}
			lit = append(lit, runes[i])
			i++
		case r == '!' && i+1 < len(runes) && runes[i+1] == '[':
			if txt, url, end, ok := parseLink(runes, i+1); ok {
				flush()
				e.image(txt, url, st)
				i = end
				continue
			}
			lit = append(lit, r)
			i++
		case r == '[':
			if txt, url, end, ok := parseLink(runes, i); ok {
				flush()
				e.link(txt, url, st)
				i = end
				continue
			}
			lit = append(lit, r)
			i++
		case r == '*', r == '_':
			n := countRun(runes, i, r)
			if j, end, ok := findEmph(runes, i, r, n); ok {
				content := string(runes[i+n : j])
				flush()
				ns := st
				switch {
				case n >= 3:
					ns.bold, ns.italic = true, true
				case n == 2:
					ns.bold = true
				default:
					ns.italic = true
				}
				e.styled(content, ns, false)
				i = end
				continue
			}
			lit = append(lit, runes[i:i+1]...)
			i++
		case r == '~':
			n := countRun(runes, i, '~')
			if n >= 2 {
				if j, ok := findClosing(runes, i+2, '~', 2); ok {
					flush()
					ns := st
					ns.strike = true
					e.styled(string(runes[i+2:j]), ns, false)
					i = j + 2
					continue
				}
			}
			lit = append(lit, runes[i:i+min(n, 1)]...)
			i += min(n, 1)
		case r == '<':
			if url, end, ok := parseAutolink(runes, i); ok {
				flush()
				e.link(url, url, st)
				i = end
				continue
			}
			if end, ok := skipHTMLTag(runes, i); ok {
				i = end
				continue
			}
			lit = append(lit, r)
			i++
		case r == 'h' && hasPrefixFold(runes[i:], "http://") || r == 'h' && hasPrefixFold(runes[i:], "https://"):
			j := i
			for j < len(runes) && !unicode.IsSpace(runes[j]) && !strings.ContainsRune("<>\"'", runes[j]) {
				j++
			}
			url := string(runes[i:j])
			trimmed := strings.TrimRight(url, ".,;:!?)]}\u201d")
			if len(trimmed) > 7 {
				flush()
				e.link(trimmed, trimmed, st)
				i += len([]rune(trimmed))
				continue
			}
			lit = append(lit, r)
			i++
		default:
			lit = append(lit, r)
			i++
		}
	}
	flush()
}

func (e *emitter) styled(s string, st istyle, code bool) {
	var b strings.Builder
	b.WriteString(`{`)
	if st.strike {
		b.WriteString(`\strike`)
	}
	if code {
		fmt.Fprintf(&b, `\f%d\fs%d\cb%d\chshdng0\chcbpat%d`, fontMono, e.sz(19), cbCode, cbCode)
	} else {
		fmt.Fprintf(&b, `\f%d\fs%d`, fontUI, e.sz(22))
	}
	if st.bold {
		b.WriteString(`\b`)
	}
	if st.italic {
		b.WriteString(`\i`)
	}
	if code {
		fmt.Fprintf(&b, `\cf%d`, cfCodeText)
	}
	b.WriteString(" ")
	e.ctrl(b.String())
	if code {
		e.text(s)
	} else {
		e.inline(s, st)
	}
	e.ctrl(`}`)
}

func (e *emitter) link(text, url string, st istyle) {
	start := e.pos()
	var b strings.Builder
	b.WriteString(`{`)
	if st.strike {
		b.WriteString(`\strike`)
	}
	fmt.Fprintf(&b, `\cf%d\ul\ulc%d`, cfLink, cfLink)
	if st.bold {
		b.WriteString(`\b`)
	}
	if st.italic {
		b.WriteString(`\i`)
	}
	b.WriteString(" ")
	e.ctrl(b.String())
	if text == "" || text == url {
		// bare URL / autolink: the visible text is the destination itself, so it
		// must NOT be re-parsed (that would recurse forever).
		e.text(text)
		if text == "" {
			e.text(url)
		}
	} else {
		e.inline(text, st)
	}
	e.ctrl(`}`)
	e.links = append(e.links, LinkSpan{Start: start, End: e.pos(), URL: url, Local: isLocalTarget(url)})
}

func (e *emitter) image(alt, url string, st istyle) {
	label := "🖼 "
	if alt != "" {
		label += alt
	} else {
		label += "image"
	}
	start := e.pos()
	e.text(label)
	end := e.pos()
	e.ctrl(`{\cf` + itoa(cfMuted) + `\ul\ulc` + itoa(cfLink) + ` `)
	e.text(" (" + url + ")")
	e.ctrl(`}`)
	e.links = append(e.links, LinkSpan{Start: start, End: e.pos(), URL: url, Local: isLocalTarget(url)})
	_ = end
	_ = st
}

func isLocalTarget(url string) bool {
	l := strings.ToLower(url)
	if strings.HasPrefix(l, "#") {
		return false
	}
	for _, p := range []string{"http://", "https://", "mailto:", "ftp://", "tel:", "data:"} {
		if strings.HasPrefix(l, p) {
			return false
		}
	}
	return true
}

// parseLink parses "[text](url)" starting at the '[' index.
func parseLink(r []rune, start int) (text, url string, end int, ok bool) {
	if start >= len(r) || r[start] != '[' {
		return "", "", 0, false
	}
	depth := 0
	i := start
	var txt []rune
	for i < len(r) {
		switch r[i] {
		case '[':
			depth++
			if depth > 1 {
				txt = append(txt, r[i])
			}
		case ']':
			depth--
			if depth == 0 {
				goto found
			}
			txt = append(txt, r[i])
		case '\\':
			if i+1 < len(r) {
				txt = append(txt, r[i+1])
				i++
			} else {
				txt = append(txt, r[i])
			}
		default:
			txt = append(txt, r[i])
		}
		i++
	}
	return "", "", 0, false
found:
	i++ // past ']'
	if i >= len(r) || r[i] != '(' {
		return "", "", 0, false
	}
	i++
	var dest []rune
	depth = 0
	for i < len(r) {
		switch r[i] {
		case '(':
			depth++
			dest = append(dest, r[i])
		case ')':
			if depth == 0 {
				goto done
			}
			depth--
			dest = append(dest, r[i])
		default:
			dest = append(dest, r[i])
		}
		i++
	}
	return "", "", 0, false
done:
	i++ // past ')'
	d := strings.TrimSpace(string(dest))
	if sp := strings.IndexAny(d, " \t"); sp >= 0 {
		d = d[:sp] // drop the optional title
	}
	d = strings.Trim(d, "<>")
	if d == "" {
		return "", "", 0, false
	}
	return string(txt), d, i, true
}

func parseAutolink(r []rune, start int) (string, int, bool) {
	if r[start] != '<' {
		return "", 0, false
	}
	i := start + 1
	var buf []rune
	for i < len(r) && r[i] != '>' && r[i] != ' ' && r[i] != '<' {
		buf = append(buf, r[i])
		i++
	}
	if i >= len(r) || r[i] != '>' {
		return "", 0, false
	}
	u := string(buf)
	low := strings.ToLower(u)
	if strings.Contains(low, "://") || strings.HasPrefix(low, "mailto:") {
		return u, i + 1, true
	}
	if at := strings.Index(u, "@"); at > 0 && !strings.ContainsAny(u, "/ ") {
		return "mailto:" + u, i + 1, true
	}
	return "", 0, false
}

func skipHTMLTag(r []rune, start int) (int, bool) {
	i := start + 1
	if i >= len(r) {
		return 0, false
	}
	if r[i] == '/' {
		i++
	}
	if i >= len(r) || !isLetter(r[i]) {
		return 0, false
	}
	for i < len(r) && r[i] != '>' {
		if r[i] == '\n' {
			return 0, false
		}
		i++
	}
	if i >= len(r) {
		return 0, false
	}
	return i + 1, true
}

func countRun(r []rune, i int, c rune) int {
	n := 0
	for i+n < len(r) && r[i+n] == c {
		n++
	}
	return n
}

func findClosing(r []rune, from int, c rune, n int) (int, bool) {
	i := from
	for i < len(r) {
		if r[i] == '\\' {
			i += 2
			continue
		}
		if r[i] == c {
			k := countRun(r, i, c)
			if k == n {
				return i, true
			}
			i += k
			continue
		}
		i++
	}
	return 0, false
}

// findEmph locates the closing run for an emphasis opener at index i (with n
// marker runes) and returns the content end index and the index just past the
// closing marker.
func findEmph(r []rune, i int, c rune, n int) (int, int, bool) {
	if i+n >= len(r) || unicode.IsSpace(r[i+n]) {
		return 0, 0, false
	}
	j := i + n
	for j < len(r) {
		if r[j] == '\\' {
			j += 2
			continue
		}
		if r[j] == c {
			k := countRun(r, j, c)
			if k >= n {
				content := r[i+n : j]
				if len(content) == 0 {
					return 0, 0, false
				}
				if c == '_' {
					// intraword underscores are not emphasis
					if j > 0 && isWordRune(r[j-1]) {
						j += k
						continue
					}
					if i+n < len(r) && isWordRune(r[i+n-1]) && i > 0 && isWordRune(r[i-1]) {
						return 0, 0, false
					}
				}
				if !unicode.IsSpace(content[len(content)-1]) {
					return j, j + n, true
				}
			}
			j += k
			continue
		}
		j++
	}
	return 0, 0, false
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isLetter(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func isPunct(r rune) bool {
	return strings.ContainsRune("\\`*_{}[]()#+-.!<>|~>\"'$%&,;:=?@^/", r)
}

func hasPrefixFold(r []rune, p string) bool {
	if len(r) < len(p) {
		return false
	}
	return strings.EqualFold(string(r[:len(p)]), p)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
