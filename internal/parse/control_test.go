package parse

import (
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
)

// Numbers in YAML front matter are kept as they were written, so that
// version 1.0 is not shown as 1. TOML front matter has no such record.
func TestFrontMatterNumbersAsWritten(t *testing.T) {
	for _, tt := range []struct {
		name, src string
		want      map[string]string
	}{
		{"YAML", "---\nversion: 1.0\nrevision: 007\nscore: 2.50\ntitle: T\nlist: [1, 2]\n---\n\ntext\n",
			map[string]string{"version": "1.0", "revision": "007", "score": "2.50"}},
		{"quoted", "---\nversion: \"1.0\"\n---\n\ntext\n", nil},
		{"TOML", "+++\nversion = 1.0\n+++\n\ntext\n", nil},
	} {
		doc, err := Parse([]byte(tt.src))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		var fm ast.FrontMatter
		for _, e := range doc.Elements {
			if f, ok := e.(ast.FrontMatter); ok {
				fm = f
			}
		}
		if len(fm.Written) != len(tt.want) {
			t.Errorf("%s: written %v, want %v", tt.name, fm.Written, tt.want)
			continue
		}
		for k, v := range tt.want {
			if fm.Written[k] != v {
				t.Errorf("%s: %s written as %q, want %q", tt.name, k, fm.Written[k], v)
			}
		}
	}
}
