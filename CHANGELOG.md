# Changelog

All notable changes to markout are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

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
