# Contributing to markout

Bug reports, fixes and new syntax support are welcome.

## Reporting a problem

Open an issue with the smallest Markdown snippet that shows the problem, the
flavor you converted it as, and whether the output was PDF or DOCX. Remove
anything private from the snippet first.

## Building and testing

You need Go (the version in `go.mod` or newer).

```sh
make build     # ./markout
make test      # go test ./...
make run       # launch the TUI without installing
```

Before sending a pull request, make sure these pass, as CI runs them:

```sh
gofmt -l .     # must print nothing
go vet ./...
go test ./...
```

### When the output changes

`internal/render` keeps a recording of what every sample document converts to
(`testdata/golden`): an outline of the DOCX body, and for PDF everything drawn
with its position and font. `TestGoldenOutput` fails when the output differs,
which is how an unintended change in layout or formatting is caught.

If you meant to change the output, record it again and include the updated
files in your pull request, where the diff shows reviewers exactly what moved:

```sh
go test ./internal/render -run Golden -update
```

### Examples

`examples/` holds documents written for markout, of the kinds people
convert. `TestGoldenExamples` converts each one and requires a conversion
with nothing to warn about. They are the place to see how a change reads in
a whole document, and a new kind of document worth supporting is worth an
example.

### Real documents

`fixtures/corpus` holds documents from public projects, unedited, with
their origins and licenses in its `SOURCES.md`. `TestGoldenCorpus` converts
each to both formats, checks that nothing is drawn off the page and that the
DOCX is consistent, and compares a few lines of summary (what the document
holds, its page count, the warnings) with a recording under
`testdata/golden/corpus`. They are there because documents written to test
a feature are written by someone who knows how the feature reads them. When
a real document shows a problem, add it to the corpus with the fix.

### Checking the output by eye

`fixtures/showcase.md` contains every element markout renders, each with a
note saying what you should see. After a change to a renderer, convert it to
both formats and read through the result:

```sh
go run . fixtures/showcase.md showcase.pdf
go run . fixtures/showcase.md showcase.docx
```

To see how a document is parsed, dump its AST as JSON:

```sh
MARKOUT_FLAVOR=gitlab go run ./cmd/debug fixtures/flavors/gitlab.md
```

## How the code is organized

A conversion runs through three stages:

1. `internal/parse` turns Markdown into the document model in `internal/ast`.
   `preprocess.go` rewrites flavor-specific syntax into forms the core parser
   understands; `markdown.go` walks the goldmark tree.
2. `internal/render` writes that model as DOCX (`docx.go`) or PDF (`pdf.go`).
3. `internal/convert` ties the two together for the TUI (`internal/tui`) and
   the command line (`main.go`).

### Adding syntax or a flavor

- A flavor is an entry in `internal/flavor/flavor.go`: a name and a set of
  `Features`. A new dialect that only combines existing features needs nothing
  else.
- New syntax gets a field in `Features`, handling in the preprocessor or the
  walker, and a case in `TestFlavorSyntax` (`internal/parse/flavor_test.go`)
  showing that the flavors that have it recognize it and the others leave it
  alone.
- Add the flavor's sample document to `fixtures/flavors/` and its row to the
  table in the README.
- A built-in theme is a file in `internal/theme/builtin/`; a new setting is
  a field in `internal/theme/theme.go` with its `key` tag, read by both
  renderers and described in `docs/themes.md` (a test checks the guide
  mentions every setting).
- Formatting that the document model cannot express yet also needs a field in
  `internal/ast` and support in both renderers.

## Pull requests

Keep a pull request to one change, describe what it does and how you checked
it, and add a line to `CHANGELOG.md` if it changes behavior.

Contributions are accepted under the project's license, the
[GNU General Public License, version 3](LICENSE).

## Planning

What goes into a version is decided before work on it starts. Each version
is a [milestone](https://github.com/hkmt-sw/markout/milestones) with one
theme, and each piece of work in it is an issue.

- **One theme a version.** The milestone says what the version is for; the
  issues under it are its whole scope.
- **The scope is closed once work starts.** An idea that comes up on the way
  becomes an issue in a later milestone, or in none. The exception is a
  defect in something the version itself adds.
- **Done means shown.** A feature has a document in `examples` that uses
  it, tests, and where the output is a DOCX, a look at it in Word.
- **Fixes do not wait.** A defect in a released version is fixed and
  released by itself, whatever milestone is being worked on.

Issues with no milestone are things worth doing that have no version yet.

## Releases

Maintainers release by pushing a version tag (`vX.Y.Z`). The release workflow
runs the tests, builds the binaries for every platform and publishes them.

- **A fix** (`X.Y.Z` with a new `Z`) is tagged when its pull request is
  merged.
- **New features** (a new `X` or `Y`) first go out as a release candidate,
  `vX.Y.0-rc1`, when every issue of the milestone is done. A candidate is
  published as a pre-release: it does not become the latest release, and
  neither the download links nor the update check offer it. It is there to
  be used on real documents for a few days.
  Problems found are fixed and tagged `-rc2` and so on; the candidate that
  holds up is tagged `vX.Y.0` on the same commit.

While a version is a candidate, its entry in `CHANGELOG.md` is headed
`## [X.Y.0] - Unreleased`. The pull request that ends the candidate period
puts the date there.
