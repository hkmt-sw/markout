package parse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
)

// A byte order mark is not part of the text: front matter after one is
// read, in every flavor that reads it, and the text is the same without it.
func TestByteOrderMark(t *testing.T) {
	const bom = "\uFEFF"
	for _, tt := range []struct {
		name   string
		flavor string
		src    string
		title  string // the title in the front matter, or ""
		shown  bool   // whether the document has front matter
	}{
		{"YAML", "markout", bom + "---\ntitle: T\ncover: true\n---\n# H\n\nx\n", "T", true},
		{"YAML in GitHub", "github", bom + "---\ntitle: T\n---\n# H\n\nx\n", "T", true},
		{"YAML and CRLF", "markout", bom + "---\r\ntitle: T\r\n---\r\n# H\r\n\r\nx\r\n", "T", true},
		{"TOML", "markout", bom + "+++\ntitle = \"T\"\n+++\n# H\n\nx\n", "T", true},
		{"JSON", "markout", bom + ";;;\n{\"title\": \"T\"}\n;;;\n# H\n\nx\n", "T", true},
		{"MultiMarkdown", "multimarkdown", bom + "Title: T\nAuthor: A\n\n# H\n\nx\n", "T", true},
		{"Pandoc title block", "pandoc", bom + "% T\n% A\n\n# H\n\nx\n", "T", true},
		{"no front matter", "markout", bom + "# H\n\nx\n", "", false},
		{"no front matter and CRLF", "commonmark", bom + "# H\r\n\r\nx\r\n", "", false},
	} {
		doc := parseDoc(t, tt.flavor, tt.src)
		var title string
		shown := false
		for _, e := range doc.Elements {
			if fm, ok := e.(ast.FrontMatter); ok {
				shown, title = true, fm.Title
			}
		}
		if shown != tt.shown || title != tt.title {
			t.Errorf("%s: front matter %v with title %q, want %v with %q", tt.name, shown, title, tt.shown, tt.title)
		}
		// The text is what it is without the mark
		plain := parseDoc(t, tt.flavor, strings.TrimPrefix(tt.src, bom))
		if got, want := allText(doc.Elements), allText(plain.Elements); got != want {
			t.Errorf("%s: text %q, want %q", tt.name, got, want)
		}
		if len(doc.Warnings) != 0 {
			t.Errorf("%s: warnings %q", tt.name, doc.Warnings)
		}
	}
}

// A byte order mark at the start of an included file is dropped as well.
func TestByteOrderMarkInInclude(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part.md"), []byte("\uFEFF# Part\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fl, _ := flavor.ByID("gitlab")
	doc, err := ParseFlavor([]byte("::include{file=part.md}\n"), fl, dir)
	if err != nil {
		t.Fatal(err)
	}
	if text := allText(doc.Elements); strings.Contains(text, "\uFEFF") || !strings.Contains(text, "Part") {
		t.Errorf("text %q", text)
	}
	headings := 0
	for _, e := range doc.Elements {
		if h, ok := e.(ast.Heading); ok {
			headings++
			if runsText(h.Runs) != "Part" {
				t.Errorf("heading %q", runsText(h.Runs))
			}
		}
	}
	if headings != 1 {
		t.Errorf("%d headings, want 1 (a mark in front of the # would make it text)", headings)
	}
}

// A flavor that does not read front matter shows it as text, and the
// conversion says so.
func TestUnreadFrontMatterWarning(t *testing.T) {
	const yaml = "---\ntitle: T\ntags:\n  - a\n  - b\n---\n# H\n\nx\n"
	for _, tt := range []struct {
		name   string
		flavor string
		src    string
		warned bool
	}{
		{"commonmark", "commonmark", yaml, true},
		{"youtrack", "youtrack", yaml, true},
		{"bitbucket", "bitbucket", yaml, true},
		{"extra", "extra", yaml, true},
		{"TOML", "commonmark", "+++\ntitle = \"T\"\n\n[extra]\nkey = 1\n+++\n# H\n", true},
		{"after a byte order mark", "commonmark", "\uFEFF" + yaml, true},
		{"CRLF", "commonmark", strings.ReplaceAll(yaml, "\n", "\r\n"), true},
		{"list in the metadata", "extra", "---\nlist:\n- a\n- b\n---\ntext\n", true},
		{"markout reads it", "markout", yaml, false},
		{"github reads it", "github", yaml, false},
		{"pandoc reads it", "pandoc", yaml, false},
		{"kramdown reads it", "kramdown", yaml, false},
		{"rule and prose", "commonmark", "---\n\nSome prose, which is not metadata.\n\n---\n\ntext\n", false},
		{"rule and a sentence with a colon", "commonmark", "---\nSee the note below: it matters\n---\ntext\n", false},
		{"rule and a key and prose", "commonmark", "---\ntitle: T\nand then some prose\n---\ntext\n", false},
		{"no closing line", "commonmark", "---\ntitle: T\n\n# H\n\ntext\n", false},
		{"nothing between", "commonmark", "---\n---\ntext\n", false},
		{"only blank lines between", "commonmark", "---\n\n---\ntext\n", false},
		{"not at the start", "commonmark", "text\n\n---\ntitle: T\n---\n", false},
		{"a key on the delimiter's line", "commonmark", "--- title: T\n---\n", false},
		{"TOML in a YAML frame", "commonmark", "---\ntitle = \"T\"\n---\ntext\n", false},
		{"JSON is left alone", "commonmark", ";;;\n{\"title\": \"T\"}\n;;;\ntext\n", false},
		{"empty", "commonmark", "", false},
	} {
		doc := parseDoc(t, tt.flavor, tt.src)
		var got []string
		for _, w := range doc.Warnings {
			if strings.Contains(w, "front matter") {
				got = append(got, w)
			}
		}
		if (len(got) > 0) != tt.warned {
			t.Errorf("%s: warnings %q, want warned: %v", tt.name, doc.Warnings, tt.warned)
			continue
		}
		if tt.warned {
			want := "The document starts with front matter, which the " + tt.flavor + " flavor does not read; it is shown as text. Choose a flavor that reads it, such as markout."
			if len(got) != 1 || got[0] != want {
				t.Errorf("%s: warning %q, want %q", tt.name, got, want)
			}
		}
	}
}
