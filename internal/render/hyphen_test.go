package render

import (
	"archive/zip"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/hyphen"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

const hyphenText = "Az eljárásrend minden olyan rendszerre vonatkozik, amely a cég üzleti adatait tárolja: " +
	"a fájlszerverre, az ügyviteli rendszer adatbázisára, a levelezésre és a virtuális gépekre. " +
	"A munkaállomások helyi lemezére nem terjed ki; azokon üzleti adatot tartósan tárolni nem szabad."

func hyphenatedTheme(mode string) theme.Set {
	set := theme.Default()
	set.PDF.Text.Hyphenate, set.DOCX.Text.Hyphenate = mode, mode
	return set
}

// laidOut renders nothing with a theme and a language, which leaves a
// renderer that text can be laid out with.
func laidOut(t *testing.T, set theme.Set, language string) *PdfRenderer {
	t.Helper()
	r := NewPdfRenderer()
	r.opts = Options{Theme: &set, Lang: language}
	if err := r.RenderToFile(&ast.Document{}, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	return r
}

func lineTexts(lines []textLine) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		var parts []string
		for _, f := range line.frags {
			parts = append(parts, f.text)
		}
		out[i] = strings.Join(parts, " ")
	}
	return out
}

// A word that does not fit at the end of a line is broken where the language
// allows, and what was broken reads as the same text.
func TestWordsAreBrokenAtLineEnds(t *testing.T) {
	r := laidOut(t, hyphenatedTheme(theme.HyphenateAlways), "hu")
	runs := []ast.InlineRun{{Text: hyphenText}}
	const width = 220.0

	lines := lineTexts(r.layout(runs, r.proseStyle(), width, width))
	whole := lineTexts(r.layout(runs, r.bodyStyle(), width, width))

	broken := 0
	joined := ""
	for i, line := range lines {
		if i < len(lines)-1 && strings.HasSuffix(line, "-") {
			broken++
			joined += strings.TrimSuffix(line, "-")
		} else {
			joined += line + " "
		}
	}
	if broken == 0 {
		t.Fatalf("no line ends in a hyphen:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(joined) != hyphenText {
		t.Errorf("the lines do not add up to the text:\n%s", strings.Join(lines, "\n"))
	}
	if len(lines) > len(whole) {
		t.Errorf("hyphenated, the text takes %d lines; whole, %d", len(lines), len(whole))
	}
	for _, line := range whole {
		if strings.HasSuffix(line, "-") {
			t.Errorf("a line of text that is not hyphenated ends in a hyphen: %s", line)
		}
	}
	for _, line := range r.layout(runs, r.proseStyle(), width, width) {
		if line.width() > width+0.01 {
			t.Errorf("a line is %.1f wide, more than %.0f", line.width(), width)
		}
	}
}

// By default words are hyphenated where text is justified and nowhere else;
// table cells only where a theme hyphenates everything.
func TestWhatIsHyphenated(t *testing.T) {
	tests := []struct {
		mode, align  string
		prose, aside bool
	}{
		{theme.HyphenateAuto, theme.AlignLeft, false, false},
		{theme.HyphenateAuto, theme.AlignJustify, true, false},
		{theme.HyphenateAlways, theme.AlignLeft, true, true},
		{theme.HyphenateNever, theme.AlignJustify, false, false},
	}
	for _, tt := range tests {
		set := hyphenatedTheme(tt.mode)
		set.PDF.Text.Align = tt.align
		r := laidOut(t, set, "hu")
		if got := r.proseStyle().hyphen != nil; got != tt.prose {
			t.Errorf("hyphenate %s, align %s: running text hyphenated = %v", tt.mode, tt.align, got)
		}
		if got := r.asideStyle().hyphen != nil; got != tt.aside {
			t.Errorf("hyphenate %s, align %s: table cells hyphenated = %v", tt.mode, tt.align, got)
		}
		if r.bodyStyle().hyphen != nil {
			t.Errorf("hyphenate %s: captions and labels are hyphenated", tt.mode)
		}
	}
}

// No more than three lines in a row end in a part of a word.
func TestNoLongColumnOfHyphens(t *testing.T) {
	r := laidOut(t, hyphenatedTheme(theme.HyphenateAlways), "hu")
	// A column this narrow breaks a word on nearly every line
	text := strings.Repeat("rendszerüzemeltetés információbiztonság visszaállíthatóság ", 8)
	lines := lineTexts(r.layout([]ast.InlineRun{{Text: text}}, r.proseStyle(), 150, 150))

	run, longest, broken := 0, 0, 0
	for _, line := range lines {
		if strings.HasSuffix(line, "-") {
			run++
			broken++
		} else {
			run = 0
		}
		longest = max(longest, run)
	}
	if longest != maxBrokenLines {
		t.Errorf("%d lines in a row end in a hyphen, not %d (%d of %d lines do):\n%s", longest, maxBrokenLines, broken, len(lines), strings.Join(lines, "\n"))
	}
}

func TestBreaksOf(t *testing.T) {
	hu := &lineBreaker{hyphen: hyphen.For("hu"), soft: true}
	en := &lineBreaker{hyphen: hyphen.For("en-GB"), soft: true}
	none := &lineBreaker{soft: true}
	never := &lineBreaker{}
	hy, as := true, false

	tests := []struct {
		name string
		b    *lineBreaker
		word string
		soft []int
		want []wordBreak
	}{
		{"by the patterns", hu, "vasút", nil, []wordBreak{{3, hy}}},
		{"punctuation around the word", hu, "(vasút),", nil, []wordBreak{{4, hy}}},
		{"a word with a digit", hu, "vasút2", nil, []wordBreak{}},
		{"a short word", hu, "alma", nil, []wordBreak{}},
		{"at its hyphen, with none added", en, "well-known", nil, []wordBreak{{5, as}}},
		{"not after one letter", en, "e-mail", nil, []wordBreak{}},
		{"not next to a digit", en, "ISO-27001", nil, []wordBreak{}},
		{"soft hyphens go before the patterns", hu, "rendszerüzemeltetés", []int{8}, []wordBreak{{8, hy}}},
		{"soft hyphens need no patterns", none, "rendszerüzemeltetés", []int{8}, []wordBreak{{8, hy}}},
		{"no language", none, "rendszerüzemeltetés", nil, nil},
		{"hyphenation turned off", never, "rendszerüzemeltetés", []int{8}, nil},
	}
	for _, tt := range tests {
		got := tt.b.breaksOf(tt.word, tt.soft)
		if len(got) == 0 && len(tt.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %q is broken at %v, not %v", tt.name, tt.word, got, tt.want)
		}
	}
}

func TestSoftHyphens(t *testing.T) {
	text, places := softHyphens("a rend\u00adszer\u00adgazda és az infor\u00admáció")
	if text != "a rendszergazda és az információ" {
		t.Errorf("text %q", text)
	}
	want := map[int][]int{1: {4, 8}, 4: {5}}
	if !reflect.DeepEqual(places, want) {
		t.Errorf("places %v, not %v", places, want)
	}
	if text, places := softHyphens("plain text"); text != "plain text" || places != nil {
		t.Errorf("text without soft hyphens came back as %q %v", text, places)
	}
}

// A soft hyphen is never drawn; it is where its word is broken, in any block
// and with no language, unless the theme turns hyphenation off.
func TestSoftHyphenIsABreakAndNotACharacter(t *testing.T) {
	text := "Az üzemeltetés rendje: rendszer\u00adüzemeltetés"
	for mode, want := range map[string][]string{
		theme.HyphenateAuto:  {"Az üzemeltetés rendje: rendszer-", "üzemeltetés"},
		theme.HyphenateNever: {"Az üzemeltetés rendje:", "rendszerüzemeltetés"},
	} {
		r := laidOut(t, hyphenatedTheme(mode), "")
		got := lineTexts(r.layout([]ast.InlineRun{{Text: text}}, r.bodyStyle(), 190, 190))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("hyphenate %s: lines %q, not %q", mode, got, want)
		}
	}
}

// Asked to hyphenate a language it cannot, markout says so once.
func TestWarnsWhenItCannotHyphenate(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte(hyphenText+"\n\n"+hyphenText+"\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		mode, language, want string
	}{
		{theme.HyphenateAlways, "", "does not say what language"},
		{theme.HyphenateAlways, "de", "no hyphenation patterns for de"},
		{theme.HyphenateAlways, "hu", ""},
		{theme.HyphenateAlways, "en-AU", ""},
		{theme.HyphenateAuto, "de", ""}, // nothing is justified
		{theme.HyphenateNever, "de", ""},
	}
	for _, tt := range tests {
		set := hyphenatedTheme(tt.mode)
		var warnings []string
		opts := Options{Theme: &set, Lang: tt.language, Warn: func(w string) { warnings = append(warnings, w) }}
		if err := RenderPdf(doc, filepath.Join(t.TempDir(), "out.pdf"), opts); err != nil {
			t.Fatal(err)
		}
		got := strings.Join(warnings, "\n")
		if tt.want == "" && got != "" || tt.want != "" && (len(warnings) != 1 || !strings.Contains(got, tt.want)) {
			t.Errorf("hyphenate %s, language %q: warnings %q, wanted %q", tt.mode, tt.language, warnings, tt.want)
		}
	}
}

func zipFile(t *testing.T, path, name string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rc.Close() }()
			data, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
	}
	t.Fatalf("%s is not in %s", name, path)
	return ""
}

// A DOCX is told to hyphenate where a PDF is hyphenated, and its headings,
// code and table cells are kept whole.
func TestDOCXIsToldToHyphenate(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte(justifyDoc+"\n```\ncode\n```\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	render := func(mode, align string) (settings, styles, outline string) {
		set := hyphenatedTheme(mode)
		set.DOCX.Text.Align = align
		out := filepath.Join(t.TempDir(), "out.docx")
		if err := RenderDocx(doc, out, Options{Theme: &set, Lang: "en-GB"}); err != nil {
			t.Fatal(err)
		}
		return zipFile(t, out, "word/settings.xml"), zipFile(t, out, "word/styles.xml"), docxOutline(t, out)
	}
	const on, whole = "<w:autoHyphenation/>", "pStyle=" + wholeStyle

	settings, styles, outline := render(theme.HyphenateAuto, theme.AlignLeft)
	if strings.Contains(settings, on) || strings.Contains(styles, "suppressAutoHyphens") || strings.Contains(outline, whole) {
		t.Error("a document set flush left has hyphenation in it")
	}

	settings, styles, outline = render(theme.HyphenateAuto, theme.AlignJustify)
	if !strings.Contains(settings, on+`<w:consecutiveHyphenLimit w:val="3"/>`) {
		t.Errorf("hyphenation is not turned on in the settings: %s", settings)
	}
	if got := strings.Count(styles, "<w:suppressAutoHyphens/>"); got != 6+9+1 {
		t.Errorf("%d styles keep words whole, not the headings, the contents and the one for the rest", got)
	}
	// Two header cells, two cells and the code block
	if got := strings.Count(outline, whole); got != 5 {
		t.Errorf("%d paragraphs are kept whole, not 5:\n%s", got, outline)
	}

	_, _, outline = render(theme.HyphenateAlways, theme.AlignLeft)
	if got := strings.Count(outline, whole); got != 1 {
		t.Errorf("hyphenating everything, %d paragraphs are kept whole, not the code block alone:\n%s", got, outline)
	}
}
