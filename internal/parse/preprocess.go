package parse

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
}

func newPreprocessor(f flavor.Features, baseDir string) *preprocessor {
	return &preprocessor{f: f, root: baseDir, baseDir: baseDir}
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
	mystTarget    = regexp.MustCompile(`^\([\w.:-]+\)=[ \t]*$`)
	ialOnlyLine   = regexp.MustCompile(`^[ \t]*\{:[^}]*\}[ \t]*$`)
	ialTrailing   = regexp.MustCompile(`[ \t]*\{:[^}]*\}[ \t]*$`)
	listItemLine  = regexp.MustCompile(`^[ \t]*(?:[*+-]|\d+[.)])[ \t]+\S`)
	abbrDef       = regexp.MustCompile(`^\*\[[^\]]+\]:`)
	includeLine   = regexp.MustCompile(`^::include\{file=([^}]+)\}[ \t]*$`)
	mdxImport     = regexp.MustCompile(`^import\s+(?:.+\sfrom\s+)?['"][^'"]+['"];?[ \t]*$`)
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

		if f.MDX && (mdxImport.MatchString(line) || mdxExport.MatchString(line)) {
			continue
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
	return append([]string{open}, body...), true
}

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
			return nil, true // embedded widgets have no document equivalent
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
	}
	body = trimBlankLines(body)

	name = strings.ToLower(name)
	switch name {
	case "math":
		out := []string{"", "```" + internalMathLang}
		if args != "" {
			out = append(out, args)
		}
		out = append(out, body...)
		return append(out, "```", "")

	case "mermaid":
		out := []string{"", "```" + internalMermaidLang}
		out = append(out, body...)
		return append(out, "```", "")

	case "code-block", "code", "sourcecode", "code-cell", "literalinclude":
		lang := ""
		if fs := strings.Fields(args); len(fs) > 0 && name != "literalinclude" {
			lang = fs[0]
		}
		out := []string{"", "```" + lang}
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

	case "toctree", "eval-rst", "raw", "bibliography", "index", "only":
		return nil

	case "admonition":
		kind := "note"
		for _, c := range strings.Fields(opts["class"]) {
			if _, ok := alertTypeFor(c); ok {
				kind = c
			}
		}
		return p.alert(kind, args, p.blocks(body))

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

	return unwrap(p.blocks(body))
}

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

	sub := &preprocessor{f: p.f, root: p.root, baseDir: filepath.Dir(path), depth: p.depth + 1, nesting: p.nesting}
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
	mathBacktick   = regexp.MustCompile("\\$`([^`\n]+)`\\$")
	roleSpan       = regexp.MustCompile("\\{([A-Za-z][\\w:.+-]*)\\}`([^`\n]+)`")
	roleTarget     = regexp.MustCompile(`^(.*?)\s*<[^>]+>$`)
	mathInlineBS   = regexp.MustCompile(`\\\\\((.+?)\\\\\)`)
	mathDisplayBS  = regexp.MustCompile(`\\\\\[(.+?)\\\\\]`)
	imageSizeEq    = regexp.MustCompile(`(!\[[^\]]*)\]\(([^)\s]+)[ \t]+=(\d*)x(\d*)\)`)
	imageAttrs     = regexp.MustCompile(`(!\[[^\]]*)\]\(([^)]*)\)\{:?([^}]*)\}`)
	attrWidth      = regexp.MustCompile(`width\s*[=:]\s*["']?(\d+(?:\.\d+)?)\s*(px|%)?`)
	imageEmbed     = regexp.MustCompile(`!\[\[([^\]|]+)(?:\|([^\]]*))?\]\]`)
	wikiLink       = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	inlineNote     = regexp.MustCompile(`\^\[([^\]]+)\]`)
	criticSub      = regexp.MustCompile(`\{~~(.+?)~>(.+?)~~\}`)
	criticAdd      = regexp.MustCompile(`\{\+\+(.+?)\+\+\}`)
	criticDel      = regexp.MustCompile(`\{--(.+?)--\}`)
	criticMark     = regexp.MustCompile(`\{==(.+?)==\}`)
	criticNote     = regexp.MustCompile(`\{>>(.+?)<<\}`)
	diffAdd        = regexp.MustCompile(`\{\+\s?(.+?)\s?\+\}|\[\+\s?(.+?)\s?\+\]`)
	diffDel        = regexp.MustCompile(`\{-\s?(.+?)\s?-\}|\[-\s?(.+?)\s?-\]`)
	bracketedSpan  = regexp.MustCompile(`\[([^\]\n]+)\]\{\.(underline|ul|mark|smallcaps)\}`)
	mdxComment     = regexp.MustCompile(`\{/\*.*?\*/\}`)
	imageExtension = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|svg|webp|bmp|avif)$`)
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

	if f.MathBrackets {
		s = mathDisplayBS.ReplaceAllStringFunc(s, func(m string) string { return "$$" + m[3:len(m)-3] + "$$" })
		s = mathInlineBS.ReplaceAllStringFunc(s, func(m string) string { return "$" + m[3:len(m)-3] + "$" })
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
