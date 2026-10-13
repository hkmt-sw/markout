package parse

import "testing"

// MyST directives that no other test reaches: options as a YAML block and
// over several lines, and the less common directives.
func TestMystDirectives(t *testing.T) {
	for src, want := range map[string]string{
		"```{line-block}\nfirst line\n\nsecond line\n```\n":                                                             "P: first linesecond line\n",
		"```{line-block}\n```\n\nafter\n":                                                                               "P: after\n",
		"```{code-block} python\n---\ncaption: main.py\nlinenos: true\n---\nprint(1)\n```\n":                            "P: {b}main.py{/}\nCODE[python]: print(1)\n",
		"```{list-table} Sizes\n:header-rows: 1\n:widths: >\n:   10\n:   20\n\n* - Name\n  - Size\n* - a\n  - 1\n```\n": "P: {b}Sizes{/}\nROW: Name | Size\nROW: a | 1\n",
		"```{mermaid}\ngraph TD\nA --> B\n```\n":                                                                        "MERMAID: graph TD\nA --> B\n",
		"```{eval-rst}\n.. note:: text\n```\n":                                                                          "CODE[rst]: .. note:: text\n",
		"```{contents}\n```\n\n# One\n":                                                                                 "TOC: One\nH1: One\n",
		"```{index} word\n```\n\nafter\n":                                                                               "P: after\n",
		"```{math}\na = b\n\nc = d\n```\n":                                                                              "MATH: a = b\nMATH: c = d\n",
		"```{toctree}\n```\n\nafter\n":                                                                                  "P: after\n",
		"```{versionchanged} 2.0\n```\n":                                                                                "P: {i}Changed in version 2.0{/}\n",
		"```{grid} 2\ninside\n```\n":                                                                                    "P: inside\n",
		"```{only} html\ninside\n```\n":                                                                                 "P: inside\n",
	} {
		if got := parseAs(t, "myst", src); got != want {
			t.Errorf("%q:\ngot:\n%swant:\n%s", src, got, want)
		}
	}
}
