# tidyfiles

Sorts the files of a folder into subfolders by type and date. Made for
download folders that have got out of hand.

```text
Downloads/                      Downloads/
├── invoice-march.pdf           ├── Documents/2026-03/invoice-march.pdf
├── IMG_2041.jpg         →      ├── Pictures/2026-04/IMG_2041.jpg
├── notes.txt                   ├── Documents/2026-04/notes.txt
└── setup.dmg                   └── Installers/setup.dmg
```

## Install

With Homebrew:

```sh
brew install example/tap/tidyfiles
```

Or download a binary from the [releases page](https://example.com/tidyfiles/releases)
and put it somewhere on your `PATH`.

## Usage

```sh
tidyfiles ~/Downloads              # sort the folder
tidyfiles --dry-run ~/Downloads    # only show what would be moved
tidyfiles --undo ~/Downloads       # put everything back
```

Nothing is ever deleted or overwritten. A file whose name is taken at the
destination gets a number: `notes (2).txt`.

### Options

| Option | Default | Meaning |
| --- | --- | --- |
| `--dry-run` | off | Print the moves without making them |
| `--by` | `type,month` | What to sort by: `type`, `month`, `year`, in any order |
| `--older-than` | (none) | Leave files newer than this alone, e.g. `30d` |
| `--undo` | off | Reverse the last run in this folder |
| `--config` | `~/.config/tidyfiles.toml` | Where the rules are read from |

## Rules

Which type a file is depends on its extension. The built-in rules can be
changed or added to in the configuration file:

```toml
[types]
Documents  = ["pdf", "docx", "txt", "md"]
Pictures   = ["jpg", "jpeg", "png", "heic"]
Installers = ["dmg", "pkg", "exe", "msi"]

[ignore]
names = [".DS_Store", "desktop.ini"]
newer_than = "7d"        # give fresh downloads a week
```

> [!TIP]
> Run with `--dry-run` after changing the rules. It costs nothing, and shows
> at once whether a rule catches more than you meant.

## How it works

1. The folder is read, without going into subfolders.
2. Each file is matched against the rules, first match wins.
3. The moves are written to a journal in the folder, `.tidyfiles-journal`.
4. The moves are made, each one ticked off in the journal.

The journal is what `--undo` reads. If the program is interrupted, the next
run finishes or reverses the half-done moves before doing anything else.

## Building from source

```sh
git clone https://example.com/tidyfiles.git
cd tidyfiles
go build ./cmd/tidyfiles
go test ./...
```

## License

MIT. See `LICENSE`.
