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

// Front matter is read in every flavor, also in those that leave it out
// (markout's cover, control block and the like are not the platform's): a
// document that starts with it has it as its first element. Anything that
// only looks like it stays text.
func TestFrontMatterInEveryFlavor(t *testing.T) {
	const yaml = "---\ntitle: T\ntags:\n  - a\n  - b\n---\n# H\n\nx\n"
	for _, tt := range []struct {
		name   string
		flavor string
		src    string
		read   bool
		title  string // the title in the front matter
		key    string // a key to look for instead, when there is no title
	}{
		{"commonmark", "commonmark", yaml, true, "T", ""},
		{"youtrack", "youtrack", yaml, true, "T", ""},
		{"bitbucket", "bitbucket", yaml, true, "T", ""},
		{"extra", "extra", yaml, true, "T", ""},
		{"TOML", "commonmark", "+++\ntitle = \"T\"\n\n[extra]\nkey = 1\n+++\n# H\n", true, "T", ""},
		{"after a byte order mark", "commonmark", "\uFEFF" + yaml, true, "T", ""},
		{"CRLF", "commonmark", strings.ReplaceAll(yaml, "\n", "\r\n"), true, "T", ""},
		{"accented keys", "commonmark", "---\ncím: Mentési eljárásrend\nszerző: Üzemeltetés\n---\n# H\n", true, "", "cím"},
		{"list in the metadata", "extra", "---\nlist:\n- a\n- b\n---\ntext\n", true, "", "list"},
		{"markout reads it", "markout", yaml, true, "T", ""},
		{"github reads it", "github", yaml, true, "T", ""},
		{"pandoc reads it", "pandoc", yaml, true, "T", ""},
		{"kramdown reads it", "kramdown", yaml, true, "T", ""},
		{"rule and prose", "commonmark", "---\n\nSome prose, which is not metadata.\n\n---\n\ntext\n", false, "", ""},
		{"rule and a sentence with a colon", "commonmark", "---\nSee the note below: it matters\n---\ntext\n", false, "", ""},
		{"rule and a key and prose", "commonmark", "---\ntitle: T\nand then some prose\n---\ntext\n", false, "", ""},
		{"no closing line", "commonmark", "---\ntitle: T\n\n# H\n\ntext\n", false, "", ""},
		{"nothing between", "commonmark", "---\n---\ntext\n", false, "", ""},
		{"only blank lines between", "commonmark", "---\n\n---\ntext\n", false, "", ""},
		{"not at the start", "commonmark", "text\n\n---\ntitle: T\n---\n", false, "", ""},
		{"a key on the delimiter's line", "commonmark", "--- title: T\n---\n", false, "", ""},
		{"TOML in a YAML frame", "commonmark", "---\ntitle = \"T\"\n---\ntext\n", false, "", ""},
		{"JSON is left alone", "commonmark", ";;;\n{\"title\": \"T\"}\n;;;\ntext\n", false, "", ""},
		{"empty", "commonmark", "", false, "", ""},
	} {
		doc := parseDoc(t, tt.flavor, tt.src)
		for _, w := range doc.Warnings {
			if strings.Contains(w, "front matter") {
				t.Errorf("%s: warning %q", tt.name, w)
			}
		}
		var fm *ast.FrontMatter
		for i, e := range doc.Elements {
			if f, ok := e.(ast.FrontMatter); ok {
				if i != 0 {
					t.Errorf("%s: front matter is element %d, want the first", tt.name, i)
				}
				fm = &f
			}
		}
		if !tt.read {
			if fm != nil {
				t.Errorf("%s: front matter %v, want none", tt.name, fm.Raw)
			}
			continue
		}
		if fm == nil {
			t.Errorf("%s: no front matter", tt.name)
			continue
		}
		if tt.key != "" {
			if _, ok := fm.Raw[tt.key]; !ok {
				t.Errorf("%s: no %q key in %v", tt.name, tt.key, fm.Raw)
			}
		} else if fm.Title != tt.title {
			t.Errorf("%s: title %q, want %q", tt.name, fm.Title, tt.title)
		}
	}
}
