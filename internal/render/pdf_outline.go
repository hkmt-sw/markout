package render

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/signintech/gopdf"

	"github.com/hkmt-sw/markout/internal/ast"
)

// The headings of a PDF are its bookmarks (the outline a viewer shows beside
// the pages) and the places links within the document jump to.

// outlineEntry is a heading's bookmark.
type outlineEntry struct {
	obj   *gopdf.OutlineObj
	level int
}

// markHeading makes the place where a heading is about to be drawn a
// bookmark, and the target of links to the heading.
func (r *PdfRenderer) markHeading(h ast.Heading, level int) {
	// Both are placed a little above the current position
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	r.pdf.SetX(r.marginLeft)
	r.pdf.SetY(r.currentY)

	if _, known := r.anchors[h.ID]; known && !r.anchored[h.ID] {
		r.pdf.SetAnchor(h.ID)
		r.anchored[h.ID] = true
		r.headingPages[h.ID] = r.page
		r.logf("anchor y=%.1f %s", r.currentY, h.ID)
	}
	if title := headingText(h.Runs); title != "" {
		r.outline = append(r.outline, outlineEntry{obj: r.pdf.AddOutlineWithPosition(title), level: level})
		r.logf("bookmark level=%d %q", level, title)
	}
}

// linkTo makes an area of the page a link: to a heading of the document for
// "#heading", to the address otherwise.
func (r *PdfRenderer) linkTo(target string, x, y, w, h float64) {
	if !strings.HasPrefix(target, "#") {
		r.pdf.AddExternalLink(target, x, y, w, h)
		return
	}
	// A link to a heading that does not exist goes nowhere
	if id, ok := r.anchors.resolve(target); ok {
		r.pdf.AddInternalLink(id, x, y, w, h)
		r.logf("goto  x=%.1f y=%.1f w=%.1f %s", x, y, w, id)
	}
}

// outlineRootObject is the number of the PDF object that holds the outline:
// gopdf makes it third, after the catalog and the page tree.
const outlineRootObject = 3

// nestOutline turns the bookmarks, which gopdf chains one after the other,
// into a tree by heading level: a heading's bookmark holds those of the
// deeper headings that follow it. It returns how many bookmarks are at the
// top, and the object number of the last of them.
func (r *PdfRenderer) nestOutline() (top, last int) {
	type node struct {
		obj      *gopdf.OutlineObj
		level    int
		children []*node
	}
	var roots []*node
	var open []*node // the chain of bookmarks the next one may go under
	for _, e := range r.outline {
		n := &node{obj: e.obj, level: e.level}
		for len(open) > 0 && open[len(open)-1].level >= n.level {
			open = open[:len(open)-1]
		}
		if len(open) == 0 {
			roots = append(roots, n)
		} else {
			parent := open[len(open)-1]
			parent.children = append(parent.children, n)
		}
		open = append(open, n)
	}

	var link func(siblings []*node, parent int)
	link = func(siblings []*node, parent int) {
		for i, n := range siblings {
			n.obj.SetParent(parent)
			n.obj.SetPrev(-1)
			n.obj.SetNext(-1)
			if i > 0 {
				n.obj.SetPrev(siblings[i-1].obj.GetIndex())
			}
			if i < len(siblings)-1 {
				n.obj.SetNext(siblings[i+1].obj.GetIndex())
			}
			if len(n.children) > 0 {
				n.obj.SetFirst(n.children[0].obj.GetIndex())
				n.obj.SetLast(n.children[len(n.children)-1].obj.GetIndex())
				link(n.children, n.obj.GetIndex())
			}
		}
	}
	link(roots, outlineRootObject)

	if len(roots) == 0 {
		return 0, 0
	}
	return len(roots), roots[len(roots)-1].obj.GetIndex()
}

// The outline's own object, as gopdf writes it. It counts every bookmark and
// names the one added last as the last, which is right only while none is
// inside another.
var outlineRoot = regexp.MustCompile(`(/Type /Outlines\n\t/Count )(\d+)(\n\t/First \d+ 0 R\n\t/Last )(\d+)( 0 R)`)

// fixOutlineRoot writes the number of top-level bookmarks and the last of
// them into the outline's object. The numbers can only get shorter, and are
// padded to the length they had, so nothing in the file moves.
func fixOutlineRoot(pdf []byte, top, last int) []byte {
	m := outlineRoot.FindSubmatchIndex(pdf)
	if m == nil {
		return pdf
	}
	count, lastObj := fmt.Sprint(top), fmt.Sprint(last)
	countPad, lastPad := (m[5]-m[4])-len(count), (m[9]-m[8])-len(lastObj)
	if countPad < 0 || lastPad < 0 {
		return pdf
	}
	var out []byte
	out = append(out, pdf[:m[4]]...)
	out = append(out, count+strings.Repeat(" ", countPad)...)
	out = append(out, pdf[m[5]:m[8]]...)
	out = append(out, lastObj...)
	out = append(out, pdf[m[9]:m[11]]...)
	out = append(out, strings.Repeat(" ", lastPad)...)
	return append(out, pdf[m[11]:]...)
}
