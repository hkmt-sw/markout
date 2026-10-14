package parse

import (
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
)

// A callout says whether its title is the default of its type, which a
// renderer may translate, or words of the author, which it must not.
func TestAlertDefaultTitle(t *testing.T) {
	for _, tc := range []struct {
		name, flavorID, src, title string
		def                        bool
	}{
		{"GitHub alert", "github", "> [!NOTE]\n> text\n", "NOTE", true},
		{"lower case", "gitlab", "> [!warning]\n> text\n", "WARNING", true},
		{"own title", "gitlab", "> [!NOTE] My title\n> text\n", "My title", false},
		{"own title equal to the default", "gitlab", "> [!NOTE] NOTE\n> text\n", "NOTE", false},
		{"admonition", "mkdocs", "!!! tip\n    text\n", "TIP", true},
		{"admonition with a title", "mkdocs", "!!! tip \"Try this\"\n    text\n", "Try this", false},
		{"other name", "mkdocs", "!!! info\n    text\n", "Info", false},
	} {
		fl, ok := flavor.ByID(tc.flavorID)
		if !ok {
			t.Fatalf("no flavor %s", tc.flavorID)
		}
		doc, err := ParseFlavor([]byte(tc.src), fl, "")
		if err != nil {
			t.Fatal(err)
		}
		var alert *ast.Alert
		for _, e := range doc.Elements {
			if a, ok := e.(ast.Alert); ok {
				alert = &a
			}
		}
		if alert == nil {
			t.Errorf("%s: no callout in %v", tc.name, doc.Elements)
			continue
		}
		if alert.Title != tc.title || alert.DefaultTitle != tc.def {
			t.Errorf("%s: title %q default %v, want %q %v", tc.name, alert.Title, alert.DefaultTitle, tc.title, tc.def)
		}
	}
}
