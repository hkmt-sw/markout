package parse

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
)

// The preprocessor rewrites flavor-specific syntax that goldmark has no
// extension for into forms the core parser already understands: block
// constructs become CommonMark blocks (callouts become "> [!type]" quotes,
// "::: mermaid" becomes a fenced block, ...) and inline markup becomes the
// internal <mo-*> tags that the AST walker turns into run formatting.

const (
	// tocSentinel is a paragraph that expands to the table of contents.
	tocSentinel = "⟦markout-toc⟧"
	// internalAlert prefixes callout types produced by the preprocessor, which
	// are honored whatever the flavor's own alert rules are.
	internalAlert = "MARKOUT:"
	// Fence languages produced by the preprocessor.
	internalMathLang    = "markout-math"
	internalMermaidLang = "markout-mermaid"
	// widthSep separates an image's alt text from a width hint ("alt¦300",
	// "alt¦50%"). Unlike Obsidian's "|" it is safe inside table cells.
	widthSep = "¦"
	// maxIncludeDepth bounds nested ::include directives.
	maxIncludeDepth = 3
	// maxIncludeBytes caps the size of an included file.
	maxIncludeBytes = 10 * 1024 * 1024
	// maxBlockNesting bounds how deep containers (callouts, quotes, tabs) are
	// rewritten; deeper content is passed through as written.
	maxBlockNesting = 32
	// maxIndent caps a line's leading whitespace. Real documents never come
	// close, and unbounded indentation lets a small file nest lists thousands
	// of levels deep, which the Markdown parser takes minutes to resolve.
	maxIndent = 200
)

type preprocessor struct {
	f           flavor.Features
	root        string           // directory of the top-level document; includes may not leave it
	baseDir     string           // directory of the file being processed
	depth       int              // include depth
	nesting     int              // container depth of the blocks being rewritten
	notes       []string         // definitions collected from ^[inline notes]
	frontMatter *ast.FrontMatter // metadata found in a non-YAML/TOML form
	left        *leftOut         // what was left out, shared with included files
}

func newPreprocessor(f flavor.Features, baseDir string) *preprocessor {
	return &preprocessor{f: f, root: baseDir, baseDir: baseDir, left: &leftOut{count: map[string]int{}}}
}

// leftOut counts the blocks that were left out of the document because
// they have no equivalent in one: what they show is made by the platform
// the Markdown was written for.
type leftOut struct {
	names []string // in order of first appearance
	count map[string]int

	// Directives markout does not know. Their content is kept, as text.
	unknown      []string
	unknownCount map[string]int

	// Files a document asks to have put in that could not be read.
	unread      []string
	unreadCount map[string]int
}

func (l *leftOut) addUnread(name string) {
	if l.unreadCount == nil {
		l.unreadCount = map[string]int{}
	}
	if l.unreadCount[name] == 0 {
		l.unread = append(l.unread, name)
	}
	l.unreadCount[name]++
}

func (l *leftOut) addUnknown(name string) {
	if l.unknownCount == nil {
		l.unknownCount = map[string]int{}
	}
	if l.unknownCount[name] == 0 {
		l.unknown = append(l.unknown, name)
	}
	l.unknownCount[name]++
}

func (l *leftOut) add(name string) {
	if l.count[name] == 0 {
		l.names = append(l.names, name)
	}
	l.count[name]++
}

// warnings says what was left out and what was not understood, or nothing.
func (l *leftOut) warnings() []string {
	list := func(names []string, count map[string]int) (int, string) {
		total := 0
		parts := make([]string, len(names))
		for i, name := range names {
			total += count[name]
			parts[i] = name
			if n := count[name]; n > 1 {
				parts[i] = fmt.Sprintf("%s (%d)", name, n)
			}
		}
		return total, strings.Join(parts, ", ")
	}
	var out []string
	if total, names := list(l.names, l.count); total > 0 {
		what := "parts of the source have no equivalent in a document and were"
		if total == 1 {
			what = "part of the source has no equivalent in a document and was"
		}
		out = append(out, fmt.Sprintf("%d %s left out: %s", total, what, names))
	}
	if total, names := list(l.unread, l.unreadCount); total > 0 {
		what := "files named by --8<-- could not be read and are"
		if total == 1 {
			what = "file named by --8<-- could not be read and is"
		}
		out = append(out, fmt.Sprintf("%d %s left out: %s", total, what, names))
	}
	if total, names := list(l.unknown, l.unknownCount); total > 0 {
		what := "directives are not known; their content is"
		if total == 1 {
			what = "directive is not known; its content is"
		}
		out = append(out, fmt.Sprintf("%d %s shown as plain text: %s", total, what, names))
	}
	return out
}

func (p *preprocessor) run(src []byte) []byte {
	s := strings.ReplaceAll(string(src), "\r\n", "\n")
	lines := p.metadata(strings.Split(s, "\n"))

	// YAML/TOML front matter belongs to the front matter extension: keep it
	// away from the rewrites below.
	var head []string
	if p.f.FrontMatter && len(lines) > 0 {
		if d := strings.TrimSpace(lines[0]); d == "---" || d == "+++" {
			for end := 1; end < len(lines); end++ {
				if strings.TrimSpace(lines[end]) == d {
					head, lines = lines[:end+1], lines[end+1:]
					break
				}
			}
		}
	}

	if p.f.MDX {
		lines = mdxDedent(lines)
	}
	lines = append(head[:len(head):len(head)], p.blocks(lines)...)
	if len(p.notes) > 0 {
		lines = append(lines, "")
		for _, n := range p.notes {
			lines = append(lines, n, "")
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// ---- metadata ---------------------------------------------------------------

var mmdMetaLine = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9 _-]*):\s+(\S.*)$`)

// metadata strips the leading metadata forms the front matter extension does
// not know (JSON front matter, MultiMarkdown headers, Pandoc title blocks).
func (p *preprocessor) metadata(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}

	if p.f.JSONFrontMatter && strings.TrimSpace(lines[0]) == ";;;" {
		for end := 1; end < len(lines); end++ {
			if strings.TrimSpace(lines[end]) != ";;;" {
				continue
			}
			var raw map[string]any
			if json.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &raw) == nil {
				p.frontMatter = frontMatterFromMap(raw)
				return lines[end+1:]
			}
			break
		}
	}

	if p.f.TitleBlock && strings.HasPrefix(lines[0], "% ") {
		raw := map[string]any{}
		keys := []string{"title", "author", "date"}
		n := 0
		for n < len(lines) && n < len(keys) && strings.HasPrefix(lines[n], "%") {
			if v := strings.TrimSpace(strings.TrimPrefix(lines[n], "%")); v != "" {
				raw[keys[n]] = v
			}
			n++
		}
		p.frontMatter = frontMatterFromMap(raw)
		return lines[n:]
	}

	if p.f.MetadataBlock && mmdMetaLine.MatchString(lines[0]) {
		raw := map[string]any{}
		last := ""
		n := 0
		for ; n < len(lines) && strings.TrimSpace(lines[n]) != ""; n++ {
			if m := mmdMetaLine.FindStringSubmatch(lines[n]); m != nil {
				last = strings.ToLower(strings.TrimSpace(m[1]))
				raw[last] = strings.TrimSpace(m[2])
			} else if last != "" {
				raw[last] = fmt.Sprintf("%v %s", raw[last], strings.TrimSpace(lines[n]))
			}
		}
		p.frontMatter = frontMatterFromMap(raw)
		return lines[n:]
	}

	return lines
}

// ---- blocks -----------------------------------------------------------------

var (
	fenceOpen     = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \t]*(.*)$")
	colonFence    = regexp.MustCompile(`^ {0,3}(:{3,})[ \t]*(.*)$`)
	directiveHead = regexp.MustCompile(`^\{([A-Za-z][\w:.+-]*)\}[ \t]*(.*)$`)
	bangAdmon     = regexp.MustCompile(`^(?:!!!|\?\?\?\+?)[ \t]+([\w-]+)((?:[ \t]+[\w-]+)*)(?:[ \t]+"(.*)")?[ \t]*$`)
	contentTab    = regexp.MustCompile(`^===\+?[ \t]+"(.*)"[ \t]*$`)
	mathLabelEnd  = regexp.MustCompile(`^\$\$\s*\([\w.:-]+\)$`)
	mathEnvBegin  = regexp.MustCompile(`^\\begin\{((?:equation|multline|gather|align|alignat|flalign|eqnarray)\*?)\}`)
	mystTarget    = regexp.MustCompile(`^\([\w.:-]+\)=[ \t]*$`)
	ialOnlyLine   = regexp.MustCompile(`^[ \t]*\{:[^}]*\}[ \t]*$`)
	ialTrailing   = regexp.MustCompile(`[ \t]*\{:[^}]*\}[ \t]*$`)
	listItemLine  = regexp.MustCompile(`^[ \t]*(?:[*+-]|\d+[.)])[ \t]+\S`)
	abbrDef       = regexp.MustCompile(`^\*\[[^\]]+\]:`)
	includeLine   = regexp.MustCompile(`^::include\{file=([^}]+)\}[ \t]*$`)
	snippetLine   = regexp.MustCompile(`^-{2,}8<-{2,}\s+"([^"]+)"$`)
	snippetFence  = regexp.MustCompile(`^-{2,}8<-{2,}$`)
	markdownTag   = regexp.MustCompile(`^<(div|span|figure|section|article|aside)\b[^<>]*\smarkdown(?:="[^"]*")?[^<>]*>$`)
	materialKeys  = regexp.MustCompile(`\+\+([A-Za-z0-9][A-Za-z0-9-]*(?:\+[A-Za-z0-9][A-Za-z0-9-]*)*)\+\+`)
	materialIcon  = regexp.MustCompile(`:(?:material|fontawesome|octicons|simple)-[a-z0-9-]+:(?:\{[^}]*\})?`)
	mdxImport     = regexp.MustCompile(`^import\s+(?:.+\sfrom\s+)?['"][^'"]+['"];?[ \t]*$`)
	mdxTabItem    = regexp.MustCompile(`^<TabItem\b([^>]*)>$`)
	mdxComponent  = regexp.MustCompile(`<([A-Z][\w.]*)(?:\s[^<>]*)?/>`)
	mdxTabsTag    = regexp.MustCompile(`^</?Tabs\b[^>]*>$|^</TabItem>$`)
	mdxExport     = regexp.MustCompile(`^export\s+(?:const|let|var|default|function)\b.*$`)
	taskNA        = regexp.MustCompile(`^([ \t]*(?:[*+-]|\d+[.)])[ \t]+)\[~\][ \t]+(.*)$`)
	optionLine    = regexp.MustCompile(`^:([\w-]+):[ \t]*(.*)$`)
	blockIDSuffix = regexp.MustCompile(`[ \t]+\^[A-Za-z0-9-]+[ \t]*$`)
)

// blocks rewrites block-level constructs line by line, leaving the contents of
// fenced code blocks untouched.
func (p *preprocessor) blocks(lines []string) []string {
	if p.nesting >= maxBlockNesting {
		return lines
	}
	p.nesting++
	defer func() { p.nesting-- }()

	f := p.f
	var out []string
	inComment := false
	var wrappers []string // HTML tags with the markdown attribute that are open

	var colonClose map[int]int
	if f.MermaidColon || f.ColonAdmonitions || f.Directives {
		colonClose = matchColonFences(lines)
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if f.Comments {
			line = stripPercentComments(line, &inComment)
			if inComment && strings.TrimSpace(line) == "" {
				continue
			}
		}

		if m := fenceOpen.FindStringSubmatch(line); m != nil {
			end := fenceEnd(lines, i+1, m[1])
			repl, verbatim := p.fenced(line, m[1], strings.TrimSpace(m[2]), lines[i+1:end])
			out = append(out, repl...)
			if verbatim && end < len(lines) {
				out = append(out, lines[end])
			}
			i = end
			continue
		}

		if n := len(line) - len(strings.TrimLeft(line, " \t")); n > maxIndent {
			line = line[n-maxIndent:]
		}

		trimmed := strings.TrimSpace(line)

		if f.BlockAttributes && trimmed == "{::comment}" {
			for i+1 < len(lines) && strings.TrimSpace(lines[i]) != "{:/comment}" {
				i++
			}
			continue
		}

		if f.MultilineBlockquote && trimmed == ">>>" {
			end := i + 1
			for end < len(lines) && strings.TrimSpace(lines[end]) != ">>>" {
				end++
			}
			out = append(out, "")
			out = append(out, prefixLines("> ", p.blocks(lines[i+1:end]))...)
			out = append(out, "")
			i = end
			continue
		}

		// A fence without a closing ":::" is not a block; it stays as text.
		if end, closed := colonClose[i]; closed {
			if m := colonFence.FindStringSubmatch(line); m != nil && m[2] != "" {
				if repl, ok := p.colonBlock(strings.TrimSpace(m[2]), lines[i+1:end]); ok {
					out = append(out, repl...)
					i = end
					continue
				}
			}
		}

		if f.BangAdmonitions {
			if m := bangAdmon.FindStringSubmatch(line); m != nil {
				body, next := indentedBody(lines, i+1)
				out = append(out, p.alert(m[1], m[3], p.blocks(body))...)
				i = next - 1
				continue
			}
		}

		if f.ContentTabs {
			if m := contentTab.FindStringSubmatch(line); m != nil {
				body, next := indentedBody(lines, i+1)
				out = append(out, "", "**"+m[1]+"**", "")
				out = append(out, p.blocks(body)...)
				out = append(out, "")
				i = next - 1
				continue
			}
		}

		if f.Directives && mystTarget.MatchString(line) {
			continue
		}
		if f.LineComments && (line == "%" || strings.HasPrefix(line, "% ")) {
			continue
		}

		// MyST closes a display formula with its label: "$$ (label)". The
		// label is where references point, and not part of the document
		if f.Directives && f.MathDollar && mathLabelEnd.MatchString(trimmed) {
			out = append(out, "$$")
			continue
		}

		// LaTeX environments that are formulas by themselves, at the top
		// level as MyST and many LaTeX-minded writers have them:
		//	\begin{align} ... \end{align}
		if (f.Directives || f.MathParens) && leadingSpaces(line) < 4 {
			if m := mathEnvBegin.FindStringSubmatch(trimmed); m != nil {
				if end := mathEnvEnd(lines, i, m[1]); end >= 0 {
					out = append(out, "", "```"+internalMathLang)
					out = append(out, lines[i:end+1]...)
					out = append(out, "```", "")
					i = end
					continue
				}
			}
		}

		// LaTeX display math with the delimiters on their own lines:
		//	\[
		//	a = b
		//	\]
		if f.MathParens && trimmed == `\[` && leadingSpaces(line) < 4 {
			if end := mathBlockEnd(lines, i+1); end > 0 {
				out = append(out, "", "$$")
				out = append(out, lines[i+1:end]...)
				out = append(out, "$$", "")
				i = end
				continue
			}
		}
		// The same on one line. Escaped brackets are also how plain "[text]"
		// is written, so the content has to look like a formula.
		if f.MathParens && len(trimmed) > 4 && strings.HasPrefix(trimmed, `\[`) && strings.HasSuffix(trimmed, `\]`) &&
			leadingSpaces(line) < 4 {
			if body := strings.TrimSpace(trimmed[2 : len(trimmed)-2]); looksLikeMath(body) && !strings.Contains(body, `\]`) {
				out = append(out, "", "$$", body, "$$", "")
				continue
			}
		}

		// A whole display formula on one line: the math extension only closes
		// a block whose delimiters sit on their own lines.
		if f.MathDollar && len(trimmed) > 4 && strings.HasPrefix(trimmed, "$$") && strings.HasSuffix(trimmed, "$$") &&
			!strings.Contains(trimmed[2:len(trimmed)-2], "$$") && leadingSpaces(line) < 4 {
			out = append(out, "", "$$", strings.TrimSpace(trimmed[2:len(trimmed)-2]), "$$", "")
			continue
		}

		// A formula that opens a line of text would be taken for the start of
		// a display block: write it with the inline delimiters instead.
		if f.MathDollar && strings.HasPrefix(line, "$$") && len(trimmed) > 4 {
			if end := strings.Index(line[2:], "$$"); end > 0 && strings.TrimSpace(line[end+4:]) != "" {
				line = "$" + line[2:end+2] + "$" + line[end+4:]
			}
		}

		if f.ImageAttributes {
			// Before attribute lists are stripped, which would take these along.
			line = mapOutsideCode(line, imageAttributes)
		}

		if f.BlockAttributes {
			if listItemLine.MatchString(line) && i+1 < len(lines) && isKramdownTOC(lines[i+1]) && p.hasTOC("{:toc}") {
				out = append(out, "", tocSentinel, "")
				i++
				continue
			}
			if ialOnlyLine.MatchString(line) {
				continue
			}
			line = ialTrailing.ReplaceAllString(line, "")
		}

		if f.Abbreviations && abbrDef.MatchString(line) {
			continue
		}

		if p.hasTOC(trimmed) {
			out = append(out, "", tocSentinel, "")
			continue
		}
		if f.MermaidColon && trimmed == "[[_TOSP_]]" {
			continue // table of subpages: nothing to list in a single document
		}

		if f.Includes {
			if m := includeLine.FindStringSubmatch(trimmed); m != nil {
				if inc, ok := p.include(strings.TrimSpace(m[1])); ok {
					out = append(out, inc...)
					continue
				}
			}
		}

		if f.Material {
			// --8<-- "file.md" puts a file in, as ::include does and under
			// the same rules; a file that cannot be read is named
			if m := snippetLine.FindStringSubmatch(trimmed); m != nil {
				out = append(out, p.snippet(m[1])...)
				continue
			}
			if snippetFence.MatchString(trimmed) {
				// Between two such lines, a file a line
				end := i + 1
				for end < len(lines) && !snippetFence.MatchString(strings.TrimSpace(lines[end])) {
					end++
				}
				if end < len(lines) {
					for _, file := range lines[i+1 : end] {
						if file = strings.TrimSpace(file); file != "" && !strings.HasPrefix(file, ";") {
							out = append(out, p.snippet(file)...)
						}
					}
					i = end
					continue
				}
			}
			// <div class="grid" markdown> holds Markdown: the tag goes, what
			// is in it stays
			if m := markdownTag.FindStringSubmatch(trimmed); m != nil {
				wrappers = append(wrappers, m[1])
				out = append(out, "")
				continue
			}
			if n := len(wrappers); n > 0 && trimmed == "</"+wrappers[n-1]+">" {
				wrappers = wrappers[:n-1]
				out = append(out, "")
				continue
			}
		}

		if f.MDX && (mdxImport.MatchString(line) || mdxExport.MatchString(line)) {
			continue
		}
		if f.MDX {
			// <Tabs> hold <TabItem label="...">s, shown one after the
			// other, each under its label
			tag := strings.TrimSpace(line)
			if m := mdxTabItem.FindStringSubmatch(tag); m != nil {
				label := mdxAttr(m[1], "label")
				if label == "" {
					label = mdxAttr(m[1], "value")
				}
				out = append(out, "")
				if label != "" {
					out = append(out, "**"+label+"**", "")
				}
				continue
			}
			if mdxTabsTag.MatchString(tag) {
				out = append(out, "")
				continue
			}
			// A component that stands by itself (<Chart data={x} />) draws
			// something on the site that a document cannot have. It is
			// left out, and the conversion says so
			mapOutsideCode(line, func(s string) string {
				for _, m := range mdxComponent.FindAllStringSubmatch(s, -1) {
					p.left.add("<" + m[1] + " />")
				}
				return s
			})
		}

		if f.InapplicableTasks {
			if m := taskNA.FindStringSubmatch(line); m != nil {
				line = m[1] + "[ ] ~~" + m[2] + "~~"
			}
		}

		if f.BlockIDs {
			line = blockIDSuffix.ReplaceAllString(line, "")
		}

		out = append(out, p.inline(line))
	}
	return out
}

func (p *preprocessor) hasTOC(marker string) bool {
	if marker == "" {
		return false
	}
	for _, m := range p.f.TOCMarkers {
		if m == marker {
			return true
		}
	}
	return false
}

func isKramdownTOC(line string) bool {
	t := strings.ReplaceAll(strings.TrimSpace(line), " ", "")
	return t == "{:toc}"
}

// fenceEnd returns the index of the line closing a fence opened with marker,
// or len(lines) when the fence runs to the end of the document.
func fenceEnd(lines []string, from int, marker string) int {
	for i := from; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if len(t) >= len(marker) && strings.Trim(t, marker[:1]) == "" && leadingSpaces(lines[i]) < 4 {
			return i
		}
	}
	return len(lines)
}

// matchColonFences pairs every ":::" opener (a fence line with a name or
// attributes) with the bare ":::" line that closes it, in one pass. Openers
// that are never closed are left out.
func matchColonFences(lines []string) map[int]int {
	var match map[int]int
	var open []int
	for i, line := range lines {
		if !strings.Contains(line, ":::") {
			continue
		}
		m := colonFence.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[2] != "" {
			open = append(open, i)
		} else if n := len(open); n > 0 {
			if match == nil {
				match = make(map[int]int)
			}
			match[open[n-1]] = i
			open = open[:n-1]
		}
	}
	return match
}

// indentedBody collects the lines following an admonition header that are
// indented by four spaces (or a tab), dedented. It returns the index of the
// first line that is not part of the body.
func indentedBody(lines []string, from int) ([]string, int) {
	var body []string
	i := from
	for ; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.TrimSpace(l) == "":
			body = append(body, "")
		case strings.HasPrefix(l, "    "):
			body = append(body, l[4:])
		case strings.HasPrefix(l, "\t"):
			body = append(body, l[1:])
		default:
			goto done
		}
	}
done:
	// Trailing blank lines belong to the surrounding document.
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
		i--
	}
	return body, i
}

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

func prefixLines(prefix string, lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(prefix+l, " ")
	}
	return out
}

func trimBlankLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// alert emits a callout in the internal "> [!MARKOUT:type] title" form. body
// must already be preprocessed.
func (p *preprocessor) alert(kind, title string, body []string) []string {
	out := []string{"", strings.TrimRight("> [!"+internalAlert+strings.ToLower(kind)+"] "+title, " ")}
	out = append(out, prefixLines("> ", trimBlankLines(body))...)
	return append(out, "")
}

func unwrap(body []string) []string {
	out := []string{""}
	out = append(out, body...)
	return append(out, "")
}

// fenced handles a fenced code block. Its body is passed through verbatim
// (and the caller keeps the closing line) unless the fence is a MyST
// directive, which replaces the whole block.
func (p *preprocessor) fenced(open, marker, info string, body []string) (lines []string, verbatim bool) {
	if strings.HasPrefix(info, "{") {
		if p.f.Directives {
			if m := directiveHead.FindStringSubmatch(info); m != nil {
				return p.directive(m[1], strings.TrimSpace(m[2]), body), false
			}
		}
		// Pandoc attribute syntax: ```{.python .numberLines}
		if end := strings.Index(info, "}"); end > 0 {
			lang := ""
			for _, a := range strings.Fields(info[1:end]) {
				if strings.HasPrefix(a, ".") {
					lang = a[1:]
					break
				}
			}
			open = open[:strings.Index(open, marker)+len(marker)] + lang
		}
	}
	// A title given to the block ("py title="main.py"", as documentation
	// sites show above the code) goes above it
	if m := fenceTitle.FindStringSubmatch(info); m != nil {
		if title := strings.TrimSpace(m[1] + m[2]); title != "" {
			indent := open[:len(open)-len(strings.TrimLeft(open, " \t"))]
			out := []string{indent + "**" + title + "**", ""}
			return append(append(out, open), body...), true
		}
	}
	return append([]string{open}, body...), true
}

var fenceTitle = regexp.MustCompile(`(?:^|\s)title=(?:"([^"]*)"|'([^']*)')`)

// colonBlock converts the body of a ":::" fence. ok is false when the fence is
// not one the flavor understands and the lines should be left alone.
func (p *preprocessor) colonBlock(head string, body []string) ([]string, bool) {
	f := p.f
	word := head
	if i := strings.IndexAny(head, " \t[{"); i >= 0 {
		word = head[:i]
	}

	if f.MermaidColon {
		switch word {
		case "mermaid":
			out := []string{"", "```" + internalMermaidLang}
			out = append(out, body...)
			return append(out, "```", ""), true
		case "video", "query-table":
			p.left.add("::: " + word) // embedded widgets have no document equivalent
			return nil, true
		}
	}

	if f.Directives {
		if m := directiveHead.FindStringSubmatch(head); m != nil {
			return p.directive(m[1], strings.TrimSpace(m[2]), body), true
		}
	}

	if !f.ColonAdmonitions {
		return nil, false
	}

	kind, title := word, strings.TrimSpace(head[len(word):])
	if strings.HasPrefix(head, "{") {
		// Pandoc attributes: ::: {.callout-note title="Heads up"}
		kind, title = "", ""
		end := strings.Index(head, "}")
		if end < 0 {
			end = len(head)
		}
		for _, a := range strings.Fields(head[1:end]) {
			if strings.HasPrefix(a, ".") && kind == "" {
				c := strings.TrimPrefix(a[1:], "callout-")
				if _, ok := alertTypeFor(c); ok {
					kind = c
				}
			}
		}
		if m := regexp.MustCompile(`title="([^"]*)"`).FindStringSubmatch(head); m != nil {
			title = m[1]
		}
	} else if strings.HasPrefix(title, "[") && strings.HasSuffix(title, "]") {
		title = title[1 : len(title)-1] // Docusaurus :::note[Title]
	}

	inner := p.blocks(body)
	if _, ok := alertTypeFor(kind); !ok {
		return unwrap(inner), true // a generic container: keep its content
	}
	return p.alert(kind, title, inner), true
}

// directive converts a MyST directive (```{name} args or :::{name} args).
func (p *preprocessor) directive(name, args string, body []string) []string {
	opts := map[string]string{}
	if len(body) > 0 && strings.TrimSpace(body[0]) == "---" {
		for i := 1; i < len(body); i++ {
			if strings.TrimSpace(body[i]) == "---" {
				body = body[i+1:]
				break
			}
			if k, v, ok := strings.Cut(body[i], ":"); ok {
				opts[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	for len(body) > 0 {
		m := optionLine.FindStringSubmatch(body[0])
		if m == nil {
			break
		}
		opts[m[1]] = m[2]
		body = body[1:]
		// A value can go on over the following lines, each after a colon
		//	:header: >
		//	:    "a", "b"
		if m[2] == ">" || m[2] == "|" {
			var value []string
			for len(body) > 0 && optionMore.MatchString(body[0]) {
				value = append(value, strings.TrimSpace(body[0][1:]))
				body = body[1:]
			}
			opts[m[1]] = strings.Join(value, " ")
		}
	}
	body = trimBlankLines(body)

	name = strings.ToLower(name)
	switch name {
	case "math":
		// Formulas separated by a blank line are formulas of their own
		if args != "" {
			body = append([]string{args}, body...)
		}
		var out []string
		var formula []string
		flush := func() {
			if len(formula) > 0 {
				out = append(out, "", "```"+internalMathLang)
				out = append(out, formula...)
				out = append(out, "```", "")
				formula = nil
			}
		}
		for _, line := range body {
			if strings.TrimSpace(line) == "" {
				flush()
				continue
			}
			formula = append(formula, line)
		}
		flush()
		return out

	case "mermaid":
		out := []string{"", "```" + internalMermaidLang}
		out = append(out, body...)
		return append(out, "```", "")

	case "code-block", "code", "sourcecode", "code-cell", "literalinclude":
		lang := ""
		if fs := strings.Fields(args); len(fs) > 0 && name != "literalinclude" {
			lang = fs[0]
		}
		out := []string{""}
		// The caption of a block (:caption: main.py) goes above it, like
		// the title of a fenced block
		if caption := opts["caption"]; caption != "" {
			out = append(out, "**"+caption+"**", "")
		}
		out = append(out, "```"+lang)
		out = append(out, body...)
		return append(out, "```", "")

	case "contents", "toc", "tableofcontents":
		return []string{"", tocSentinel, ""}

	case "figure", "image":
		alt := opts["alt"]
		if w := widthHint(opts["width"]); w != "" {
			alt += widthSep + w
		}
		out := []string{"", "![" + alt + "](<" + args + ">)", ""}
		out = append(out, p.blocks(body)...) // figure caption
		return append(out, "")

	case "eval-rst":
		// reStructuredText is not read, but what is written in it is not
		// dropped either: it is shown as it is
		out := []string{"", "```rst"}
		out = append(out, body...)
		return append(out, "```", "")

	case "toctree":
		// The pages of the project that belong under this one. They are not
		// part of this document, but which they are is: they are listed
		var out []string
		for _, line := range body {
			entry := strings.TrimSpace(line)
			if m := roleTarget.FindStringSubmatch(entry); m != nil && m[1] != "" {
				entry = m[1] // "Title <page>"
			}
			if entry != "" && entry != "self" {
				out = append(out, "- "+entry)
			}
		}
		if len(out) == 0 {
			return nil
		}
		if caption := opts["caption"]; caption != "" {
			out = append([]string{"**" + caption + "**", ""}, out...)
		}
		return append(append([]string{""}, out...), "")

	case "raw":
		// Markup for one output format. HTML is read like any HTML in the
		// document; markup for another format has no place here
		if format := strings.Fields(strings.ToLower(args)); len(format) > 0 && format[0] == "html" {
			return append(append([]string{""}, body...), "")
		}
		p.left.add("{raw} " + args)
		return nil

	case "bibliography", "index":
		// Lists Sphinx gathers from the whole project
		p.left.add("{" + name + "}")
		return nil

	case "admonition":
		kind := "note"
		for _, c := range strings.Fields(opts["class"]) {
			if _, ok := alertTypeFor(c); ok {
				kind = c
			}
		}
		return p.alert(kind, args, p.blocks(body))

	case "versionadded", "versionchanged", "deprecated", "versionremoved":
		// "Added in version 1.2: what was added"
		label := map[string]string{
			"versionadded": "Added in version", "versionchanged": "Changed in version",
			"deprecated": "Deprecated since version", "versionremoved": "Removed in version",
		}[name]
		version, rest, _ := strings.Cut(args, " ")
		head := "*" + label + " " + version + "*"
		if rest = strings.TrimSpace(rest); rest != "" {
			head += ": " + rest
		}
		inner := unwrap(p.blocks(body))
		if text := strings.TrimSpace(strings.Join(inner, "\n")); text != "" {
			head += ":"
		}
		return append(append([]string{"", head, ""}, inner...), "")

	case "tab-set", "tabs", "grid", "table":
		// Containers: what they hold is shown, under their caption if any
		out := []string{""}
		if args != "" && name == "table" {
			out = append(out, "**"+args+"**", "")
		}
		return append(append(out, p.blocks(body)...), "")

	case "line-block":
		// Every line is a line
		var out []string
		for _, line := range body {
			if strings.TrimSpace(line) != "" {
				out = append(out, p.inline(strings.TrimSpace(line))+"\\")
			}
		}
		if len(out) == 0 {
			return nil
		}
		out[len(out)-1] = strings.TrimSuffix(out[len(out)-1], "\\")
		return append(append([]string{""}, out...), "")

	case "only":
		// Content for one output format or another; a document shows it
		return unwrap(p.blocks(body))

	case "list-table", "csv-table":
		var rows [][]string
		header := 0
		if name == "list-table" {
			rows = listTableRows(body)
			header, _ = strconv.Atoi(opts["header-rows"])
		} else {
			rows = csvRows(strings.Join(body, "\n"))
			if head := csvRows(opts["header"]); len(head) == 1 {
				rows, header = append(head, rows...), 1
			}
		}
		if table := pipeTable(rows, header > 0); table != nil {
			out := []string{""}
			if args != "" {
				out = append(out, "**"+args+"**", "")
			}
			out = append(out, table...)
			return append(out, "")
		}
		// Not in the form expected: the content is kept as it is

	case "dropdown", "card", "tab-item", "topic", "sidebar", "margin":
		out := []string{""}
		if args != "" {
			out = append(out, "**"+args+"**", "")
		}
		out = append(out, p.blocks(body)...)
		return append(out, "")
	}

	if _, ok := alertTypeFor(name); ok {
		// The argument of a plain admonition is the start of its content.
		if args != "" {
			body = append([]string{args, ""}, body...)
		}
		return p.alert(name, "", p.blocks(body))
	}

	// A directive markout does not know keeps what it holds, argument
	// included, and the conversion says that it was not understood
	if name != "list-table" && name != "csv-table" {
		p.left.addUnknown("{" + name + "}")
	}
	if args != "" {
		body = append([]string{args, ""}, body...)
	}
	return unwrap(p.blocks(body))
}

var (
	listTableRow = regexp.MustCompile(`^[*+-]\s+-(?:\s+(.*))?$`)
	optionMore   = regexp.MustCompile(`^:\s+\S`)
)

// listTableRows reads the body of a list-table: a list of rows, each a list
// of cells.
//
//   - - Name
//   - Value
//   - - a
//   - 1
func listTableRows(body []string) [][]string {
	var rows [][]string
	for _, line := range body {
		text := strings.TrimSpace(line)
		switch m := listTableRow.FindStringSubmatch(text); {
		case m != nil:
			rows = append(rows, []string{strings.TrimSpace(m[1])})
		case len(rows) == 0:
			if text != "" {
				return nil // something else comes first: not a list of rows
			}
		case strings.HasPrefix(text, "- ") || text == "-":
			rows[len(rows)-1] = append(rows[len(rows)-1], strings.TrimSpace(text[1:]))
		case text != "":
			// A cell goes on over several lines
			cells := rows[len(rows)-1]
			cells[len(cells)-1] = strings.TrimSpace(cells[len(cells)-1] + " " + text)
		}
	}
	return rows
}

// csvRows reads comma-separated values, as a csv-table has them.
func csvRows(text string) [][]string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil
	}
	return rows
}

// pipeTable writes rows as a Markdown table. Its first row is the header; a
// table that has none gets an empty one, which a Markdown table must have.
func pipeTable(rows [][]string, header bool) []string {
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	if columns == 0 {
		return nil
	}
	if !header {
		rows = append([][]string{make([]string, columns)}, rows...)
	}
	line := func(cells []string) string {
		var b strings.Builder
		b.WriteString("|")
		for c := 0; c < columns; c++ {
			cell := ""
			if c < len(cells) {
				cell = strings.ReplaceAll(strings.Join(strings.Fields(cells[c]), " "), "|", "\\|")
			}
			b.WriteString(" " + cell + " |")
		}
		return b.String()
	}
	out := []string{line(rows[0]), "|" + strings.Repeat(" --- |", columns)}
	for _, row := range rows[1:] {
		out = append(out, line(row))
	}
	return out
}

var (
	mdxOpenTag  = regexp.MustCompile(`^<[A-Z][\w.]*(?:\s[^<>]*)?>$`)
	mdxCloseTag = regexp.MustCompile(`^</[A-Z][\w.]*>$`)
)

// mdxDedent takes away indentation that means nothing in MDX. There, text
// indented by four spaces is text, and writers indent the content of <Tabs>
// and the like as they would indent code; in Markdown four spaces make a
// code block. The content of a component is moved left by the indentation
// of its first line, which keeps the lists and code in it as they are, and
// outside of lists nothing is left indented far enough to become code.
func mdxDedent(lines []string) []string {
	out := make([]string, len(lines))
	var inside []int // per open component: its content's indentation, -1 until seen
	fence := ""      // the marker of the code block being passed through
	fenceStrip := 0  // how far that block was moved left
	inList := false  // in a list, indentation says what belongs to an item
	for i, line := range lines {
		text := strings.TrimSpace(line)
		switch {
		case fence == "" && mdxCloseTag.MatchString(text):
			if len(inside) > 0 {
				inside = inside[:len(inside)-1]
			}
			out[i], inList = text, false
			continue
		case fence == "" && mdxOpenTag.MatchString(text) && !strings.HasSuffix(text, "/>"):
			inside = append(inside, -1)
			out[i], inList = text, false
			continue
		}

		// Out of the component
		if len(inside) > 0 && text != "" {
			base := &inside[len(inside)-1]
			if *base < 0 {
				*base = leadingSpaces(line)
			}
			line = line[min(leadingSpaces(line), *base):]
		}
		lead := leadingSpaces(line)

		if fence != "" {
			if strings.HasPrefix(text, fence) && strings.Trim(text, fence[:1]) == "" {
				fence = ""
			}
			out[i] = line[min(lead, fenceStrip):]
			continue
		}
		switch {
		case text == "":
		case listItemLine.MatchString(line):
			inList = true
		case lead == 0:
			inList = false
		}
		opens := strings.HasPrefix(text, "```") || strings.HasPrefix(text, "~~~")
		strip := 0
		if !inList && lead >= 4 {
			strip = lead
		}
		if opens {
			fence, fenceStrip = text[:3], strip
		}
		out[i] = line[strip:]
	}
	return out
}

// snippet reads a file named by --8<--. One that cannot be read (it is not
// there, or it is outside the directory of the document) is left out and
// named in the warning.
func (p *preprocessor) snippet(file string) []string {
	// "file.md:3:8" takes some of its lines; the whole file is what is read
	name := file
	if i := strings.Index(name, ":"); i > 0 {
		name = name[:i]
	}
	if lines, ok := p.include(name); ok {
		return append(append([]string{""}, lines...), "")
	}
	p.left.addUnread(`"` + file + `"`)
	return nil
}

// materialInline rewrites the inline additions of MkDocs Material: keys are
// shown as code, and icons, which are pictures from a font, are left out.
func (p *preprocessor) materialInline(s string) string {
	// ++ctrl+alt+del++ is shown as Ctrl+Alt+Del. The plus signs of "a++b++"
	// are part of words and not keys, and {++text++} is an insertion
	wordChar := func(i int) bool {
		if i < 0 || i >= len(s) {
			return false
		}
		c := s[i]
		return c == '+' || c == '_' || c == '{' || c == '}' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= 0x80
	}
	var b strings.Builder
	last := 0
	for _, m := range materialKeys.FindAllStringSubmatchIndex(s, -1) {
		if wordChar(m[0]-1) || wordChar(m[1]) {
			continue
		}
		keys := strings.Split(s[m[2]:m[3]], "+")
		for i, key := range keys {
			words := strings.Split(key, "-")
			for j, w := range words {
				words[j] = strings.ToUpper(w[:1]) + w[1:]
			}
			keys[i] = "`" + strings.Join(words, " ") + "`"
		}
		b.WriteString(s[last:m[0]])
		b.WriteString(strings.Join(keys, "+"))
		last = m[1]
	}
	b.WriteString(s[last:])
	s = b.String()
	return materialIcon.ReplaceAllStringFunc(s, func(string) string {
		p.left.add("icons")
		return ""
	})
}

// mdxAttr returns the value of an attribute of a JSX tag, as label="x".
func mdxAttr(attrs, name string) string {
	m := regexp.MustCompile(`\b` + name + `=(?:"([^"]*)"|'([^']*)')`).FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}
	return m[1] + m[2]
}

// mathEnvEnd finds the line that ends the LaTeX environment that begins on
// line start, or -1.
func mathEnvEnd(lines []string, start int, env string) int {
	end := `\end{` + env + `}`
	for i := start; i < len(lines) && i < start+maxMathEnvLines; i++ {
		if strings.Contains(lines[i], end) {
			return i
		}
		if i > start && strings.TrimSpace(lines[i]) == "" {
			return -1 // a formula has no blank lines in it
		}
	}
	return -1
}

// maxMathEnvLines is the longest LaTeX environment that is looked for.
const maxMathEnvLines = 200

// include reads a file named by ::include. The file must be a regular file
// inside the directory of the top-level document (or below it): absolute
// paths, "..", and symlinks that lead outside are refused, so a document
// cannot pull arbitrary files from the machine into its output.
func (p *preprocessor) include(rel string) ([]string, bool) {
	if p.depth >= maxIncludeDepth || p.root == "" || filepath.IsAbs(rel) {
		return nil, false
	}
	path := filepath.Join(p.baseDir, rel)
	inRoot, err := filepath.Rel(p.root, path)
	if err != nil {
		return nil, false
	}

	root, err := os.OpenRoot(p.root)
	if err != nil {
		return nil, false
	}
	defer root.Close()

	info, err := root.Stat(inRoot)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxIncludeBytes {
		return nil, false
	}
	file, err := root.Open(inRoot)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxIncludeBytes))
	if err != nil {
		return nil, false
	}

	sub := &preprocessor{f: p.f, root: p.root, baseDir: filepath.Dir(path), depth: p.depth + 1, nesting: p.nesting, left: p.left}
	lines := sub.blocks(strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"))
	p.notes = append(p.notes, sub.notes...)
	return unwrap(lines), true
}

// stripPercentComments removes Obsidian %% comments %% from a line. inComment
// carries an unterminated comment over to the following lines.
func stripPercentComments(line string, inComment *bool) string {
	if !*inComment && !strings.Contains(line, "%%") {
		return line
	}
	var b strings.Builder
	for {
		i := strings.Index(line, "%%")
		if i < 0 {
			if !*inComment {
				b.WriteString(line)
			}
			break
		}
		if !*inComment {
			b.WriteString(line[:i])
		}
		*inComment = !*inComment
		line = line[i+2:]
	}
	return b.String()
}

// ---- inline -----------------------------------------------------------------

var (
	mathBacktick     = regexp.MustCompile("\\$`([^`\n]+)`\\$")
	roleSpan         = regexp.MustCompile("\\{([A-Za-z][\\w:.+-]*)\\}`([^`\n]+)`")
	roleTarget       = regexp.MustCompile(`^(.*?)\s*<[^>]+>$`)
	mathInlineParen  = regexp.MustCompile(`\\\((.+?)\\\)`)
	mathDisplayParen = regexp.MustCompile(`\\\[(.+?)\\\]`)
	mathInlineBS     = regexp.MustCompile(`\\\\\((.+?)\\\\\)`)
	mathDisplayBS    = regexp.MustCompile(`\\\\\[(.+?)\\\\\]`)
	imageSizeEq      = regexp.MustCompile(`(!\[[^\]]*)\]\(([^)\s]+)[ \t]+=(\d*)x(\d*)\)`)
	imageAttrs       = regexp.MustCompile(`(!\[[^\]]*)\]\(([^)]*)\)\{:?([^}]*)\}`)
	attrWidth        = regexp.MustCompile(`width\s*[=:]\s*["']?(\d+(?:\.\d+)?)\s*(px|%)?`)
	imageEmbed       = regexp.MustCompile(`!\[\[([^\]|]+)(?:\|([^\]]*))?\]\]`)
	wikiLink         = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	inlineNote       = regexp.MustCompile(`\^\[([^\]]+)\]`)
	criticSub        = regexp.MustCompile(`\{~~(.+?)~>(.+?)~~\}`)
	criticAdd        = regexp.MustCompile(`\{\+\+(.+?)\+\+\}`)
	criticDel        = regexp.MustCompile(`\{--(.+?)--\}`)
	criticMark       = regexp.MustCompile(`\{==(.+?)==\}`)
	criticNote       = regexp.MustCompile(`\{>>(.+?)<<\}`)
	diffAdd          = regexp.MustCompile(`\{\+\s?(.+?)\s?\+\}|\[\+\s?(.+?)\s?\+\]`)
	diffDel          = regexp.MustCompile(`\{-\s?(.+?)\s?-\}|\[-\s?(.+?)\s?-\]`)
	bracketedSpan    = regexp.MustCompile(`\[([^\]\n]+)\]\{\.(underline|ul|mark|smallcaps)\}`)
	mdxComment       = regexp.MustCompile(`\{/\*.*?\*/\}`)
	imageExtension   = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|svg|webp|bmp|avif)$`)
)

// inline rewrites the inline extensions of one line.
func (p *preprocessor) inline(line string) string {
	f := p.f
	// These contain backticks themselves, so they run before code spans are
	// set aside.
	if f.MathBacktick {
		line = mathBacktick.ReplaceAllStringFunc(line, func(m string) string {
			return "$" + m[2:len(m)-2] + "$"
		})
	}
	if f.Directives {
		line = roleSpan.ReplaceAllStringFunc(line, func(m string) string {
			sm := roleSpan.FindStringSubmatch(m)
			return role(sm[1], sm[2])
		})
	}
	return mapOutsideCode(line, p.inlineText)
}

// role renders a MyST {role}`content` span.
func role(name, content string) string {
	switch strings.ToLower(name) {
	case "math":
		return "$" + content + "$"
	case "sub", "subscript":
		return "<mo-sub>" + content + "</mo-sub>"
	case "sup", "superscript":
		return "<mo-sup>" + content + "</mo-sup>"
	case "kbd", "code", "samp", "file", "guilabel", "menuselection", "command", "program":
		return "`" + content + "`"
	}
	// Cross-reference roles: {ref}`Title <target>` shows the title.
	if m := roleTarget.FindStringSubmatch(content); m != nil && m[1] != "" {
		return m[1]
	}
	return content
}

func (p *preprocessor) inlineText(s string) string {
	f := p.f
	if f.Material {
		s = p.materialInline(s)
	}

	if f.MathBrackets {
		s = mathDisplayBS.ReplaceAllStringFunc(s, func(m string) string { return "$$" + m[3:len(m)-3] + "$$" })
		s = mathInlineBS.ReplaceAllStringFunc(s, func(m string) string { return "$" + m[3:len(m)-3] + "$" })
	}

	if f.MathParens {
		s = replaceLatexMath(s, mathDisplayParen, "$$", looksLikeMath)
		s = replaceLatexMath(s, mathInlineParen, "$", func(body string) bool {
			// A lone symbol such as \(x\) is math; several plain words in
			// escaped parentheses are prose.
			return looksLikeMath(body) || !strings.ContainsAny(strings.TrimSpace(body), " \t")
		})
	}

	if f.ImageSizeEquals {
		s = imageSizeEq.ReplaceAllStringFunc(s, func(m string) string {
			sm := imageSizeEq.FindStringSubmatch(m)
			alt := sm[1]
			if sm[3] != "" {
				alt += widthSep + sm[3]
			}
			return alt + "](" + sm[2] + ")"
		})
	}

	if f.ImageEmbeds {
		s = imageEmbed.ReplaceAllStringFunc(s, func(m string) string {
			sm := imageEmbed.FindStringSubmatch(m)
			target := strings.TrimSpace(sm[1])
			if !imageExtension.MatchString(target) {
				return target // an embedded note: keep its name
			}
			alt := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
			if w, _, _ := strings.Cut(sm[2], "x"); isDigits(w) {
				alt += widthSep + w
			}
			return "![" + alt + "](<" + target + ">)"
		})
	}

	if f.WikiLinks != flavor.WikiLinksNone {
		s = wikiLink.ReplaceAllStringFunc(s, func(m string) string {
			sm := wikiLink.FindStringSubmatch(m)
			if strings.HasPrefix(sm[1], "_") {
				return m // [[_TOC_]] and friends
			}
			if sm[2] == "" {
				return sm[1]
			}
			if f.WikiLinks == flavor.WikiLinksLabelTarget {
				return sm[1]
			}
			return sm[2]
		})
	}

	if f.InlineFootnotes {
		s = inlineNote.ReplaceAllStringFunc(s, func(m string) string {
			label := fmt.Sprintf("markout-inline-%d-%d", p.depth, len(p.notes)+1)
			p.notes = append(p.notes, "[^"+label+"]: "+m[2:len(m)-1])
			return "[^" + label + "]"
		})
	}

	if f.CriticMarkup {
		s = criticSub.ReplaceAllString(s, "<mo-del>$1</mo-del><mo-ins>$2</mo-ins>")
		s = criticAdd.ReplaceAllString(s, "<mo-ins>$1</mo-ins>")
		s = criticDel.ReplaceAllString(s, "<mo-del>$1</mo-del>")
		s = criticMark.ReplaceAllString(s, "<mo-mark>$1</mo-mark>")
		s = criticNote.ReplaceAllString(s, "")
	}

	if f.InlineDiff {
		s = diffAdd.ReplaceAllString(s, "<mo-ins>$1$2</mo-ins>")
		s = diffDel.ReplaceAllString(s, "<mo-del>$1$2</mo-del>")
	}

	if f.BracketedSpans {
		s = bracketedSpan.ReplaceAllStringFunc(s, func(m string) string {
			sm := bracketedSpan.FindStringSubmatch(m)
			switch sm[2] {
			case "underline", "ul":
				return "<mo-u>" + sm[1] + "</mo-u>"
			case "mark":
				return "<mo-mark>" + sm[1] + "</mo-mark>"
			}
			return sm[1]
		})
	}

	if f.Highlight {
		s = replaceDelim(s, "==", "mo-mark", true)
	}
	if f.InsertCaret {
		s = replaceDelim(s, "^^", "mo-u", true)
	}
	if f.Superscript {
		s = replaceDelim(s, "^", "mo-sup", false)
	}
	if f.Subscript {
		s = replaceDelim(s, "~", "mo-sub", false)
	}

	if f.MDX {
		s = mdxComment.ReplaceAllString(s, "")
	}
	return s
}

// imageAttributes folds the width of an attribute list that follows an image
// (![a](u){width=50%}) into the image's alt text and drops the list.
func imageAttributes(s string) string {
	return imageAttrs.ReplaceAllStringFunc(s, func(m string) string {
		sm := imageAttrs.FindStringSubmatch(m)
		alt := sm[1]
		if w := widthHint(sm[3]); w != "" {
			alt += widthSep + w
		}
		return alt + "](" + sm[2] + ")"
	})
}

// maxMathBlockLines bounds how far a "\[" line looks for its "\]".
const maxMathBlockLines = 200

// mathBlockEnd returns the index of the "\]" line closing a display formula
// opened on the line before from, or -1 if there is none nearby.
func mathBlockEnd(lines []string, from int) int {
	for i := from; i < len(lines) && i < from+maxMathBlockLines; i++ {
		switch strings.TrimSpace(lines[i]) {
		case `\]`:
			return i
		case `\[`:
			return -1 // another opener first: this one was never closed
		}
	}
	return -1
}

// looksLikeMath reports whether text has something only a formula would: a
// TeX command, a superscript or subscript, or an operator.
func looksLikeMath(body string) bool {
	return strings.ContainsAny(body, `\^_=+<>`) || strings.Contains(body, " - ") ||
		strings.Contains(body, "*") || strings.Contains(body, "/")
}

// replaceLatexMath rewrites \(...\) or \[...\] spans matched by re to the
// dollar delimiters the math extension reads, where accept says the content
// is a formula. A delimiter that is itself escaped ("\\(") is left alone.
func replaceLatexMath(s string, re *regexp.Regexp, delim string, accept func(string) bool) string {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		start, end, body := m[0], m[1], s[m[2]:m[3]]
		if (start > 0 && s[start-1] == '\\') || strings.HasSuffix(body, `\`) || !accept(body) {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(delim + strings.TrimSpace(body) + delim)
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// widthHint extracts a width from an attribute list ("width=300", "50%",
// "300px") in the form the image converter understands: "300" or "50%".
func widthHint(attrs string) string {
	attrs = strings.TrimSpace(attrs)
	if attrs == "" {
		return ""
	}
	num, unit := "", ""
	if m := attrWidth.FindStringSubmatch(attrs); m != nil {
		num, unit = m[1], m[2]
	} else if m := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(px|%)?$`).FindStringSubmatch(attrs); m != nil {
		num, unit = m[1], m[2]
	} else {
		return ""
	}
	if i := strings.Index(num, "."); i >= 0 {
		num = num[:i]
	}
	if unit == "%" {
		return num + "%"
	}
	return num
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// mapOutsideCode applies fn to the parts of a line that are not inside a
// backtick code span.
func mapOutsideCode(line string, fn func(string) string) string {
	if !strings.Contains(line, "`") {
		return fn(line)
	}
	var b strings.Builder
	rest := line
	for {
		open := strings.Index(rest, "`")
		if open < 0 {
			b.WriteString(fn(rest))
			break
		}
		n := 1
		for open+n < len(rest) && rest[open+n] == '`' {
			n++
		}
		ticks := rest[open : open+n]
		// Find a closing run of exactly n backticks.
		close := -1
		for j := open + n; j < len(rest); {
			k := strings.Index(rest[j:], ticks)
			if k < 0 {
				break
			}
			k += j
			end := k + n
			for end < len(rest) && rest[end] == '`' {
				end++
			}
			if end-k == n {
				close = k
				break
			}
			j = end
		}
		if close < 0 {
			b.WriteString(fn(rest))
			break
		}
		b.WriteString(fn(rest[:open]))
		b.WriteString(rest[open : close+n])
		rest = rest[close+n:]
	}
	return b.String()
}

// replaceDelim wraps text enclosed in a pair of delim runs in the given tag.
// A run only counts when it is exactly len(delim) long, so "~sub~" leaves
// "~~strike~~" alone. Without allowSpace the content may not contain spaces,
// which is how Pandoc reads ^super^ and ~sub~script.
func replaceDelim(s, delim, tag string, allowSpace bool) string {
	if !strings.Contains(s, delim) {
		return s
	}
	c, n := delim[0], len(delim)

	// run reports whether an exact-length, unescaped delimiter run starts at i.
	run := func(i int) bool {
		if i+n > len(s) || s[i:i+n] != delim {
			return false
		}
		if i > 0 && (s[i-1] == c || s[i-1] == '\\') {
			return false
		}
		return i+n >= len(s) || s[i+n] != c
	}

	var b strings.Builder
	i := 0
	for i < len(s) {
		open := -1
		for j := i; j < len(s); j++ {
			if run(j) && j+n < len(s) && s[j+n] != ' ' && !(c == '^' && j > 0 && s[j-1] == '[') {
				open = j
				break
			}
		}
		if open < 0 {
			break
		}
		closeAt := -1
		for j := open + n + 1; j < len(s); j++ {
			if !allowSpace && s[j] == ' ' {
				break
			}
			if run(j) && s[j-1] != ' ' {
				closeAt = j
				break
			}
		}
		if closeAt < 0 {
			b.WriteString(s[i : open+n])
			i = open + n
			continue
		}
		b.WriteString(s[i:open])
		b.WriteString("<" + tag + ">" + s[open+n:closeAt] + "</" + tag + ">")
		i = closeAt + n
	}
	b.WriteString(s[i:])
	return b.String()
}
