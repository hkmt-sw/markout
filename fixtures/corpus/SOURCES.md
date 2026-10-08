# Corpus

Real documents from public projects, kept as they were written. The tests
convert every one of them to PDF and DOCX (`TestGoldenCorpus` in
`internal/render`) and check that nothing is drawn off the page, that the
DOCX is consistent, and that a summary of the result (what the document
holds, how many pages it makes, what the conversion warns about) is the one
recorded. Sample documents written for markout show what it is meant to
do; these show what it meets.

The directory a document is in names the flavor it is read as.

To add one: put the file under the directory of its flavor, add it to the
table with the commit it was taken from and its license, put the license
text under `licenses/` if the project is new here, and record its summary:

```sh
go test ./internal/render -run Golden -update
```

Only take documents whose license allows redistribution, and do not edit
them: a document that is fixed up no longer shows what real ones look like.

## Sources

| File | Project | Path there | Commit | License |
| --- | --- | --- | --- | --- |
| `github/cli.md` | [cli/cli](https://github.com/cli/cli) | [`README.md`](https://github.com/cli/cli/blob/8db3c3107a174c51de4758f53d5b27911650931b/README.md) | `8db3c3107a17` | [MIT](licenses/cli-cli.txt) |
| `github/fzf.md` | [junegunn/fzf](https://github.com/junegunn/fzf) | [`README.md`](https://github.com/junegunn/fzf/blob/33a3456921a17c53f42f8610783b6d379d652d3e/README.md) | `33a3456921a1` | [MIT](licenses/junegunn-fzf.txt) |
| `github/bubbletea.md` | [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) | [`README.md`](https://github.com/charmbracelet/bubbletea/blob/96d69d2f7eb182bb311be08fc56ed2c208239aff/README.md) | `96d69d2f7eb1` | [MIT](licenses/charmbracelet-bubbletea.txt) |
| `github/ripgrep-guide.md` | [BurntSushi/ripgrep](https://github.com/BurntSushi/ripgrep) | [`GUIDE.md`](https://github.com/BurntSushi/ripgrep/blob/3fce3b5bb0236da2df6d99672afb8a719642eca7/GUIDE.md) | `3fce3b5bb023` | [Unlicense](licenses/BurntSushi-ripgrep.txt) |
| `mkdocs/admonitions.md` | [squidfunk/mkdocs-material](https://github.com/squidfunk/mkdocs-material) | [`docs/reference/admonitions.md`](https://github.com/squidfunk/mkdocs-material/blob/6d3dc570d51064a3f55d189bd22c2390b07d46fe/docs/reference/admonitions.md) | `6d3dc570d510` | [MIT](licenses/squidfunk-mkdocs-material.txt) |
| `mkdocs/code-blocks.md` | [squidfunk/mkdocs-material](https://github.com/squidfunk/mkdocs-material) | [`docs/reference/code-blocks.md`](https://github.com/squidfunk/mkdocs-material/blob/6d3dc570d51064a3f55d189bd22c2390b07d46fe/docs/reference/code-blocks.md) | `6d3dc570d510` | [MIT](licenses/squidfunk-mkdocs-material.txt) |
| `mkdocs/content-tabs.md` | [squidfunk/mkdocs-material](https://github.com/squidfunk/mkdocs-material) | [`docs/reference/content-tabs.md`](https://github.com/squidfunk/mkdocs-material/blob/6d3dc570d51064a3f55d189bd22c2390b07d46fe/docs/reference/content-tabs.md) | `6d3dc570d510` | [MIT](licenses/squidfunk-mkdocs-material.txt) |
| `mkdocs/data-tables.md` | [squidfunk/mkdocs-material](https://github.com/squidfunk/mkdocs-material) | [`docs/reference/data-tables.md`](https://github.com/squidfunk/mkdocs-material/blob/6d3dc570d51064a3f55d189bd22c2390b07d46fe/docs/reference/data-tables.md) | `6d3dc570d510` | [MIT](licenses/squidfunk-mkdocs-material.txt) |
| `myst/typography.md` | [executablebooks/MyST-Parser](https://github.com/executablebooks/MyST-Parser) | [`docs/syntax/typography.md`](https://github.com/executablebooks/MyST-Parser/blob/723cffcf84213f0cb58695b27eec9ad72052b53a/docs/syntax/typography.md) | `723cffcf8421` | [MIT](licenses/executablebooks-MyST-Parser.txt) |
| `myst/admonitions.md` | [executablebooks/MyST-Parser](https://github.com/executablebooks/MyST-Parser) | [`docs/syntax/admonitions.md`](https://github.com/executablebooks/MyST-Parser/blob/723cffcf84213f0cb58695b27eec9ad72052b53a/docs/syntax/admonitions.md) | `723cffcf8421` | [MIT](licenses/executablebooks-MyST-Parser.txt) |
| `myst/math.md` | [executablebooks/MyST-Parser](https://github.com/executablebooks/MyST-Parser) | [`docs/syntax/math.md`](https://github.com/executablebooks/MyST-Parser/blob/723cffcf84213f0cb58695b27eec9ad72052b53a/docs/syntax/math.md) | `723cffcf8421` | [MIT](licenses/executablebooks-MyST-Parser.txt) |
| `myst/tables.md` | [executablebooks/MyST-Parser](https://github.com/executablebooks/MyST-Parser) | [`docs/syntax/tables.md`](https://github.com/executablebooks/MyST-Parser/blob/723cffcf84213f0cb58695b27eec9ad72052b53a/docs/syntax/tables.md) | `723cffcf8421` | [MIT](licenses/executablebooks-MyST-Parser.txt) |
| `docusaurus/tabs.mdx` | [facebook/docusaurus](https://github.com/facebook/docusaurus) | [`website/docs/guides/markdown-features/markdown-features-tabs.mdx`](https://github.com/facebook/docusaurus/blob/26d988c76b1d2b2005dfde4063be807491d601d8/website/docs/guides/markdown-features/markdown-features-tabs.mdx) | `26d988c76b1d` | [MIT](licenses/facebook-docusaurus.txt) |
| `docusaurus/admonitions.mdx` | [facebook/docusaurus](https://github.com/facebook/docusaurus) | [`website/docs/guides/markdown-features/markdown-features-admonitions.mdx`](https://github.com/facebook/docusaurus/blob/26d988c76b1d2b2005dfde4063be807491d601d8/website/docs/guides/markdown-features/markdown-features-admonitions.mdx) | `26d988c76b1d` | [MIT](licenses/facebook-docusaurus.txt) |
| `docusaurus/code-blocks.mdx` | [facebook/docusaurus](https://github.com/facebook/docusaurus) | [`website/docs/guides/markdown-features/markdown-features-code-blocks.mdx`](https://github.com/facebook/docusaurus/blob/26d988c76b1d2b2005dfde4063be807491d601d8/website/docs/guides/markdown-features/markdown-features-code-blocks.mdx) | `26d988c76b1d` | [MIT](licenses/facebook-docusaurus.txt) |
| `docusaurus/math-equations.mdx` | [facebook/docusaurus](https://github.com/facebook/docusaurus) | [`website/docs/guides/markdown-features/markdown-features-math-equations.mdx`](https://github.com/facebook/docusaurus/blob/26d988c76b1d2b2005dfde4063be807491d601d8/website/docs/guides/markdown-features/markdown-features-math-equations.mdx) | `26d988c76b1d` | [MIT](licenses/facebook-docusaurus.txt) |

The documents are the work of their authors and remain under the licenses
above; they are not covered by markout's own license.
