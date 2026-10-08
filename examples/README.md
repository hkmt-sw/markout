# Examples

Documents of the kinds people convert with markout, written to show what
comes out. Convert any of them to see for yourself:

```sh
markout examples/report/risk-assessment.md risk-assessment.pdf
markout examples/report/risk-assessment.md risk-assessment.docx
```

The organizations, people, products and numbers in them are made up.

## Policies and procedures

| File | What it is | What it shows |
| --- | --- | --- |
| [`policy/information-security-policy.md`](policy/information-security-policy.md) | A top-level policy with version, classification and approver in its front matter | Table of contents, definition lists, tables, numbered rules with sub-points, callouts, a footnote, a document history |
| [`policy/incident-response-procedure.md`](policy/incident-response-procedure.md) | What to do when something goes wrong | A severity table, the process as a flowchart, steps with code and warnings inside them, a checklist, footnotes |
| [`policy/mentesi-eljarasrend.md`](policy/mentesi-eljarasrend.md) | A backup and restore procedure, in Hungarian | Accented text throughout, Hungarian quotation marks, right-aligned columns |

## Technical documentation

| File | What it is | What it shows |
| --- | --- | --- |
| [`technical/architecture.md`](technical/architecture.md) | An architecture description on the outline of arc42 | Two flowcharts, one with subgraphs; SQL and YAML with syntax highlighting; a link to a heading |
| [`technical/runbook.md`](technical/runbook.md) | A runbook for a routine operation | A table without a header row, code blocks inside numbered steps, a troubleshooting table |

## Reports

| File | What it is | What it shows |
| --- | --- | --- |
| [`report/risk-assessment.md`](report/risk-assessment.md) | A risk assessment with an executive summary | Formulas in the text and on lines of their own, formulas in table headers, aligned columns, a footnote |

## Everyday documents

| File | What it is | What it shows |
| --- | --- | --- |
| [`everyday/meeting-minutes.md`](everyday/meeting-minutes.md) | Minutes of a team meeting | Task lists, decisions, an action table |
| [`everyday/project-readme.md`](everyday/project-readme.md) | The README of a small command-line tool | Shell, TOML and plain-text code blocks, an options table |
| [`everyday/assistant-answer.md`](everyday/assistant-answer.md) | An explanation as an AI assistant writes one | Math in `\(…\)` and `\[…\]`, an aligned derivation, Python with highlighting |

## Documentation-site Markdown

These two are written in the Markdown of a documentation generator. Name the
flavor when converting them:

```sh
markout --flavor myst examples/myst/user-guide.md user-guide.pdf
markout --flavor docusaurus examples/docusaurus/getting-started.mdx getting-started.pdf
```

| File | What it is | What it shows |
| --- | --- | --- |
| [`myst/user-guide.md`](myst/user-guide.md) | A page of a Sphinx manual in MyST | Directives and roles: admonitions, `list-table` and `csv-table`, a `math` block with a label, `toctree`, tabs, version notes |
| [`docusaurus/getting-started.mdx`](docusaurus/getting-started.mdx) | A page of a Docusaurus site in MDX | `<Tabs>` with indented content, admonitions with titles, code blocks with titles, `<details>`, heading ids |

## They are tests too

`TestGoldenExamples` converts every file here to PDF and DOCX on each
change. It checks that nothing is drawn outside the margins and that the DOCX
is consistent, compares a summary of each result with a recording, and
requires that the conversion has **nothing to warn about**: an example that
loses a character or cannot typeset a formula would be a poor example.

To add one, put it in the directory of its kind (or a directory named after
its flavor, if it is not in the default one), list it above, and record its
summary:

```sh
go test ./internal/render -run Golden -update
```
