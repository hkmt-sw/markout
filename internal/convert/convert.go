package convert

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/render"
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

// Error types for conversion
var (
	ErrFileNotFound  = errors.New("input file not found")
	ErrFileTooLarge  = errors.New("input file exceeds maximum size (10MB)")
	ErrInvalidFormat = errors.New("could not determine output format from extension")
	ErrParseFailure  = errors.New("failed to parse markdown")
	ErrRenderFailure = errors.New("failed to render document")
	ErrOutputFailure = errors.New("failed to write output file")
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
}

// ConvertFile converts a Markdown file to the specified output format using
// the default flavor.
// If format is FormatUnknown, it will be auto-detected from the output filename.
func ConvertFile(input, output string, format OutputFormat) error {
	return ConvertFileWith(input, output, format, Options{})
}

// ConvertFileWith converts a Markdown file to the specified output format.
// If format is FormatUnknown, it will be auto-detected from the output filename.
func ConvertFileWith(input, output string, format OutputFormat, opts Options) error {
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

	renderOpts := render.Options{BaseDir: filepath.Dir(input), RemoteImages: opts.RemoteImages}

	// Render to output format
	switch format {
	case FormatDOCX:
		if err := render.RenderDocx(doc, output, renderOpts); err != nil {
			return &ConversionError{Op: "render DOCX", Err: fmt.Errorf("%w: %v", ErrRenderFailure, err)}
		}
	case FormatPDF:
		if err := render.RenderPdf(doc, output, renderOpts); err != nil {
			return &ConversionError{Op: "render PDF", Err: fmt.Errorf("%w: %v", ErrRenderFailure, err)}
		}
	default:
		return &ConversionError{Op: "render", Err: ErrInvalidFormat}
	}

	return nil
}

// loadDocument validates, reads and parses the input file.
func loadDocument(input string, fl flavor.Flavor) (*ast.Document, error) {
	// Validate input file exists
	info, err := os.Stat(input)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ConversionError{Op: "validate input", Err: ErrFileNotFound}
		}
		return nil, &ConversionError{Op: "validate input", Err: err}
	}

	// Validate file size
	if info.Size() > MaxFileSize {
		return nil, &ConversionError{
			Op:  "validate input",
			Err: fmt.Errorf("%w: file is %d bytes", ErrFileTooLarge, info.Size()),
		}
	}

	// Read input file
	content, err := os.ReadFile(input)
	if err != nil {
		return nil, &ConversionError{Op: "read input", Err: err}
	}

	// Parse markdown
	doc, err := parse.ParseFlavor(content, fl, filepath.Dir(input))
	if err != nil {
		return nil, &ConversionError{Op: "parse markdown", Err: fmt.Errorf("%w: %v", ErrParseFailure, err)}
	}
	return doc, nil
}

// RemoteHost is a server a document would download images from.
type RemoteHost struct {
	Host   string // host name or IP address, with the port if one is given
	Images int    // number of images referenced on it
	Local  bool   // the address is on this machine or the local network
}

// RemoteImageHosts lists the servers that converting input would download
// images from, in order of first appearance. Nothing is fetched or resolved.
// Callers show the list to the user and set Options.RemoteImages only if the
// user agrees.
func RemoteImageHosts(input string, opts Options) ([]RemoteHost, error) {
	fl := opts.Flavor
	if fl.ID == "" {
		fl = flavor.Default()
	}
	doc, err := loadDocument(input, fl)
	if err != nil {
		return nil, err
	}

	var hosts []RemoteHost
	index := map[string]int{}
	for _, elem := range doc.Elements {
		img, ok := elem.(ast.Image)
		if !ok || !render.IsRemoteURL(img.URL) {
			continue
		}
		host := img.URL
		if u, err := url.Parse(img.URL); err == nil && u.Host != "" {
			host = u.Host
		}
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
// that is only reachable on the local network. It looks at the name alone and
// never resolves it, so a public name that points at a private address is not
// detected.
func isLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))

	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true // a bare name resolves on the local network only
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".lan", ".home", ".home.arpa", ".intranet", ".corp"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
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
