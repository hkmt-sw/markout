# Changelog

All notable changes to markout are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [1.10.0] - Unreleased

Policy documents: the parts of a policy or procedure that are typed into Word
by hand today. In progress.

### Added

- A cover page, in PDF and DOCX. `cover: true` in the front matter puts the
  title in large type on a page of its own, with the `subtitle` under it and
  the rest of the control block below; the text starts on the next page. A
  theme can ask for one for every document that has front matter with
  `enabled = true` under `[cover]`, and `cover: false` takes it off one
  document. The theme sets the sizes and colors of the title and subtitle and
  how far down the page the title starts. The title is the `title` of the
  front matter, or the first top-level heading. The cover has no header or
  footer, and counts as page 1. In DOCX the title and subtitle are in Word's
  Title and Subtitle styles. A `cover` value that is not `true` or `false` is
  ignored, and markout says so.
- `{version}` and `{classification}` in headers and footers, filled in from
  the front matter as the control block shows them.
- A classification band, in PDF and DOCX. A document whose front matter has a
  `classification` gets a band across the top edge of every page, the cover
  included, with the classification centered in it in bold type. The band
  says the classification as written; the level it belongs to gives the
  colors. Four levels are built in, known by English and Hungarian names:
  `public` (`nyilvános`), `internal` (`belső`, `belső használatra`),
  `confidential` (`bizalmas`) and `secret` (`titkos`, `szigorúan titkos`). A
  classification no level knows gets neutral colors, and markout says so. A
  theme adds names, changes colors, adds levels with `[classification.NAME]`
  and sets the size and height of the band under `[classification]`. In DOCX
  the band is a text box in the header, anchored to the page; a theme without
  a header gets a header part for it, and the header text and the margins
  stay where they are.
- A watermark, in PDF and DOCX. `watermark: TERVEZET` in the front matter
  draws the text in light gray across every page, from the bottom left to the
  top right, behind the text and shrunk if it would not fit. A theme sets the
  color and size under `[watermark]`. In DOCX it is Word's own text watermark.
  A watermark that is not text is ignored, and markout says so.
- A version history table, in PDF and DOCX. `history: git` in the front
  matter lists the commits that touched the file and carry a `Changelog:`
  trailer, newest first and following the file through renames: the day of
  the commit, its author and the text of the trailer. A `Version:` trailer on
  any of them adds a Version column. The table is titled and labeled in the
  language of the document (Hungarian or English) and comes right after the
  control block, at the top of the page after the cover if there is one. It
  reads the repository on this machine only. Without git, outside a
  repository, for a file never committed or with no `Changelog:` trailer
  markout says so and converts the document without the table. A `history`
  value other than `git` is ignored, and markout says so.
- PDF/A-2u output, for archiving. `--pdfa` on the command line, `pdfa: true`
  in the front matter (`pdfa: false` turns it off) or the PDF/A setting in
  the TUI (`F2`, then `a`; saved with the other settings) makes a PDF that
  conforms to PDF/A-2u, checked with veraPDF: the file has an identifier, XMP
  metadata made from the same title, author, language and date as its Info
  dictionary, an sRGB output intent, and the other things the standard
  asks of the fonts, streams and links. A plain PDF is made as before. A
  `pdfa` value that is not `true` or `false` is ignored, and markout says so;
  `--pdfa` with a `.docx` output warns that it applies to PDF only. The
  cover page, the classification band and the watermark conform. The
  tagged PDF/A-2a and PDF/UA are not made. CI converts the sample documents
  with `--pdfa` and validates them with a pinned veraPDF. The color profile
  is the CC0 sRGB profile of Compact ICC Profiles.
- A guide, [Writing a policy document](docs/policy-documents.md), that takes
  a policy from an empty file to a PDF with a cover page, control block,
  classification band, version history and headers, with a table of what to
  do when something does not appear; and an example policy that uses all of
  it, `examples/policy/jogosultsagkezelesi-szabalyzat.md`.
- `F5` or `m` in the file browser shows only Markdown files and directories,
  and shows all files again when pressed a second time. The list stays on the
  entry that was highlighted. The choice is not saved (#130).

### Changed

- Documents that already have a `classification` now get a band on every
  page. A theme turns it off with `band = false` under `[classification]`.
- The front matter is shown as a document control block: a table with the
  labels in a column on the left and a thin line between the fields, in PDF
  and DOCX, instead of a box of `key: value` lines. The keys a policy
  carries come first, in this order and under labels in the language of the
  document (Hungarian or English): `title`, `version`, `status`, `author`,
  `owner`, `approved-by`, `date`, `review-date`, `classification`. They are
  found whatever their case and with `_` for `-` (`approved_by`). Other keys
  follow under their own names, as before. The labels can be renamed under
  `[control.labels]` in a theme. A number such as `version: 1.0` is shown as
  written, not as `1`.

### Fixed

- The file browser drew directories in blue and Markdown files, the ones
  that can be converted, in the plain text color. Markdown files are blue
  now and directories are bold in the plain text color; other files stay dim (#130).
- Going up a directory put the cursor on the first entry. It is on the
  directory that was just left now (#130).
- A folder that macOS does not let the terminal app read, such as Documents
  or Downloads before the app is allowed there, showed only "cannot read
  directory". On macOS markout now says what has happened and where to allow
  it, and `p` opens the Files and Folders list of System Settings (#130).
- Front matter with nothing to show, such as only a list of `tags`, gave an
  empty box in DOCX. It gives none, as in PDF.
- In DOCX the first row of every table was a header row that Word repeats on
  each page a table runs on to, also in tables without a header and in the
  tables markout draws code blocks, callouts and the control block with. Only
  a table's real header row repeats now. (#94)
- Callouts and the table of contents were titled in English in a document of
  another language. In a Hungarian document they now read MEGJEGYZÉS, TIPP,
  FONTOS, FIGYELEM, VIGYÁZAT and Tartalomjegyzék; a title the author wrote is
  kept as written. A theme renames them with `label` under `[alert.note]` and
  the other kinds, and with `title` under the new `[toc]`. Callouts of other
  kinds, such as MkDocs `info` or `danger`, keep the title they had. (#95)
- A no-break space (`&nbsp;`) did not keep words together in PDF: a line could
  break at it, and a name such as "Kovács Anna" was broken in a narrow table
  column. The narrow no-break space and the figure space did the same. (#96)
- A keyboard key such as `++a-++` or `++a--b++` made markout crash. It is
  left as typed now. A bug of this kind in the future is reported as an error
  instead of a crash. (#99)
- In the TUI, Enter during a conversion started a second one into the same
  file. It does nothing now until the first one is done. (#100)
- The PDF and DOCX were written straight into the output file, so a full disk
  or a stopped conversion left a broken file in place of the earlier one, and
  in DOCX a failed write was reported as saved. The output is written next to
  it first and replaces it only when it is complete; the intermediate DOCX no
  longer goes to `<output>.tmp`. (#101)
- A settings file that could not be read, for example after a typo, was
  replaced by the defaults the next time the TUI saved. markout now says so,
  uses the defaults without saving them, and when a setting is changed keeps
  the old file as `config.json.bak`. The settings file is written as a whole
  and only its owner can read it. (#102)
- A device or a pipe given as the input, such as `/dev/zero`, was read with
  no size limit or blocked. Only a regular file is read now, and never more
  than the 10 MB limit. (#104)
- An image width such as `{width=9999999999999999}` overflowed and gave the
  image a negative size in DOCX. Widths are capped. (#105)
- A theme could set `nan` or `inf` as a size or length, and page sizes,
  margins and spacing had no upper limit; the output had nonsense sizes in
  it. Such values are errors now: a page side is 72 to 14400 points, a margin
  less than half the page and a spacing 0 to 400 points. (#106)
- An option markout does not know was taken as a file name, and one file
  name alone opened the TUI. Both are errors now. (#107)
- An error of a conversion kept the kind of failure but not its cause for
  programs that check it. (#108)
- In the TUI, `o` waited until the program that opens the output returned,
  which with some viewers is when the viewer is closed, and a failure was
  not shown. (#109)
- A table of more than 63 columns made the DOCX fail. DOCX shows the first 63
  columns now, and markout says so. (#111)
- An SVG of extreme proportions printed an error of the image library over
  the screen, or took a great deal of memory. Such an image is a placeholder
  now, and markout says so. (#112)
- A theme's `description` or `name` that was not text was ignored without a
  word. It is an error now. (#114)
- A file that starts with a UTF-8 byte order mark, as many Windows editors
  save it, had its front matter shown as text, so the cover page, the control
  block and the classification band were missing without a word. The mark is
  dropped now, in included files too. (#121)
- The `commonmark`, `youtrack`, `bitbucket` and `extra` flavors do not read
  front matter, and a document that started with one lost it without a word.
  markout warns now and names a flavor that reads it. (#122)
- `cover: yes` and `pdfa: yes` were ignored, since `yes` is text in YAML, and
  the warning for `cover: "true"` read "not true". `yes` and `no` count now as
  well as `true` and `false`, in any case and in quotes, and the warning
  quotes the value it ignored. (#123)
- The `commonmark`, `youtrack`, `bitbucket` and `extra` flavors did not read
  front matter, so a document in one of them had no cover page, control
  block, watermark or classification band, and the `---` and the keys were
  shown as text. The cover page and the rest are markout's own, not the
  platform's: front matter that starts the file is now read in every flavor.
  This replaces the warning of #122, which is gone. (#126)

### Security

- Before downloading images referenced by URL, markout asks the user, but the
  list of servers it showed left out images in lists, and a server could
  redirect the download to another one. Every image is listed now, images are
  downloaded only from the servers the user agreed to, and redirects to other
  servers are refused.
- A server the user agreed to was trusted by its name, which can lead to an
  address on this machine or the local network. markout now checks the
  address it connects to, and refuses one that is not public unless the
  server was shown as local. Through a proxy (`HTTP_PROXY`, `HTTPS_PROXY`)
  the name is checked before the request; where markout cannot look the name
  up and only the proxy can, it says so, and the proxy decides.

## [1.9.0] - 2026-10-14

Typography: text that is justified and hyphenated.

### Added

- The language of a document. `lang: hu` in the front matter says what
  language a document is written in (`language` is read too); for documents
  that do not say, there is `--lang` on the command line and a **Language**
  tab in the settings of the TUI. The language is written into the DOCX,
  where Word checks the spelling and hyphenates by it, and into the PDF,
  where a screen reader reads by it. With no language given the output is
  what it was.
- Justified text. `align = "justify"` under `[text]` in a theme of your own
  sets paragraphs, list items, quotes and callouts flush on both sides, in
  PDF and DOCX. Headings, table cells, captions and code stay flush left,
  and so does a line of a PDF whose word spaces would have to grow to more
  than three times their width. No built-in theme is justified.
- Hyphenation. Words that do not fit at the end of a line are broken there,
  by the rules of the document's language: in PDF by markout, which has the
  rules of Hungarian, American English and British English; in DOCX by the
  word processor, which the document now tells to. `hyphenate` under
  `[text]` in a theme says where: `"auto"` (the default) in justified text,
  `"always"` in all body text, table cells included, `"never"` nowhere.
  Headings, captions, code, short words and words with digits stay whole,
  and no more than three lines in a row end in a hyphen. When a PDF cannot
  be hyphenated because its language is not named or is another one, markout
  says so. A theme that is not justified gives the output it gave before.
- A soft hyphen (`&shy;`) marks where a word may be broken in PDF, in any
  text and in any language.

### Changed

- `lang` and `language` are no longer shown among the fields of the front
  matter, and front matter that holds nothing else shows no box.

### Fixed

- A soft hyphen (`&shy;`) in a word was drawn in PDF as a hyphen in the
  middle of the line. It is no longer drawn.

## [1.8.3] - 2026-10-13

Maintenance. Nothing a document shows has changed.

### Changed

- The seven most intricate functions were taken apart into smaller ones:
  the reading of blocks and of MyST directives, the breaking of text into
  lines in PDF, and the reading, ordering, placing and drawing of a Mermaid
  flowchart. Each was compared with what it replaced on generated input
  (60,000 charts, 61,000 documents, over a million laid-out paragraphs)
  before the old code was removed.
- The small findings of a SonarQube analysis of 1.8.0 were dealt with:
  repeated texts are named, and a function with eight parameters has four.

## [1.8.2] - 2026-10-13

A flowchart no longer stops a conversion.

### Fixed

- A Mermaid flowchart with a connection to a subgraph whose first member is
  itself a subgraph named in a connection stopped the whole conversion with
  an error, and no file was written:

  ```
  subgraph outer
    subgraph inner
      inner --> B
    end
  end
  outer --> A
  ```

  The connection now goes to a box inside, as other connections to a
  subgraph do. Found by laying out 60,000 generated charts; three of them
  failed, all for this reason.

## [1.8.1] - 2026-10-13

An e-mail address in the text opens the mail program.

### Fixed

- An e-mail address written in the text (`info@example.com` or
  `<info@example.com>`) became a link to the bare address, which no program
  opens. The link is now `mailto:info@example.com`, in PDF and DOCX. A link
  written out as `[text](mailto:...)` was right before.
- An address written as `www.example.com` was shown as
  `http://www.example.com`. It is shown as written; only the link has the
  scheme.

## [1.8.0] - 2026-10-08

Mermaid flowcharts are drawn.

### Added

- Flowcharts. A Mermaid `graph` or `flowchart` is laid out and drawn by
  markout itself: boxes of every shape, arrows and lines of every kind with
  their text, subgraphs in frames, and the four directions. In PDF the chart
  is lines and text, sharp at any zoom; in DOCX it is a picture. A chart
  wider than the text is made smaller to fit.
- A warning naming the kinds of diagram that are not drawn (sequence, class,
  state, Gantt and the rest), and one for a flowchart that could not be
  read. Those are shown as their source in a panel, as all diagrams used to
  be.

- MkDocs Material: `--8<-- "file.md"` snippets are put in, under the rules
  of `::include` (the file has to be in the document's directory or below
  it); a snippet that cannot be read is named in a warning. Keys
  (`++ctrl+alt+del++`) are shown as `Ctrl`+`Alt`+`Del`. Icons
  (`:material-check:`) are left out, with a warning. Markdown inside HTML
  that has the `markdown` attribute (grid cards) is read.
- Page numbers in the PDF table of contents: each entry has the page its
  heading is on, at the right, after a row of dots. The number is a link
  like the title.
- Examples: the [`examples`](examples/README.md) directory has eleven
  documents of the kinds people convert (a policy, an incident procedure, an
  architecture description, a runbook, a risk assessment, minutes, a README,
  an assistant's answer, a MyST page, a Docusaurus page, and a procedure in
  Hungarian). They are converted by the tests, which require that the
  conversion has nothing to warn about. The examples are in the public
  domain (CC0 1.0), to be copied as the start of your own documents.
- A table whose header row is empty has no header row: the way Markdown
  writes a table without one. A MyST `{csv-table}` without a header no
  longer gets an empty one.

### Changed

- The `[diagram]` colors of a theme are the colors of a chart's boxes, as
  well as of the panel that holds the source of a diagram that is not drawn.

### Fixed

- A formula the math translator failed on (`$\hat{}$`, an empty `cases`)
  printed the failure and the formula into the terminal, over the screen of
  the interactive mode too, and the warning about it carried the
  translator's own message. Nothing is printed now, and the warning says
  that the formula could not be read.
- Text copied from a formula in a PDF, or searched for, was wrong:
  `E = mc²` came out as other characters, because the italic letters of
  formulas were written into the file in a way readers misread. Formulas
  are now found by search and read correctly by screen readers, with their
  italic and bold letters as the plain letters they stand for.
- Docusaurus: text indented inside `<Tabs>` and other components, or
  anywhere outside a list, became a code block, with its backticks showing.
  In MDX indentation does not make code, and it no longer does here.
- A flowchart that cannot be read is reported with the line the problem is
  on.
- In PDF a heading could be the last thing on a page, with what it heads on
  the next. A heading now goes to the next page together with what follows
  it. The same holds for the header row of a table and its first row, and
  for a term and its definition. A chart as tall as a page is drawn a little
  smaller, to leave room for its heading on the same page.
- MyST: the `:caption:` of a `code-block` was dropped. It is shown above the
  block.

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
- Docusaurus: a JSX component that stands by itself (`<Chart />`) shows
  nothing in a document and was dropped without a word. It is still left
  out, and the conversion now names it.

### Changed

- New features now go out as a release candidate first (`v1.8.0-rc1`),
  published as a pre-release that the download links and the update check
  do not offer, and become a release after being used for a few days. Fixes
  are still released as they are merged. See `CONTRIBUTING.md`.
- A build of a release candidate is told when its release is out.
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
