package convert

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/history"
	"github.com/hkmt-sw/markout/internal/lang"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/render"
	"github.com/hkmt-sw/markout/internal/theme"
)

// OutputFormat represents the output file format
type OutputFormat int

const (
	FormatUnknown OutputFormat = iota
	FormatDOCX
	FormatPDF
)

// String returns the string representation of the format
func (f OutputFormat) String() string {
	switch f {
	case FormatDOCX:
		return "DOCX"
	case FormatPDF:
		return "PDF"
	default:
		return "Unknown"
	}
}

// MaxFileSize is the maximum allowed input file size (10MB)
const MaxFileSize = 10 * 1024 * 1024

// opValidateInput names the step that checks the input file, in errors.
const opValidateInput = "validate input"

// Error types for conversion
var (
	ErrFileNotFound  = errors.New("input file not found")
	ErrFileTooLarge  = errors.New("input file exceeds maximum size (10MB)")
	ErrNotRegular    = errors.New("input is not a regular file")
	ErrInvalidFormat = errors.New("could not determine output format from extension")
	ErrParseFailure  = errors.New("failed to parse markdown")
	ErrRenderFailure = errors.New("failed to render document")
	ErrOutputFailure = errors.New("failed to write output file")
	ErrInternal      = errors.New("internal error")
)

// ConversionError wraps an error with additional context
type ConversionError struct {
	Op  string
	Err error
}

func (e *ConversionError) Error() string {
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

func (e *ConversionError) Unwrap() error {
	return e.Err
}

// DetectFormat determines the output format from a filename extension
func DetectFormat(filename string) OutputFormat {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".docx":
		return FormatDOCX
	case ".pdf":
		return FormatPDF
	default:
		return FormatUnknown
	}
}

// Options tune how a file is converted.
type Options struct {
	// Flavor is the Markdown dialect the input is written in. The zero value
	// selects the default flavor.
	Flavor flavor.Flavor
	// RemoteImages allows images referenced by http(s) URL to be downloaded.
	// It is off unless the user agreed to it: fetching them tells the servers
	// named in the document who opened it, and a document can point at
	// addresses on the local network. See RemoteImageHosts.
	RemoteImages bool
	// RemoteHosts are the servers the user agreed to download images from,
	// as RemoteHostNames gives them for the list RemoteImageHosts returned.
	// Images are downloaded from these servers only, and redirects are
	// followed only to them. With none, RemoteImages allows nothing.
	RemoteHosts []string
	// Theme is how the output looks; nil means the default theme.
	Theme *theme.Set
	// Lang is the language of documents that do not name one themselves,
	// as a language tag ("hu", "en-GB"). Empty means none.
	Lang string
	// PDFA asks for PDF/A-2u in PDF output; DOCX output ignores it. The
	// document's own pdfa key, when it is true or false, decides instead.
	PDFA bool
	// Warn, if set, is called with each thing the output does not show the
	// way the document has it (characters no font can draw, for example).
	// The conversion still succeeds.
	Warn func(message string)
}

// ConvertFile converts a Markdown file to the specified output format using
// the default flavor.
// If format is FormatUnknown, it will be auto-detected from the output filename.
func ConvertFile(input, output string, format OutputFormat) error {
	return ConvertFileWith(input, output, format, Options{})
}

// ConvertFileWith converts a Markdown file to the specified output format.
// If format is FormatUnknown, it will be auto-detected from the output filename.
// A bug that makes the conversion panic is returned as an error, so that the
// programs using it can report it instead of crashing.
func ConvertFileWith(input, output string, format OutputFormat, opts Options) (err error) {
	defer recoverInternal("convert", &err)

	fl := opts.Flavor
	if fl.ID == "" {
		fl = flavor.Default()
	}

	// Auto-detect format if not specified
	if format == FormatUnknown {
		format = DetectFormat(output)
		if format == FormatUnknown {
			return &ConversionError{Op: "detect format", Err: ErrInvalidFormat}
		}
	}

	doc, err := loadDocument(input, fl)
	if err != nil {
		return err
	}

	if opts.Warn != nil {
		for _, w := range doc.Warnings {
			opts.Warn(w)
		}
	}

	addVersionHistory(doc, input, opts.Warn)

	// What the document says of its language goes before what it is told
	language := doc.Lang
	if language == "" {
		language, _ = lang.Normalize(opts.Lang)
	}

	renderOpts := render.Options{BaseDir: filepath.Dir(input), RemoteImages: opts.RemoteImages, RemoteHosts: opts.RemoteHosts, Theme: opts.Theme, Lang: language, PDFA: opts.PDFA, Warn: opts.Warn}

	// Render to output format
	switch format {
	case FormatDOCX:
		if err := render.RenderDocx(doc, output, renderOpts); err != nil {
			return &ConversionError{Op: "render DOCX", Err: fmt.Errorf("%w: %w", ErrRenderFailure, err)}
		}
	case FormatPDF:
		if err := render.RenderPdf(doc, output, renderOpts); err != nil {
			return &ConversionError{Op: "render PDF", Err: fmt.Errorf("%w: %w", ErrRenderFailure, err)}
		}
	default:
		return &ConversionError{Op: "render", Err: ErrInvalidFormat}
	}

	return nil
}

// recoverInternal turns a panic into an error in *err. It is deferred by the
// entry points that read a document written by someone else: a bug that a
// document sets off must not take the whole program down with a stack trace
// nobody asked for. The panic value is kept in the message so that it can be
// reported.
func recoverInternal(op string, err *error) {
	if r := recover(); r != nil {
		*err = &ConversionError{Op: op, Err: fmt.Errorf("%w: %v (this is a bug in markout, please report it with the document that caused it)", ErrInternal, r)}
	}
}

// loadDocument validates, reads and parses the input file.
func loadDocument(input string, fl flavor.Flavor) (*ast.Document, error) {
	// Validate input file exists
	info, err := os.Stat(input)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ConversionError{Op: opValidateInput, Err: ErrFileNotFound}
		}
		return nil, &ConversionError{Op: opValidateInput, Err: err}
	}

	// A device or a pipe reports a size of 0 and has no end (/dev/zero), or
	// blocks until someone writes to it, so only regular files are read.
	if !info.Mode().IsRegular() {
		return nil, &ConversionError{Op: opValidateInput, Err: fmt.Errorf("%w: %s", ErrNotRegular, input)}
	}

	// Validate file size
	if info.Size() > MaxFileSize {
		return nil, &ConversionError{
			Op:  opValidateInput,
			Err: fmt.Errorf("%w: file is %d bytes", ErrFileTooLarge, info.Size()),
		}
	}

	// Read input file. The limit is applied to what is read as well, because
	// the size above can be out of date by now (a file that is still being
	// written) or wrong for the file system; one byte more than the limit is
	// read to tell a file of exactly the limit from a larger one.
	file, err := os.Open(input)
	if err != nil {
		return nil, &ConversionError{Op: "read input", Err: err}
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil {
		return nil, &ConversionError{Op: "read input", Err: err}
	}
	if len(content) > MaxFileSize {
		return nil, &ConversionError{
			Op:  opValidateInput,
			Err: fmt.Errorf("%w: file is larger than %d bytes", ErrFileTooLarge, MaxFileSize),
		}
	}

	// Parse markdown
	doc, err := parse.ParseFlavor(content, fl, filepath.Dir(input))
	if err != nil {
		return nil, &ConversionError{Op: "parse markdown", Err: fmt.Errorf("%w: %w", ErrParseFailure, err)}
	}
	return doc, nil
}

// addVersionHistory puts the version history of the input file, read from its
// git repository, after the front matter of a document that asks for it. When
// there is none to show, the document is left as it is and warn, if set, says
// why.
func addVersionHistory(doc *ast.Document, input string, warn func(string)) {
	if len(doc.Elements) == 0 {
		return
	}
	fm, ok := doc.Elements[0].(ast.FrontMatter)
	if !ok {
		return
	}
	asked, warning := history.Asked(fm)
	if asked {
		var entries []ast.VersionEntry
		entries, warning = history.Of(input)
		if len(entries) > 0 {
			doc.Elements = slices.Insert(doc.Elements, 1, ast.Element(ast.VersionHistory{Entries: entries}))
		}
	}
	if warning != "" && warn != nil {
		warn(warning)
	}
}

// RemoteHost is a server a document would download images from.
type RemoteHost struct {
	Host   string // host name or IP address, with the port if one is given
	Images int    // number of images referenced on it
	Local  bool   // the address is on this machine or the local network
}

// RemoteHostNames gives the names of hosts as Options.RemoteHosts takes them.
func RemoteHostNames(hosts []RemoteHost) []string {
	names := make([]string, len(hosts))
	for i, h := range hosts {
		names[i] = h.Host
	}
	return names
}

// RemoteImageHosts lists the servers that converting input would download
// images from, in order of first appearance. Nothing is fetched or resolved.
// Callers show the list to the user and, only if the user agrees, set
// Options.RemoteImages and Options.RemoteHosts to it. The conversion
// downloads from no other server.
func RemoteImageHosts(input string, opts Options) (hosts []RemoteHost, err error) {
	defer recoverInternal("list remote images", &err)

	fl := opts.Flavor
	if fl.ID == "" {
		fl = flavor.Default()
	}
	doc, err := loadDocument(input, fl)
	if err != nil {
		return nil, err
	}

	index := map[string]int{}
	for _, img := range ast.Images(doc.Elements) {
		if !render.IsRemoteURL(img.URL) {
			continue
		}
		host := render.RemoteHostOf(img.URL)
		i, seen := index[host]
		if !seen {
			i = len(hosts)
			index[host] = i
			hosts = append(hosts, RemoteHost{Host: host, Local: isLocalHost(host)})
		}
		hosts[i].Images++
	}
	return hosts, nil
}

// isLocalHost reports whether a URL host names this machine or an address
// that is only reachable on the local network, by the name alone. The download
// uses the same rule (render.IsLocalHost) to decide which servers may be at a
// non-public address, so what the user is shown is what is allowed.
func isLocalHost(hostport string) bool {
	return render.IsLocalHost(hostport)
}

// ConvertFileAuto converts a Markdown file, auto-detecting format from output extension
func ConvertFileAuto(input, output string) error {
	return ConvertFile(input, output, FormatUnknown)
}

// GenerateOutputPath creates an output path by replacing the input file extension
func GenerateOutputPath(input string, format OutputFormat) string {
	ext := filepath.Ext(input)
	base := strings.TrimSuffix(input, ext)

	switch format {
	case FormatDOCX:
		return base + ".docx"
	case FormatPDF:
		return base + ".pdf"
	default:
		return base + ".docx"
	}
}
