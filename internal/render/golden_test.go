package render

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// The golden tests convert every sample document and compare what was
// produced with a recording under testdata/golden: for DOCX an outline of the
// document XML (paragraphs, runs with their formatting, tables, images), for
// PDF the trace of everything drawn (text, position, font, boxes, lines).
//
// A failure means the output changed. If the change is intended, record the
// new output and review the diff like any other change:
//
//	go test ./internal/render -run Golden -update
var update = flag.Bool("update", false, "rewrite the golden files with the current output")

const fixturesDir = "../../fixtures"

// goldenFixtures lists the sample documents with the flavor each is read as:
// the ones under flavors/ by their file name, the rest with the default.
func goldenFixtures(t *testing.T) map[string]flavor.Flavor {
	t.Helper()
	out := map[string]flavor.Flavor{}
	files, _ := filepath.Glob(filepath.Join(fixturesDir, "flavors", "*.md"))
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".md")
		if fl, ok := flavor.ByID(id); ok {
			out[f] = fl
		}
	}
	top, _ := filepath.Glob(filepath.Join(fixturesDir, "*.md"))
	for _, f := range top {
		out[f] = flavor.Default()
	}
	if len(out) < 10 {
		t.Fatalf("found only %d fixtures under %s", len(out), fixturesDir)
	}
	return out
}

func TestGoldenOutput(t *testing.T) {
	for path, fl := range goldenFixtures(t) {
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := parse.ParseFlavor(src, fl, filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			// Remote images stay off: the output must not depend on the network.
			opts := Options{BaseDir: filepath.Dir(path)}
			dir := t.TempDir()

			docxPath := filepath.Join(dir, "out.docx")
			if err := RenderDocx(doc, docxPath, opts); err != nil {
				t.Fatalf("render DOCX: %v", err)
			}
			checkGolden(t, name+".docx.txt", docxOutline(t, docxPath))

			pdf := NewPdfRenderer()
			pdf.opts = opts
			pdf.trace = &strings.Builder{}
			if err := pdf.RenderToFile(doc, filepath.Join(dir, "out.pdf")); err != nil {
				t.Fatalf("render PDF: %v", err)
			}
			checkGolden(t, name+".pdf.txt", pdf.trace.String())
		})
	}
}

// The built-in themes are recorded too, on the showcase document, so a change
// to a theme or to how the renderers apply one shows up.
func TestGoldenThemes(t *testing.T) {
	path := filepath.Join(fixturesDir, "showcase.md")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse.ParseFlavor(src, flavor.Default(), filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range (theme.Loader{}).List() {
		if info.Name == theme.DefaultName {
			continue // recorded by TestGoldenOutput
		}
		t.Run(info.Name, func(t *testing.T) {
			set, err := theme.Loader{}.Load(info.Name)
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{BaseDir: filepath.Dir(path), Theme: &set}
			dir := t.TempDir()

			docxPath := filepath.Join(dir, "out.docx")
			if err := RenderDocx(doc, docxPath, opts); err != nil {
				t.Fatalf("render DOCX: %v", err)
			}
			checkGolden(t, "theme-"+info.Name+".docx.txt", docxOutline(t, docxPath))

			pdf := NewPdfRenderer()
			pdf.opts = opts
			pdf.trace = &strings.Builder{}
			if err := pdf.RenderToFile(doc, filepath.Join(dir, "out.pdf")); err != nil {
				t.Fatalf("render PDF: %v", err)
			}
			checkGolden(t, "theme-"+info.Name+".pdf.txt", pdf.trace.String())
		})
	}
}

// checkGolden compares got with the recorded file, or rewrites it with -update.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no recording for %s (run with -update to create it): %v", name, err)
	}
	want := strings.ReplaceAll(string(data), "\r\n", "\n")
	if got == want {
		return
	}
	t.Errorf("%s differs from the recording (rerun with -update if the change is intended)\n%s", name, firstDifference(want, got))
}

// firstDifference shows the first lines where two outputs part ways.
func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("line %d (of %d recorded, %d produced):\n  recorded: %s\n  produced: %s", i+1, len(w), len(g), wl, gl)
		}
	}
	return ""
}

// docxOutline reduces a DOCX file to a text outline of its body: one line per
// paragraph, run, table, row and cell, with the formatting that affects how
// it looks.
func docxOutline(t *testing.T, path string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	read := func(name string) []byte {
		for _, f := range zr.File {
			if f.Name == name {
				rc, err := f.Open()
				if err != nil {
					t.Fatal(err)
				}
				defer rc.Close()
				data, err := io.ReadAll(rc)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
		}
		return nil
	}

	// Hyperlinks are stored as relationship ids; show where they point.
	targets := map[string]string{}
	relDec := xml.NewDecoder(bytes.NewReader(read("word/_rels/document.xml.rels")))
	for {
		tok, err := relDec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
			targets[attr(se, "Id")] = attr(se, "Target")
		}
	}

	body := read("word/document.xml")
	if body == nil {
		t.Fatal("word/document.xml not found")
	}

	var out strings.Builder
	depth := 0
	line := func(format string, args ...any) {
		out.WriteString(strings.Repeat("  ", depth))
		fmt.Fprintf(&out, format, args...)
		out.WriteByte('\n')
	}

	// Properties are collected while a paragraph, run or cell is open and
	// printed with it.
	var paraProps, runProps, cellProps []string
	var runText strings.Builder
	paraOpen, inRun, inText, inRunProps, inParaProps, inCellProps := false, false, false, false, false, false
	link := ""

	flushPara := func() {
		if paraOpen {
			return
		}
		paraOpen = true
		line("P%s", props(paraProps))
	}

	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("document.xml: %v", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch name := el.Name.Local; name {
			case "tbl":
				line("TABLE")
				depth++
			case "gridCol":
				line("COL w=%s", attr(el, "w"))
			case "tr":
				line("ROW")
				depth++
			case "tc":
				cellProps = nil
			case "tcPr":
				inCellProps = true
			case "p":
				paraProps, paraOpen = nil, false
			case "pPr":
				inParaProps = true
			case "hyperlink":
				link = targets[attr(el, "id")]
				if anchor := attr(el, "anchor"); anchor != "" {
					link = "#" + anchor
				}
			case "bookmarkStart":
				paraProps = append(paraProps, "bookmark="+attr(el, "name"))
			case "numId":
				paraProps = append(paraProps, "list="+attr(el, "val"))
			case "ilvl":
				paraProps = append(paraProps, "level="+attr(el, "val"))
			case "fldChar":
				flushPara()
				depth++
				line("FIELD %s", attr(el, "fldCharType"))
				depth--
			case "r":
				inRun, runProps = true, nil
				runText.Reset()
			case "rPr":
				inRunProps = true
			case "t", "instrText":
				inText = inRun
			case "br":
				if inRun {
					runText.WriteString("<br>")
				}
			case "tab":
				if inRun {
					runText.WriteString("<tab>")
				}
			case "extent":
				flushPara()
				depth++
				line("IMAGE cx=%s cy=%s", attr(el, "cx"), attr(el, "cy"))
				depth--
			case "cantSplit", "tblHeader":
				line("%s", name)
			case "tblInd":
				line("INDENT %s", attr(el, "w"))
			default:
				p := property(el)
				switch {
				case p == "":
				case inRunProps:
					runProps = append(runProps, p)
				case inParaProps:
					paraProps = append(paraProps, p)
				case inCellProps:
					cellProps = append(cellProps, p)
				}
			}

		case xml.EndElement:
			switch el.Name.Local {
			case "tbl", "tr", "tc":
				depth--
			case "tcPr":
				inCellProps = false
				line("CELL%s", props(cellProps))
				depth++
			case "pPr":
				inParaProps = false
			case "p":
				flushPara()
			case "hyperlink":
				link = ""
			case "rPr":
				inRunProps = false
			case "t", "instrText":
				inText = false
			case "r":
				inRun = false
				if runText.Len() == 0 {
					break
				}
				flushPara()
				if link != "" {
					runProps = append(runProps, "link="+link)
				}
				depth++
				line("R%s %q", props(runProps), runText.String())
				depth--
			}

		case xml.CharData:
			if inText {
				runText.Write(el)
			}
		}
	}
	out.WriteString(stylesOutline(t, read("word/styles.xml")))
	out.WriteString(listsOutline(t, read("word/numbering.xml")))
	return out.String()
}

// stylesOutline lists the styles the renderer defines from the theme, with
// what they give the text.
func stylesOutline(t *testing.T, styles []byte) string {
	t.Helper()
	want := map[string]bool{"Normal": true, "Hyperlink": true, "TOC1": true, "TOC2": true}
	for i := 1; i <= 6; i++ {
		want[fmt.Sprintf("Heading%d", i)] = true
	}
	var lines []string
	var id string
	var found []string
	dec := xml.NewDecoder(bytes.NewReader(styles))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("styles.xml: %v", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch name := el.Name.Local; {
			case name == "style":
				id, found = attr(el, "styleId"), nil
			case !want[id]:
			case name == "keepNext" || name == "keepLines":
				found = append(found, name)
			case name == "outlineLvl":
				found = append(found, "outline="+attr(el, "val"))
			default:
				if p := property(el); p != "" {
					found = append(found, p)
				}
			}
		case xml.EndElement:
			if el.Name.Local == "style" {
				if want[id] {
					lines = append(lines, fmt.Sprintf("STYLE %s%s\n", id, props(found)))
				}
				id = ""
			}
		}
	}
	// The file has them in no particular order
	sort.Strings(lines)
	return strings.Join(lines, "")
}

// listsOutline lists the lists of numbering.xml and the first levels of the
// two definitions they are made from.
func listsOutline(t *testing.T, numbering []byte) string {
	t.Helper()
	if numbering == nil {
		return ""
	}
	var out strings.Builder
	var level, format, text, indent, num string
	inAbstract, restarts := false, false
	dec := xml.NewDecoder(bytes.NewReader(numbering))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("numbering.xml: %v", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "abstractNum":
				inAbstract = true
				fmt.Fprintf(&out, "LIST DEFINITION %s\n", attr(el, "abstractNumId"))
			case "lvl":
				level = attr(el, "ilvl")
			case "numFmt":
				format = attr(el, "val")
			case "lvlText":
				text = attr(el, "val")
			case "ind":
				indent = attr(el, "left") + "/" + attr(el, "hanging")
			case "num":
				num, restarts = attr(el, "numId"), false
			case "startOverride":
				restarts = true
			case "abstractNumId":
				if !inAbstract {
					num += " definition=" + attr(el, "val")
				}
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "abstractNum":
				inAbstract = false
			case "lvl":
				if level < "3" {
					fmt.Fprintf(&out, "  LEVEL %s %s %q indent=%s\n", level, format, text, indent)
				}
			case "num":
				if restarts {
					num += " restarts"
				}
				fmt.Fprintf(&out, "LIST %s\n", num)
			}
		}
	}
	return out.String()
}

// property renders a formatting element as "name=value", or "" for elements
// that do not affect the outline.
func property(el xml.StartElement) string {
	switch name := el.Name.Local; name {
	case "b", "i", "strike", "caps", "smallCaps":
		if v := attr(el, "val"); v == "false" || v == "0" {
			return ""
		}
		return name
	case "u", "highlight", "color", "jc", "vertAlign", "pStyle", "rStyle", "sz":
		return name + "=" + attr(el, "val")
	case "rFonts":
		return "font=" + attr(el, "ascii")
	case "shd":
		return "fill=" + attr(el, "fill")
	case "tcW":
		return "w=" + attr(el, "w")
	case "ind":
		if right := attr(el, "right"); right != "" && right != "0" {
			return "indent=" + attr(el, "left") + "/" + right
		}
		return "indent=" + attr(el, "left")
	case "spacing":
		out := "spacing=" + attr(el, "before") + "/" + attr(el, "after")
		if line := attr(el, "line"); line != "" {
			out += " line=" + line + "/" + attr(el, "lineRule")
		}
		return out
	case "top", "left", "bottom", "right":
		return fmt.Sprintf("border-%s=%s/%s/%s", name, attr(el, "val"), attr(el, "sz"), attr(el, "color"))
	}
	return ""
}

func props(p []string) string {
	if len(p) == 0 {
		return ""
	}
	p = append([]string(nil), p...)
	sort.Strings(p)
	return "{" + strings.Join(p, " ") + "}"
}

func attr(el xml.StartElement, name string) string {
	for _, a := range el.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
