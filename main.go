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
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/mattn/go-isatty"

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
	remote := "" // --remote-images: ask, allow or deny

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
		case arg == "--remote-images":
			if i+1 >= len(args) {
				fail("--remote-images needs ask, allow or deny")
			}
			i++
			remote = args[i]
		case strings.HasPrefix(arg, "--remote-images="):
			remote = strings.TrimPrefix(arg, "--remote-images=")
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
		opts.RemoteImages = allowRemoteImages(input, opts, remote)
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

// allowRemoteImages decides whether a direct conversion may download the
// images the document references by URL. policy is the --remote-images value;
// when it is not given the user is asked if there is a terminal to ask on,
// and the images are skipped otherwise.
func allowRemoteImages(input string, opts convert.Options, policy string) bool {
	interactive := stdinIsTerminal()
	switch policy {
	case "":
		policy = "deny"
		if interactive {
			policy = "ask"
		}
	case "ask", "allow", "deny":
	default:
		fail("--remote-images must be ask, allow or deny, not %q", policy)
	}
	if policy == "ask" && !interactive {
		fail("--remote-images=ask needs a terminal; use allow or deny")
	}

	// A file that cannot be read or parsed is reported by the conversion.
	hosts, _ := convert.RemoteImageHosts(input, opts)
	if len(hosts) == 0 {
		return false
	}
	if policy == "allow" {
		return true
	}

	fmt.Fprintln(os.Stderr, "Warning: this document downloads images from the internet:")
	for _, h := range hosts {
		note := ""
		if h.Local {
			note = "  <- local network address!"
		}
		plural := "s"
		if h.Images == 1 {
			plural = ""
		}
		fmt.Fprintf(os.Stderr, "  %-40s %d image%s%s\n", printable(h.Host), h.Images, plural, note)
	}

	if policy == "deny" {
		fmt.Fprintln(os.Stderr, "Skipped: they are left as placeholders. Use --remote-images=allow to download them.")
		return false
	}

	fmt.Fprintln(os.Stderr, "These servers will see your IP address and that you opened this document.")
	fmt.Fprint(os.Stderr, "Download them at your own risk? [y/N] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	fmt.Fprintln(os.Stderr, "Skipped: the images are left as placeholders.")
	return false
}

func stdinIsTerminal() bool {
	fd := os.Stdin.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// printable replaces control characters so a host name taken from a document
// cannot write escape sequences to the terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return '?'
	}, s)
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
      --remote-images <mode>      Images referenced by URL: ask (default in a
                                  terminal), allow, or deny (default otherwise)
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
