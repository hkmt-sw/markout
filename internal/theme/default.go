package theme

// mm is points per millimetre, rounded the way the renderers always have, so
// the default margins land exactly where they did.
const mm = 2.8346

// A4 paper.
const (
	a4Width  = 595.28
	a4Height = 841.89
)

// Default is markout's standard look. Its PDF and DOCX forms differ in fonts,
// margins and heading sizes for historical reasons; spacing is the same in
// both.
func Default() Set {
	alerts := Alert{
		Padding:    8,
		BarWidth:   3,
		SpaceAfter: 8,
		Note:       AlertColors{Background: Hex("DBEAFE"), Border: Hex("3B82F6")},
		Tip:        AlertColors{Background: Hex("DCFCE7"), Border: Hex("22C55E")},
		Important:  AlertColors{Background: Hex("F3E8FF"), Border: Hex("A855F7")},
		Caution:    AlertColors{Background: Hex("FEF9C3"), Border: Hex("EAB308")},
		Warning:    AlertColors{Background: Hex("FEE2E2"), Border: Hex("EF4444")},
	}
	diagram := Diagram{Background: Hex("ECFDF5"), Border: Hex("10B981"), Text: Hex("065F46")}
	box := Box{Background: Hex("F8F9FC"), Border: Hex("E2E4EB")}

	pdf := Theme{
		Name:        "default",
		Description: "The standard markout look",
		Page: Page{
			Width: a4Width, Height: a4Height,
			MarginTop: 20 * mm, MarginRight: 20 * mm, MarginBottom: 20 * mm, MarginLeft: 20 * mm,
		},
		Fonts: Fonts{Body: "sans", Heading: "sans", Code: "mono"},
		Text: Text{
			Size: 11, LineHeight: 16, ParagraphSpacing: 8,
			Color: Hex("1A1A2E"), Muted: Hex("4A4A68"), Faint: Hex("8888A0"),
		},
		Heading: [6]Heading{
			{Size: 24, LineHeight: 28, SpaceBefore: 20, SpaceAfter: 12},
			{Size: 20, LineHeight: 24, SpaceBefore: 18, SpaceAfter: 10},
			{Size: 16, LineHeight: 20, SpaceBefore: 14, SpaceAfter: 8},
			{Size: 14, LineHeight: 18, SpaceBefore: 12, SpaceAfter: 6},
			{Size: 12, LineHeight: 16, SpaceBefore: 10, SpaceAfter: 6},
			{Size: 11, LineHeight: 14, SpaceBefore: 10, SpaceAfter: 6},
		},
		Link: Link{Color: Hex("2563EB")},
		Code: Code{
			Color:     Hex("CF222E"),
			BlockSize: 9, BlockLineHeight: 12, Padding: 8, SpaceAfter: 8,
			BlockColor: Black, Background: Hex("F5F5F5"), Border: Hex("C8C8C8"),
			// Each is readable on the light panels the themes use
			Syntax: Syntax{
				Highlight: true,
				Keyword:   Hex("CF222E"), String: Hex("0A3069"), Comment: Hex("57606A"),
				Number: Hex("0550AE"), Function: Hex("6639BA"), Type: Hex("953800"),
			},
		},
		List: List{Indent: 20, SpaceAfter: 6},
		Table: Table{
			CellPadding: 4, LineHeight: 13, SpaceAfter: 8,
			Border: Hex("D1D5DE"), HeaderBackground: Hex("F1F3F9"),
			RowBackground: White, StripeBackground: Hex("F8F9FC"),
		},
		Quote:    Quote{Indent: 20, BarWidth: 3, SpaceAfter: 4, Bar: Hex("B4B4B4"), Italic: true},
		Rule:     Rule{Width: 1, Space: 12, Color: Hex("B4B4B4")},
		Alert:    alerts,
		Box:      box,
		Diagram:  diagram,
		Footnote: Footnote{Size: 9, LineHeight: 14, Rule: Hex("B4B4B4")},
		Caption:  Caption{Size: 9},
		Header:   Running{Size: 9, Color: Hex("4A4A68")},
		Footer:   Running{Size: 9, Color: Hex("4A4A68")},
		Colors: Colors{
			Math: Hex("7C3AED"), Highlight: Hex("FFF082"),
			Inserted: Hex("16A34A"), Deleted: Hex("CF222E"),
		},
	}

	docx := pdf
	docx.Page.Width, docx.Page.Height = 210*mm, 297*mm
	docx.Page.MarginTop, docx.Page.MarginRight = 25*mm, 25*mm
	docx.Page.MarginBottom, docx.Page.MarginLeft = 25*mm, 25*mm
	docx.Fonts = Fonts{Body: "Georgia", Heading: "Georgia", Code: "Consolas"}
	for i, size := range []float64{28, 22, 17, 14, 12, 11} {
		docx.Heading[i].Size = size
	}
	docx.Code.BlockSize = 9.5
	docx.Code.BlockColor = Hex("24292F")
	docx.Code.Background = Hex("F6F8FA")
	docx.Table.StripeBackground = Hex("F8F8F8")
	docx.Quote.Bar = Hex("3B82F6")

	return Set{PDF: pdf, DOCX: docx}
}
