# Writing a policy document

This guide takes a policy or procedure from an empty file to a finished PDF
or DOCX: a cover page, the document control table, a classification band, a
watermark, a version history and running headers. Each step adds one thing,
and each one is optional.

- [1. Start from the example](#1-start-from-the-example)
- [2. The front matter](#2-the-front-matter)
- [3. A cover page](#3-a-cover-page)
- [4. Classification and watermark](#4-classification-and-watermark)
- [5. A version history from git](#5-a-version-history-from-git)
- [6. Headers and footers](#6-headers-and-footers)
- [7. PDF/A for the archive](#7-pdfa-for-the-archive)
- [8. Convert it](#8-convert-it)
- [When something does not appear](#when-something-does-not-appear)

## 1. Start from the example

[`examples/policy/jogosultsagkezelesi-szabalyzat.md`](../examples/policy/jogosultsagkezelesi-szabalyzat.md)
is a short policy that uses everything below. Convert it first, to see what
you are aiming for:

```sh
markout examples/policy/jogosultsagkezelesi-szabalyzat.md policy.pdf
```

Then copy it, keep the front matter, and replace the text.

## 2. The front matter

Everything that is not the text itself goes into the front matter: a block
of `key: value` lines between two `---` lines, **at the very top of the
file**: no text or heading may come before the first `---`.

```markdown
---
title: Mentési eljárásrend
version: "1.2"
status: Jóváhagyva
author: Informatikai üzemeltetés
owner: Informatikai vezető
approved-by: Ügyvezető
date: 2026-09-20
review-date: 2027-09-20
classification: Belső
lang: hu
---

# Mentési eljárásrend

The text starts here.
```

markout sets these keys as a table at the top of the document, labelled in
the language that `lang` names (Hungarian for `hu`, English otherwise). Leave
out the ones you do not need. Any other key with a text value is added to the
table under its own name. The README's
[Document control](../README.md#document-control) section lists the keys and
their order.

## 3. A cover page

Add `cover: true` (or `cover: yes`), and a `subtitle` if you want one:

```markdown
---
title: Mentési eljárásrend
subtitle: Informatikai üzemeltetés
version: "1.2"
classification: Belső
cover: true
---
```

The first page then holds the title in large type, the subtitle under it, and
the control table under them. The text starts on page 2. The cover page has no
header or footer, though it counts as page 1.

To give every document a cover page without writing `cover: true` in each, use
a theme with `enabled = true` under `[cover]`; `cover: false` (or `no`) takes
it off one document. The theme also sets the sizes and colors of the cover.
See [Cover page](themes.md#cover-page) in the theme guide.

## 4. Classification and watermark

`classification` puts a colored band across the top of every page, the cover
included. The level decides the color: `public`/`nyilvános` green,
`internal`/`belső` blue, `confidential`/`bizalmas` amber,
`secret`/`titkos` red. Any other value gets a gray band, and markout warns.

`watermark` writes its text in light gray diagonally across every page, which
suits a draft:

```markdown
classification: Bizalmas
watermark: TERVEZET
```

Remove the `watermark` line when the document is approved. See
[Classification and watermark](../README.md#classification-and-watermark).

## 5. A version history from git

If the document is kept in git, `history: git` builds the version history
table from the commits of the file. Only commits with a `Changelog:` line as
the last paragraph of their message are listed:

```sh
git commit -m "Tighten the retention rule" \
  -m "Changelog: A mentések megőrzési ideje 90 nap." \
  --trailer "Version: 1.3"
```

Commit the file at least once with such a line before you convert it;
otherwise markout warns and leaves the table out. See
[Version history](../README.md#version-history).

## 6. Headers and footers

The built-in `report` theme puts the title and date in the header and the page
numbers in the footer:

```sh
markout --theme report policy.md policy.pdf
```

For your own line, say the classification on the left and the version on the
right, write a small theme:

```toml
extends = "report"

[header]
left  = "{title}"
right = "{classification} · v{version}"
```

See [Headers and footers](themes.md#headers-and-footers).

## 7. PDF/A for the archive

If the PDF goes into an archive or a records system, add `pdfa: true` to the
front matter, or convert with `--pdfa`. See [PDF/A](../README.md#pdfa).

## 8. Convert it

On the command line:

```sh
markout policy.md policy.pdf
markout policy.md policy.docx
```

In the TUI (`markout` with no arguments), highlight the file and press
`Enter`; `F3` switches between PDF and DOCX.

The front matter is only read by flavors that know it. The default flavor,
`markout`, does. `commonmark`, `youtrack`, `bitbucket` and `extra` do not, and
markout warns if you use one of them on a document with front matter. In the
TUI the flavor is under `F2`, and it is saved, so check it if a cover page that
worked before stops appearing.

## When something does not appear

markout says when it ignores something: on the command line in a `Warning:`
line, in the TUI in the status line after the conversion. Read it first.

| What you see | Why | What to do |
| --- | --- | --- |
| The `---` and the keys appear as text on the first page | Text comes before the first `---`, or the flavor does not read front matter (markout warns about the flavor) | Make `---` the first line of the file; use the `markout` flavor (`F2` in the TUI, or `--flavor markout`) |
| No cover page, and a warning about `cover` | The value is not one markout knows | Write `cover: true` or `cover: yes` |
| No cover page, and no warning | The front matter has no `cover` key, and the theme gives no cover | Add `cover: true` |
| A gray band, and a warning about the classification | The classification is not one of the known levels | Use one of the names in [section 4](#4-classification-and-watermark), or add your level in a theme |
| No version history table, and a warning | The file is not committed, or no commit has a `Changelog:` line | Commit with a `Changelog:` line, as in [section 5](#5-a-version-history-from-git) |
| The control table is in English in a Hungarian document | `lang` is missing | Add `lang: hu` |

If the document still does not come out as you expect, open an issue with the
file (or its first lines) and the markout version (`markout --version`).
