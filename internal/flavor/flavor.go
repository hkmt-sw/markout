// Package flavor describes the Markdown dialects markout can interpret.
//
// A Flavor is a named set of Features. The parser consults the features to
// decide which syntax extensions are active, so the same document converts the
// way it would render on the platform it was written for.
package flavor

import "strings"

// Features is the set of syntax extensions on top of CommonMark that a flavor
// enables. The zero value is plain CommonMark with raw HTML shown literally.
type Features struct {
	// Block and inline extensions shared by many dialects.
	Tables            bool // pipe tables
	Strikethrough     bool // ~~text~~
	TaskLists         bool // - [ ] / - [x]
	Linkify           bool // bare URLs become links
	Footnotes         bool // [^1] references and definitions
	InlineFootnotes   bool // ^[inline note]
	DefinitionLists   bool // Term / : definition
	HeadingAttributes bool // # Title {#id .class}
	Typographer       bool // smart quotes, dashes, ellipses
	HTML              bool // interpret raw HTML (<u>, <sub>, <details>, <img>, ...)
	Emoji             bool // :shortcode:

	// Document metadata.
	FrontMatter     bool // YAML (---) and TOML (+++) front matter
	JSONFrontMatter bool // ;;; delimited JSON front matter
	MetadataBlock   bool // MultiMarkdown "Key: value" header
	TitleBlock      bool // Pandoc "% title" block

	// Math.
	MathDollar     bool // $inline$ and $$block$$
	MathFence      bool // ```math
	MathLatexFence bool // ```latex rendered as a formula
	MathBacktick   bool // $`inline`$
	MathBrackets   bool // \\(inline\\) and \\[block\\]

	// Diagrams.
	MermaidFence bool // ```mermaid
	MermaidColon bool // ::: mermaid ... :::

	// Callouts.
	Alerts           bool // > [!NOTE] with the five GitHub types
	AlertTitles      bool // > [!NOTE] Custom title
	Callouts         bool // > [!any-type]- Title (Obsidian)
	ColonAdmonitions bool // :::note ... :::
	BangAdmonitions  bool // !!! note "Title" with an indented body
	Directives       bool // MyST ```{note} / :::{note} directives and {role}`x`
	ContentTabs      bool // === "Tab" with an indented body

	// TOCMarkers are the lines that expand to a table of contents.
	TOCMarkers []string

	// Inline markup.
	Highlight      bool // ==text==
	Superscript    bool // ^text^
	Subscript      bool // ~text~
	InsertCaret    bool // ^^text^^
	InlineDiff     bool // {+ added +} {- removed -}
	CriticMarkup   bool // {++add++} {--del--} {==mark==} {>>note<<} {~~a~>b~~}
	BracketedSpans bool // [text]{.underline}
	WikiLinks      WikiLinkStyle
	Comments       bool // %% hidden %%
	BlockIDs       bool // trailing ^block-id
	ColorChips     bool // `#RRGGBB`, `rgb(...)`, `hsl(...)`

	// Images.
	ImageAttributes bool // ![a](u){width=50%}
	ImageSizeEquals bool // ![a](u =500x250)
	ImageEmbeds     bool // ![[file.png|300]]

	// Platform-specific block syntax.
	MultilineBlockquote bool // >>> ... >>>
	InapplicableTasks   bool // - [~] not applicable
	Includes            bool // ::include{file=part.md}
	JSONTables          bool // ```json:table
	Abbreviations       bool // *[HTML]: Hyper Text Markup Language
	BlockAttributes     bool // {: .class} attribute lists
	MDX                 bool // import/export lines and {/* comments */}
	LineComments        bool // lines starting with "% " are comments
}

// WikiLinkStyle says whether [[double bracket]] links are understood and which
// side of the pipe carries the visible text.
type WikiLinkStyle int

const (
	WikiLinksNone        WikiLinkStyle = iota
	WikiLinksTargetLabel               // [[target|label]]
	WikiLinksLabelTarget               // [[label|target]]
)

// Flavor is a selectable Markdown dialect.
type Flavor struct {
	ID          string // stable identifier used in settings and on the command line
	Name        string // display name
	Description string // one-line summary of what sets the dialect apart
	Features    Features
}

// DefaultID is the flavor used when nothing is configured.
const DefaultID = "markout"

// gfm is the feature set of GitHub Flavored Markdown as specified; the
// platform flavors that build on it start from here.
var gfm = Features{
	Tables:        true,
	Strikethrough: true,
	TaskLists:     true,
	Linkify:       true,
	HTML:          true,
}

func with(base Features, mod func(*Features)) Features {
	mod(&base)
	return base
}

var all = []Flavor{
	{
		ID:          "markout",
		Name:        "Markout (universal)",
		Description: "Best effort: every extension that does not conflict with another",
		Features: with(gfm, func(f *Features) {
			f.Footnotes, f.InlineFootnotes, f.DefinitionLists = true, true, true
			f.HeadingAttributes, f.Emoji = true, true
			f.FrontMatter, f.JSONFrontMatter = true, true
			f.MathDollar, f.MathFence, f.MathBacktick = true, true, true
			f.MermaidFence, f.MermaidColon = true, true
			f.Alerts, f.AlertTitles, f.Callouts = true, true, true
			f.ColonAdmonitions, f.BangAdmonitions, f.Directives, f.ContentTabs = true, true, true, true
			f.TOCMarkers = []string{"[[_TOC_]]", "[TOC]", "{{TOC}}", "[[toc]]", "{:toc}"}
			f.Highlight, f.InlineDiff, f.CriticMarkup, f.BracketedSpans = true, true, true, true
			f.Comments, f.ColorChips = true, true
			f.ImageAttributes, f.ImageSizeEquals, f.ImageEmbeds = true, true, true
			f.MultilineBlockquote, f.InapplicableTasks, f.JSONTables = true, true, true
			f.Abbreviations, f.BlockAttributes = true, true
		}),
	},
	{
		ID:          "commonmark",
		Name:        "CommonMark",
		Description: "The strict standard, no extensions",
		Features:    Features{HTML: true},
	},
	{
		ID:          "github",
		Name:        "GitHub (GFM)",
		Description: "Tables, task lists, alerts, footnotes, math, Mermaid, emoji",
		Features: with(gfm, func(f *Features) {
			f.Footnotes, f.Emoji, f.FrontMatter = true, true, true
			f.MathDollar, f.MathFence, f.MathBacktick = true, true, true
			f.MermaidFence = true
			f.Alerts = true
			f.ColorChips = true
		}),
	},
	{
		ID:          "gitlab",
		Name:        "GitLab (GLFM)",
		Description: "GFM plus [[_TOC_]], inline diff, >>> quotes, color chips, includes",
		Features: with(gfm, func(f *Features) {
			f.Footnotes, f.DefinitionLists, f.Emoji = true, true, true
			f.FrontMatter, f.JSONFrontMatter = true, true
			f.MathDollar, f.MathFence, f.MathBacktick = true, true, true
			f.MermaidFence = true
			f.Alerts, f.AlertTitles = true, true
			f.TOCMarkers = []string{"[[_TOC_]]", "[TOC]"}
			f.InlineDiff, f.ColorChips = true, true
			f.WikiLinks = WikiLinksLabelTarget
			f.ImageAttributes = true
			f.MultilineBlockquote, f.InapplicableTasks, f.Includes, f.JSONTables = true, true, true, true
		}),
	},
	{
		ID:          "youtrack",
		Name:        "YouTrack",
		Description: "CommonMark plus tables, checklists, ```latex, Mermaid, sized images",
		Features: with(gfm, func(f *Features) {
			f.MathLatexFence = true
			f.MermaidFence = true
			f.ImageAttributes = true
		}),
	},
	{
		ID:          "azure",
		Name:        "Azure DevOps",
		Description: "GFM plus [[_TOC_]], ::: mermaid, $math$, =WxH image sizes",
		Features: with(gfm, func(f *Features) {
			f.Emoji, f.FrontMatter = true, true
			f.MathDollar = true
			f.MermaidColon = true
			f.TOCMarkers = []string{"[[_TOC_]]"}
			f.ImageSizeEquals = true
		}),
	},
	{
		ID:          "bitbucket",
		Name:        "Bitbucket",
		Description: "Tables, footnotes, definition lists, [TOC], wiki links; no raw HTML",
		Features: Features{
			Tables:          true,
			Strikethrough:   true,
			Linkify:         true,
			Footnotes:       true,
			DefinitionLists: true,
			Emoji:           true,
			TOCMarkers:      []string{"[TOC]"},
			WikiLinks:       WikiLinksTargetLabel,
			Abbreviations:   true,
		},
	},
	{
		ID:          "obsidian",
		Name:        "Obsidian",
		Description: "Wiki links, ![[embeds]], ==highlight==, callouts, %%comments%%",
		Features: with(gfm, func(f *Features) {
			f.Footnotes, f.InlineFootnotes, f.FrontMatter = true, true, true
			f.MathDollar = true
			f.MermaidFence = true
			f.Alerts, f.AlertTitles, f.Callouts = true, true, true
			f.Highlight, f.Comments, f.BlockIDs = true, true, true
			f.WikiLinks = WikiLinksTargetLabel
			f.ImageEmbeds = true
		}),
	},
	{
		ID:          "pandoc",
		Name:        "Pandoc",
		Description: "Fenced divs, ^super^ and ~sub~script, inline notes, spans, title block",
		Features: with(gfm, func(f *Features) {
			f.Linkify = false
			f.Footnotes, f.InlineFootnotes, f.DefinitionLists = true, true, true
			f.HeadingAttributes, f.Typographer = true, true
			f.FrontMatter, f.TitleBlock = true, true
			f.MathDollar = true
			f.ColonAdmonitions = true
			f.Superscript, f.Subscript, f.BracketedSpans = true, true, true
			f.ImageAttributes = true
		}),
	},
	{
		ID:          "multimarkdown",
		Name:        "MultiMarkdown",
		Description: "Metadata header, {{TOC}}, CriticMarkup, \\\\(math\\\\), ^super and ~sub~",
		Features: Features{
			Tables:          true,
			Footnotes:       true,
			DefinitionLists: true,
			Typographer:     true,
			HTML:            true,
			FrontMatter:     true,
			MetadataBlock:   true,
			MathDollar:      true,
			MathBrackets:    true,
			TOCMarkers:      []string{"{{TOC}}"},
			Superscript:     true,
			Subscript:       true,
			CriticMarkup:    true,
			Abbreviations:   true,
		},
	},
	{
		ID:          "extra",
		Name:        "Markdown Extra",
		Description: "PHP Markdown Extra: tables, footnotes, definition lists, abbreviations",
		Features: Features{
			Tables:            true,
			Footnotes:         true,
			DefinitionLists:   true,
			HeadingAttributes: true,
			HTML:              true,
			Abbreviations:     true,
		},
	},
	{
		ID:          "kramdown",
		Name:        "kramdown (Jekyll)",
		Description: "Markdown Extra plus {:toc}, {: .class} attribute lists, $$math$$",
		Features: Features{
			Tables:            true,
			Footnotes:         true,
			DefinitionLists:   true,
			HeadingAttributes: true,
			Typographer:       true,
			HTML:              true,
			FrontMatter:       true,
			MathDollar:        true,
			TOCMarkers:        []string{"{:toc}"},
			ImageAttributes:   true,
			Abbreviations:     true,
			BlockAttributes:   true,
		},
	},
	{
		ID:          "myst",
		Name:        "MyST (Sphinx / Jupyter Book)",
		Description: "```{directive} and :::{directive} blocks, {role}`text`, $math$",
		Features: with(gfm, func(f *Features) {
			f.Footnotes, f.DefinitionLists, f.HeadingAttributes = true, true, true
			f.FrontMatter = true
			f.MathDollar = true
			f.ColonAdmonitions, f.Directives, f.LineComments = true, true, true
			f.ImageAttributes = true
		}),
	},
	{
		ID:          "mkdocs",
		Name:        "MkDocs Material",
		Description: "!!! admonitions, === tabs, [TOC], ==mark==, ^^insert^^, CriticMarkup",
		Features: with(gfm, func(f *Features) {
			f.Footnotes, f.DefinitionLists, f.HeadingAttributes, f.Emoji = true, true, true, true
			f.FrontMatter = true
			f.MathDollar = true
			f.MermaidFence = true
			f.BangAdmonitions, f.ContentTabs = true, true
			f.TOCMarkers = []string{"[TOC]"}
			f.Highlight, f.Superscript, f.Subscript, f.InsertCaret, f.CriticMarkup = true, true, true, true, true
			f.ImageAttributes = true
			f.Abbreviations, f.BlockAttributes = true, true
		}),
	},
	{
		ID:          "docusaurus",
		Name:        "Docusaurus (MDX)",
		Description: ":::note admonitions, {#heading-ids}, MDX imports and comments stripped",
		Features: with(gfm, func(f *Features) {
			f.Footnotes, f.HeadingAttributes, f.FrontMatter = true, true, true
			f.MathDollar = true
			f.MermaidFence = true
			f.ColonAdmonitions = true
			f.MDX = true
		}),
	},
}

// All returns every supported flavor, the default first.
func All() []Flavor {
	out := make([]Flavor, len(all))
	copy(out, all)
	return out
}

// Default returns the flavor used when nothing is configured.
func Default() Flavor {
	return all[0]
}

// ByID looks a flavor up by its identifier, ignoring case. A few common
// aliases (gfm, glfm, ado, mmd, ...) are accepted.
func ByID(id string) (Flavor, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if alias, ok := aliases[id]; ok {
		id = alias
	}
	for _, f := range all {
		if f.ID == id {
			return f, true
		}
	}
	return Flavor{}, false
}

var aliases = map[string]string{
	"default":         "markout",
	"universal":       "markout",
	"auto":            "markout",
	"cm":              "commonmark",
	"gfm":             "github",
	"glfm":            "gitlab",
	"ado":             "azure",
	"azuredevops":     "azure",
	"azure-devops":    "azure",
	"mmd":             "multimarkdown",
	"markdown-extra":  "extra",
	"markdownextra":   "extra",
	"php-extra":       "extra",
	"jekyll":          "kramdown",
	"sphinx":          "myst",
	"mkdocs-material": "mkdocs",
	"material":        "mkdocs",
	"mdx":             "docusaurus",
	"quarto":          "pandoc",
	"rmarkdown":       "pandoc",
}
