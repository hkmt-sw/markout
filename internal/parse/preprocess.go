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
	// MyST directives that hold a table.
	directiveListTable = "list-table"
	directiveCSVTable  = "csv-table"
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

// byteOrderMark is the UTF-8 byte order mark that some editors, Notepad
// among them, put at the start of a file. It is not part of the text.
const byteOrderMark = "\uFEFF"

// dropBOM removes a byte order mark from the start of a source.
func dropBOM(s string) string {
	return strings.TrimPrefix(s, byteOrderMark)
}

var (
	// yamlKeyLine is a "key: value" line, or "key:" that holds a mapping or a list.
	yamlKeyLine = regexp.MustCompile(`^(?:[\pL_][\pL\pN_.-]*|"[^"]+"|'[^']+'):(?:[ \t].*)?$`)
	// tomlKeyLine is a "key = value" line, or a [table] header.
	tomlKeyLine = regexp.MustCompile(`^(?:(?:[\pL_][\pL\pN_.-]*|"[^"]+"|'[^']+')[ \t]*=[ \t]*\S.*|\[{1,2}[\pL_][\pL\pN_. -]*\]{1,2})$`)
)

// startsWithFrontMatter reports whether the lines open with YAML (---) or
// TOML (+++) front matter. Metadata is told from a horizontal rule followed by
// prose by its lines: all of them are keys with values, or continue a key.
func startsWithFrontMatter(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	d := strings.TrimSpace(lines[0])
	if d != "---" && d != "+++" {
		return false
	}
	keys := false
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == d:
			return keys
		case trimmed == "":
		case keys && (line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(line, "- ")):
		case d == "---" && yamlKeyLine.MatchString(strings.TrimRight(line, " \t")):
			keys = true
		case d == "+++" && tomlKeyLine.MatchString(strings.TrimRight(line, " \t")):
			keys = true
		default:
			return false
		}
	}
	return false
}

func (p *preprocessor) run(src []byte) []byte {
	s := strings.ReplaceAll(dropBOM(string(src)), "\r\n", "\n")
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
	materialKeys  = regexp.MustCompile(`\+\+([A-Za-z0-9]+(?:-[A-Za-z0-9]+)*(?:\+[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*)*)\+\+`)
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

	s := &blockScan{lines: lines}
	if p.f.MermaidColon || p.f.ColonAdmonitions || p.f.Directives {
		s.colonClose = matchColonFences(lines)
	}
	for s.i = 0; s.i < len(lines); s.i++ {
		s.line = lines[s.i]
		if p.block(s) {
			continue
		}
		s.out = append(s.out, p.inline(p.rewriteLine(s.line)))
	}
	return s.out
}

// blockScan is where a pass over the lines of a document is: the line being
// read, and what has been written.
type blockScan struct {
	lines   []string
	i       int    // index of the line being read; a block moves it to its last line
	line    string // the line, as rewritten so far
	trimmed string // the line without the space around it, before it is rewritten
	out     []string

	inComment  bool        // inside a %% comment %% that runs over lines
	wrappers   []string    // HTML tags with the markdown attribute that are open
	colonClose map[int]int // the line that closes each ":::" fence
}

func (s *blockScan) add(lines ...string) { s.out = append(s.out, lines...) }

// block handles the line being read if it is, or begins, something other
// than a line of text, and reports whether it did. A line of text may be
// rewritten on the way.
func (p *preprocessor) block(s *blockScan) bool {
	if p.f.Comments {
		s.line = stripPercentComments(s.line, &s.inComment)
		if s.inComment && strings.TrimSpace(s.line) == "" {
			return true
		}
	}
	if p.fencedBlock(s) {
		return true
	}

	if n := len(s.line) - len(strings.TrimLeft(s.line, " \t")); n > maxIndent {
		s.line = s.line[n-maxIndent:]
	}
	s.trimmed = strings.TrimSpace(s.line)

	if p.kramdownComment(s) || p.multilineQuote(s) || p.colonFenced(s) || p.indentedBlock(s) || p.hiddenLine(s) || p.displayMath(s) {
		return true
	}
	p.leadingMath(s)
	if p.f.ImageAttributes {
		// Before attribute lists are stripped, which would take these along.
		s.line = mapOutsideCode(s.line, imageAttributes)
	}
	return p.attributeLine(s) || p.contentsLine(s) || p.includeLine(s) || p.materialLine(s) || p.mdxLine(s)
}

// rewriteLine makes the changes to a line of text that depend on the whole
// line.
func (p *preprocessor) rewriteLine(line string) string {
	if p.f.InapplicableTasks {
		if m := taskNA.FindStringSubmatch(line); m != nil {
			line = m[1] + "[ ] ~~" + m[2] + "~~"
		}
	}
	if p.f.BlockIDs {
		line = blockIDSuffix.ReplaceAllString(line, "")
	}
	return line
}

// fencedBlock handles a fenced code block, whose content is not rewritten.
func (p *preprocessor) fencedBlock(s *blockScan) bool {
	m := fenceOpen.FindStringSubmatch(s.line)
	if m == nil {
		return false
	}
	end := fenceEnd(s.lines, s.i+1, m[1])
	repl, verbatim := p.fenced(s.line, m[1], strings.TrimSpace(m[2]), s.lines[s.i+1:end])
	s.add(repl...)
	if verbatim && end < len(s.lines) {
		s.add(s.lines[end])
	}
	s.i = end
	return true
}

// kramdownComment leaves out what is between {::comment} and {:/comment}.
func (p *preprocessor) kramdownComment(s *blockScan) bool {
	if !p.f.BlockAttributes || s.trimmed != "{::comment}" {
		return false
	}
	for s.i+1 < len(s.lines) && strings.TrimSpace(s.lines[s.i]) != "{:/comment}" {
		s.i++
	}
	return true
}

// multilineQuote makes the lines between two ">>>" a quote.
func (p *preprocessor) multilineQuote(s *blockScan) bool {
	if !p.f.MultilineBlockquote || s.trimmed != ">>>" {
		return false
	}
	end := s.i + 1
	for end < len(s.lines) && strings.TrimSpace(s.lines[end]) != ">>>" {
		end++
	}
	s.add("")
	s.add(prefixLines("> ", p.blocks(s.lines[s.i+1:end]))...)
	s.add("")
	s.i = end
	return true
}

// colonFenced handles a block between ":::" fences. A fence without a
// closing ":::" is not a block; it stays as text.
func (p *preprocessor) colonFenced(s *blockScan) bool {
	end, closed := s.colonClose[s.i]
	if !closed {
		return false
	}
	m := colonFence.FindStringSubmatch(s.line)
	if m == nil || m[2] == "" {
		return false
	}
	repl, ok := p.colonBlock(strings.TrimSpace(m[2]), s.lines[s.i+1:end])
	if !ok {
		return false
	}
	s.add(repl...)
	s.i = end
	return true
}

// indentedBlock handles the blocks whose content is the indented lines
// under them: "!!! note" admonitions and "=== Tab" content tabs.
func (p *preprocessor) indentedBlock(s *blockScan) bool {
	if p.f.BangAdmonitions {
		if m := bangAdmon.FindStringSubmatch(s.line); m != nil {
			body, next := indentedBody(s.lines, s.i+1)
			s.add(p.alert(m[1], m[3], p.blocks(body))...)
			s.i = next - 1
			return true
		}
	}
	if p.f.ContentTabs {
		if m := contentTab.FindStringSubmatch(s.line); m != nil {
			body, next := indentedBody(s.lines, s.i+1)
			s.add("", "**"+m[1]+"**", "")
			s.add(p.blocks(body)...)
			s.add("")
			s.i = next - 1
			return true
		}
	}
	return false
}

// hiddenLine reports a line that is not part of the document: the target
// of a reference, or a comment.
func (p *preprocessor) hiddenLine(s *blockScan) bool {
	if p.f.Directives && mystTarget.MatchString(s.line) {
		return true
	}
	return p.f.LineComments && (s.line == "%" || strings.HasPrefix(s.line, "% "))
}

// displayMath handles the ways of writing a formula on lines of its own that
// the math extension does not read.
func (p *preprocessor) displayMath(s *blockScan) bool {
	f := p.f
	// MyST closes a display formula with its label: "$$ (label)". The
	// label is where references point, and not part of the document
	if f.Directives && f.MathDollar && mathLabelEnd.MatchString(s.trimmed) {
		s.add("$$")
		return true
	}
	if leadingSpaces(s.line) >= 4 {
		return false
	}
	if p.mathEnvironment(s) || p.bracketMath(s) {
		return true
	}
	// A whole display formula on one line: the math extension only closes
	// a block whose delimiters sit on their own lines.
	t := s.trimmed
	if f.MathDollar && len(t) > 4 && strings.HasPrefix(t, "$$") && strings.HasSuffix(t, "$$") &&
		!strings.Contains(t[2:len(t)-2], "$$") {
		s.add("", "$$", strings.TrimSpace(t[2:len(t)-2]), "$$", "")
		return true
	}
	return false
}

// mathEnvironment handles LaTeX environments that are formulas by
// themselves, at the top level as MyST and many LaTeX-minded writers have
// them:
//
//	\begin{align} ... \end{align}
func (p *preprocessor) mathEnvironment(s *blockScan) bool {
	if !p.f.Directives && !p.f.MathParens {
		return false
	}
	m := mathEnvBegin.FindStringSubmatch(s.trimmed)
	if m == nil {
		return false
	}
	end := mathEnvEnd(s.lines, s.i, m[1])
	if end < 0 {
		return false
	}
	s.add("", "```"+internalMathLang)
	s.add(s.lines[s.i : end+1]...)
	s.add("```", "")
	s.i = end
	return true
}

// bracketMath handles LaTeX display math between \[ and \].
func (p *preprocessor) bracketMath(s *blockScan) bool {
	if !p.f.MathParens {
		return false
	}
	// The delimiters on their own lines:
	//	\[
	//	a = b
	//	\]
	t := s.trimmed
	if t == `\[` {
		if end := mathBlockEnd(s.lines, s.i+1); end > 0 {
			s.add("", "$$")
			s.add(s.lines[s.i+1 : end]...)
			s.add("$$", "")
			s.i = end
			return true
		}
	}
	// The same on one line. Escaped brackets are also how plain "[text]"
	// is written, so the content has to look like a formula.
	if len(t) > 4 && strings.HasPrefix(t, `\[`) && strings.HasSuffix(t, `\]`) {
		if body := strings.TrimSpace(t[2 : len(t)-2]); looksLikeMath(body) && !strings.Contains(body, `\]`) {
			s.add("", "$$", body, "$$", "")
			return true
		}
	}
	return false
}

// leadingMath rewrites a formula that opens a line of text, which would be
// taken for the start of a display block, with the inline delimiters.
func (p *preprocessor) leadingMath(s *blockScan) {
	if !p.f.MathDollar || !strings.HasPrefix(s.line, "$$") || len(s.trimmed) <= 4 {
		return
	}
	if end := strings.Index(s.line[2:], "$$"); end > 0 && strings.TrimSpace(s.line[end+4:]) != "" {
		s.line = "$" + s.line[2:end+2] + "$" + s.line[end+4:]
	}
}

// attributeLine handles kramdown's attribute lists and abbreviations: lines
// that only hold them are left out, and attributes are taken off the end of
// the others.
func (p *preprocessor) attributeLine(s *blockScan) bool {
	if p.f.BlockAttributes {
		if listItemLine.MatchString(s.line) && s.i+1 < len(s.lines) && isKramdownTOC(s.lines[s.i+1]) && p.hasTOC("{:toc}") {
			s.add("", tocSentinel, "")
			s.i++
			return true
		}
		if ialOnlyLine.MatchString(s.line) {
			return true
		}
		s.line = ialTrailing.ReplaceAllString(s.line, "")
	}
	return p.f.Abbreviations && abbrDef.MatchString(s.line)
}

// contentsLine handles a line that asks for a table of contents.
func (p *preprocessor) contentsLine(s *blockScan) bool {
	if p.hasTOC(s.trimmed) {
		s.add("", tocSentinel, "")
		return true
	}
	// A table of subpages: nothing to list in a single document
	return p.f.MermaidColon && s.trimmed == "[[_TOSP_]]"
}

// includeLine puts in the file an ::include line names.
func (p *preprocessor) includeLine(s *blockScan) bool {
	if !p.f.Includes {
		return false
	}
	m := includeLine.FindStringSubmatch(s.trimmed)
	if m == nil {
		return false
	}
	inc, ok := p.include(strings.TrimSpace(m[1]))
	if !ok {
		return false
	}
	s.add(inc...)
	return true
}

// materialLine handles what MkDocs Material writes on lines of its own:
// snippets, and HTML that holds Markdown.
func (p *preprocessor) materialLine(s *blockScan) bool {
	if !p.f.Material {
		return false
	}
	if p.snippets(s) {
		return true
	}
	// <div class="grid" markdown> holds Markdown: the tag goes, what
	// is in it stays
	if m := markdownTag.FindStringSubmatch(s.trimmed); m != nil {
		s.wrappers = append(s.wrappers, m[1])
		s.add("")
		return true
	}
	if n := len(s.wrappers); n > 0 && s.trimmed == "</"+s.wrappers[n-1]+">" {
		s.wrappers = s.wrappers[:n-1]
		s.add("")
		return true
	}
	return false
}

// snippets puts in the files named with --8<--, as ::include does and under
// the same rules; a file that cannot be read is named.
func (p *preprocessor) snippets(s *blockScan) bool {
	if m := snippetLine.FindStringSubmatch(s.trimmed); m != nil {
		s.add(p.snippet(m[1])...)
		return true
	}
	if !snippetFence.MatchString(s.trimmed) {
		return false
	}
	// Between two such lines, a file a line
	end := s.i + 1
	for end < len(s.lines) && !snippetFence.MatchString(strings.TrimSpace(s.lines[end])) {
		end++
	}
	if end >= len(s.lines) {
		return false
	}
	for _, file := range s.lines[s.i+1 : end] {
		if file = strings.TrimSpace(file); file != "" && !strings.HasPrefix(file, ";") {
			s.add(p.snippet(file)...)
		}
	}
	s.i = end
	return true
}

// mdxLine handles what MDX writes on lines of its own: imports and exports,
// which are left out, and tabs. It notes the components in other lines.
func (p *preprocessor) mdxLine(s *blockScan) bool {
	if !p.f.MDX {
		return false
	}
	if mdxImport.MatchString(s.line) || mdxExport.MatchString(s.line) {
		return true
	}
	// <Tabs> hold <TabItem label="...">s, shown one after the
	// other, each under its label
	tag := strings.TrimSpace(s.line)
	if m := mdxTabItem.FindStringSubmatch(tag); m != nil {
		label := mdxAttr(m[1], "label")
		if label == "" {
			label = mdxAttr(m[1], "value")
		}
		s.add("")
		if label != "" {
			s.add("**"+label+"**", "")
		}
		return true
	}
	if mdxTabsTag.MatchString(tag) {
		s.add("")
		return true
	}
	// A component that stands by itself (<Chart data={x} />) draws
	// something on the site that a document cannot have. It is
	// left out, and the conversion says so
	mapOutsideCode(s.line, func(text string) string {
		for _, m := range mdxComponent.FindAllStringSubmatch(text, -1) {
			p.left.add("<" + m[1] + " />")
		}
		return text
	})
	return false
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
	opts, body := directiveOptions(body)
	d := directiveBlock{name: strings.ToLower(name), args: args, opts: opts, body: trimBlankLines(body)}
	if out, ok := p.knownDirective(d); ok {
		return out
	}

	if _, ok := alertTypeFor(d.name); ok {
		// The argument of a plain admonition is the start of its content.
		return p.alert(d.name, "", p.blocks(d.withArgs()))
	}

	// A directive markout does not know keeps what it holds, argument
	// included, and the conversion says that it was not understood
	if d.name != directiveListTable && d.name != directiveCSVTable {
		p.left.addUnknown("{" + d.name + "}")
	}
	return unwrap(p.blocks(d.withArgs()))
}

// directiveBlock is a directive taken apart: its name in lower case, what
// follows the name, its options, and what it holds.
type directiveBlock struct {
	name, args string
	opts       map[string]string
	body       []string
}

// withArgs is the content with the argument as its first paragraph.
func (d directiveBlock) withArgs() []string {
	if d.args == "" {
		return d.body
	}
	return append([]string{d.args, ""}, d.body...)
}

// titled is the start of what a directive with a caption becomes: a blank
// line, and the caption in bold if there is one.
func titled(caption string) []string {
	out := []string{""}
	if caption != "" {
		out = append(out, "**"+caption+"**", "")
	}
	return out
}

// directiveOptions reads the options a directive begins with, as a YAML
// block between "---" lines or as ":key: value" lines, and returns them
// with the lines that follow.
func directiveOptions(body []string) (map[string]string, []string) {
	opts := map[string]string{}
	body = yamlOptions(opts, body)
	body = colonOptions(opts, body)
	return opts, body
}

// yamlOptions reads options written as a block between "---" lines.
func yamlOptions(opts map[string]string, body []string) []string {
	if len(body) == 0 || strings.TrimSpace(body[0]) != "---" {
		return body
	}
	for i := 1; i < len(body); i++ {
		if strings.TrimSpace(body[i]) == "---" {
			return body[i+1:]
		}
		if k, v, ok := strings.Cut(body[i], ":"); ok {
			opts[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return body
}

// colonOptions reads options written as ":key: value" lines.
func colonOptions(opts map[string]string, body []string) []string {
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
	return body
}

// knownDirective converts the directives markout knows. It reports false
// for the others, and for a table that is not in the form expected.
func (p *preprocessor) knownDirective(d directiveBlock) ([]string, bool) {
	switch d.name {
	case "math":
		return mathDirective(d), true
	case "mermaid":
		out := []string{"", "```" + internalMermaidLang}
		out = append(out, d.body...)
		return append(out, "```", ""), true
	case "code-block", "code", "sourcecode", "code-cell", "literalinclude":
		return codeDirective(d), true
	case "contents", "toc", "tableofcontents":
		return []string{"", tocSentinel, ""}, true
	case "figure", "image":
		return p.figureDirective(d), true
	case "eval-rst":
		// reStructuredText is not read, but what is written in it is not
		// dropped either: it is shown as it is
		out := []string{"", "```rst"}
		out = append(out, d.body...)
		return append(out, "```", ""), true
	case "toctree":
		return toctreeDirective(d), true
	case "raw":
		return p.rawDirective(d), true
	case "bibliography", "index":
		// Lists Sphinx gathers from the whole project
		p.left.add("{" + d.name + "}")
		return nil, true
	case "admonition":
		return p.admonitionDirective(d), true
	case "versionadded", "versionchanged", "deprecated", "versionremoved":
		return p.versionDirective(d), true
	case "tab-set", "tabs", "grid", "table":
		// Containers: what they hold is shown, under their caption if any
		out := []string{""}
		if d.name == "table" {
			out = titled(d.args)
		}
		return append(append(out, p.blocks(d.body)...), ""), true
	case "line-block":
		return p.lineBlockDirective(d), true
	case "only":
		// Content for one output format or another; a document shows it
		return unwrap(p.blocks(d.body)), true
	case directiveListTable, directiveCSVTable:
		return tableDirective(d)
	case "dropdown", "card", "tab-item", "topic", "sidebar", "margin":
		return append(append(titled(d.args), p.blocks(d.body)...), ""), true
	}
	return nil, false
}

// mathDirective makes each formula of a {math} directive a formula of its
// own: they are separated by a blank line.
func mathDirective(d directiveBlock) []string {
	body := d.body
	if d.args != "" {
		body = append([]string{d.args}, body...)
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
}

// codeDirective makes a fenced code block of a directive that holds code.
func codeDirective(d directiveBlock) []string {
	lang := ""
	if fs := strings.Fields(d.args); len(fs) > 0 && d.name != "literalinclude" {
		lang = fs[0]
	}
	// The caption of a block (:caption: main.py) goes above it, like
	// the title of a fenced block
	out := titled(d.opts["caption"])
	out = append(out, "```"+lang)
	out = append(out, d.body...)
	return append(out, "```", "")
}

// figureDirective makes an image of a figure, with its caption under it.
func (p *preprocessor) figureDirective(d directiveBlock) []string {
	alt := d.opts["alt"]
	if w := widthHint(d.opts["width"]); w != "" {
		alt += widthSep + w
	}
	out := []string{"", "![" + alt + "](<" + d.args + ">)", ""}
	out = append(out, p.blocks(d.body)...) // figure caption
	return append(out, "")
}

// toctreeDirective lists the pages of the project that belong under this
// one. They are not part of this document, but which they are is.
func toctreeDirective(d directiveBlock) []string {
	var out []string
	for _, line := range d.body {
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
	if caption := d.opts["caption"]; caption != "" {
		out = append([]string{"**" + caption + "**", ""}, out...)
	}
	return append(append([]string{""}, out...), "")
}

// rawDirective keeps markup for one output format if that is HTML, which
// is read like any HTML in the document; markup for another format has no
// place here.
func (p *preprocessor) rawDirective(d directiveBlock) []string {
	if format := strings.Fields(strings.ToLower(d.args)); len(format) > 0 && format[0] == "html" {
		return append(append([]string{""}, d.body...), "")
	}
	p.left.add("{raw} " + d.args)
	return nil
}

// admonitionDirective makes a callout with a title of its own; its class
// says of what kind.
func (p *preprocessor) admonitionDirective(d directiveBlock) []string {
	kind := "note"
	for _, c := range strings.Fields(d.opts["class"]) {
		if _, ok := alertTypeFor(c); ok {
			kind = c
		}
	}
	return p.alert(kind, d.args, p.blocks(d.body))
}

// versionDirective writes out a note on a version, such as "Added in
// version 1.2: what was added".
func (p *preprocessor) versionDirective(d directiveBlock) []string {
	label := map[string]string{
		"versionadded": "Added in version", "versionchanged": "Changed in version",
		"deprecated": "Deprecated since version", "versionremoved": "Removed in version",
	}[d.name]
	version, rest, _ := strings.Cut(d.args, " ")
	head := "*" + label + " " + version + "*"
	if rest = strings.TrimSpace(rest); rest != "" {
		head += ": " + rest
	}
	inner := unwrap(p.blocks(d.body))
	if text := strings.TrimSpace(strings.Join(inner, "\n")); text != "" {
		head += ":"
	}
	return append(append([]string{"", head, ""}, inner...), "")
}

// lineBlockDirective keeps every line of a {line-block} a line.
func (p *preprocessor) lineBlockDirective(d directiveBlock) []string {
	var out []string
	for _, line := range d.body {
		if strings.TrimSpace(line) != "" {
			out = append(out, p.inline(strings.TrimSpace(line))+"\\")
		}
	}
	if len(out) == 0 {
		return nil
	}
	out[len(out)-1] = strings.TrimSuffix(out[len(out)-1], "\\")
	return append(append([]string{""}, out...), "")
}

// tableDirective makes a table of a {list-table} or a {csv-table}. It
// reports false for one that is not in the form expected, whose content is
// kept as it is.
func tableDirective(d directiveBlock) ([]string, bool) {
	var rows [][]string
	header := 0
	if d.name == directiveListTable {
		rows = listTableRows(d.body)
		header, _ = strconv.Atoi(d.opts["header-rows"])
	} else {
		rows = csvRows(strings.Join(d.body, "\n"))
		if head := csvRows(d.opts["header"]); len(head) == 1 {
			rows, header = append(head, rows...), 1
		}
	}
	table := pipeTable(rows, header > 0)
	if table == nil {
		return nil, false
	}
	return append(append(titled(d.args), table...), ""), true
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
	lines := sub.blocks(strings.Split(strings.ReplaceAll(dropBOM(string(data)), "\r\n", "\n"), "\n"))
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
