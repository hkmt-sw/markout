package parse

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
)

// dump renders a document as one compact line per element, with each run's
// formatting in braces, so tests can assert on what a flavor recognized.
func dump(doc *ast.Document) string {
	var b strings.Builder
	for _, e := range doc.Elements {
		dumpElement(&b, e, "")
	}
	return b.String()
}

func dumpRuns(runs []ast.InlineRun) string {
	var b strings.Builder
	for _, r := range runs {
		var flags []string
		add := func(on bool, name string) {
			if on {
				flags = append(flags, name)
			}
		}
		add(r.Bold, "b")
		add(r.Italic, "i")
		add(r.Code, "code")
		add(r.Strikethrough, "s")
		add(r.Underline, "u")
		add(r.Highlight, "mark")
		add(r.Superscript, "sup")
		add(r.Subscript, "sub")
		add(r.Inserted, "ins")
		add(r.Deleted, "del")
		add(r.Math, "math")
		add(r.Link != "", "link="+r.Link)
		add(r.ColorChip != "", "chip="+r.ColorChip)
		add(r.FootnoteIndex > 0, fmt.Sprintf("fn=%d", r.FootnoteIndex))
		if len(flags) > 0 {
			b.WriteString("{" + strings.Join(flags, ",") + "}")
		}
		b.WriteString(r.Text)
		if len(flags) > 0 {
			b.WriteString("{/}")
		}
	}
	return b.String()
}

func dumpElement(b *strings.Builder, e ast.Element, indent string) {
	switch v := e.(type) {
	case ast.Heading:
		fmt.Fprintf(b, "%sH%d: %s\n", indent, v.Level, dumpRuns(v.Runs))
	case ast.Paragraph:
		fmt.Fprintf(b, "%sP: %s\n", indent, dumpRuns(v.Runs))
	case ast.List:
		var items func(items []ast.ListItem, indent string)
		items = func(list []ast.ListItem, indent string) {
			for _, it := range list {
				task := ""
				if it.IsTask {
					task = "☐ "
					if it.Checked {
						task = "☑ "
					}
				}
				fmt.Fprintf(b, "%sLI: %s%s\n", indent, task, dumpRuns(it.Runs))
				items(it.Children, indent+"  ")
			}
		}
		items(v.Items, indent)
	case ast.CodeBlock:
		fmt.Fprintf(b, "%sCODE[%s]: %s\n", indent, v.Language, strings.TrimSpace(v.Code))
	case ast.Table:
		rows := append([]ast.TableRow{v.Header}, v.Rows...)
		for _, row := range rows {
			var cells []string
			for _, c := range row.Cells {
				cells = append(cells, dumpRuns(c.Runs))
			}
			fmt.Fprintf(b, "%sROW: %s\n", indent, strings.Join(cells, " | "))
		}
	case ast.Blockquote:
		fmt.Fprintf(b, "%sQUOTE\n", indent)
		for _, c := range v.Elements {
			dumpElement(b, c, indent+"  ")
		}
	case ast.Alert:
		fmt.Fprintf(b, "%sALERT[%s] %s\n", indent, v.Type, v.Title)
		for _, c := range v.Elements {
			dumpElement(b, c, indent+"  ")
		}
	case ast.Image:
		fmt.Fprintf(b, "%sIMG: %s -> %s w=%d\n", indent, v.Alt, v.URL, v.Width)
	case ast.MermaidDiagram:
		fmt.Fprintf(b, "%sMERMAID: %s\n", indent, strings.TrimSpace(v.Source))
	case ast.MathBlock:
		fmt.Fprintf(b, "%sMATH: %s\n", indent, v.Expression)
	case ast.TableOfContents:
		var titles []string
		for _, it := range v.Items {
			titles = append(titles, it.Title)
		}
		fmt.Fprintf(b, "%sTOC: %s\n", indent, strings.Join(titles, ", "))
	case ast.FrontMatter:
		fmt.Fprintf(b, "%sMETA: title=%q author=%q date=%q\n", indent, v.Title, v.Author, v.Date)
	case ast.DescriptionList:
		for _, it := range v.Items {
			var defs []string
			for _, d := range it.Definitions {
				defs = append(defs, dumpRuns(d))
			}
			fmt.Fprintf(b, "%sDL: %s => %s\n", indent, dumpRuns(it.Term), strings.Join(defs, " ; "))
		}
	case ast.FootnoteDefinition:
		fmt.Fprintf(b, "%sFOOTNOTE %d\n", indent, v.Index)
		for _, c := range v.Elements {
			dumpElement(b, c, indent+"  ")
		}
	case ast.HorizontalRule:
		fmt.Fprintf(b, "%sHR\n", indent)
	}
}

func parseAs(t *testing.T, id, src string) string {
	t.Helper()
	fl, ok := flavor.ByID(id)
	if !ok {
		t.Fatalf("unknown flavor %q", id)
	}
	doc, err := ParseFlavor([]byte(src), fl, "")
	if err != nil {
		t.Fatalf("parse as %s: %v", id, err)
	}
	return dump(doc)
}

// TestFlavorSyntax checks, per flavor, that its own syntax is recognized and
// that syntax belonging to other flavors is left alone.
func TestFlavorSyntax(t *testing.T) {
	tests := []struct {
		name   string
		flavor string
		src    string
		want   []string // substrings the dump must contain
		absent []string // substrings it must not contain
	}{
		{
			name:   "commonmark has no extensions",
			flavor: "commonmark",
			src:    "~~a~~ :smile: https://x.example\n\n| a | b |\n|---|---|\n\n- [ ] t\n\n> [!NOTE]\n> q\n\n`#FF0000`\n\n---\ntitle: x\n---\n",
			want:   []string{"~~a~~ :smile: https://x.example", "QUOTE", "{code}#FF0000{/}"},
			absent: []string{"{s}", "ROW:", "☐", "ALERT", "chip=", "link=", "META"},
		},
		{
			name:   "commonmark renders inline html",
			flavor: "commonmark",
			src:    "H<sub>2</sub>O and <u>under</u> and <a href=\"https://x.example\">link</a>",
			want:   []string{"{sub}2{/}", "{u}under{/}", "{link=https://x.example}link{/}"},
		},
		{
			name:   "github core",
			flavor: "github",
			src:    "~~a~~ :rocket:\n\n- [x] t\n\n> [!TIP]\n> body\n\n$x$ and $`y`$ and `#0969DA` and `rgb(255, 0, 0)`\n\n```mermaid\ngraph TD;\n```\n\n```math\na\n```\n",
			want:   []string{"{s}a{/}", "🚀", "LI: ☑ t", "ALERT[TIP] TIP", "{math}x{/}", "{math}y{/}", "chip=#0969DA", "chip=#FF0000", "MERMAID: graph TD;", "MATH: a"},
		},
		{
			name:   "github alerts have no titles and no other types",
			flavor: "github",
			src:    "> [!NOTE] same line\n> body\n\n> [!danger]\n> quote\n\n==x== [[_TOC_]]\n",
			want:   []string{"ALERT[NOTE] NOTE\n  P: same line body", "QUOTE", "==x=="},
			absent: []string{"ALERT[WARNING]", "{mark}", "TOC:"},
		},
		{
			name:   "gitlab extensions",
			flavor: "gitlab",
			src:    "# T\n\n[[_TOC_]]\n\n{+ add +} {- del -} [+ a2 +] [- d2 -]\n\n>>>\nq1\n\nq2\n>>>\n\n> [!warning] Custom\n> body\n\n- [~] na\n\n`hsl(0, 100%, 50%)`\n\n![i](a.png){width=200 height=100}\n\nTerm\n: Def\n",
			want: []string{"TOC: T", "{ins}add{/}", "{del}del{/}", "{ins}a2{/}", "{del}d2{/}", "QUOTE\n  P: q1\n  P: q2",
				"ALERT[WARNING] Custom\n  P: body", "LI: ☐ {s}na{/}", "chip=#FF0000", "IMG: i -> a.png w=200", "DL: Term => Def"},
		},
		{
			name:   "gitlab json front matter and json table",
			flavor: "gitlab",
			src:    ";;;\n{\"title\": \"Doc\", \"author\": \"Me\"}\n;;;\n\n```json:table\n{\"fields\": [{\"key\": \"a\", \"label\": \"A\"}, \"b\"], \"items\": [{\"a\": 1, \"b\": \"x\"}]}\n```\n",
			want:   []string{`META: title="Doc" author="Me"`, "ROW: A | b", "ROW: 1 | x"},
		},
		{
			name:   "gitlab toml front matter",
			flavor: "gitlab",
			src:    "+++\ntitle = \"Toml\"\n+++\n\ntext\n",
			want:   []string{`META: title="Toml"`},
		},
		{
			name:   "youtrack",
			flavor: "youtrack",
			src:    "* [x] done\n\n```latex\n\\frac{a}{b}\n```\n\n```mermaid\ngraph LR;\n```\n\n![s](s.png){width=300}\n\n$x$ :smile:\n\n> [!NOTE]\n> q\n\n[^1]\n\n[^1]: n\n",
			want:   []string{"LI: ☑ done", "MATH: \\frac{a}{b}", "MERMAID: graph LR;", "IMG: s -> s.png w=300", "$x$ :smile:", "QUOTE"},
			absent: []string{"{math}", "ALERT", "FOOTNOTE"},
		},
		{
			name:   "azure devops",
			flavor: "azure",
			src:    "# T\n\n[[_TOC_]]\n\n[[_TOSP_]]\n\n::: mermaid\ngraph TD;\n:::\n\n![a](a.png =500x250)\n\n$x$ :smile:\n\n```mermaid\nnot a diagram\n```\n",
			want:   []string{"TOC: T", "MERMAID: graph TD;", "IMG: a -> a.png w=500", "{math}x{/}", "😄", "CODE[mermaid]: not a diagram"},
			absent: []string{"TOSP"},
		},
		{
			name:   "bitbucket",
			flavor: "bitbucket",
			src:    "# T\n\n[TOC]\n\n~~d~~ [[Home]] <u>raw</u>\n\n*[HTML]: Hyper Text\n\nA\n: B\n",
			want:   []string{"TOC: T", "{s}d{/}", " Home ", "<u>raw</u>", "DL: A => B"},
			absent: []string{"{u}", "Hyper Text", "[[Home]]"},
		},
		{
			name:   "obsidian",
			flavor: "obsidian",
			src:    "[[Note]] [[Note#H|label]] ==hi there== %%secret%% end ^blk-1\n\n%%\nhidden\n%%\n\n![[pic.png|320]]\n\n> [!faq]- Folded title\n> body\n\nnote^[inline]\n",
			want: []string{"P: Note label {mark}hi there{/}  end\n", "IMG: pic -> pic.png w=320",
				"ALERT[NOTE] Folded title\n  P: body", "{fn=1}", "FOOTNOTE 1\n  P: inline"},
			absent: []string{"secret", "hidden", "blk-1", "[["},
		},
		{
			name:   "obsidian keeps pipe width hint",
			flavor: "obsidian",
			src:    "![Diagram|640](d.png)\n",
			want:   []string{"IMG: Diagram -> d.png w=640"},
		},
		{
			name:   "pandoc",
			flavor: "pandoc",
			src:    "% Title\n% Author\n% 2026-01-02\n\n# H {#id .cls}\n\nH~2~O 2^10^ ~~gone~~ \"q\" [span]{.underline} note^[n]\n\n::: warning\nw\n:::\n\n::: {.callout-tip title=\"T\"}\nt\n:::\n\n::: columns\nplain\n:::\n\n```{.python .numberLines}\npass\n```\n\n$$ a = b $$\n",
			want: []string{`META: title="Title" author="Author" date="2026-01-02"`, "H1: H\n", "{sub}2{/}", "{sup}10{/}", "{s}gone{/}", "“q”",
				"{u}span{/}", "{fn=1}", "ALERT[WARNING] WARNING\n  P: w", "ALERT[TIP] T\n  P: t", "P: plain", "CODE[python]: pass", "MATH: a = b"},
			absent: []string{"{#id", ":::"},
		},
		{
			name:   "multimarkdown",
			flavor: "multimarkdown",
			src:    "Title: MMD\nAuthor: F\n\n# H\n\n{{TOC}}\n\nx^2^ H~2~O \\\\(a\\\\) {++in++} {--out--} {~~a~>b~~} {==m==}{>>c<<} ~~no~~\n",
			want:   []string{`META: title="MMD" author="F"`, "TOC: H", "{sup}2{/}", "{sub}2{/}", "{math}a{/}", "{ins}in{/}", "{del}out{/}", "{del}a{/}{ins}b{/}", "{mark}m{/}", "~~no~~"},
			absent: []string{"{s}", ">>c<<"},
		},
		{
			name:   "markdown extra",
			flavor: "extra",
			src:    "# H {#top}\n\n| a |\n|---|\n| 1 |\n\nA\n: B\n\n*[X]: Y\n\n~~no~~ https://x.example\n\n- [ ] t\n",
			want:   []string{"H1: H\n", "ROW: a", "DL: A => B", "~~no~~ https://x.example"},
			absent: []string{"{s}", "link=", "☐", "*[X]"},
		},
		{
			name:   "kramdown",
			flavor: "kramdown",
			src:    "# H\n{: .no_toc}\n\n* TOC\n{:toc}\n\npara\n{: .lead}\n\n![l](l.png){: width=\"200\"}\n\n{::comment}\nhidden\n{:/comment}\n\n$$a^2$$ inline\n",
			want:   []string{"TOC: H", "P: para\n", "IMG: l -> l.png w=200", "{math}a^2{/}"},
			absent: []string{"{:", "hidden", "no_toc", "lead"},
		},
		{
			name:   "myst",
			flavor: "myst",
			src:    "(target)=\n# H\n\n% comment\n\n```{note}\nn **b**\n```\n\n:::{warning}\nw\n:::\n\n```{admonition} Title\n:class: tip\nbody\n```\n\n```{code-block} python\n:linenos:\npass\n```\n\n```{math}\na\n```\n\n```{figure} p.png\n:width: 50%\n:alt: Alt\ncap\n```\n\n{math}`x` H{sub}`2`O {ref}`shown <t>`\n",
			want: []string{"H1: H", "ALERT[NOTE] NOTE\n  P: n {b}b{/}", "ALERT[WARNING] WARNING\n  P: w", "ALERT[TIP] Title\n  P: body",
				"CODE[python]: pass", "MATH: a", "IMG: Alt -> p.png w=300", "P: cap", "{math}x{/}", "{sub}2{/}", "shown"},
			absent: []string{"target", "comment", "linenos", "{note}", "<t>"},
		},
		{
			name:   "mkdocs material",
			flavor: "mkdocs",
			src:    "# H\n\n[TOC]\n\n!!! note \"Custom\"\n    body **b**\n\n    - item\n\n??? danger\n    d\n\n=== \"Tab A\"\n    ```py\n    pass\n    ```\n\nafter\n\n==m== ^^i^^ x^2^ H~2~O {++c++}\n\n![i](i.png){ width=\"300\" }\n",
			want: []string{"TOC: H", "ALERT[NOTE] Custom\n  P: body {b}b{/}\n  P: • item", "ALERT[WARNING] Danger\n  P: d", "P: {b}Tab A{/}", "CODE[py]: pass",
				"P: after", "{mark}m{/}", "{u}i{/}", "{sup}2{/}", "{sub}2{/}", "{ins}c{/}", "IMG: i -> i.png w=300"},
		},
		{
			name:   "docusaurus",
			flavor: "docusaurus",
			src:    "import Tabs from '@theme/Tabs';\n\nexport const x = 1;\n\n# H {#id}\n\n{/* comment */}\n\n:::note\n\nn\n\n:::\n\n:::tip[Bracket]\n\nt\n\n:::\n\n:::danger Plain title\n\nd\n\n:::\n",
			want:   []string{"H1: H", "ALERT[NOTE] NOTE\n  P: n", "ALERT[TIP] Bracket\n  P: t", "ALERT[WARNING] Plain title\n  P: d"},
			absent: []string{"import", "export", "comment", "{#id"},
		},
		{
			name:   "markout universal",
			flavor: "markout",
			src:    "# H\n\n[TOC]\n\n==m== {+ a +} :tada:\n\n!!! tip\n    t\n\n:::warning\nw\n:::\n\n::: mermaid\ngraph TD;\n:::\n\n> [!NOTE]\n> n\n\n~sub~ stays strikethrough\n",
			want: []string{"TOC: H", "{mark}m{/}", "{ins}a{/}", "🎉", "ALERT[TIP] TIP\n  P: t", "ALERT[WARNING] WARNING\n  P: w",
				"MERMAID: graph TD;", "ALERT[NOTE] NOTE\n  P: n", "{s}sub{/}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAs(t, tt.flavor, tt.src)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("unexpected %q in:\n%s", a, got)
				}
			}
		})
	}
}

// The LaTeX delimiters AI assistants write for math are read by the default
// flavor, without turning ordinary escaped brackets into formulas.
func TestLatexMathDelimiters(t *testing.T) {
	src := strings.Join([]string{
		`Inline \(E = mc^2\) and a lone symbol \(x\), then \( \frac{a}{b} \).`,
		``,
		`\[`,
		`\int_0^1 x^2 \, dx = \frac{1}{3}`,
		`\]`,
		``,
		`\[ a^2 + b^2 = c^2 \]`,
		``,
		`Mid-sentence display \[ y = kx \] continues.`,
		``,
		`Not math: a citation \[1\], a note \[see below\], and \(an aside in parentheses\).`,
		``,
		`Escaped backslash: \\(not math\\).`,
		``,
		"Code stays code: `\\(a+b\\)`.",
		``,
		"```",
		`\[`,
		`x = 1`,
		`\]`,
		"```",
	}, "\n")

	got := parseAs(t, "markout", src)
	for _, want := range []string{
		"{math}E = mc^2{/}", "{math}x{/}", `{math}\frac{a}{b}{/}`,
		`MATH: \int_0^1 x^2 \, dx = \frac{1}{3}`,
		"MATH: a^2 + b^2 = c^2",
		"{math}y = kx{/}",
		"a citation [1], a note [see below], and (an aside in parentheses).",
		`{code}\(a+b\){/}`,
		"CODE[]: \\[\nx = 1\n\\]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "{math}not math") {
		t.Errorf("an escaped backslash started a formula:\n%s", got)
	}

	// GitHub itself does not read these delimiters, so its flavor leaves them.
	if gh := parseAs(t, "github", `Inline \(E = mc^2\).`); strings.Contains(gh, "{math}") {
		t.Errorf("the github flavor read \\( \\) as math:\n%s", gh)
	}
	// MultiMarkdown's doubled form keeps working alongside.
	if mmd := parseAs(t, "multimarkdown", `\\\\(a+b\\\\) and \\\\[c = d\\\\]`); strings.Count(mmd, "{math}") != 2 {
		t.Errorf("multimarkdown math broke:\n%s", mmd)
	}
}

// Flavor syntax inside code must reach the output untouched.
func TestFlavorSyntaxIgnoredInCode(t *testing.T) {
	src := "`==a== {+ b +} [[c]]`\n\n```\n!!! note\n    ==x==\n::: mermaid\n[TOC]\n```\n"
	got := parseAs(t, "markout", src)
	for _, w := range []string{"{code}==a== {+ b +} [[c]]{/}", "CODE[]: !!! note\n    ==x==\n::: mermaid\n[TOC]"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestGitLabInclude(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part.md"), []byte("included **text**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fl, _ := flavor.ByID("gitlab")
	doc, err := ParseFlavor([]byte("before\n\n::include{file=part.md}\n\n::include{file=missing.md}\n"), fl, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := dump(doc)
	for _, w := range []string{"P: before", "P: included {b}text{/}", "::include{file=missing.md}"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

// Every flavor must parse every sample document without error.
func TestFlavorFixtures(t *testing.T) {
	files, err := filepath.Glob("../../fixtures/flavors/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no flavor fixtures found (%v)", err)
	}
	for _, fl := range flavor.All() {
		for _, file := range files {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := ParseFlavor(src, fl, filepath.Dir(file))
			if err != nil {
				t.Errorf("%s as %s: %v", filepath.Base(file), fl.ID, err)
			} else if len(doc.Elements) == 0 {
				t.Errorf("%s as %s: empty document", filepath.Base(file), fl.ID)
			}
		}
	}
}

func TestReplaceDelim(t *testing.T) {
	tests := []struct {
		in, delim  string
		allowSpace bool
		want       string
	}{
		{"a ==b c== d", "==", true, "a <t>b c</t> d"},
		{"a == b == c", "==", true, "a == b == c"},
		{"====", "==", true, "===="},
		{"H~2~O", "~", false, "H<t>2</t>O"},
		{"~~strike~~ and ~sub~", "~", false, "~~strike~~ and <t>sub</t>"},
		{"~a b~", "~", false, "~a b~"},
		{"x^2^ + y^2^", "^", false, "x<t>2</t> + y<t>2</t>"},
		{"[^1][^2]", "^", false, "[^1][^2]"},
		{`\==a==`, "==", true, `\==a==`},
	}
	for _, tt := range tests {
		if got := replaceDelim(tt.in, tt.delim, "t", tt.allowSpace); got != tt.want {
			t.Errorf("replaceDelim(%q, %q) = %q, want %q", tt.in, tt.delim, got, tt.want)
		}
	}
}

func TestColorChip(t *testing.T) {
	tests := map[string]string{
		"#F00":                "#F00",
		"rgb(255, 0, 0)":      "#FF0000",
		"RGBA(0,128,255,0.5)": "#0080FF",
		"hsl(120, 100%, 50%)": "#00FF00",
		"hsl(0, 0%, 100%)":    "#FFFFFF",
		"rgb(300, 0, 0)":      "",
		"console.log(1)":      "",
	}
	for in, want := range tests {
		if got := colorChip(in); got != want {
			t.Errorf("colorChip(%q) = %q, want %q", in, got, want)
		}
	}
}

// Escapes and character references are resolved in text, and left alone in
// code.
func TestEscapesAndEntitiesAreResolved(t *testing.T) {
	got := parseAs(t, "commonmark", "\\*not emphasis\\* \\# not a heading \\\\ &copy; &amp; &#169; &#xA9; `\\* &amp;`\n")
	if want := "*not emphasis* # not a heading \\ © & © © {code}\\* &amp;{/}"; !strings.Contains(got, want) {
		t.Errorf("got:\n%s\nwant it to contain:\n%s", got, want)
	}
}

// A date written without quotes is decoded as a time; it is shown as written.
func TestFrontMatterDate(t *testing.T) {
	for src, want := range map[string]string{
		"---\ndate: 2026-10-06\n---\n\nx\n":           `date="2026-10-06"`,
		"---\ndate: \"6 October 2026\"\n---\n\nx\n":   `date="6 October 2026"`,
		"---\ndate: 2026-10-06T14:30:00Z\n---\n\nx\n": `date="2026-10-06 14:30"`,
	} {
		if got := parseAs(t, "markout", src); !strings.Contains(got, want) {
			t.Errorf("%q: got %s, want %s", src, got, want)
		}
	}
}
