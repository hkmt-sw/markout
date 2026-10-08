package render

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// codeSpan is a piece of a line of code in one color.
type codeSpan struct {
	text  string
	color theme.Color
}

// maxHighlightBytes is the largest code block that is highlighted. Lexing is
// done with backtracking patterns, whose time on a long block is not bounded
// by its length; a longer block is shown in one color.
const maxHighlightBytes = 200 * 1024

// highlight splits the code of a block into its lines, and each line into
// spans by what the code means in the block's language. A block that names
// no language, or one that is not known, comes back as one span per line in
// the block's text color, and so does every block when the theme has
// highlighting off.
func highlight(cb ast.CodeBlock, t theme.Theme) [][]codeSpan {
	code := t.Code
	// The code ends with a line break, which is not a line of its own
	source := strings.TrimSuffix(strings.ReplaceAll(cb.Code, "\r\n", "\n"), "\n")
	source = strings.ReplaceAll(source, "\r", "")

	plain := func() [][]codeSpan {
		var lines [][]codeSpan
		for _, line := range strings.Split(source, "\n") {
			lines = append(lines, []codeSpan{{line, code.BlockColor}})
		}
		return lines
	}
	lexer := lexerFor(cb.Language)
	if !code.Syntax.Highlight || lexer == nil || len(source) > maxHighlightBytes {
		return plain()
	}
	// Lexers read whole lines; the line break added here is taken off again
	tokens, err := chroma.Tokenise(chroma.Coalesce(lexer), nil, source+"\n")
	if err != nil {
		return plain()
	}

	lines := [][]codeSpan{nil}
	add := func(text string, color theme.Color) {
		if text == "" {
			return
		}
		line := &lines[len(lines)-1]
		// Blank space has no color of its own: it joins what is next to it,
		// and so do pieces of the same color, to keep the spans few
		if n := len(*line); n > 0 {
			last := &(*line)[n-1]
			switch {
			case last.color == color || strings.TrimSpace(text) == "":
				last.text += text
				return
			case strings.TrimSpace(last.text) == "":
				last.text, last.color = last.text+text, color
				return
			}
		}
		*line = append(*line, codeSpan{text, color})
	}
	for _, token := range tokens {
		color := syntaxColor(token.Type, t)
		for i, part := range strings.Split(token.Value, "\n") {
			if i > 0 {
				lines = append(lines, nil)
			}
			add(part, color)
		}
	}
	if n := len(lines); n > 1 && len(lines[n-1]) == 0 {
		lines = lines[:n-1]
	}
	// A lexer may leave out or add to the text; the code comes first
	if joined(lines) != source {
		return plain()
	}
	for i, line := range lines {
		if len(line) == 0 {
			lines[i] = []codeSpan{{"", code.BlockColor}}
		}
	}
	return lines
}

func joined(lines [][]codeSpan) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, span := range line {
			b.WriteString(span.text)
		}
	}
	return b.String()
}

// lexerFor finds the lexer for the language a code block names, which may
// be followed by options ("go title=main.go") or written as an attribute
// ("{.python}").
func lexerFor(language string) chroma.Lexer {
	fields := strings.Fields(language)
	if len(fields) == 0 {
		return nil
	}
	name := strings.ToLower(strings.Trim(fields[0], "{}.,"))
	if name == "" || name == "text" || name == "plain" || name == "plaintext" || name == "txt" {
		return nil
	}
	return lexers.Get(name)
}

// syntaxColor picks the theme's color for a kind of token.
func syntaxColor(t chroma.TokenType, th theme.Theme) theme.Color {
	s := th.Code.Syntax
	switch {
	case t.InCategory(chroma.Comment):
		return s.Comment
	case t == chroma.KeywordType:
		return s.Type
	case t.InCategory(chroma.Keyword), t == chroma.NameTag, t == chroma.OperatorWord:
		return s.Keyword
	case t.InSubCategory(chroma.LiteralString):
		return s.String
	case t.InSubCategory(chroma.LiteralNumber), t == chroma.NameConstant:
		return s.Number
	case t == chroma.NameFunction, t == chroma.NameFunctionMagic, t == chroma.NameAttribute, t == chroma.NameDecorator:
		return s.Function
	case t == chroma.NameClass, t == chroma.NameBuiltin, t == chroma.NameBuiltinPseudo, t == chroma.NameException, t == chroma.NameNamespace:
		return s.Type
	case t == chroma.GenericInserted: // the lines of a diff
		return th.Colors.Inserted
	case t == chroma.GenericDeleted:
		return th.Colors.Deleted
	case t == chroma.GenericHeading, t == chroma.GenericSubheading:
		return s.Function
	}
	return th.Code.BlockColor
}
