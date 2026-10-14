package theme

import (
	"reflect"
	"strings"
	"testing"
)

// A theme can give the callouts and the table of contents words of its own.
func TestAlertLabelAndTOCTitleSettings(t *testing.T) {
	dir := t.TempDir()
	set, err := Loader{}.Load(writeTheme(t, dir, "words.toml", `
[alert.note]
label = "Megjegyzés"

[alert.warning]
label = "Danger"

[toc]
title = "Contents"

[docx.toc]
title = "Tartalom"
`))
	if err != nil {
		t.Fatal(err)
	}
	a := set.PDF.Alert
	if a.Note.Label != "Megjegyzés" || a.Warning.Label != "Danger" || a.Tip.Label != "" || a.Important.Label != "" || a.Caution.Label != "" {
		t.Errorf("alert labels: %+v", a)
	}
	if set.PDF.TOC.Title != "Contents" || set.DOCX.TOC.Title != "Tartalom" {
		t.Errorf("toc titles: pdf %q, docx %q", set.PDF.TOC.Title, set.DOCX.TOC.Title)
	}
	if d := Default().PDF; d.TOC.Title != "" || d.Alert.Note.Label != "" {
		t.Errorf("the default theme names the words: %+v %+v", d.TOC, d.Alert.Note)
	}

	// The settings are exported, and exporting what was loaded gives it back
	text := Export(set)
	for _, must := range []string{"[alert.note]", `"Megjegyzés"`, "[toc]", `"Contents"`, "[docx.toc]", `"Tartalom"`} {
		if !strings.Contains(text, must) {
			t.Errorf("export lacks %q:\n%s", must, text)
		}
	}
	got, err := Loader{}.Load(writeTheme(t, dir, "words-copy.toml", text))
	if err != nil {
		t.Fatalf("the exported theme does not load: %v\n%s", err, text)
	}
	got.PDF.Name, got.DOCX.Name = set.PDF.Name, set.DOCX.Name
	if !closeEnough(reflect.ValueOf(got), reflect.ValueOf(set)) {
		t.Errorf("round trip changed the theme:\n got %+v\nwant %+v", got.PDF.Alert, set.PDF.Alert)
	}
}
