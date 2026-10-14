package theme

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

// contrast is the WCAG contrast ratio between two colors.
func contrast(a, b Color) float64 {
	lum := func(c Color) float64 {
		channel := func(v uint8) float64 {
			x := float64(v) / 255
			if x <= 0.03928 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
	}
	hi, lo := lum(a), lum(b)
	if lo > hi {
		hi, lo = lo, hi
	}
	return (hi + 0.05) / (lo + 0.05)
}

func TestDefaultClassificationLevels(t *testing.T) {
	for name, th := range map[string]Theme{"pdf": Default().PDF, "docx": Default().DOCX} {
		cls := th.Classification
		if !cls.Band || cls.Size <= 0 || cls.Height < cls.Size {
			t.Errorf("%s: classification %+v", name, cls)
		}
		if got, want := cls.LevelNames(), []string{"confidential", "internal", "public", "secret"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: levels %v, want %v", name, got, want)
		}
		if c := contrast(cls.Color, cls.Background); c < 4.5 {
			t.Errorf("%s: the neutral colors have a contrast of %.2f", name, c)
		}
		seen := map[Color]string{}
		for _, level := range cls.LevelNames() {
			l := cls.Levels[level]
			if c := contrast(l.Color, l.Background); c < 4.5 {
				t.Errorf("%s: %s has a contrast of %.2f", name, level, c)
			}
			if other, dup := seen[l.Background]; dup || l.Background == cls.Background {
				t.Errorf("%s: %s has the background of %s or of the neutral band", name, level, other)
			}
			seen[l.Background] = level
		}
		if c := contrast(th.Text.Color, th.Watermark.Color); c < 7 {
			t.Errorf("%s: text over the watermark has a contrast of %.2f", name, c)
		}
	}
}

func TestClassificationFind(t *testing.T) {
	cls := Default().PDF.Classification
	for _, tc := range []struct {
		value, level string // level "" for none
	}{
		{"Public", "public"}, {"PUBLIC", "public"}, {"nyilvános", "public"}, {"Nyilvános", "public"},
		{"internal", "internal"}, {"  Internal ", "internal"}, {"Belső", "internal"}, {"belső használatra", "internal"},
		{"Belső használatra", "internal"}, {"BELSŐ   HASZNÁLATRA", "internal"}, {"\tBelső használatra\n", "internal"},
		{"Confidential", "confidential"}, {"Bizalmas", "confidential"},
		{"Secret", "secret"}, {"Titkos", "secret"}, {"Szigorúan titkos", "secret"}, {"SZIGORÚAN TITKOS", "secret"},
		{"", ""}, {"   ", ""}, {"Top secret", ""}, {"belso", ""}, {"Belső használatra!", ""},
	} {
		level, ok := cls.Find(tc.value)
		if ok != (tc.level != "") || ok && !reflect.DeepEqual(level, cls.Levels[tc.level]) {
			t.Errorf("Find(%q) = %v, %v; want level %q", tc.value, level, ok, tc.level)
		}
	}
}

func TestClassificationSettings(t *testing.T) {
	base := Default().PDF.Classification
	set, err := Loader{}.Load(writeTheme(t, t.TempDir(), "levels.toml", `
[classification]
size       = 9
height     = 14
background = "#444444"

[classification.confidential]
names = ["Reserved", "bizalmas"]
background = "#112233"

[classification.restricted]
names = ["Restricted", "Korlátozott"]
color = "#000000"

[pdf.classification.secret]
names = ["Eyes only"]

[docx.classification]
band = false

[watermark]
color = "#DDDDDD"
size  = 80
`))
	if err != nil {
		t.Fatal(err)
	}

	cls := set.PDF.Classification
	if !cls.Band || cls.Size != 9 || cls.Height != 14 || cls.Background != Hex("444444") || cls.Color != White {
		t.Errorf("classification %+v", cls)
	}
	conf := cls.Levels["confidential"]
	if want := []string{"confidential", "bizalmas", "Reserved"}; !reflect.DeepEqual(conf.Names, want) {
		t.Errorf("confidential has the names %v, want those of the default and Reserved added", conf.Names)
	}
	if conf.Background != Hex("112233") || conf.Color != base.Levels["confidential"].Color {
		t.Errorf("confidential: %+v", conf)
	}
	// A level the theme adds has the neutral colors where it sets none
	if got := cls.Levels["restricted"]; got.Color != Hex("000000") || got.Background != Hex("444444") || len(got.Names) != 2 {
		t.Errorf("restricted: %+v", got)
	}
	for value, level := range map[string]string{"RESERVED": "confidential", "korlátozott": "restricted", "Eyes  Only": "secret", "Bizalmas": "confidential", "Titkos": "secret"} {
		if got, ok := cls.Find(value); !ok || !reflect.DeepEqual(got, cls.Levels[level]) {
			t.Errorf("Find(%q) = %+v, %v; want %s", value, got, ok, level)
		}
	}
	if got, want := cls.LevelNames(), []string{"confidential", "internal", "public", "restricted", "secret"}; !reflect.DeepEqual(got, want) {
		t.Errorf("levels %v, want %v", got, want)
	}

	// [docx.classification] touches DOCX only, and what the theme extends is
	// left as it was
	if set.DOCX.Classification.Band {
		t.Error("band = false in [docx.classification] did not reach DOCX")
	}
	if _, ok := set.DOCX.Classification.Levels["secret"]; !ok || len(set.DOCX.Classification.Levels["secret"].Names) != 3 {
		t.Errorf("DOCX secret: %+v", set.DOCX.Classification.Levels["secret"])
	}
	if got := Default().PDF.Classification; !reflect.DeepEqual(got, base) || len(base.Levels) != 4 || len(base.Levels["confidential"].Names) != 2 {
		t.Errorf("the default theme changed: %+v", got)
	}

	if w := set.PDF.Watermark; w.Color != Hex("DDDDDD") || w.Size != 80 {
		t.Errorf("watermark %+v", w)
	}
	if w := Default().PDF.Watermark; w.Color != Hex("E6E6E6") || w.Size != 96 {
		t.Errorf("default watermark %+v", w)
	}
}

func TestClassificationErrors(t *testing.T) {
	for text, want := range map[string]string{
		"[classification]\nband = \"no\"\n":                      "classification.band must be true or false",
		"[classification]\nsize = 0\n":                           "classification.size is 0 points",
		"[classification]\nheight = 500\n":                       "classification.height is 500 points",
		"[classification]\nbackground = 5\n":                     "classification.background: a color is written in quotes",
		"[classification.secret]\nnames = \"secret\"\n":          "classification.secret.names must be a list of text",
		"[classification.secret]\nnames = [1]\n":                 "classification.secret.names must be a list of text",
		"[classification.secret]\nnames = [\" \"]\n":             "classification.secret.names must be a list of text",
		"[classification.secret]\ncolour = \"#000\"\n":           "classification.secret.colour is not a setting", //nolint:misspell // the British spelling is the unknown setting being tested
		"[classification.secret]\ncolor = \"red\"\n":             "classification.secret.color",
		"[classification.secret]\nbackground = 1\n":              "classification.secret.background: a color is written in quotes",
		"[classification.extra]\ncolor = \"#000\"\n":             "classification.extra needs names",
		"[classification.internal]\nnames = [\"  BIZALMAS \"]\n": `"  BIZALMAS " is a name of both confidential and internal`,
		"[classification]\nlevel = 3\n":                          "classification.level is not a setting",
		"[watermark]\nsize = 0\n":                                "watermark.size is 0 points",
		"[watermark]\ncolor = \"light\"\n":                       "watermark.color",
		"[watermark.x]\nsize = 1\n":                              "watermark.x is not a setting",
		"[pdf.classification.extra]\nnames = [\"secret\"]\n":     `is a name of both extra and secret`,
	} {
		_, err := Loader{}.Load(writeTheme(t, t.TempDir(), "bad.toml", text))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", text, err, want)
		}
	}
}

// The levels of a theme are exported, DOCX ones where they differ, and
// loading the export gives the same levels back.
func TestExportRoundTripsClassification(t *testing.T) {
	dir := t.TempDir()
	want, err := Loader{}.Load(writeTheme(t, dir, "levels.toml", `
[classification]
band = false

[classification.restricted]
names = ["Restricted", "Korlátozott"]
background = "#445566"

[docx.classification.public]
background = "#00AA00"
`))
	if err != nil {
		t.Fatal(err)
	}
	text := Export(want)
	for _, must := range []string{"[classification]", "[classification.secret]", `names      = ["secret", "titkos", "szigorúan titkos"]`,
		"[classification.restricted]", "[docx.classification.public]", "[watermark]"} {
		if !strings.Contains(text, must) {
			t.Errorf("export lacks %q", must)
		}
	}
	if n := strings.Count(text, "[docx.classification."); n != 1 {
		t.Errorf("%d levels are exported for DOCX, want only the one that differs", n)
	}

	got, err := Loader{}.Load(writeTheme(t, dir, "copy.toml", text))
	if err != nil {
		t.Fatalf("the exported theme does not load: %v\n%s", err, text)
	}
	got.PDF.Name, got.DOCX.Name = want.PDF.Name, want.DOCX.Name
	if !closeEnough(reflect.ValueOf(got), reflect.ValueOf(want)) {
		t.Errorf("round trip changed the theme:\n got %+v\nwant %+v", got.PDF.Classification, want.PDF.Classification)
	}
}
