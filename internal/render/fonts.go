package render

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/hkmt-sw/markout/internal/theme"
)

// Embedded fonts so PDF output works on every OS (and renders Hungarian
// accents — including ő/ű — correctly) without depending on system fonts.
//
// Liberation Sans/Serif/Mono are metric-compatible with Arial, Times New
// Roman and Courier New and cover Latin Extended-A in full. Licensed under the
// SIL Open Font License 1.1; see fonts/LICENSE.
var (
	//go:embed fonts/LiberationSans-Regular.ttf
	fontSansRegular []byte
	//go:embed fonts/LiberationSans-Bold.ttf
	fontSansBold []byte
	//go:embed fonts/LiberationSans-Italic.ttf
	fontSansItalic []byte
	//go:embed fonts/LiberationSans-BoldItalic.ttf
	fontSansBoldItalic []byte
	//go:embed fonts/LiberationSerif-Regular.ttf
	fontSerifRegular []byte
	//go:embed fonts/LiberationSerif-Bold.ttf
	fontSerifBold []byte
	//go:embed fonts/LiberationSerif-Italic.ttf
	fontSerifItalic []byte
	//go:embed fonts/LiberationSerif-BoldItalic.ttf
	fontSerifBoldItalic []byte
	//go:embed fonts/LiberationMono-Regular.ttf
	fontMonoRegular []byte
)

// fontFiles is the data of a font family, by style ("", "B", "I", "BI").
// Only the regular style is required.
type fontFiles map[string][]byte

// pdfFamily is a family loaded into a PDF.
type pdfFamily struct {
	name   string          // base name it is registered under
	styles map[string]bool // styles that have their own font
	ascent float64         // height above the baseline, as a fraction of the size
}

// builtinFamilies are the families every theme can use. Their names are the
// ones the renderer has always registered them under.
var builtinFamilies = map[string]struct {
	name   string
	files  fontFiles
	ascent float64
}{
	"sans":  {"Arial", fontFiles{"": fontSansRegular, "B": fontSansBold, "I": fontSansItalic, "BI": fontSansBoldItalic}, 0.905},
	"serif": {"Serif", fontFiles{"": fontSerifRegular, "B": fontSerifBold, "I": fontSerifItalic, "BI": fontSerifBoldItalic}, 0.891},
	"mono":  {"Courier", fontFiles{"": fontMonoRegular}, 0.833},
}

var styleSuffix = map[string]string{"": "", "B": "-Bold", "I": "-Italic", "BI": "-BoldItalic"}

// maxFontBytes caps the size of a font file named by a theme.
const maxFontBytes = 30 * 1024 * 1024

// loadFonts registers the families the theme uses, plus the built-in sans as
// the fallback for anything that cannot be loaded.
func (r *PdfRenderer) loadFonts() error {
	r.families = map[string]*pdfFamily{}
	fonts := r.t.Fonts
	for _, key := range []string{"sans", fonts.Body, fonts.Heading, fonts.Code} {
		if _, done := r.families[key]; done {
			continue
		}
		name, files, ascent := "", fontFiles(nil), 0.0
		if b, ok := builtinFamilies[key]; ok {
			name, files, ascent = b.name, b.files, b.ascent
		} else if fam, ok := fonts.Families[key]; ok {
			var err error
			if files, err = readFontFamily(fam); err != nil {
				return fmt.Errorf("font %q: %w", key, err)
			}
			name, ascent = "Custom-"+key, ttfAscent(files[""])
		} else {
			continue // validated when the theme was loaded; fall back to sans
		}

		loaded := &pdfFamily{name: name, styles: map[string]bool{}, ascent: ascent}
		for _, style := range []string{"", "B", "I", "BI"} {
			data, ok := files[style]
			if !ok {
				continue
			}
			if err := r.pdf.AddTTFFontData(name+styleSuffix[style], data); err != nil {
				return fmt.Errorf("font %q: %w", key, err)
			}
			loaded.styles[style] = true
		}
		r.families[key] = loaded
	}
	return nil
}

func readFontFamily(fam theme.FontFamily) (fontFiles, error) {
	files := fontFiles{}
	for style, path := range map[string]string{"": fam.Regular, "B": fam.Bold, "I": fam.Italic, "BI": fam.BoldItalic} {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > maxFontBytes {
			return nil, fmt.Errorf("%s is not a font file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		files[style] = data
	}
	if files[""] == nil {
		return nil, fmt.Errorf("no regular font file")
	}
	return files, nil
}

// family returns the loaded family for a theme font name, falling back to the
// built-in sans.
func (r *PdfRenderer) family(key string) *pdfFamily {
	if f, ok := r.families[key]; ok {
		return f
	}
	return r.families["sans"]
}

// fontName picks the registered font for a family and style. A style the
// family lacks falls back to the nearest one it has.
func (f *pdfFamily) fontName(style string) string {
	for _, try := range fallbackStyles[style] {
		if f.styles[try] {
			return f.name + styleSuffix[try]
		}
	}
	return f.name
}

var fallbackStyles = map[string][]string{
	"":   {""},
	"B":  {"B", ""},
	"I":  {"I", ""},
	"BI": {"BI", "B", "I", ""},
}

// ttfAscent reads a TrueType font's ascent as a fraction of its em size, from
// the hhea and head tables. It returns a typical value if the font cannot be
// read.
func ttfAscent(data []byte) float64 {
	const typical = 0.9
	if len(data) < 12 {
		return typical
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	var ascender, unitsPerEm float64
	for i := 0; i < numTables; i++ {
		rec := 12 + 16*i
		if rec+16 > len(data) {
			return typical
		}
		tag := string(data[rec : rec+4])
		offset := int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		switch tag {
		case "hhea":
			if offset+6 <= len(data) {
				ascender = float64(int16(binary.BigEndian.Uint16(data[offset+4 : offset+6])))
			}
		case "head":
			if offset+20 <= len(data) {
				unitsPerEm = float64(binary.BigEndian.Uint16(data[offset+18 : offset+20]))
			}
		}
	}
	if ascender <= 0 || unitsPerEm <= 0 {
		return typical
	}
	return ascender / unitsPerEm
}
