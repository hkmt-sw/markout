package theme

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTheme(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAppliesOnlyWhatTheFileSets(t *testing.T) {
	dir := t.TempDir()
	path := writeTheme(t, dir, "mine.toml", `
description = "Mine"

[page]
size   = "Letter"
margin = "1in"
margin-left = "30mm"

[text]
size  = 12
color = "#333"

[heading]
color = "#AA0000"

[heading.h1]
size = 40

[quote]
italic = false

[docx.text]
size = 10.5
`)
	set, err := Loader{}.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	base := Default()

	for name, th := range map[string]Theme{"pdf": set.PDF, "docx": set.DOCX} {
		if th.Name != "mine" || th.Description != "Mine" {
			t.Errorf("%s: name %q, description %q", name, th.Name, th.Description)
		}
		if th.Page.Width != 612 || th.Page.Height != 792 {
			t.Errorf("%s: page is %gx%g, want Letter", name, th.Page.Width, th.Page.Height)
		}
		if th.Page.MarginTop != 72 || th.Page.MarginRight != 72 || th.Page.MarginBottom != 72 {
			t.Errorf("%s: margins %+v, want 1in", name, th.Page)
		}
		if got := th.Page.MarginLeft; got < 85.03 || got > 85.05 {
			t.Errorf("%s: left margin %g, want 30mm", name, got)
		}
		if th.Text.Color != (Color{0x33, 0x33, 0x33}) {
			t.Errorf("%s: text color %v", name, th.Text.Color)
		}
		for i, h := range th.Heading {
			if h.Color != (Color{0xAA, 0, 0}) {
				t.Errorf("%s: h%d color %v", name, i+1, h.Color)
			}
		}
		if th.Heading[0].Size != 40 || th.Quote.Italic {
			t.Errorf("%s: h1 size %g, quote italic %v", name, th.Heading[0].Size, th.Quote.Italic)
		}
	}
	if set.PDF.Text.Size != 12 || set.DOCX.Text.Size != 10.5 {
		t.Errorf("text size pdf %g docx %g, want 12 and 10.5", set.PDF.Text.Size, set.DOCX.Text.Size)
	}

	// Everything the file does not mention is the default's.
	if set.PDF.Heading[1] != withColor(base.PDF.Heading[1], Color{0xAA, 0, 0}) || set.PDF.Table != base.PDF.Table ||
		set.PDF.Alert != base.PDF.Alert || set.DOCX.Fonts.Body != "Georgia" || set.DOCX.Heading[1].Size != 22 {
		t.Error("settings the file does not mention changed")
	}
}

func withColor(h Heading, c Color) Heading {
	h.Color = c
	return h
}

func TestExtendsAndOrientation(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "base.toml", "[text]\nsize = 14\n\n[link]\ncolor = \"#00FF00\"\n")
	writeTheme(t, dir, "child.toml", "extends = \"base\"\n\n[link]\ncolor = \"#0000FF\"\n\n[page]\norientation = \"landscape\"\n")
	writeTheme(t, dir, "byfile.toml", "extends = \"./base.toml\"\n")

	l := Loader{UserDir: dir}
	child, err := l.Load("child")
	if err != nil {
		t.Fatal(err)
	}
	if child.PDF.Text.Size != 14 || child.PDF.Link.Color != (Color{0, 0, 255}) {
		t.Errorf("child: text %g, link %v", child.PDF.Text.Size, child.PDF.Link.Color)
	}
	if child.PDF.Page.Width < child.PDF.Page.Height || child.DOCX.Page.Width < child.DOCX.Page.Height {
		t.Errorf("landscape page is %gx%g", child.PDF.Page.Width, child.PDF.Page.Height)
	}
	if byFile, err := l.Load(filepath.Join(dir, "byfile.toml")); err != nil || byFile.PDF.Text.Size != 14 {
		t.Errorf("extending by path: %v, size %g", err, byFile.PDF.Text.Size)
	}

	var names []string
	for _, info := range l.List() {
		names = append(names, info.Name)
	}
	if got := strings.Join(names, ","); got != "default,classic,compact,modern,report,base,byfile,child" {
		t.Errorf("List() = %s", got)
	}

	writeTheme(t, dir, "a.toml", "extends = \"b\"\n")
	writeTheme(t, dir, "b.toml", "extends = \"a\"\n")
	if _, err := l.Load("a"); err == nil || !strings.Contains(err.Error(), "extends itself") {
		t.Errorf("a cycle was not reported: %v", err)
	}
}

func TestFonts(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "Inter-Regular.ttf", "not really a font")
	path := writeTheme(t, dir, "fonts.toml", `
[fonts]
body = "serif"
heading = "inter"
code = "mono"

[fonts.family.inter]
regular = "Inter-Regular.ttf"
name = "Inter"

[docx.fonts]
code = "Fira Code"
`)
	set, err := Loader{}.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f := set.PDF.Fonts; f.Body != "serif" || f.Heading != "inter" || f.Code != "mono" {
		t.Errorf("pdf fonts %+v", f)
	}
	if got := set.PDF.Fonts.Families["inter"].Regular; got != filepath.Join(dir, "Inter-Regular.ttf") {
		t.Errorf("font path %q is not resolved against the theme file", got)
	}
	// DOCX gets font names a word processor knows.
	if f := set.DOCX.Fonts; f.Body != "Times New Roman" || f.Heading != "Inter" || f.Code != "Fira Code" {
		t.Errorf("docx fonts %+v", f)
	}
	// The built-in default must not have picked up the family.
	if len(Default().PDF.Fonts.Families) != 0 {
		t.Error("loading a theme changed the default theme")
	}
}

func TestErrorsNameTheProblem(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]string{
		"[text]\nsize = \n":                    "theme", // syntax error, reported by the parser
		"[text]\nsizee = 12\n":                 "text.sizee is not a setting",
		"[txt]\nsize = 12\n":                   "no section [txt]",
		"[text]\ncolor = \"red\"\n":            "text.color",
		"[text]\ncolor = 12\n":                 "a color is written in quotes",
		"[text]\nsize = \"big\"\n":             "is not a length",
		"[page]\nsize = \"B5\"\n":              "not a paper size",
		"[page]\norientation = \"sideways\"\n": "neither portrait nor landscape",
		"[page]\nmargin = \"200mm\"\n":         "no room for text",
		"[text]\nsize = 0\n":                   "text.size is 0 points",
		"[fonts]\nbody = \"Georgia\"\n":        "not a font PDF can use",
		"[fonts.family.x]\nbold = \"b.ttf\"\n": "needs a regular font file",
		"[fonts]\nbody=\"x\"\n[fonts.family.x]\nregular = \"missing.ttf\"\n": "cannot be read",
		"[fonts.family.sans]\nregular = \"a.ttf\"\n":                         "built-in font",
		"extends = \"nope\"\n":     `no theme named "nope"`,
		"[heading.h7]\nsize = 1\n": "heading.h7 is not a setting",
		"[pdf.text]\nsizee = 1\n":  "pdf.text.sizee",
		"text = 5\n":               "must be a section",
	}
	i := 0
	for text, want := range tests {
		i++
		path := writeTheme(t, dir, "bad"+string(rune('a'+i))+".toml", text)
		_, err := Loader{UserDir: dir}.Load(path)
		if err == nil {
			t.Errorf("%q was accepted", text)
		} else if !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %q does not mention %q", text, err, want)
		} else if !strings.Contains(err.Error(), filepath.Base(path)) {
			t.Errorf("%q: error %q does not name the file", text, err)
		}
	}

	if _, err := (Loader{}).Load("no-such-theme"); err == nil {
		t.Error("an unknown theme name was accepted")
	}
	if _, err := (Loader{}).Load(filepath.Join(dir, "missing.toml")); err == nil {
		t.Error("a missing theme file was accepted")
	}
}

// Exporting a theme and loading the result gives the same theme back.
func TestExportRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := Default()
	text := Export(want)
	for _, must := range []string{"[page]", "[heading.h1]", "[alert.warning]", "[docx.fonts]", `body = "Georgia"`, "# space after a paragraph"} {
		if !strings.Contains(text, must) {
			t.Errorf("export lacks %q", must)
		}
	}

	got, err := Loader{}.Load(writeTheme(t, dir, "default-copy.toml", text))
	if err != nil {
		t.Fatalf("the exported theme does not load: %v\n%s", err, text)
	}
	got.PDF.Name, got.DOCX.Name = want.PDF.Name, want.DOCX.Name
	// Lengths are written to a thousandth of a point.
	if !closeEnough(reflect.ValueOf(got), reflect.ValueOf(want)) {
		t.Errorf("round trip changed the theme:\n got %+v\nwant %+v", got, want)
	}
}

func closeEnough(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Float64:
		d := a.Float() - b.Float()
		return d < 0.001 && d > -0.001
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !closeEnough(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Array:
		for i := 0; i < a.Len(); i++ {
			if !closeEnough(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		return a.Len() == b.Len()
	}
	return reflect.DeepEqual(a.Interface(), b.Interface())
}

func TestParseLength(t *testing.T) {
	for in, want := range map[any]float64{int64(12): 12, 10.5: 10.5, "12": 12, "12pt": 12, "1in": 72, "25.4mm": 72, "2.54cm": 72, "16px": 12, " 1 IN ": 72} {
		got, err := parseLength(in)
		if err != nil || got < want-0.001 || got > want+0.001 {
			t.Errorf("parseLength(%v) = %g, %v; want %g", in, got, err, want)
		}
	}
	for _, bad := range []any{"wide", "12em", true, "", "1 2"} {
		if _, err := parseLength(bad); err == nil {
			t.Errorf("parseLength(%v) accepted", bad)
		}
	}
}

func TestHeaderAndFooterSettings(t *testing.T) {
	dir := t.TempDir()
	set, err := Loader{}.Load(writeTheme(t, dir, "hf.toml", `
[header]
left = "{title}"
rule = true

[footer]
center = "{page} / {pages}"
size = 8
color = "#777777"
`))
	if err != nil {
		t.Fatal(err)
	}
	for name, th := range map[string]Theme{"pdf": set.PDF, "docx": set.DOCX} {
		if th.Header.Left != "{title}" || !th.Header.Rule || th.Header.Size != 9 {
			t.Errorf("%s header: %+v", name, th.Header)
		}
		if th.Footer.Center != "{page} / {pages}" || th.Footer.Size != 8 || th.Footer.Color != (Color{0x77, 0x77, 0x77}) {
			t.Errorf("%s footer: %+v", name, th.Footer)
		}
	}
	if !Default().PDF.Header.Empty() || !Default().DOCX.Footer.Empty() {
		t.Error("the default theme has a header or footer")
	}

	// A margin too small to hold the text is refused, with the fix named.
	_, err = Loader{}.Load(writeTheme(t, dir, "tight.toml", "[page]\nmargin = \"5mm\"\n\n[footer]\ncenter = \"{page}\"\n"))
	if err == nil || !strings.Contains(err.Error(), "page.margin-bottom") {
		t.Errorf("a footer in a 5mm margin: %v", err)
	}
}
