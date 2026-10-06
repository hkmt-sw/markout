package theme

import "testing"

func TestParseColor(t *testing.T) {
	for in, want := range map[string]Color{
		"#1A1A2E": {26, 26, 46},
		"1a1a2e":  {26, 26, 46},
		"#FFF":    {255, 255, 255},
		"abc":     {170, 187, 204},
	} {
		got, err := ParseColor(in)
		if err != nil || got != want {
			t.Errorf("ParseColor(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "#12", "#12345", "red", "#GGGGGG", "#1234567"} {
		if _, err := ParseColor(bad); err == nil {
			t.Errorf("ParseColor(%q) accepted an invalid color", bad)
		}
	}
	if c := Hex("2563EB"); c.String() != "#2563EB" || c.Hex() != "2563EB" {
		t.Errorf("round trip gave %s / %s", c.String(), c.Hex())
	}
}

// The default theme must be complete: a zero size or line height would draw
// nothing or stack lines on top of each other.
func TestDefaultIsComplete(t *testing.T) {
	set := Default()
	for name, th := range map[string]Theme{"pdf": set.PDF, "docx": set.DOCX} {
		if th.Page.Width <= 0 || th.Page.Height <= 0 || th.Page.ContentWidth() <= 0 {
			t.Errorf("%s: page %+v", name, th.Page)
		}
		if th.Fonts.Body == "" || th.Fonts.Heading == "" || th.Fonts.Code == "" {
			t.Errorf("%s: fonts %+v", name, th.Fonts)
		}
		if th.Text.Size <= 0 || th.Text.LineHeight < th.Text.Size {
			t.Errorf("%s: text %+v", name, th.Text)
		}
		for i, h := range th.Heading {
			if h.Size <= 0 || h.LineHeight < h.Size {
				t.Errorf("%s: heading %d %+v", name, i+1, h)
			}
			if i > 0 && h.Size > th.Heading[i-1].Size {
				t.Errorf("%s: heading %d is larger than heading %d", name, i+1, i)
			}
		}
		if th.Code.BlockSize <= 0 || th.Code.BlockLineHeight < th.Code.BlockSize || th.Table.LineHeight <= 0 ||
			th.Footnote.Size <= 0 || th.Caption.Size <= 0 {
			t.Errorf("%s: a block size is not set", name)
		}
	}
}
