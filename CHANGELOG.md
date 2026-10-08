# Changelog

All notable changes to markout are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [1.7.2] - 2026-10-08

Fixes found by converting real documents, and a slower way of releasing.

### Fixed

- MyST: a display formula closed with its label (`$$ (label)`) was not seen
  to end, and took the rest of the document into the formula.
- MyST: `{list-table}` written with more than one space after the `*`, and
  `{csv-table}` with its header on a line of its own (`:header: >`), were
  not read as tables.
- MyST: the formulas of a `{math}` block separated by a blank line are
  formulas of their own, and LaTeX environments at the top level
  (`\begin{align}` … `\end{align}`) are formulas.
- Math: a formula of several lines broken with `\\` needs no `aligned`
  around it, and `gather`, `multline`, `eqnarray` and the starred forms of
  environments are read.
- MyST `{toctree}` is shown as the list of pages it names, and `{raw} html`
  is read like other HTML in the document, instead of both being left out.
  `{versionadded}`, `{versionchanged}`, `{deprecated}`, `{table}`,
  `{tab-set}` and `{line-block}` are understood.

### Changed

- New features now go out as a release candidate first (`v1.8.0-rc1`),
  published as a pre-release that the download links and the update check
  do not offer, and become a release after being used for a few days. Fixes
  are still released as they are merged. See `CONTRIBUTING.md`.
- A build of a release candidate is told when its release, or a later
  candidate, is out.
- The tests convert a corpus of real documents from public projects
  (`fixtures/corpus`): READMEs and pages of the MkDocs Material, MyST and
  Docusaurus documentation.

## [1.7.1] - 2026-10-08

Nothing in a document goes missing without a word, in the documentation
dialects too.

### Fixed

- MyST `{eval-rst}` blocks vanished from the output. Their content is now
  shown as source. `{only}` blocks vanished as well; their content is shown.
- MyST `{toctree}`, `{raw}`, `{bibliography}` and `{index}`, and the
  `video` and `query-table` blocks of Azure DevOps wikis, have nothing a
  document can show and are still left out, but the conversion now warns
  and names them.
- A directive markout does not know lost its argument. The argument is kept
  with the content, and the conversion warns that the directive was not
  understood.
- MyST `{list-table}` came out as a nested list and `{csv-table}` as a line
  of text. Both are tables now, with their header row and caption.
- The `title="…"` of a code block was dropped. It is shown above the block.
- The labels of Docusaurus `<Tabs>` were dropped. Each tab's content is
  shown under its label.

## [1.7.0] - 2026-10-08

Formulas are typeset.

### Added

- Math in PDF. Formulas written in LaTeX are laid out by markout itself:
  fractions, roots, sub- and superscripts, sums and integrals with their
  limits, brackets that grow with what they enclose, matrices, `cases` and
  `aligned`, accents, Greek letters and the usual symbols, bold, calligraphic
  and blackboard letters. A formula in a line of text stands on its
  baseline; one on a line of its own is centered, and made smaller if it is
  wider than the text.
- Math in DOCX. Formulas are Word equations: Word typesets them with its own
  math font, and they can be edited there.
- A warning when formulas could not be typeset, saying how many and why the
  first one could not: a command markout does not know, or a formula that is
  not well-formed. Such a formula is shown as its source, as all formulas
  used to be.

### Changed

- Typeset formulas have the color of the text around them. The `math` color
  of a theme is now the color of a formula shown as its source.
- The release binaries are about 1.2 MB larger, for the math font and the
  LaTeX reader.

## [1.6.0] - 2026-10-08

### Added

- Syntax highlighting. A code block that names its language (` ```go `,
  ` ```python `, ...) has its keywords, strings, comments, numbers, function
  names and type names in colors of their own, in PDF and in DOCX. Almost
  300 languages are known, by the names GitHub uses. A block without a
  language, or with one that is not known, is shown in one color as before.
- Themes set the colors under `[code.syntax]`, and `highlight = false` there
  turns highlighting off. See the [theme guide](docs/themes.md#codesyntax).

### Changed

- The release binaries are about 3 MB larger, for the language definitions.

## [1.5.0] - 2026-10-08

PDFs you can find your way around in.

### Added

- PDF bookmarks. The headings of a document are its outline, nested by
  level, which viewers show in a pane beside the pages. A PDF with headings
  opens with that pane showing where the viewer follows the document's wish.
- Links to a heading of the same document (`[text](#heading)`) jump to it in
  PDF, as they do in DOCX since 1.4.0. The heading can be named by the
  anchor GitHub and GitLab give it, accented letters included.
- The entries of a PDF table of contents are links to their headings.

### Fixed

- A link to `#something` was written into a PDF as a link to the web address
  "#something", which did nothing or opened a browser. A link to a heading
  the document does not have is now plain colored text.

## [1.4.0] - 2026-10-08

A DOCX is a Word document to go on working in: its headings, lists, table of
contents and links are Word's own.

### Changed

- DOCX headings are in Word's Heading 1 to Heading 6 styles. They show in the
  navigation pane, a heading stays with the paragraph after it at a page
  break, and changing a style in Word changes every heading. The styles are
  written from the theme, so headings look as before.
- DOCX body text takes its font, size and color from the Normal style
  instead of carrying them on every piece of text. Text is now exactly the
  theme's size: where a theme asks for 11 pt (the default theme does), the
  size was left out of the document and word processors showed 10 pt.
- DOCX lists are Word lists. Bullets and numbers are drawn by the word
  processor, a long item's following lines line up under its text, and
  adding or removing an item in Word renumbers the list. Items sit a little
  further from their bullet than before.
- A DOCX table of contents is a field Word can update (right-click, Update
  Field) to get page numbers. Until then it lists the headings as before.

### Added

- Links to a heading of the same document (`[text](#heading)`) work in DOCX,
  and so do the entries of a table of contents: they jump to the heading.
  The heading can be named the way GitHub and GitLab name it, with accented
  letters kept.

## [1.3.4] - 2026-10-08

List items keep everything written under them.

### Fixed

- A code block, table, quote, callout, rule or formula inside a list item was
  left out of the output, in PDF and in DOCX. A step such as "1. Run this:"
  followed by an indented code block came out as the text alone. These are
  now shown under the item, indented to its text, and a numbered list goes on
  counting after them.
- A second paragraph in a list item was glued to the end of the first with no
  space between them. It is a paragraph of its own under the item now.
- An image in a list item was reduced to its alt text. It is embedded like
  any other image.
- A bulleted list inside a numbered one was lettered (a., b.) as if it were
  numbered too, and a numbered list inside a bulleted one got bullets. A
  nested list is shown the way it is written.
- The numbering of a nested list did not start again under the next item: the
  second item's sub-steps went on from where the first item's stopped.
- A footnote dropped the lists, quotes and code blocks in it. They are shown
  as lines of the note. In DOCX the paragraphs of a footnote ran together;
  each starts on a new line.
- A list inside a callout dropped the code blocks and tables of its items.

## [1.3.3] - 2026-10-08

Nothing in a document should go missing from its PDF without a word.

### Fixed

- A code block longer than a page ran off the bottom of it, and the lines
  below the edge were not visible anywhere. It now continues on the following
  pages, and starts on the page it is on instead of leaving that page empty.
  The same goes for callouts, Mermaid boxes and front matter. A block that
  fits on one page is still kept together.
- A quote that runs onto a second page has its bar on both pages.
- Text with nowhere to break ran past the right edge of the page: a long
  line of code, a URL, a long word in a table cell, a long formula, a long
  heading in a table of contents, an image caption. Such text is now broken
  to the width it has.
- Tabs in code are expanded to tab stops four characters apart; they were
  drawn as a single space.
- Symbols the PDF fonts do have (™, №, ►, ● and others) were dropped along
  with emoji. Only characters the font in use really lacks are left out now,
  so a theme with a font of your own can show Chinese, Japanese or Korean
  text, or emoji.

### Added

- A warning when characters are left out of a PDF because no font has them,
  naming the characters, and another when the document has right-to-left
  text, which PDF output does not lay out. A direct conversion prints them to
  standard error; the TUI shows them in the status line. The file is still
  written.

### Changed

- The README no longer says that math and diagrams "come out right": both
  are shown as their source. A new [Limits](README.md#limits) section lists
  this and what else markout does not do yet.

## [1.3.2] - 2026-10-06

### Changed

- The default theme lays DOCX out with spacing too. Its DOCX output had no
  space between headings and paragraphs, indented lists with spaces, and drew
  quote bars and rules with characters; it now uses the spacing, indents and
  borders its PDF form has always had. Fonts, sizes and colors are unchanged.
  Existing documents come out more open in DOCX and may run to more pages;
  PDF output is not affected.

### Fixed

- Word no longer asks "This document contains fields that may refer to other
  files. Do you want to update the fields in this document?" when opening a
  DOCX that has page numbers in its header or footer.

## [1.3.1] - 2026-10-06

### Changed

- Themes lay out DOCX too. Line heights, the space before and after
  headings, paragraphs, lists, tables, code blocks, quotes, rules and
  callouts, the padding of panels and table cells, and list and quote indents
  are now written into DOCX; they used to apply to PDF only. With a theme,
  lists are indented with real indents, quote bars and rules are borders, and
  tables span the text width. The default theme keeps its plain DOCX layout.
- DOCX keeps inline formatting in headings, table cells, definition lists
  and footnotes (bold, italic, code, links), which showed plain text.
- DOCX table cells use the theme's text color like other text.

## [1.3.0] - 2026-10-06

Themes: choose how the output looks, or design your own.

### Added

- Four more built-in themes next to the default: `classic`, `modern`,
  `compact` and `report`. See the [theme guide](docs/themes.md).
- Themes. A theme file (TOML) sets page size and margins, fonts, sizes,
  spacing and colors, and extends another theme so it lists only what it
  changes. Select one with `--theme NAME|FILE` or on the new Theme tab of the
  settings (`F2`, `Tab`); `--list-themes` and `--export-theme` help with
  making your own. Themes placed in the themes directory are available by
  name.
- Headers and footers: a theme can put text at the left, center and right of
  the top and bottom margin of every page, with `{title}`, `{author}`,
  `{date}`, `{page}` and `{pages}`, and an optional rule.
- A built-in serif font for PDF, and fonts of your own loaded from `.ttf`
  files named in a theme.
- Paper sizes other than A4 (A3, A5, Letter, Legal, Tabloid, or any width and
  height) and landscape orientation, in both formats.
- `fixtures/showcase.md`, a document with every element and a note on what
  each should look like, for checking a conversion by eye.

### Changed

- PDF output shows inline formatting everywhere text appears. Bold, italic,
  inline code, links, strikethrough, underline, highlight, superscript and
  subscript used to be drawn only in paragraphs; headings, list items, table
  cells, quotes, callouts, definition lists and footnotes showed plain text.
- Inline code in PDF sits on the baseline of the surrounding text and is no
  longer preceded by an over-wide space.
- Long terms and definitions of a definition list wrap in PDF instead of
  running off the page.
- In PDF, the text of lists, quotes and footnotes uses the same ink color as
  paragraphs (it was pure black).
- PDF tables honor column alignment (`:---:`, `---:`), as DOCX already did.
- The settings dialog (`F2`) has two tabs, Flavor and Theme.

### Fixed

- A front matter date written without quotes was shown as a full timestamp
  (`2026-10-06 00:00:00 +0000 UTC`); it is shown as written.
- PDF code blocks and diagram panels ended with an empty line.
- The PDF metadata panel left empty rows for values it does not show, and
  extra front matter fields appeared in a different order from run to run.

## [1.2.2] - 2026-10-06

### Added

- The default flavor reads the LaTeX math delimiters that AI assistants
  commonly write: `\(E = mc^2\)` inline and `\[ … \]` for display formulas,
  on one line or with the delimiters on their own lines. Escaped brackets that
  are plain text, such as `\[1\]`, stay text.

### Fixed

- Backslash escapes and character references were shown as written: `\*`
  appeared with its backslash and `&copy;` as those six characters. They are
  now resolved (`*`, `©`).

## [1.2.1] - 2026-10-06

### Fixed

- SVG images were shown at twice their own size in both PDF and DOCX output.
  They now appear at the size the SVG declares, like other images.

### Changed

- The test suite records the DOCX structure and PDF layout of every sample
  document and compares against it, runs the built binary end to end, and CI
  runs on Linux, macOS and Windows.

## [1.2.0] - 2026-10-06

### Added

- Update notifications: the TUI checks for a newer release at most once a day
  and announces it in the top border. `F2` then `u` turns the check off, and
  `markout --check-update` looks on demand. Command-line conversions never
  check, and nothing is installed automatically.
- `markout --version` reports the right version for binaries installed with
  `go install`.

## [1.1.2] - 2026-10-06

### Security

- Release binaries are built with the current Go release instead of Go 1.24.2,
  which is no longer supported and carried known vulnerabilities in the
  standard library (among them in `net/http` and `crypto/tls`, used when a
  document references an image by URL). Building from source now needs Go 1.26
  or newer.
- Updated `golang.org/x/sys`.
- Images referenced by URL are no longer downloaded silently. The TUI lists
  the servers a document would contact and asks whether to load the images,
  skip them or cancel; the command line asks `[y/N]`, and skips them when
  there is no terminal to ask on. `--remote-images allow|deny|ask` sets the
  behavior explicitly. Addresses on the local machine or network are flagged.
- GitLab `::include{file=…}` only reads regular files inside the directory of
  the document being converted. Absolute paths, `..` and symlinks leading
  outside are ignored, so a document can no longer copy arbitrary local files
  into its output.
- Local images must be regular files of at most 20 MB. Referencing a device
  such as `/dev/zero` as an image used to exhaust memory.
- Inputs that made a conversion run for minutes now finish at once: thousands
  of unclosed or deeply nested `:::` blocks, and lists nested thousands of
  levels deep.
- On Windows, opening the converted file (`o`) no longer goes through
  `cmd.exe`, which interpreted characters such as `&` in the file name.
- The TUI replaces control characters in file names, so a name cannot emit
  terminal escape sequences.
- Release binaries come with a signed build provenance attestation, and the
  workflows pin every GitHub Action to a commit.

### Changed

- Scripts that relied on remote images being downloaded need
  `--remote-images allow`.
- A `:::` block that is never closed is left as text instead of swallowing the
  rest of the document.

## [1.1.1] - 2026-10-06

### Changed

- Updated dependencies: docxgo 2.14.0, gopdf 0.38.1, goldmark 1.8.6. DOCX
  tables now carry real column widths in their grid definition, which some
  viewers rely on; PDF output is unchanged.
- The install snippet in the README looks up the latest release by itself.

## [1.1.0] - 2026-10-06

The first public release.

### Added

- Markdown flavors: the input can be interpreted as CommonMark, GitHub, GitLab,
  YouTrack, Azure DevOps, Bitbucket, Obsidian, Pandoc, MultiMarkdown, Markdown
  Extra, kramdown, MyST, MkDocs Material or Docusaurus, or with the universal
  default that combines them. See "Markdown flavors" in the README.
- `F2` in the TUI opens a settings dialog to choose the flavor; the choice is
  saved and reused.
- `--flavor <name>` and `--list-flavors` on the command line.
- Underline, highlight, superscript, subscript and inserted/deleted text in
  both output formats.
- Raw HTML is interpreted: inline tags such as `<u>`, `<sub>`, `<sup>`,
  `<mark>`, `<kbd>` and `<a>`, plus `<details>`/`<summary>`, headings and
  `<img>` in HTML blocks.
- Emoji shortcodes (`:rocket:`), `rgb()`/`hsl()` color chips, GitLab
  `json:table` blocks and `::include` directives.
- Images are embedded in PDF and DOCX output, including SVG (with text) and
  width hints.

### Changed

- markout is licensed under the GNU General Public License, version 3.
- The Go module path is `github.com/hkmt-sw/markout`, so
  `go install github.com/hkmt-sw/markout@latest` works.
- Callouts lay out lists, code and tables from their body, and wrap long text
  in PDF output.

### Fixed

- PDF output no longer puts a space between adjacent pieces of formatted text
  (for example between bold text and the comma after it).
- Definition lists lost the text of their definitions.
- A display formula written on one line (`$$ a = b $$`) swallowed the rest of
  the document.
- Text in table cells was clipped in PDF output.

Versions before 1.1.0 were private pre-releases and are listed for reference.

## [1.0.2] - 2026-06-03

### Changed

- The README install instructions follow the latest release automatically.

## [1.0.1] - 2026-06-03

### Fixed

- PDF output embeds the Liberation fonts, so it renders the same on every
  platform and covers Hungarian accented characters.

## [1.0.0] - 2026-06-03

First release: an interactive terminal UI and a direct command-line mode that
convert Markdown to DOCX and PDF, with prebuilt binaries for macOS, Linux and
Windows.
