# Changelog

All notable changes to markout are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

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
