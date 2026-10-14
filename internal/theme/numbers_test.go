package theme

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// TOML reads nan and inf as numbers, and a comparison with NaN is never true:
// no setting may let them, or a value too large for the format, through.
func TestNumbersAreChecked(t *testing.T) {
	dir := t.TempDir()
	huge := "1" + strings.Repeat("0", 400)
	tests := []struct{ text, want string }{
		{"[text]\nsize = nan\n", "text.size: NaN is not a length"},
		{"[text]\nsize = inf\n", "text.size: +Inf is not a length"},
		{"[text]\nsize = -inf\n", "text.size: -Inf is not a length"},
		{"[text]\nsize = 401\n", "text.size is 401 points"},
		{"[text]\nline-height = nan\n", "text.line-height: NaN"},
		{"[text]\nparagraph-spacing = nan\n", "text.paragraph-spacing: NaN"},
		{"[text]\nparagraph-spacing = inf\n", "text.paragraph-spacing: +Inf"},
		{"[text]\nparagraph-spacing = -1\n", "text.paragraph-spacing is -1 points"},
		{"[text]\nparagraph-spacing = 401\n", "text.paragraph-spacing is 401 points"},
		{"[text]\nsize = \"" + huge + "pt\"\n", "is not a length"},
		{"[page]\nwidth = nan\n", "page.width: NaN"},
		{"[page]\nwidth = inf\n", "page.width: +Inf"},
		{"[page]\nheight = -inf\n", "page.height: -Inf"},
		{"[page]\nwidth = 14401\n", "too large to be a page"},
		{"[page]\nheight = 100000\n", "too large to be a page"},
		{"[page]\nwidth = 71\n", "too small to be a page"},
		{"[page]\nmargin = nan\n", "page.margin: NaN"},
		{"[page]\nmargin = inf\n", "page.margin: +Inf"},
		{"[page]\nmargin-left = nan\n", "page.margin-left: NaN"},
		{"[page]\nmargin-top = -1\n", "page.margin-top is -1 points; it cannot be negative"},
		{"[page]\nwidth = 14400\nheight = 14400\nmargin-top = 7200\n", "page.margin-top is 7200 points; it must be less than half"},
		{"[page]\nwidth = 14400\nheight = 14400\nmargin-left = 7200\n", "page.margin-left is 7200 points; it must be less than half"},
		{"[cover]\ntop = nan\n", "cover.top: NaN"},
		{"[cover]\ntop = 100000\n", "cover.top is 100000 points"},
		{"[cover]\ntitle-size = inf\n", "cover.title-size: +Inf"},
		{"[heading]\nspace-before = nan\n", "heading.space-before: NaN"},
		{"[heading.h2]\nspace-after = 1000\n", "heading.h2.space-after is 1000 points"},
		{"[heading.h3]\nspace-before = -2\n", "heading.h3.space-before is -2 points"},
		{"[list]\nindent = inf\n", "list.indent: +Inf"},
		{"[quote]\nindent = -3\n", "quote.indent is -3 points"},
		{"[rule]\nwidth = -inf\n", "rule.width: -Inf"},
		{"[table]\ncell-padding = 500\n", "table.cell-padding is 500 points"},
		{"[alert]\npadding = nan\n", "alert.padding: NaN"},
		{"[docx.code]\npadding = 1000\n", "for DOCX: code.padding is 1000 points"},
		{"[pdf.text]\nsize = nan\n", "pdf.text.size: NaN"},
	}
	for i, tc := range tests {
		path := writeTheme(t, dir, "num"+strconv.Itoa(i)+".toml", tc.text)
		_, err := Loader{UserDir: dir}.Load(path)
		if err == nil {
			t.Errorf("%q was accepted", tc.text)
		} else if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %q does not mention %q", tc.text, err, tc.want)
		}
	}

	// The limits themselves are allowed
	ok := "[page]\nwidth = 14400\nheight = 14400\nmargin-left = 0\nmargin-right = 7199\nmargin-top = 0\nmargin-bottom = 7199\n" +
		"[text]\nsize = 400\nparagraph-spacing = 400\n"
	if _, err := (Loader{UserDir: dir}).Load(writeTheme(t, dir, "limits.toml", ok)); err != nil {
		t.Errorf("values at the limits were refused: %v", err)
	}
}

func TestParseLengthRefusesNaNAndInf(t *testing.T) {
	for _, bad := range []any{math.NaN(), math.Inf(1), math.Inf(-1), "1" + strings.Repeat("0", 400)} {
		if n, err := parseLength(bad); err == nil {
			t.Errorf("parseLength(%v) = %g, accepted", bad, n)
		}
	}
}
