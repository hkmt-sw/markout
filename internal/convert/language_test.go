package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What a document says of its language goes before what the conversion is
// told; a document that says nothing takes that.
func TestLanguageOfTheDocumentGoesFirst(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct{ name, src, option, want string }{
		{"document", "---\nlang: hu\n---\n\ntext\n", "en-GB", "/Lang (hu)"},
		{"option", "text\n", "en_gb", "/Lang (en-GB)"},
		{"neither", "text\n", "", ""},
		{"option that is not a language", "text\n", "English", ""},
	} {
		in, out := filepath.Join(dir, "in.md"), filepath.Join(dir, "out.pdf")
		if err := os.WriteFile(in, []byte(tt.src), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ConvertFileWith(in, out, FormatPDF, Options{Lang: tt.option}); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		data, _ := os.ReadFile(out)
		if tt.want == "" && strings.Contains(string(data), "/Lang") {
			t.Errorf("%s: the PDF names a language", tt.name)
		}
		if tt.want != "" && !strings.Contains(string(data), tt.want) {
			t.Errorf("%s: the PDF does not have %s", tt.name, tt.want)
		}
	}
}
