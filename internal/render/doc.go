// Package render draws a document of the package ast as a DOCX or a PDF file.
// The two formats have a renderer each, and the files that belong to one are
// named after it.
//
// The PDF renderer, PdfRenderer in pdf.go, lays text out itself and draws it
// with the gopdf library and the embedded Liberation fonts. Its satellite
// files are named pdf_*.go: line layout and drawing (pdf_text.go), hyphenation,
// math, diagrams, the cover page, the version history, bookmarks and links
// (pdf_outline.go), the classification band and watermark, the ToUnicode maps,
// and the PDF/A post-processing (pdf_pdfa.go).
//
// The DOCX renderer, DocxRenderer in docx.go, writes the document through a
// library and then rewrites parts of the saved file to add what the library
// cannot: heading styles, numbered lists, a table of contents, bookmarks,
// equations and page headers. Its satellite files are named docx_*.go.
//
// What both formats share has files without a prefix: the control block of
// the front matter (control.go), the classification (classification.go), the
// words put on a page (labels.go), headers and footers (running.go), the
// version history (history.go), headings and anchors (headings.go), images
// and their download (image.go, fetch.go), code highlighting, formulas and
// diagrams. RenderPdf and RenderDocx are the entry points.
package render
