package parse

import (
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
)

// The language a document names in its front matter is the document's, in
// its usual form. It is not a field that is shown, and front matter that
// holds nothing else shows nothing.
func TestFrontMatterLanguage(t *testing.T) {
	for _, tt := range []struct {
		name, flavor, src string
		lang              string
		shown             bool // the document has a front matter element
		warned            bool
	}{
		{"lang", "markout", "---\nlang: hu\n---\n\ntext\n", "hu", false, false},
		{"another form", "markout", "---\ntitle: T\nlang: en_gb\n---\n\ntext\n", "en-GB", true, false},
		{"language", "markout", "---\ntitle: T\nlanguage: en-US\n---\n\ntext\n", "en-US", true, false},
		{"lang before language", "markout", "---\nlanguage: en\nlang: hu\n---\n\ntext\n", "hu", false, false},
		{"TOML", "markout", "+++\nlang = \"hu\"\n+++\n\ntext\n", "hu", false, false},
		{"MultiMarkdown", "multimarkdown", "Title: T\nLanguage: hu\n\ntext\n", "hu", true, false},
		{"not a tag", "markout", "---\ntitle: T\nlang: magyar\n---\n\ntext\n", "", true, true},
		{"not text", "markout", "---\ntitle: T\nlang: [hu, en]\n---\n\ntext\n", "", true, false},
		{"none", "markout", "---\ntitle: T\n---\n\ntext\n", "", true, false},
		{"no front matter", "markout", "text\n", "", false, false},
	} {
		fl, _ := flavor.ByID(tt.flavor)
		doc, err := ParseFlavor([]byte(tt.src), fl, "")
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if doc.Lang != tt.lang {
			t.Errorf("%s: language %q, want %q", tt.name, doc.Lang, tt.lang)
		}
		shown := false
		for _, e := range doc.Elements {
			if fm, ok := e.(ast.FrontMatter); ok {
				shown = true
				for _, key := range []string{"lang", "language"} {
					if _, ok := fm.Raw[key].(string); ok {
						t.Errorf("%s: %s is among the fields shown", tt.name, key)
					}
				}
			}
		}
		if shown != tt.shown {
			t.Errorf("%s: front matter shown: %v, want %v", tt.name, shown, tt.shown)
		}
		if warned := strings.Contains(strings.Join(doc.Warnings, "\n"), "is not a language tag"); warned != tt.warned {
			t.Errorf("%s: warned: %v, want %v (%q)", tt.name, warned, tt.warned, doc.Warnings)
		}
	}
}
