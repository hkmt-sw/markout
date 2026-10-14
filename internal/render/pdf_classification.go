package render

import "math"

// The classification band and the watermark are put on a page as it is added,
// before anything else. So the watermark lies behind the text and whatever the
// text sits on: a table cell, a code block or a callout with a background
// covers it, as it covers a page's white.

// watermarkAngle runs the watermark from the bottom left to the top right of
// the page.
const watermarkAngle = 45.0

// watermarkFill is how much of the diagonal of the page a watermark may take.
const watermarkFill = 0.9

// drawPageMarks draws the watermark and the classification band on the page
// just added. The drawing state the caller had is left as it was.
func (r *PdfRenderer) drawPageMarks() {
	if !r.band.drawn && r.watermark == "" {
		return
	}
	face, size, text, fill := r.face, r.fontSize, r.text, r.fill
	x, y := r.pdf.GetX(), r.pdf.GetY()

	if r.watermark != "" {
		r.drawWatermark()
	}
	if r.band.drawn {
		r.drawBand()
	}

	_ = r.pdf.SetFont(face, "", size)
	r.face, r.fontSize, r.font = face, size, fontLabel(face, size)
	r.textColor(text)
	r.fillColor(fill)
	r.pdf.SetXY(x, y)
}

// drawBand draws the strip across the top of the page: the background over the
// full width, and the classification centered in it, in bold. A classification
// too long for the page is set smaller.
func (r *PdfRenderer) drawBand() {
	cls := r.t.Classification
	b := r.band

	r.fillColor(b.background)
	r.rect(0, 0, r.pageWidth, cls.Height, "F")

	size := cls.Size
	r.setFont(r.bodyFont(), "B", size)
	text := r.usable(b.text)
	if text == "" {
		return
	}
	width, _ := r.pdf.MeasureTextWidth(text)
	if room := r.pageWidth - 2*r.marginLeft; width > room && room > 0 {
		size *= room / width
		r.setFont(r.bodyFont(), "B", size)
		width, _ = r.pdf.MeasureTextWidth(text)
	}
	r.textColor(b.color)
	r.pdf.SetX((r.pageWidth - width) / 2)
	r.pdf.SetY((cls.Height - size) / 2)
	r.cell(text)
}

// drawWatermark draws the watermark text across the middle of the page, at
// 45 degrees. It is drawn at the size the theme gives, or smaller if it would
// not fit between the corners of the page.
func (r *PdfRenderer) drawWatermark() {
	mark := r.t.Watermark
	size := mark.Size
	r.setFont(r.bodyFont(), "B", size)
	text := r.usable(r.watermark)
	if text == "" {
		return
	}
	width, _ := r.pdf.MeasureTextWidth(text)

	// The text and its height must fit along the diagonal of the shorter side
	// of the page, which is as far as a line at 45 degrees can run, less a
	// tenth to keep the ends off the edges
	if limit := math.Min(r.pageWidth, r.pageHeight) * math.Sqrt2 * watermarkFill; width+size > limit {
		size = limit / (width/size + 1)
		r.setFont(r.bodyFont(), "B", size)
		width, _ = r.pdf.MeasureTextWidth(text)
	}

	cx, cy := r.pageWidth/2, r.pageHeight/2
	x, y := cx-width/2, cy-size/2
	r.textColor(mark.Color)
	r.logf("mark  x=%.1f y=%.1f angle=%g %s %q %s", x, y, watermarkAngle, r.font, text, r.text)
	r.pdf.Rotate(watermarkAngle, cx, cy)
	r.pdf.SetX(x)
	r.pdf.SetY(y)
	_ = r.pdf.Cell(nil, text)
	r.pdf.RotateReset()
}
