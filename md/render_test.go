package md

import (
	"strings"
	"testing"
)

func render(t *testing.T, src string) Result {
	t.Helper()
	return Render(src, Options{Theme: LightTheme()})
}

// The RTF must be structurally valid: every structural brace balanced.
// Escaped braces (\{ \}) are literal text and must be ignored.
func TestBalancedBraces(t *testing.T) {
	src := "# T\n\ntext with {braces} and \\{escaped\\}\n\n```go\nif a { b }\n```\n\n| a | b |\n|---|---|\n| {x} | y |\n\n> quote *here*\n\n- one\n- two\n  - nested\n"
	res := render(t, src)
	depth := 0
	for i := 0; i < len(res.RTF); i++ {
		switch res.RTF[i] {
		case '\\':
			i++ // skip escaped char
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				t.Fatalf("unbalanced } at %d", i)
			}
		}
	}
	if depth != 0 {
		t.Fatalf("unbalanced braces, depth=%d", depth)
	}
}

func TestHeadersAndSizes(t *testing.T) {
	res := render(t, "# H1\n\n## H2\n\npara\n")
	if !strings.Contains(res.RTF, `\fs38`) {
		t.Errorf("h1 size missing: %s", head(res.RTF))
	}
	if !strings.Contains(res.RTF, `\fs30`) {
		t.Errorf("h2 size missing")
	}
	if !strings.Contains(res.RTF, `\fs22`) {
		t.Errorf("body size missing")
	}
	if !strings.HasPrefix(res.RTF, `{\rtf1`) {
		t.Errorf("bad header: %q", head(res.RTF))
	}
	if !strings.HasSuffix(res.RTF, "}") {
		t.Errorf("rtf not closed")
	}
}

// The mirror text must be exactly what RichEdit will hold: one \r per
// paragraph, tabs preserved, no markup.
func TestMirrorText(t *testing.T) {
	res := render(t, "# Titre\n\nhello *world* and `code`\n\n- a\n- b\n")
	want := "Titre\rhello world and code\r•\ta\r•\tb\r"
	if res.Text != want {
		t.Errorf("mirror mismatch\n got %q\nwant %q", res.Text, want)
	}
}

// Link spans must point at the displayed link text inside the mirror.
func TestLinkSpans(t *testing.T) {
	src := "Voir [la doc](https://example.com/a) et aussi [notes](sous/dossier/notes.md) plus <https://auto.example> et https://bare.example/x.\n"
	res := render(t, src)
	r := []rune(res.Text)
	if len(res.Links) != 4 {
		t.Fatalf("expected 4 links, got %d (%+v) text=%q", len(res.Links), res.Links, res.Text)
	}
	wantText := []string{"la doc", "notes", "https://auto.example", "https://bare.example/x"}
	wantURL := []string{"https://example.com/a", "sous/dossier/notes.md", "https://auto.example", "https://bare.example/x"}
	for i, l := range res.Links {
		if l.Start < 0 || l.End > len(r) || l.Start >= l.End {
			t.Fatalf("link %d bad range %d..%d (len %d)", i, l.Start, l.End, len(r))
		}
		if got := string(r[l.Start:l.End]); got != wantText[i] {
			t.Errorf("link %d text = %q, want %q", i, got, wantText[i])
		}
		if l.URL != wantURL[i] {
			t.Errorf("link %d url = %q, want %q", i, l.URL, wantURL[i])
		}
	}
	if res.Links[0].Local {
		t.Errorf("http link flagged local")
	}
	if !res.Links[1].Local {
		t.Errorf("relative path not flagged local")
	}
}

func TestEscaping(t *testing.T) {
	res := render(t, "a {b} c \\*d\\* é 😀\n")
	if !strings.Contains(res.RTF, `\{b\}`) {
		t.Errorf("braces not escaped: %s", res.RTF)
	}
	if !strings.Contains(res.RTF, `\u233?`) {
		t.Errorf("accent not escaped")
	}
	// U+1F600 is encoded as the UTF-16 surrogate pair D83D DE00, written as
	// signed 16-bit values per the RTF spec.
	if !strings.Contains(res.RTF, `\u-10179?`) || !strings.Contains(res.RTF, `\u-8704?`) {
		t.Errorf("emoji surrogate pair missing: %s", res.RTF)
	}
	if !strings.Contains(res.Text, "é 😀") {
		t.Errorf("mirror lost unicode: %q", res.Text)
	}
}

func TestCodeBlock(t *testing.T) {
	res := render(t, "```python\nprint(\"hi\")\n```\n")
	if !strings.Contains(res.RTF, `\f1`) {
		t.Errorf("monospace font not used")
	}
	if !strings.Contains(res.RTF, `print("hi")`) {
		t.Errorf("code text missing: %s", res.RTF)
	}
	if !strings.Contains(res.Text, `print("hi")`) {
		t.Errorf("mirror missing code: %q", res.Text)
	}
}

func TestTable(t *testing.T) {
	res := render(t, "| Nom | Âge |\n|:---|---:|\n| Alex | 30 |\n")
	if !strings.Contains(res.RTF, `\trowd`) || !strings.Contains(res.RTF, `\cellx`) {
		t.Errorf("table structure missing")
	}
	if !strings.Contains(res.RTF, `\qr`) {
		t.Errorf("right alignment missing")
	}
	if !strings.Contains(res.Text, "Alex\t30\r") && !strings.Contains(res.Text, "Alex") {
		t.Errorf("table content missing: %q", res.Text)
	}
}

func TestFrontMatterSkipped(t *testing.T) {
	res := render(t, "---\ntitle: x\ntags: [a]\n---\n\n# Real\n")
	if strings.Contains(res.Text, "title") {
		t.Errorf("front matter leaked: %q", res.Text)
	}
	if !strings.Contains(res.Text, "Real") {
		t.Errorf("content lost: %q", res.Text)
	}
}

func TestTaskList(t *testing.T) {
	res := render(t, "- [ ] todo\n- [x] done\n")
	if !strings.Contains(res.RTF, `\u9744?`) || !strings.Contains(res.RTF, `\u9745?`) {
		t.Errorf("checkboxes missing: %s", res.RTF)
	}
}

func TestNestedList(t *testing.T) {
	res := render(t, "1. premier\n2. second\n   - sub a\n   - sub b\n3. trois\n")
	r := []rune(res.Text)
	got := string(r)
	if !strings.Contains(got, "1.\tpremier\r") {
		t.Errorf("ordered marker missing: %q", got)
	}
	if !strings.Contains(got, "•\tsub a\r") {
		t.Errorf("nested bullet missing: %q", got)
	}
	if !strings.Contains(got, "3.\ttrois\r") {
		t.Errorf("third item missing: %q", got)
	}
}

func TestBlockquote(t *testing.T) {
	res := render(t, "> quoted **line**\n> second\n")
	if !strings.Contains(res.RTF, `\brdrl\brdrs`) {
		t.Errorf("quote bar missing")
	}
	// consecutive "> " lines are a single paragraph, like CommonMark
	if !strings.Contains(res.Text, "quoted line second\r") {
		t.Errorf("quote mirror wrong: %q", res.Text)
	}
}

func TestHrAndIndentedCode(t *testing.T) {
	res := render(t, "para\n\n---\n\n    indented code\n")
	if !strings.Contains(res.RTF, `\brdrb\brdrs`) {
		t.Errorf("hr missing")
	}
	if !strings.Contains(res.Text, "indented code") {
		t.Errorf("indented code missing: %q", res.Text)
	}
}

func TestScale(t *testing.T) {
	res := Render("# T\n", Options{Theme: LightTheme(), Scale: 1.5})
	if !strings.Contains(res.RTF, `\fs57`) {
		t.Errorf("scale not applied: %s", head(res.RTF))
	}
}

func TestNoPanicOnGarbage(t *testing.T) {
	inputs := []string{
		"", "\n\n\n", "```", "```go", "|", "|-|", "[](", "[a](", "[a](b", "***", "*", "_", "__",
		"#", "#######", ">", "-", "1.", "1)", "\x00\x01", "text\r\nwith\r\ncrlf",
		"[![img](a.png)](b.md)", "| a |\n| - |\n| \\| |", "> > nested\n> > quote",
		"a\tb", strings.Repeat("# h\n", 500), "😀 {}", "{}{}{",
	}
	for i, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on input %d (%q): %v", i, in, r)
				}
			}()
			Render(in, Options{Theme: DarkTheme()})
		}()
	}
}

func TestDarkThemeColors(t *testing.T) {
	res := Render("# T\n", Options{Theme: DarkTheme()})
	if !strings.Contains(res.RTF, `\red230\green237\blue243`) {
		t.Errorf("dark text colour missing")
	}
}

func head(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
