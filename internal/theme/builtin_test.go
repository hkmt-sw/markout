package theme

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// The themes that ship with markout must load, be described, and look the
// same in both formats where the formats can agree.
func TestBuiltinThemes(t *testing.T) {
	want := []string{"default", "classic", "compact", "modern", "report"}
	var got []string
	for _, info := range (Loader{}).List() {
		got = append(got, info.Name)
		if info.Description == "" || strings.HasPrefix(info.Description, "cannot be used") {
			t.Errorf("%s: description %q", info.Name, info.Description)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("built-in themes are %v, want %v", got, want)
	}

	def := Default()
	for _, name := range want[1:] {
		set, err := Loader{}.Load(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if set.PDF.Name != name || reflect.DeepEqual(set.PDF.Text, def.PDF.Text) {
			t.Errorf("%s does not differ from the default", name)
		}

		// Sizes, spacing and colors are the same in PDF and DOCX; only the
		// font names and the exact paper width are format-specific.
		pdf, docx := set.PDF, set.DOCX
		docx.Fonts, docx.Page.Width, docx.Page.Height = pdf.Fonts, pdf.Page.Width, pdf.Page.Height
		if !reflect.DeepEqual(pdf, docx) {
			t.Errorf("%s looks different in PDF and DOCX:\n pdf  %+v\n docx %+v", name, pdf, docx)
		}
		for _, font := range []string{set.DOCX.Fonts.Body, set.DOCX.Fonts.Heading, set.DOCX.Fonts.Code} {
			if _, isKey := BuiltinFonts[font]; isKey || font == "" {
				t.Errorf("%s: DOCX font %q is not a font name", name, font)
			}
		}
	}
}

// The guide documents every section and setting a theme file can have.
func TestGuideCoversEverySetting(t *testing.T) {
	data, err := os.ReadFile("../../docs/themes.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := string(data)

	var check func(t reflect.Type, section string)
	check = func(rt reflect.Type, section string) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			key := f.Tag.Get("key")
			if key == "" || key == "-" {
				continue
			}
			ft := f.Type
			if ft.Kind() == reflect.Array {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && ft != reflect.TypeOf(Color{}) {
				name := key
				if section != "" {
					name = section + "." + key
				}
				if !strings.Contains(guide, "["+name) {
					t.Errorf("the guide does not mention the section [%s]", name)
				}
				check(ft, name)
				continue
			}
			if !strings.Contains(guide, "`"+key+"`") {
				t.Errorf("the guide does not mention the setting %s in [%s]", key, section)
			}
		}
	}
	check(reflect.TypeOf(Theme{}), "")

	for _, other := range []string{"`extends`", "`description`", "`size`", "`orientation`", "`margin`", "[fonts.family.", "[pdf", "[docx", "{title}", "{author}", "{date}", "{page}", "{pages}", "--export-theme", "--list-themes"} {
		if !strings.Contains(guide, other) {
			t.Errorf("the guide does not mention %s", other)
		}
	}
}
