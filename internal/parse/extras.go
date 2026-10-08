package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"

	gmast "github.com/yuin/goldmark/ast"

	"github.com/hkmt-sw/markout/internal/ast"
)

// ---- alerts -----------------------------------------------------------------

// alertKinds maps the callout names used across flavors (GitHub alerts,
// Obsidian callouts, MkDocs/Docusaurus/MyST admonitions) onto the five alert
// styles the renderers draw.
var alertKinds = map[string]ast.AlertType{
	"note": ast.AlertNote, "info": ast.AlertNote, "abstract": ast.AlertNote,
	"summary": ast.AlertNote, "tldr": ast.AlertNote, "todo": ast.AlertNote,
	"question": ast.AlertNote, "help": ast.AlertNote, "faq": ast.AlertNote,
	"example": ast.AlertNote, "quote": ast.AlertNote, "cite": ast.AlertNote,
	"seealso": ast.AlertNote,

	"tip": ast.AlertTip, "hint": ast.AlertTip, "success": ast.AlertTip,
	"check": ast.AlertTip, "done": ast.AlertTip,

	"important": ast.AlertImportant,

	"caution": ast.AlertCaution, "attention": ast.AlertCaution,

	"warning": ast.AlertWarning, "danger": ast.AlertWarning, "error": ast.AlertWarning,
	"bug": ast.AlertWarning, "failure": ast.AlertWarning, "fail": ast.AlertWarning,
	"missing": ast.AlertWarning,
}

// alertTypeFor returns the alert style for a callout name. Unknown names fall
// back to a note and report false.
func alertTypeFor(name string) (ast.AlertType, bool) {
	t, ok := alertKinds[strings.ToLower(name)]
	return t, ok
}

func isGitHubAlert(name string) bool {
	switch strings.ToLower(name) {
	case "note", "tip", "important", "caution", "warning":
		return true
	}
	return false
}

// defaultAlertTitle is the title of a callout that does not set its own.
func defaultAlertTitle(name string) string {
	if isGitHubAlert(name) {
		return strings.ToUpper(name)
	}
	name = strings.ReplaceAll(name, "-", " ")
	if name == "" {
		return "Note"
	}
	return strings.ToUpper(name[:1]) + strings.ToLower(name[1:])
}

// nodeStart returns the source offset of the first text under an inline node,
// or -1 when it has none.
func nodeStart(n gmast.Node) int {
	switch t := n.(type) {
	case *gmast.Text:
		return t.Segment.Start
	case *gmast.RawHTML:
		if t.Segments.Len() > 0 {
			return t.Segments.At(0).Start
		}
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if s := nodeStart(c); s >= 0 {
			return s
		}
	}
	return -1
}

// flattenBlock converts a block nested in a callout or quote into paragraphs,
// the one element kind the renderers lay out inside those containers.
func (w *astWalker) flattenBlock(node gmast.Node) []ast.Element {
	switch c := node.(type) {
	case *gmast.Paragraph:
		return []ast.Element{w.convertParagraph(c)}

	case *gmast.TextBlock:
		return []ast.Element{ast.NewParagraph(w.extractInlineRuns(c)...)}

	case *gmast.Heading:
		runs := w.extractInlineRuns(c)
		for i := range runs {
			runs[i].Bold = true
		}
		return []ast.Element{ast.NewParagraph(runs...)}

	case *gmast.List:
		list := w.convertList(c, 0)
		return flattenListItems(list.Items, list.Ordered, 0)

	case *gmast.FencedCodeBlock, *gmast.CodeBlock:
		var out []ast.Element
		lines := node.Lines()
		for i := 0; i < lines.Len(); i++ {
			line := lines.At(i)
			text := strings.TrimRight(string(line.Value(w.source)), "\r\n")
			out = append(out, ast.NewParagraph(ast.NewCodeRun(text)))
		}
		return out

	case *gmast.Blockquote:
		var out []ast.Element
		for child := c.FirstChild(); child != nil; child = child.NextSibling() {
			out = append(out, w.flattenBlock(child)...)
		}
		return out

	case *extTable:
		table := w.convertTable(c)
		rows := append([]ast.TableRow{table.Header}, table.Rows...)
		var out []ast.Element
		for i, row := range rows {
			var runs []ast.InlineRun
			for j, cell := range row.Cells {
				if j > 0 {
					runs = append(runs, ast.NewInlineRun(" | "))
				}
				for _, r := range cell.Runs {
					r.Bold = r.Bold || i == 0
					runs = append(runs, r)
				}
			}
			out = append(out, ast.NewParagraph(runs...))
		}
		return out

	case *extMathBlock:
		return []ast.Element{ast.NewParagraph(ast.InlineRun{Text: w.convertMathBlock(c).Expression, Math: true})}

	case *gmast.HTMLBlock:
		var out []ast.Element
		for _, e := range w.convertHTMLBlock(c) {
			if p, ok := e.(ast.Paragraph); ok {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}

func flattenListItems(items []ast.ListItem, ordered bool, level int) []ast.Element {
	bullets := []string{"•", "◦", "▪"}
	var out []ast.Element
	for i, item := range items {
		prefix := bullets[level%len(bullets)]
		switch {
		case item.IsTask && item.Checked:
			prefix = "[x]"
		case item.IsTask:
			prefix = "[ ]"
		case ordered:
			prefix = strconv.Itoa(i+1) + "."
		}
		runs := append([]ast.InlineRun{ast.NewInlineRun(prefix + " ")}, item.Runs...)
		out = append(out, ast.NewParagraph(runs...))
		for _, block := range item.Blocks {
			out = append(out, flattenElement(block)...)
		}
		out = append(out, flattenListItems(item.Children, item.ChildrenOrdered, level+1)...)
	}
	return out
}

// flattenElement does for a converted block what flattenBlock does for a
// parsed one: it keeps the text, as paragraphs.
func flattenElement(elem ast.Element) []ast.Element {
	codeLines := func(code string) []ast.Element {
		var out []ast.Element
		for _, line := range strings.Split(strings.TrimRight(code, "\r\n"), "\n") {
			out = append(out, ast.NewParagraph(ast.NewCodeRun(strings.TrimRight(line, "\r"))))
		}
		return out
	}
	nested := func(elems []ast.Element) []ast.Element {
		var out []ast.Element
		for _, e := range elems {
			out = append(out, flattenElement(e)...)
		}
		return out
	}
	switch e := elem.(type) {
	case ast.Paragraph:
		return []ast.Element{e}
	case ast.Heading:
		runs := append([]ast.InlineRun(nil), e.Runs...)
		for i := range runs {
			runs[i].Bold = true
		}
		return []ast.Element{ast.NewParagraph(runs...)}
	case ast.CodeBlock:
		return codeLines(e.Code)
	case ast.MermaidDiagram:
		return codeLines(e.Source)
	case ast.MathBlock:
		return []ast.Element{ast.NewParagraph(ast.InlineRun{Text: e.Expression, Math: true})}
	case ast.Blockquote:
		return nested(e.Elements)
	case ast.Alert:
		return nested(e.Elements)
	case ast.List:
		return flattenListItems(e.Items, e.Ordered, 0)
	case ast.Table:
		var out []ast.Element
		for i, row := range append([]ast.TableRow{e.Header}, e.Rows...) {
			var runs []ast.InlineRun
			for j, cell := range row.Cells {
				if j > 0 {
					runs = append(runs, ast.NewInlineRun(" | "))
				}
				for _, r := range cell.Runs {
					r.Bold = r.Bold || i == 0
					runs = append(runs, r)
				}
			}
			out = append(out, ast.NewParagraph(runs...))
		}
		return out
	case ast.Image:
		text := e.Alt
		if text == "" {
			text = e.URL
		}
		return []ast.Element{ast.NewParagraph(ast.NewInlineRun("[" + text + "]"))}
	}
	return nil
}

// ---- HTML -------------------------------------------------------------------

// htmlState tracks the inline HTML tags that are open while a block's runs are
// collected, so <u>text</u> formats the runs between the two tags.
type htmlState struct {
	bold, italic, code, strike int
	underline, mark, sup, sub  int
	ins, del                   int
	link                       string
}

// apply adds the formatting of the open tags to a run.
func (h *htmlState) apply(r *ast.InlineRun) {
	r.Bold = r.Bold || h.bold > 0
	r.Italic = r.Italic || h.italic > 0
	r.Code = r.Code || h.code > 0
	r.Strikethrough = r.Strikethrough || h.strike > 0
	r.Underline = r.Underline || h.underline > 0
	r.Highlight = r.Highlight || h.mark > 0
	r.Superscript = r.Superscript || h.sup > 0
	r.Subscript = r.Subscript || h.sub > 0
	r.Inserted = r.Inserted || h.ins > 0
	r.Deleted = r.Deleted || h.del > 0
	if r.Link == "" {
		r.Link = h.link
	}
}

var (
	htmlTag     = regexp.MustCompile(`(?s)^<(/?)([A-Za-z][A-Za-z0-9-]*)([^>]*)>$`)
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlSkip    = regexp.MustCompile(`(?is)^\s*<(script|style|iframe|video|audio|object)\b`)
	htmlSummary = regexp.MustCompile(`(?is)<summary[^>]*>(.*?)</summary>`)
	htmlHeading = regexp.MustCompile(`(?is)<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	htmlImg     = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	htmlBreak   = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|li|td|th|tr|h[1-6])>`)
	htmlAnyTag  = regexp.MustCompile(`(?s)<[^>]+>`)
)

// tag processes one inline HTML tag. It returns text to emit in its place, if
// any: the tag itself when the flavor shows HTML literally, or a space for a
// line break. The <mo-*> tags written by the preprocessor always apply.
func (h *htmlState) tag(raw string, htmlOn bool) (string, bool) {
	m := htmlTag.FindStringSubmatch(raw)
	name := ""
	if m != nil {
		name = strings.ToLower(m[2])
	}
	if !strings.HasPrefix(name, "mo-") && !htmlOn {
		return raw, true
	}
	if m == nil {
		return "", false // a comment or processing instruction
	}

	var counter *int
	switch name {
	case "mo-ins":
		counter = &h.ins
	case "mo-del":
		counter = &h.del
	case "mo-mark", "mark":
		counter = &h.mark
	case "mo-u", "u", "ins":
		counter = &h.underline
	case "mo-sup", "sup":
		counter = &h.sup
	case "mo-sub", "sub":
		counter = &h.sub
	case "b", "strong":
		counter = &h.bold
	case "i", "em", "cite", "var", "dfn":
		counter = &h.italic
	case "code", "kbd", "samp", "tt":
		counter = &h.code
	case "s", "strike", "del":
		counter = &h.strike
	case "a":
		if m[1] == "/" {
			h.link = ""
		} else {
			h.link = htmlAttr(m[3], "href")
		}
		return "", false
	case "br":
		return " ", true
	default:
		return "", false
	}

	if m[1] == "/" {
		if *counter > 0 {
			*counter--
		}
	} else if !strings.HasSuffix(m[3], "/") {
		*counter++
	}
	return "", false
}

// htmlAttr returns the value of an attribute in a tag's attribute text.
func htmlAttr(attrs, name string) string {
	re := regexp.MustCompile(`(?i)(?:^|\s)` + name + `\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	m := re.FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1] + m[2] + m[3])
}

// htmlPlainText strips the tags from an HTML fragment.
func htmlPlainText(s string) string {
	s = htmlBreak.ReplaceAllString(s, " ")
	s = htmlAnyTag.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

// convertHTMLBlock turns a block of raw HTML into document elements: the
// summary of a <details> section, headings, images, and whatever text is left.
func (w *astWalker) convertHTMLBlock(n *gmast.HTMLBlock) []ast.Element {
	var buf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		buf.Write(line.Value(w.source))
	}
	if n.HasClosure() {
		buf.Write(n.ClosureLine.Value(w.source))
	}
	raw := buf.String()

	if !w.f.HTML {
		// The flavor does not render HTML: show the markup as written.
		if text := strings.Join(strings.Fields(raw), " "); text != "" {
			return []ast.Element{ast.NewParagraph(ast.NewInlineRun(text))}
		}
		return nil
	}

	raw = htmlComment.ReplaceAllString(raw, "")
	if htmlSkip.MatchString(raw) {
		return nil
	}

	var out []ast.Element
	raw = htmlSummary.ReplaceAllStringFunc(raw, func(m string) string {
		if t := htmlPlainText(htmlSummary.FindStringSubmatch(m)[1]); t != "" {
			out = append(out, ast.NewParagraph(ast.NewBoldRun("► "+t)))
		}
		return ""
	})
	raw = htmlHeading.ReplaceAllStringFunc(raw, func(m string) string {
		sm := htmlHeading.FindStringSubmatch(m)
		if t := htmlPlainText(sm[2]); t != "" {
			out = append(out, ast.NewHeading(int(sm[1][0]-'0'), ast.NewInlineRun(t)))
		}
		return ""
	})

	var images []ast.Element
	raw = htmlImg.ReplaceAllStringFunc(raw, func(m string) string {
		if src := htmlAttr(m, "src"); src != "" {
			img := ast.NewImage(htmlAttr(m, "alt"), src, htmlAttr(m, "title"))
			if width := strings.TrimSuffix(htmlAttr(m, "width"), "px"); width != "" {
				if strings.HasSuffix(width, "%") {
					pct, _ := strconv.Atoi(strings.TrimSuffix(width, "%"))
					img.Width = pct * percentWidthBasePx / 100
				} else {
					img.Width, _ = strconv.Atoi(width)
				}
			}
			images = append(images, img)
		}
		return ""
	})

	if text := htmlPlainText(raw); text != "" {
		out = append(out, ast.NewParagraph(ast.NewInlineRun(text)))
	}
	return append(out, images...)
}

// ---- GitLab JSON tables -----------------------------------------------------

// jsonTable converts the body of a ```json:table block into a table.
func jsonTable(code string) (ast.Table, bool) {
	var spec struct {
		Fields []json.RawMessage `json:"fields"`
		Items  []map[string]any  `json:"items"`
	}
	if err := json.Unmarshal([]byte(code), &spec); err != nil || len(spec.Items) == 0 {
		return ast.Table{}, false
	}

	var keys, labels []string
	for _, raw := range spec.Fields {
		var name string
		var field struct {
			Key   string `json:"key"`
			Label string `json:"label"`
		}
		if json.Unmarshal(raw, &name) == nil {
			field.Key = name
		} else if json.Unmarshal(raw, &field) != nil || field.Key == "" {
			continue
		}
		if field.Label == "" {
			field.Label = field.Key
		}
		keys, labels = append(keys, field.Key), append(labels, field.Label)
	}
	if len(keys) == 0 {
		for k := range spec.Items[0] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		labels = keys
	}

	var header ast.TableRow
	alignments := make([]ast.Alignment, len(keys))
	for _, l := range labels {
		header.Cells = append(header.Cells, ast.NewTableCell(ast.NewInlineRun(l)))
	}
	var rows []ast.TableRow
	for _, item := range spec.Items {
		var row ast.TableRow
		for _, k := range keys {
			text := ""
			if v := item[k]; v != nil {
				text = fmt.Sprint(v)
			}
			row.Cells = append(row.Cells, ast.NewTableCell(ast.NewInlineRun(text)))
		}
		rows = append(rows, row)
	}
	return ast.NewTable(header, rows, alignments), true
}

// ---- color chips ------------------------------------------------------------

var (
	rgbChip = regexp.MustCompile(`(?i)^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*(?:,\s*[\d.]+%?\s*)?\)$`)
	hslChip = regexp.MustCompile(`(?i)^hsla?\(\s*(\d{1,3})\s*,\s*(\d{1,3})%?\s*,\s*(\d{1,3})%?\s*(?:,\s*[\d.]+%?\s*)?\)$`)
)

// colorChip returns the hex color an inline code span denotes (`#F00`,
// `rgb(255, 0, 0)`, `hsl(0, 100%, 50%)`), or "" when it is not a color.
func colorChip(text string) string {
	if colorChipPattern.MatchString(text) {
		return text
	}
	if m := rgbChip.FindStringSubmatch(text); m != nil {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		if r > 255 || g > 255 || b > 255 {
			return ""
		}
		return fmt.Sprintf("#%02X%02X%02X", r, g, b)
	}
	if m := hslChip.FindStringSubmatch(text); m != nil {
		h, _ := strconv.Atoi(m[1])
		s, _ := strconv.Atoi(m[2])
		l, _ := strconv.Atoi(m[3])
		if s > 100 || l > 100 {
			return ""
		}
		r, g, b := hslToRGB(float64(h%360), float64(s)/100, float64(l)/100)
		return fmt.Sprintf("#%02X%02X%02X", r, g, b)
	}
	return ""
}

func hslToRGB(h, s, l float64) (r, g, b int) {
	c := (1 - abs(2*l-1)) * s
	hp := h / 60
	x := c * (1 - abs(mod2(hp)-1))
	var r1, g1, b1 float64
	switch {
	case hp < 1:
		r1, g1 = c, x
	case hp < 2:
		r1, g1 = x, c
	case hp < 3:
		g1, b1 = c, x
	case hp < 4:
		g1, b1 = x, c
	case hp < 5:
		r1, b1 = x, c
	default:
		r1, b1 = c, x
	}
	m := l - c/2
	round := func(v float64) int { return int((v+m)*255 + 0.5) }
	return round(r1), round(g1), round(b1)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func mod2(v float64) float64 {
	for v >= 2 {
		v -= 2
	}
	return v
}
