// Command markout is a terminal UI for converting Markdown files to DOCX or PDF.
//
// Run it with no arguments to launch the interactive TUI:
//
//	markout
//
// Or convert directly from the command line:
//
//	markout input.md output.docx
//	markout input.md output.pdf
//	markout --flavor gitlab input.md output.pdf
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/hkmt-sw/markout/internal/convert"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/settings"
	"github.com/hkmt-sw/markout/internal/tui"
)

// version is the build version, overridden at release time via:
//
//	go build -ldflags "-X main.version=1.0.0"
var version = "dev"

func main() {
	var positional []string
	flavorID := ""

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help" || (arg == "help" && len(args) == 1):
			printUsage()
			return
		case arg == "-v" || arg == "--version" || (arg == "version" && len(args) == 1):
			fmt.Printf("markout %s\n", version)
			return
		case arg == "--list-flavors" || arg == "--flavors":
			printFlavors()
			return
		case arg == "-f" || arg == "--flavor":
			if i+1 >= len(args) {
				fail("%s needs a flavor name (see --list-flavors)", arg)
			}
			i++
			flavorID = args[i]
		case strings.HasPrefix(arg, "--flavor="):
			flavorID = strings.TrimPrefix(arg, "--flavor=")
		default:
			positional = append(positional, arg)
		}
	}

	// The flavor comes from the command line, else from the saved settings.
	fl := settings.Load().MarkdownFlavor()
	if flavorID != "" {
		chosen, ok := flavor.ByID(flavorID)
		if !ok {
			fail("unknown flavor %q (see --list-flavors)", flavorID)
		}
		fl = chosen
	}

	// Non-interactive mode: markout <input.md> <output.docx|output.pdf>
	if len(positional) >= 2 {
		input, output := positional[0], positional[1]
		opts := convert.Options{Flavor: fl}
		if err := convert.ConvertFileWith(input, output, convert.FormatUnknown, opts); err != nil {
			fail("%v", err)
		}
		fmt.Printf("Saved to: %s\n", output)
		return
	}

	// Interactive mode.
	if err := tui.Run(fl); err != nil {
		fail("%v", err)
	}
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", a...)
	os.Exit(1)
}

func printUsage() {
	fmt.Print(`markout - Markdown to DOCX/PDF converter

Usage:
  markout                         Launch the interactive TUI
  markout <input.md> <out.docx>   Convert directly (format from extension)
  markout <input.md> <out.pdf>    Convert directly to PDF
  markout -h | --help             Show this help
  markout -v | --version          Show the version

Options:
  -f, --flavor <name>             Markdown flavor to interpret the input as
                                  (default: the one saved in the TUI settings)
      --list-flavors              List the supported flavors
`)
}

func printFlavors() {
	current := settings.Load().MarkdownFlavor().ID
	for _, f := range flavor.All() {
		mark := " "
		if f.ID == current {
			mark = "*"
		}
		fmt.Printf("%s %-14s %-30s %s\n", mark, f.ID, f.Name, f.Description)
	}
	fmt.Println("\n* = saved default (change it with F2 in the TUI)")
}
