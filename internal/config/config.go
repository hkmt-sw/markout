package config

// ============================================================================
// MARKOUT DESIGN SYSTEM - "Editorial Precision"
// Modern, professional document styling inspired by Notion/Stripe docs
// ============================================================================

// Page dimensions in millimeters (A4)
const (
	PageWidthMM  = 210.0
	PageHeightMM = 297.0
)

// Margins in millimeters (25mm = ~1 inch for professional look)
const (
	MarginTopMM    = 25.0
	MarginBottomMM = 25.0
	MarginLeftMM   = 25.0
	MarginRightMM  = 25.0
)

// Content area dimensions
const (
	ContentWidthMM  = PageWidthMM - MarginLeftMM - MarginRightMM  // 160mm
	ContentHeightMM = PageHeightMM - MarginTopMM - MarginBottomMM // 247mm
)

// DXA conversion (1 inch = 1440 DXA, 1 inch = 25.4mm)
const DXAPerMM = 56.6929

// Page dimensions in DXA (for DOCX)
const (
	PageWidthDXA  int64 = 11906 // 210 * 56.6929
	PageHeightDXA int64 = 16838 // 297 * 56.6929
)

// Margins in DXA
const (
	MarginTopDXA    int64 = 1417 // 25 * 56.6929
	MarginBottomDXA int64 = 1417
	MarginLeftDXA   int64 = 1417
	MarginRightDXA  int64 = 1417
)

// ============================================================================
// TYPOGRAPHY
// ============================================================================

// Font names
const (
	// Primary fonts (system fonts for compatibility)
	FontHeading = "Calibri"  // Clean, modern sans-serif
	FontBody    = "Georgia"  // Classic, readable serif
	FontCode    = "Consolas" // Excellent monospace

	// PDF-specific (using system fonts)
	FontHeadingPDF = "Arial"   // Will use Arial-Bold for headings
	FontBodyPDF    = "Arial"   // Arial for body (Georgia not available)
	FontCodePDF    = "Courier" // Courier New
)

// Font sizes in points - refined type scale
const (
	FontSizeH1      = 28.0
	FontSizeH2      = 22.0
	FontSizeH3      = 17.0
	FontSizeH4      = 14.0
	FontSizeH5      = 12.0
	FontSizeH6      = 11.0
	FontSizeDefault = 11.0
	FontSizeCode    = 9.5
	FontSizeSmall   = 9.0
)

// Line heights in points
const (
	LineHeightH1      = 34.0
	LineHeightH2      = 28.0
	LineHeightH3      = 22.0
	LineHeightH4      = 18.0
	LineHeightH5      = 16.0
	LineHeightH6      = 15.0
	LineHeightDefault = 17.0
	LineHeightCode    = 15.0
)

// Half-points for DOCX (1 point = 2 half-points)
const (
	FontSizeDefaultHPS int64 = 22 // 11 * 2
	FontSizeCodeHPS    int64 = 19 // 9.5 * 2
	FontSizeH1HPS      int64 = 56 // 28 * 2
	FontSizeH2HPS      int64 = 44 // 22 * 2
	FontSizeH3HPS      int64 = 34 // 17 * 2
	FontSizeH4HPS      int64 = 28 // 14 * 2
	FontSizeH5HPS      int64 = 24 // 12 * 2
	FontSizeH6HPS      int64 = 22 // 11 * 2
)

// ============================================================================
// SPACING
// ============================================================================

// Spacing in points (for headings and elements)
const (
	SpacingBeforeH1  = 32.0
	SpacingAfterH1   = 16.0
	SpacingBeforeH2  = 28.0
	SpacingAfterH2   = 12.0
	SpacingBeforeH3  = 24.0
	SpacingAfterH3   = 10.0
	SpacingBeforeH4  = 20.0
	SpacingAfterH4   = 8.0
	SpacingBeforeH5  = 16.0
	SpacingAfterH5   = 6.0
	SpacingBeforeH6  = 14.0
	SpacingAfterH6   = 6.0
	SpacingParagraph = 12.0
)

// Spacing in twips for DOCX (1 point = 20 twips)
const (
	SpacingBeforeH1Twips  int64 = 640 // 32 * 20
	SpacingAfterH1Twips   int64 = 320 // 16 * 20
	SpacingBeforeH2Twips  int64 = 560 // 28 * 20
	SpacingAfterH2Twips   int64 = 240 // 12 * 20
	SpacingBeforeH3Twips  int64 = 480 // 24 * 20
	SpacingAfterH3Twips   int64 = 200 // 10 * 20
	SpacingBeforeH4Twips  int64 = 400 // 20 * 20
	SpacingAfterH4Twips   int64 = 160 // 8 * 20
	SpacingBeforeH5Twips  int64 = 320 // 16 * 20
	SpacingAfterH5Twips   int64 = 120 // 6 * 20
	SpacingBeforeH6Twips  int64 = 280 // 14 * 20
	SpacingAfterH6Twips   int64 = 120 // 6 * 20
	SpacingParagraphTwips int64 = 240 // 12 * 20
)

// ============================================================================
// COLORS - "Ink & Paper" palette
// ============================================================================

// Colors (RGB hex values for DOCX)
const (
	// Text colors
	ColorTextPrimary   = "1A1A2E" // Deep ink blue
	ColorTextSecondary = "4A4A68" // Muted prose
	ColorTextTertiary  = "8888A0" // Subtle notes

	// Accent colors
	ColorAccent      = "2563EB" // Electric blue (links)
	ColorAccentHover = "1D4ED8" // Pressed state

	// Surface colors
	ColorSurface         = "FFFFFF" // Clean paper
	ColorSurfaceElevated = "F8F9FC" // Subtle lift

	// Border colors
	ColorBorder       = "E2E4EB" // Whisper lines
	ColorBorderStrong = "C9CDD6" // Definition

	// Code colors
	ColorCodeBackground = "F6F8FA" // Terminal paper
	ColorCodeText       = "24292F" // Monospace ink
	ColorCodeAccent     = "CF222E" // Syntax red (inline code)

	// Blockquote
	ColorBlockquoteBorder     = "3B82F6" // Attention bar
	ColorBlockquoteBackground = "EFF6FF" // Highlight wash

	// Table
	ColorTableHeader = "F1F3F9" // Column emphasis
	ColorTableRowAlt = "F8F9FC" // Zebra stripe
	ColorTableBorder = "D1D5DE" // Grid lines

	// Status
	ColorSuccess = "16A34A" // Checkmark green
	ColorWarning = "CA8A04" // Caution amber

	// Legacy aliases for compatibility
	ColorLink = "2563EB"

	// Alert/Callout colors - NOTE (blue)
	ColorAlertNoteBg     = "DBEAFE"
	ColorAlertNoteBorder = "3B82F6"

	// Alert/Callout colors - TIP (green)
	ColorAlertTipBg     = "DCFCE7"
	ColorAlertTipBorder = "22C55E"

	// Alert/Callout colors - IMPORTANT (purple)
	ColorAlertImportantBg     = "F3E8FF"
	ColorAlertImportantBorder = "A855F7"

	// Alert/Callout colors - CAUTION (amber)
	ColorAlertCautionBg     = "FEF9C3"
	ColorAlertCautionBorder = "EAB308"

	// Alert/Callout colors - WARNING (red)
	ColorAlertWarningBg     = "FEE2E2"
	ColorAlertWarningBorder = "EF4444"

	// Math expression text color (purple)
	ColorMathText = "7C3AED"

	// Mermaid diagram placeholder
	ColorMermaidBg     = "ECFDF5"
	ColorMermaidBorder = "10B981"
	ColorMermaidText   = "065F46"
)

// RGB color values for PDF
type RGB struct {
	R, G, B uint8
}

var (
	// Text colors
	ColorTextPrimaryRGB   = RGB{26, 26, 46}
	ColorTextSecondaryRGB = RGB{74, 74, 104}
	ColorTextTertiaryRGB  = RGB{136, 136, 160}

	// Accent
	ColorAccentRGB = RGB{37, 99, 235}
	ColorLinkRGB   = RGB{37, 99, 235}

	// Code
	ColorCodeBackgroundRGB = RGB{246, 248, 250}
	ColorCodeTextRGB       = RGB{36, 41, 47}
	ColorCodeAccentRGB     = RGB{207, 34, 46}

	// Blockquote
	ColorBlockquoteBorderRGB     = RGB{59, 130, 246}
	ColorBlockquoteBackgroundRGB = RGB{239, 246, 255}

	// Table
	ColorTableHeaderRGB = RGB{241, 243, 249}
	ColorTableRowAltRGB = RGB{248, 249, 252}
	ColorTableBorderRGB = RGB{209, 213, 222}

	// Status
	ColorSuccessRGB = RGB{22, 163, 74}
	ColorWarningRGB = RGB{202, 138, 4}

	// Alert/Callout - NOTE (blue)
	ColorAlertNoteBgRGB     = RGB{219, 234, 254}
	ColorAlertNoteBorderRGB = RGB{59, 130, 246}

	// Alert/Callout - TIP (green)
	ColorAlertTipBgRGB     = RGB{220, 252, 231}
	ColorAlertTipBorderRGB = RGB{34, 197, 94}

	// Alert/Callout - IMPORTANT (purple)
	ColorAlertImportantBgRGB     = RGB{243, 232, 255}
	ColorAlertImportantBorderRGB = RGB{168, 85, 247}

	// Alert/Callout - CAUTION (amber)
	ColorAlertCautionBgRGB     = RGB{254, 249, 195}
	ColorAlertCautionBorderRGB = RGB{234, 179, 8}

	// Alert/Callout - WARNING (red)
	ColorAlertWarningBgRGB     = RGB{254, 226, 226}
	ColorAlertWarningBorderRGB = RGB{239, 68, 68}

	// Math
	ColorMathTextRGB = RGB{124, 58, 237}

	// Highlighted text (==mark==)
	ColorHighlightRGB = RGB{255, 240, 130}

	// Mermaid
	ColorMermaidBgRGB     = RGB{236, 253, 245}
	ColorMermaidBorderRGB = RGB{16, 185, 129}
	ColorMermaidTextRGB   = RGB{6, 95, 70}
)

// ============================================================================
// ELEMENT-SPECIFIC STYLING
// ============================================================================

// List styling
const (
	ListIndentMM             = 8.0 // Indent per level
	ListBulletWidthMM        = 6.0
	ListIndentDXA      int64 = 454 // 8 * 56.6929
	ListBulletWidthDXA int64 = 340
)

// Code block styling
const (
	CodeBlockPaddingMM          = 4.0
	CodeBlockIndentMM           = 4.0
	CodeBlockIndentDXA    int64 = 227 // 4 * 56.6929
	CodeBlockBorderRadius       = 6.0 // points for PDF
)

// Blockquote styling
const (
	BlockquoteIndentMM            = 6.0
	BlockquoteBorderWidthMM       = 1.5
	BlockquotePaddingMM           = 4.0
	BlockquoteIndentDXA     int64 = 340 // 6 * 56.6929
)

// Table styling
const (
	TableBorderWidthPt        = 0.5
	TableCellPaddingMM        = 3.0
	TableCellPaddingDXA int64 = 170 // 3 * 56.6929
)

// Horizontal rule
const HorizontalRuleWidthPt = 1.0

// Maximum file size for conversion (10MB)
const MaxFileSizeBytes = 10 * 1024 * 1024

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

// MMToDXA converts millimeters to DXA units
func MMToDXA(mm float64) int64 {
	return int64(mm * DXAPerMM)
}

// PointsToTwips converts points to twips
func PointsToTwips(pt float64) int64 {
	return int64(pt * 20)
}

// PointsToHalfPoints converts points to half-points
func PointsToHalfPoints(pt float64) int64 {
	return int64(pt * 2)
}

// MMToPoints converts millimeters to points (1mm = 2.8346 points)
func MMToPoints(mm float64) float64 {
	return mm * 2.8346
}
