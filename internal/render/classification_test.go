package render

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// classTheme is the default theme with a level added and another given a name,
// as a theme file would.
func classTheme() theme.Classification {
	cls := theme.Default().PDF.Classification
	levels := map[string]theme.ClassificationLevel{}
	for name, level := range cls.Levels {
		levels[name] = level
	}
	secret := levels["secret"]
	secret.Names = append(append([]string(nil), secret.Names...), "Eyes only")
	levels["secret"] = secret
	levels["restricted"] = theme.ClassificationLevel{
		Names: []string{"Restricted"}, Color: theme.Hex("000000"), Background: theme.Hex("FFD54F"),
	}
	cls.Levels = levels
	return cls
}

func TestClassificationFor(t *testing.T) {
	std := theme.Default().PDF.Classification
	off := std
	off.Band = false
	neutral := classBand{drawn: true, color: std.Color, background: std.Background}
	level := func(name, text string) classBand {
		return classBand{drawn: true, text: text, color: std.Levels[name].Color, background: std.Levels[name].Background}
	}
	unknown := func(text string) string {
		return `the classification "` + text + `" in the front matter is not one the theme knows (confidential, internal, public, secret); it has a band in neutral colors.`
	}
	for _, tc := range []struct {
		name    string
		raw     map[string]any
		cls     theme.Classification
		want    classBand
		warning string
	}{
		{"no classification", map[string]any{"title": "T"}, std, classBand{}, ""},
		{"empty", map[string]any{"classification": "  "}, std, classBand{}, ""},
		{"a level", map[string]any{"classification": "Confidential"}, std, level("confidential", "Confidential"), ""},
		{"the text is as written", map[string]any{"classification": "PUBLIC"}, std, level("public", "PUBLIC"), ""},
		{"spaces around", map[string]any{"classification": "  internal  "}, std, level("internal", "internal"), ""},
		{"Hungarian", map[string]any{"classification": "Szigorúan titkos"}, std, level("secret", "Szigorúan titkos"), ""},
		{"Hungarian, another case", map[string]any{"classification": "belső HASZNÁLATRA"}, std, level("internal", "belső HASZNÁLATRA"), ""},
		{"Hungarian, a short name", map[string]any{"classification": "Bizalmas"}, std, level("confidential", "Bizalmas"), ""},
		{"key in other case", map[string]any{"Classification": "Titkos"}, std, level("secret", "Titkos"), ""},
		{"nobody knows it", map[string]any{"classification": "Top secret"}, std,
			classBand{drawn: true, text: "Top secret", color: neutral.color, background: neutral.background}, unknown("Top secret")},
		{"a name the theme added", map[string]any{"classification": "eyes ONLY"}, classTheme(), level("secret", "eyes ONLY"), ""},
		{"a level the theme added", map[string]any{"classification": "restricted"}, classTheme(),
			classBand{drawn: true, text: "restricted", color: theme.Hex("000000"), background: theme.Hex("FFD54F")}, ""},
		{"a name the theme did not add", map[string]any{"classification": "Eyes only"}, std,
			classBand{drawn: true, text: "Eyes only", color: neutral.color, background: neutral.background}, unknown("Eyes only")},
		{"the warning lists the theme's levels", map[string]any{"classification": "Nope"}, classTheme(),
			classBand{drawn: true, text: "Nope", color: neutral.color, background: neutral.background},
			`the classification "Nope" in the front matter is not one the theme knows (confidential, internal, public, restricted, secret); it has a band in neutral colors.`},
		{"band off", map[string]any{"classification": "Secret"}, off, classBand{}, ""},
		{"band off, unknown value", map[string]any{"classification": "Nope"}, off, classBand{}, ""},
		{"a list has no text", map[string]any{"classification": []any{"a"}}, std, classBand{}, ""},
	} {
		got, warning := classificationFor(ast.FrontMatter{Raw: tc.raw}, tc.cls)
		if got != tc.want || warning != tc.warning {
			t.Errorf("%s: got %+v, %q; want %+v, %q", tc.name, got, warning, tc.want, tc.warning)
		}
	}

	// Only front matter at the head of the document counts
	doc := &ast.Document{Elements: []ast.Element{ast.FrontMatter{Raw: map[string]any{"classification": "Secret"}}}}
	if band, _ := classificationOf(doc, std); !band.drawn {
		t.Error("the classification of the front matter was not found")
	}
	doc = &ast.Document{Elements: []ast.Element{ast.Paragraph{}, ast.FrontMatter{Raw: map[string]any{"classification": "Secret"}}}}
	if band, _ := classificationOf(doc, std); band.drawn {
		t.Error("front matter that does not head the document has a band")
	}
}

func TestWatermarkFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     map[string]any
		text    string
		warning string
	}{
		{"none", map[string]any{"title": "T"}, "", ""},
		{"text", map[string]any{"watermark": "DRAFT"}, "DRAFT", ""},
		{"spaces are tidied", map[string]any{"watermark": "  TOP \t SECRET "}, "TOP SECRET", ""},
		{"key in other case", map[string]any{"Watermark": "DRAFT"}, "DRAFT", ""},
		{"empty", map[string]any{"watermark": ""}, "", ""},
		{"a number", map[string]any{"watermark": int64(2026)}, "", "watermark in the front matter must be text, not 2026; it was ignored."},
		{"a flag", map[string]any{"watermark": true}, "", "watermark in the front matter must be text, not true; it was ignored."},
		{"a list", map[string]any{"watermark": []any{"a", "b"}}, "", "watermark in the front matter must be text, not [a b]; it was ignored."},
		{"a date", map[string]any{"watermark": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, "", "watermark in the front matter must be text, not 2026-10-01 00:00:00 +0000 UTC; it was ignored."},
	} {
		text, warning := watermarkFor(ast.FrontMatter{Raw: tc.raw})
		if text != tc.text || warning != tc.warning {
			t.Errorf("%s: got %q, %q; want %q, %q", tc.name, text, warning, tc.text, tc.warning)
		}
	}
}

// The watermark steers the conversion: it is never a row of the control block,
// where the classification is one.
func TestWatermarkIsNotAControlRow(t *testing.T) {
	fm := ast.FrontMatter{Raw: map[string]any{"title": "T", "classification": "Secret", "watermark": "DRAFT", "Watermark": "OTHER"}}
	var labels []string
	for _, f := range controlFields(fm, "en", theme.ControlLabels{}) {
		labels = append(labels, f.label)
	}
	if got := strings.Join(labels, ","); got != "Title,Classification" {
		t.Errorf("the rows are %s", got)
	}

	trace := pdfTraceWith(t, "---\ntitle: T\nclassification: Secret\nwatermark: DRAFT\n---\n\nbody\n", theme.Default())
	if strings.Contains(trace, `"watermark"`) || strings.Contains(trace, `"Watermark"`) || strings.Count(trace, `"DRAFT"`) != 1 {
		t.Errorf("the watermark is in the control block:\n%s", trace)
	}
}

func TestBandOnEveryPage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		front string
		set   theme.Set
		pages int
	}{
		{"no cover", "---\ntitle: T\nclassification: Belső használatra\n---\n\n", theme.Default(), 3},
		{"cover", "---\ntitle: T\nclassification: Belső használatra\ncover: true\n---\n\n", theme.Default(), 4},
		{"a header", "---\ntitle: T\nclassification: Belső használatra\n---\n\n", runningTheme(), 3},
	} {
		trace, warnings := coverRender(t, longDoc(tc.front), tc.set)
		if len(warnings) > 0 {
			t.Errorf("%s: warnings %q", tc.name, warnings)
		}
		pages := lastPage(trace)
		if pages < tc.pages {
			t.Errorf("%s: %d pages, too few to tell", tc.name, pages)
		}
		for page := 1; page <= pages; page++ {
			prefix := "p" + strconv.Itoa(page) + " "
			lines := strings.Split(onPage(trace, page), "\n")
			// The band is the first thing on the page: a strip across the full
			// width at the top, in the level's colors, and in it the text as the
			// document writes it, in bold, centered (the text is 72.3 points
			// wide, so it starts at 261.5) and in the middle of the strip
			if lines[0] != prefix+"rect  x=0.0 y=0.0 w=595.3 h=16.0 fill=#1565C0" {
				t.Errorf("%s: page %d starts with %q", tc.name, page, lines[0])
			}
			if want := prefix + `text  x=261.5 y=3.8 Arial-Bold/8.5 "Belső használatra" #FFFFFF`; lines[1] != want {
				t.Errorf("%s: page %d: the band text is %q, want %q", tc.name, page, lines[1], want)
			}
		}
		// Once on each page, and once more in the control block
		if n := strings.Count(trace, `"Belső használatra"`); n != pages+1 {
			t.Errorf("%s: the classification is drawn %d times", tc.name, n)
		}
	}
}

// lastPage is the number of the last page in a trace.
func lastPage(trace string) int {
	last := 0
	for _, line := range strings.Split(trace, "\n") {
		if n, ok := strings.CutPrefix(line, "p"); ok {
			if num, _, _ := strings.Cut(n, " "); num != "" {
				if v, err := strconv.Atoi(num); err == nil && v > last {
					last = v
				}
			}
		}
	}
	return last
}

func TestNoBand(t *testing.T) {
	off := theme.Default()
	off.PDF.Classification.Band = false
	for _, tc := range []struct {
		name  string
		front string
		set   theme.Set
	}{
		{"no classification", "---\ntitle: T\n---\n\n", theme.Default()},
		{"no front matter", "", theme.Default()},
		{"the theme has none", "---\ntitle: T\nclassification: Secret\n---\n\n", off},
	} {
		trace, warnings := coverRender(t, longDoc(tc.front), tc.set)
		if strings.Contains(trace, "fill=#B91C1C") || strings.Contains(trace, "x=0.0 y=0.0") || len(warnings) > 0 {
			t.Errorf("%s: a band was drawn, or a warning given: %q\n%s", tc.name, warnings, onPage(trace, 1))
		}
	}

	// The classification is a row of the control block either way
	trace, _ := coverRender(t, "---\ntitle: T\nclassification: Secret\n---\n", off)
	if !strings.Contains(trace, `"Classification"`) || !strings.Contains(trace, `"Secret"`) {
		t.Errorf("the control block lacks the classification:\n%s", trace)
	}
}

func TestUnknownClassificationWarns(t *testing.T) {
	trace, warnings := coverRender(t, "---\ntitle: T\nclassification: Top secret\n---\n\nbody\n", theme.Default())
	// The band says what the document says, in neutral colors
	for _, want := range []string{"rect  x=0.0 y=0.0 w=595.3 h=16.0 fill=#6B7280", `Arial-Bold/8.5 "Top secret" #FFFFFF`} {
		if !strings.Contains(onPage(trace, 1), want) {
			t.Errorf("the band lacks %s:\n%s", want, onPage(trace, 1))
		}
	}
	want := `the classification "Top secret" in the front matter is not one the theme knows (confidential, internal, public, secret); it has a band in neutral colors.`
	if len(warnings) != 1 || warnings[0] != want {
		t.Errorf("warnings %q, want %q", warnings, want)
	}

	// A theme that knows the value does not warn
	set := theme.Default()
	set.PDF.Classification = classTheme()
	_, warnings = coverRender(t, "---\nclassification: Eyes only\n---\n\nbody\n", set)
	if len(warnings) > 0 {
		t.Errorf("warnings %q", warnings)
	}
}

func TestWatermarkOnEveryPage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		front string
		set   theme.Set
		pages int
	}{
		{"plain", "---\nwatermark: TERVEZET\n---\n\n", theme.Default(), 3},
		{"cover, header and band", "---\ntitle: T\nwatermark: TERVEZET\nclassification: Secret\ncover: true\n---\n\n", runningTheme(), 4},
	} {
		trace, warnings := coverRender(t, longDoc(tc.front), tc.set)
		if len(warnings) > 0 {
			t.Errorf("%s: warnings %q", tc.name, warnings)
		}
		for page := 1; page <= tc.pages; page++ {
			lines := strings.Split(onPage(trace, page), "\n")
			// Centered on the page (the text is 507.7 points wide), at 45 degrees, in
			// light gray, and drawn first: what the page holds is drawn over it
			want := "p" + strconv.Itoa(page) + ` mark  x=47.3 y=372.9 angle=45 Arial-Bold/96 "TERVEZET" #E6E6E6`
			if lines[0] != want {
				t.Errorf("%s: page %d starts with %q, want %q", tc.name, page, lines[0], want)
			}
			if n := strings.Count(onPage(trace, page), " mark "); n != 1 {
				t.Errorf("%s: page %d has %d watermarks", tc.name, page, n)
			}
		}
	}

	// No key, no watermark
	if trace, _ := coverRender(t, longDoc("---\ntitle: T\n---\n\n"), theme.Default()); strings.Contains(trace, " mark ") {
		t.Error("a watermark without the key")
	}
}

func TestWatermarkSizeAndColorFromTheme(t *testing.T) {
	set := theme.Default()
	set.PDF.Watermark = theme.Watermark{Color: theme.Hex("CCCCCC"), Size: 60}
	trace, _ := coverRender(t, "---\nwatermark: DRAFT\n---\n\nbody\n", set)
	if want := `Arial-Bold/60 "DRAFT" #CCCCCC`; !strings.Contains(trace, want) {
		t.Errorf("the trace lacks %s:\n%s", want, trace)
	}
}

// Text too long for the diagonal is drawn smaller, so that it stays on the page.
func TestWatermarkShrinksToFit(t *testing.T) {
	trace, _ := coverRender(t, "---\nwatermark: A WATERMARK THAT IS MUCH TOO LONG FOR THE PAGE\n---\n\nbody\n", theme.Default())
	line := strings.Split(onPage(trace, 1), "\n")[0]
	i := strings.Index(line, "Arial-Bold/")
	var size float64
	if fields := strings.Fields(line[i+len("Arial-Bold/"):]); len(fields) > 0 {
		if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
			size = v
		}
	}
	x, y := fieldOf(t, line, "x="), fieldOf(t, line, "y=")
	// The text is centered, and slanted as it is stays within the width of the
	// page: its half-width and half its height, along and across a line at 45
	// degrees, make up the half-width of the box that holds it
	cx, cy := 595.28/2, 841.89/2
	half := cx - x
	if size >= 96 || size < 8 || math.Abs(y+size/2-cy) > 0.1 || (half+size/2)/math.Sqrt2 > cx*0.95 {
		t.Errorf("the watermark has size %g, half-width %.1f, at y=%.1f: %s", size, half, y, line)
	}
}

// fieldOf reads the number after key in a line of the trace.
func fieldOf(t *testing.T, line, key string) float64 {
	t.Helper()
	i := strings.Index(line, key)
	if i < 0 {
		t.Fatalf("no %s in %q", key, line)
	}
	rest := line[i+len(key):]
	if end := strings.IndexByte(rest, ' '); end >= 0 {
		rest = rest[:end]
	}
	v, err := strconv.ParseFloat(rest, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWatermarkThatIsNotTextWarns(t *testing.T) {
	for front, value := range map[string]string{"watermark: 2026": "2026", "watermark: true": "true", "watermark: [a, b]": "[a b]"} {
		trace, warnings := coverRender(t, "---\ntitle: T\n"+front+"\n---\n\nbody\n", theme.Default())
		want := "watermark in the front matter must be text, not " + value + "; it was ignored."
		if len(warnings) != 1 || warnings[0] != want {
			t.Errorf("%s: warnings %q, want %q", front, warnings, want)
		}
		if strings.Contains(trace, " mark ") || strings.Contains(trace, `"watermark"`) {
			t.Errorf("%s: the watermark was drawn, or is a row of the control block:\n%s", front, trace)
		}
	}
}

// The watermark and the band go on the page as it is added, so what is drawn
// after belongs to the page's content, whichever way the page came about.
func TestPageMarksLeaveTheDrawingStateAlone(t *testing.T) {
	front := "---\ntitle: T\nclassification: Secret\nwatermark: DRAFT\n---\n\n"
	off := theme.Default()
	off.PDF.Classification.Band = false
	with, _ := coverRender(t, longDoc(front), theme.Default())
	without, _ := coverRender(t, longDoc(strings.Replace(front, "watermark: DRAFT\n", "", 1)), off)
	// Apart from the marks, the pages are the same
	var kept []string
	for _, line := range strings.Split(with, "\n") {
		if !strings.Contains(line, " mark ") && !strings.Contains(line, "x=0.0 y=0.0") && !strings.Contains(line, "Arial-Bold/8.5") {
			kept = append(kept, line)
		}
	}
	if strings.Join(kept, "\n") != without {
		t.Error("the marks changed how the rest of the document is drawn")
	}
}

// A header that a band would run into goes under the band.
func TestHeaderMakesRoomForTheBand(t *testing.T) {
	set := runningTheme()
	front := "---\ntitle: The Title\nclassification: Secret\n---\n\n"

	// Margin enough: the header sits where it did, in the middle of its margin
	trace, _ := coverRender(t, longDoc(front), set)
	if want := `p1 text  x=56.7 y=23.8 Arial/9 "The Title"`; !strings.Contains(trace, want) {
		t.Errorf("the header moved:\n%s", onPage(trace, 1))
	}

	// A top margin of 30 points holds the band and the header only if the
	// header goes in what is left under the band
	for _, th := range []*theme.Theme{&set.PDF, &set.DOCX} {
		th.Page.MarginTop = 30
	}
	trace, _ = coverRender(t, longDoc(front), set)
	if want := `p1 text  x=56.7 y=18.5 Arial/9 "The Title"`; !strings.Contains(trace, want) {
		t.Errorf("the header is not under the band:\n%s", onPage(trace, 1))
	}
	trace, _ = coverRender(t, longDoc("---\ntitle: The Title\n---\n\n"), set)
	if want := `p1 text  x=56.7 y=10.5 Arial/9 "The Title"`; !strings.Contains(trace, want) {
		t.Errorf("without a band the header moved:\n%s", onPage(trace, 1))
	}
}
