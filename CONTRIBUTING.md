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
- Formatting that the document model cannot express yet also needs a field in
  `internal/ast` and support in both renderers.

## Pull requests

Keep a pull request to one change, describe what it does and how you checked
it, and add a line to `CHANGELOG.md` if it changes behavior.

Contributions are accepted under the project's license, the
[GNU General Public License, version 3](LICENSE).

## Releases

Maintainers release by pushing a version tag (`vX.Y.Z`). The release workflow
runs the tests, builds the binaries for every platform and publishes them.
