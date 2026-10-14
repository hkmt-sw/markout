package render

import (
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// coverRender converts Markdown to PDF and returns the trace of what was
// drawn and the warnings.
func coverRender(t *testing.T, md string, set theme.Set) (string, []string) {
	t.Helper()
	return coverRenderIn(t, flavor.Default(), md, set)
}

// coverRenderIn is coverRender for a document read as the flavor reads it.
func coverRenderIn(t *testing.T, fl flavor.Flavor, md string, set theme.Set) (string, []string) {
	t.Helper()
	doc, err := parse.ParseFlavor([]byte(md), fl, "")
	if err != nil {
		t.Fatal(err)
	}
	var warnings []string
	r := NewPdfRenderer()
	r.opts = Options{Theme: &set, Warn: func(w string) { warnings = append(warnings, w) }}
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	return r.trace.String(), warnings
}

// themedCover is the default theme with covers on for every document.
func themedCover() theme.Set {
	set := theme.Default()
	set.PDF.Cover.Enabled = true
	return set
}

// onPage returns the lines of a trace drawn on one page.
func onPage(trace string, page int) string {
	prefix := "p" + strconv.Itoa(page) + " "
	var out []string
	for _, line := range strings.Split(trace, "\n") {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func TestCoverFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     map[string]any
		themed  bool
		cover   bool
		warning string
	}{
		{"nothing said, theme off", map[string]any{"title": "T"}, false, false, ""},
		{"nothing said, theme on", map[string]any{"title": "T"}, true, true, ""},
		{"asked for", map[string]any{"cover": true}, false, true, ""},
		{"refused", map[string]any{"cover": false}, true, false, ""},
		{"key in other case", map[string]any{"Cover": true}, false, true, ""},
		{"true, theme false", map[string]any{"cover": true}, false, true, ""},
		{"false, theme false", map[string]any{"cover": false}, false, false, ""},
		{"string \"true\", theme false", map[string]any{"cover": "true"}, false, true, ""},
		{"string \"False\", theme false", map[string]any{"cover": "False"}, false, false, ""},
		{"\"yes\", theme false", map[string]any{"cover": "yes"}, false, true, ""},
		{"\"YES\", theme false", map[string]any{"cover": "YES"}, false, true, ""},
		{"\" no \", theme false", map[string]any{"cover": " no "}, false, false, ""},
		{"\"maybe\", theme false", map[string]any{"cover": "maybe"}, false, false, "cover in the front matter must be true, false, yes or no, not \"maybe\"; it was ignored."},
		{"number, theme false", map[string]any{"cover": int64(1)}, false, false, "cover in the front matter must be true, false, yes or no, not \"1\"; it was ignored."},
		{"missing key, theme false", map[string]any{"title": "T"}, false, false, ""},
		{"true, theme true", map[string]any{"cover": true}, true, true, ""},
		{"false, theme true", map[string]any{"cover": false}, true, false, ""},
		{"string \"true\", theme true", map[string]any{"cover": "true"}, true, true, ""},
		{"string \"False\", theme true", map[string]any{"cover": "False"}, true, false, ""},
		{"\"yes\", theme true", map[string]any{"cover": "yes"}, true, true, ""},
		{"\"YES\", theme true", map[string]any{"cover": "YES"}, true, true, ""},
		{"\" no \", theme true", map[string]any{"cover": " no "}, true, false, ""},
		{"\"maybe\", theme true", map[string]any{"cover": "maybe"}, true, true, "cover in the front matter must be true, false, yes or no, not \"maybe\"; it was ignored."},
		{"number, theme true", map[string]any{"cover": int64(1)}, true, true, "cover in the front matter must be true, false, yes or no, not \"1\"; it was ignored."},
		{"missing key, theme true", map[string]any{"title": "T"}, true, true, ""},
	} {
		cover, warning := coverFor(ast.FrontMatter{Raw: tc.raw}, tc.themed)
		if cover != tc.cover || warning != tc.warning {
			t.Errorf("%s: got %v, %q; want %v, %q", tc.name, cover, warning, tc.cover, tc.warning)
		}
	}
}

func TestCoverWhenAsked(t *testing.T) {
	const body = "\n# The Heading\n\nbody text\n"
	for _, tc := range []struct {
		name  string
		md    string
		set   theme.Set
		cover bool
		warns bool
	}{
		{"front matter asks", "---\ntitle: The Title\ncover: true\n---\n" + body, theme.Default(), true, false},
		{"theme asks", "---\ntitle: The Title\n---\n" + body, themedCover(), true, false},
		{"front matter refuses", "---\ntitle: The Title\ncover: false\n---\n" + body, themedCover(), false, false},
		{"nobody asks", "---\ntitle: The Title\n---\n" + body, theme.Default(), false, false},
		{"no front matter", body, themedCover(), false, false},
		{"only a language", "---\nlang: hu\n---\n" + body, themedCover(), false, false},
		{"yes asks", "---\ntitle: The Title\ncover: yes\n---\n" + body, theme.Default(), true, false},
		{"no refuses", "---\ntitle: The Title\ncover: \"No\"\n---\n" + body, themedCover(), false, false},
		{"another word asks nothing", "---\ntitle: The Title\ncover: maybe\n---\n" + body, theme.Default(), false, true},
		{"another word does not refuse", "---\ntitle: The Title\ncover: \"maybe\"\n---\n" + body, themedCover(), true, true},
		{"TOML front matter", "+++\ntitle = \"The Title\"\ncover = true\n+++\n" + body, theme.Default(), true, false},
	} {
		trace, warnings := coverRender(t, tc.md, tc.set)
		if got := strings.Contains(onPage(trace, 1), "Arial-Bold/28 "); got != tc.cover {
			t.Errorf("%s: cover = %v, want %v\n%s", tc.name, got, tc.cover, trace)
		}
		if tc.cover && !strings.Contains(onPage(trace, 2), `"text"`) {
			t.Errorf("%s: the text does not start on page 2", tc.name)
		}
		if !tc.cover && strings.Contains(trace, "p2 ") {
			t.Errorf("%s: a short document has a second page", tc.name)
		}
		if got := len(warnings) > 0; got != tc.warns {
			t.Errorf("%s: warnings %q", tc.name, warnings)
		}
		if tc.warns && !strings.Contains(warnings[0], "cover") {
			t.Errorf("%s: the warning does not name the key: %q", tc.name, warnings[0])
		}
	}
}

// Front matter is read in every flavor, so it gives a cover in each of them.
func TestCoverInEveryFlavor(t *testing.T) {
	const md = "---\ntitle: The Title\ncover: true\n---\n\n# The Heading\n\nbody text\n"
	for _, id := range []string{"commonmark", "youtrack", "bitbucket", "extra"} {
		fl, ok := flavor.ByID(id)
		if !ok {
			t.Fatalf("no flavor %s", id)
		}
		trace, warnings := coverRenderIn(t, fl, md, theme.Default())
		if !strings.Contains(onPage(trace, 1), "Arial-Bold/28 ") {
			t.Errorf("%s: no cover\n%s", id, trace)
		}
		if len(warnings) > 0 {
			t.Errorf("%s: warnings %q", id, warnings)
		}
	}
}

func TestCoverPage(t *testing.T) {
	md := "---\ntitle: The Title\nsubtitle: The Subtitle\nversion: 1.0\nstatus: Final\nscope: Everything\ncover: true\n---\n\n# The Heading\n\nbody text\n"
	trace, warnings := coverRender(t, md, theme.Default())
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
	page1, page2 := onPage(trace, 1), onPage(trace, 2)

	// The title starts below the top margin by Cover.Top (56.7 + 160), at the
	// left margin, and the subtitle follows in its own type.
	for _, want := range []string{
		`text  x=56.7 y=216.7 Arial-Bold/28 "The Title" #000000`,
		`text  x=56.7 y=257.4 Arial/16 "The Subtitle" #4A4A68`,
		`Arial-Bold/11 "Version"`, `Arial/11 "1.0"`,
		`Arial-Bold/11 "Status"`, `Arial/11 "Final"`,
		`Arial-Bold/11 "scope"`, `Arial/11 "Everything"`,
	} {
		if !strings.Contains(page1, want) {
			t.Errorf("page 1 lacks %s:\n%s", want, page1)
		}
	}

	// What the page says is not a row of the block, and cover is none
	for _, not := range []string{`"Title"`, `"The Title" #1A1A2E`, `"subtitle"`, `"The Subtitle" #1A1A2E`, `"cover"`, `"true"`} {
		if strings.Contains(page1, not) {
			t.Errorf("page 1 has %s:\n%s", not, page1)
		}
	}
	if n := strings.Count(trace, `"The Title"`); n != 1 {
		t.Errorf("the title is drawn %d times", n)
	}

	// The text starts on page 2, and the cover is not a bookmark
	if !strings.Contains(page2, `Arial-Bold/24 "Heading"`) || !strings.Contains(page2, `"body"`) {
		t.Errorf("page 2:\n%s", page2)
	}
	if strings.Contains(trace, `bookmark level=1 "The Title"`) || strings.Count(trace, "bookmark") != 1 {
		t.Errorf("the cover is in the outline:\n%s", trace)
	}
}

// Without a cover the subtitle is a row of the block, as any other key.
func TestSubtitleWithoutCover(t *testing.T) {
	md := "---\ntitle: The Title\nsubtitle: The Subtitle\ncover: false\n---\n\ntext\n"
	trace, _ := coverRender(t, md, themedCover())
	for _, want := range []string{`Arial-Bold/11 "Title"`, `Arial/11 "The Title"`, `Arial-Bold/11 "subtitle"`, `Arial/11 "The Subtitle"`} {
		if !strings.Contains(trace, want) {
			t.Errorf("missing %s:\n%s", want, trace)
		}
	}
	if strings.Contains(trace, `"cover"`) || strings.Contains(trace, `"false"`) {
		t.Errorf("the cover key is shown:\n%s", trace)
	}
}

// The cover key is not a row of the block, whatever it holds.
func TestCoverKeyIsNeverARow(t *testing.T) {
	for _, value := range []string{"true", "false", `"yes"`, "3", "[a, b]"} {
		trace, _ := coverRender(t, "---\ntitle: T\nversion: 2\nCover: "+value+"\n---\n\ntext\n", theme.Default())
		if strings.Contains(strings.ToLower(trace), `"cover"`) || strings.Contains(trace, `"yes"`) {
			t.Errorf("cover: %s is shown:\n%s", value, trace)
		}
	}

	fm := ast.FrontMatter{Raw: map[string]any{"title": "T", "Cover": "yes", "scope": "S"}}
	fields := controlFields(fm, "", theme.ControlLabels{})
	if want := []controlField{{"Title", "T"}, {"scope", "S"}}; !reflect.DeepEqual(fields, want) {
		t.Errorf("fields = %v, want %v", fields, want)
	}
	fields = controlFields(fm, "", theme.ControlLabels{}, "title", "scope")
	if len(fields) != 0 {
		t.Errorf("fields left out are shown: %v", fields)
	}
}

// With nothing left for the block, there is none; with no title at all, the
// first heading is the title.
func TestCoverTitleAndEmptyBlock(t *testing.T) {
	trace, _ := coverRender(t, "---\ncover: true\nversion: 3\n---\n\n# A Heading Title\n\ntext\n", theme.Default())
	if want := `text  x=56.7 y=216.7 Arial-Bold/28 "A Heading Title" #000000`; !strings.Contains(onPage(trace, 1), want) {
		t.Errorf("the heading is not the title:\n%s", onPage(trace, 1))
	}
	if !strings.Contains(onPage(trace, 2), `Arial-Bold/24 "Title"`) {
		t.Errorf("the heading is not on page 2:\n%s", onPage(trace, 2))
	}

	trace, _ = coverRender(t, "---\ntitle: Only\ncover: true\n---\n\ntext\n", theme.Default())
	if strings.Contains(trace, "rect") {
		t.Errorf("an empty block is drawn:\n%s", trace)
	}

	// A document of nothing but front matter has no page after the cover
	trace, _ = coverRender(t, "---\ntitle: Only\ncover: true\n---\n", theme.Default())
	if strings.Contains(trace, "p2 ") {
		t.Errorf("a page after the cover:\n%s", trace)
	}
}

// A long title is wrapped to the width of the page.
func TestCoverTitleWraps(t *testing.T) {
	title := strings.Repeat("Információbiztonsági szabályzat ", 4)
	trace, _ := coverRender(t, "---\ntitle: "+title+"\ncover: true\n---\n\ntext\n", theme.Default())
	n := 0
	for _, line := range strings.Split(onPage(trace, 1), "\n") {
		if strings.Contains(line, "Arial-Bold/28") {
			n++
		}
	}
	if n < 3 {
		t.Errorf("the title is on %d lines", n)
	}
}

// The cover counts as a page but has no header or footer.
func TestCoverHasNoHeaderOrFooter(t *testing.T) {
	set := runningTheme()
	md := "---\ntitle: The Title\nauthor: An Author\ncover: true\n---\n\n" + strings.Repeat("A paragraph of body text.\n\n", 60)
	trace, _ := coverRender(t, md, set)

	if strings.Contains(onPage(trace, 1), "Arial/9 ") || strings.Contains(onPage(trace, 1), "line  x=56.7 y=37.8") {
		t.Errorf("the cover has a header or footer:\n%s", onPage(trace, 1))
	}
	pages := strings.Count(trace, "Page ")
	if pages != 2 {
		t.Fatalf("%d footers, want 2 (pages 2 and 3):\n%s", pages, trace)
	}
	for page := 2; page <= 3; page++ {
		p := onPage(trace, page)
		for _, want := range []string{`Arial/9 "The Title"`, `"An Author"`, `"Page ` + strconv.Itoa(page) + ` of 3"`} {
			if !strings.Contains(p, want) {
				t.Errorf("page %d lacks %s", page, want)
			}
		}
	}

	// A theme without a cover has them on page 1
	trace, _ = coverRender(t, strings.Replace(md, "cover: true", "cover: false", 1), set)
	if !strings.Contains(onPage(trace, 1), `"Page 1 of `) {
		t.Errorf("no footer on page 1 without a cover")
	}
}

// With a table of contents the page numbers count the cover.
func TestCoverCountsInTableOfContents(t *testing.T) {
	md := "---\ntitle: T\ncover: true\n---\n\n[[toc]]\n\n# One\n\ntext\n"
	trace, _ := coverRender(t, md, theme.Default())
	// The contents are on page 2, with the first heading on it too
	if !strings.Contains(onPage(trace, 2), `"One"`) {
		t.Fatalf("page 2:\n%s", onPage(trace, 2))
	}
	if !strings.Contains(onPage(trace, 2), `"2" `) {
		t.Errorf("the contents do not give page 2 for the heading:\n%s", onPage(trace, 2))
	}
}

func TestRunningVersionAndClassification(t *testing.T) {
	set := theme.Default()
	for _, th := range []*theme.Theme{&set.PDF, &set.DOCX} {
		th.Header.Left = "v{version}"
		th.Header.Right = "{classification}"
		th.Footer.Left = "{title} {version} [{classification}]"
	}
	trace, _ := coverRender(t, "---\ntitle: Policy\nversion: 1.0\nApproved_By: X\nclassification: Internal\n---\n\ntext\n", set)
	for _, want := range []string{`Arial/9 "v1.0"`, `Arial/9 "Internal"`, `Arial/9 "Policy 1.0 [Internal]"`} {
		if !strings.Contains(trace, want) {
			t.Errorf("missing %s:\n%s", want, trace)
		}
	}

	// A missing value leaves nothing behind
	trace, _ = coverRender(t, "---\ntitle: Policy\n---\n\ntext\n", set)
	if !strings.Contains(trace, `Arial/9 "Policy  []"`) || strings.Contains(trace, "{") {
		t.Errorf("placeholders without values:\n%s", trace)
	}
}

func TestDocInfoVersionAndClassification(t *testing.T) {
	md := "---\ntitle: T\nVersion: 1.0\nclassification: Belső\n---\n\ntext\n"
	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	info := infoOf(doc)
	if info.version != "1.0" || info.classification != "Belső" {
		t.Errorf("info = %+v", info)
	}
	if got := info.static("{version}/{classification}/{page}"); got != "1.0/Belső/{page}" {
		t.Errorf("static = %q", got)
	}
}
