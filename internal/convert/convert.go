package convert

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	// Validate input file exists
	info, err := os.Stat(input)
	if err != nil {
		if os.IsNotExist(err) {
			return &ConversionError{Op: "validate input", Err: ErrFileNotFound}
		}
		return &ConversionError{Op: "validate input", Err: err}
	}

	// Validate file size
	if info.Size() > MaxFileSize {
		return &ConversionError{
			Op:  "validate input",
			Err: fmt.Errorf("%w: file is %d bytes", ErrFileTooLarge, info.Size()),
		}
	}

	// Read input file
	content, err := os.ReadFile(input)
	if err != nil {
		return &ConversionError{Op: "read input", Err: err}
	}

	// Parse markdown
	doc, err := parse.ParseFlavor(content, fl, filepath.Dir(input))
	if err != nil {
		return &ConversionError{Op: "parse markdown", Err: fmt.Errorf("%w: %v", ErrParseFailure, err)}
	}

	// Render to output format
	switch format {
	case FormatDOCX:
		if err := render.RenderDocxToFileWithBaseDir(doc, output, filepath.Dir(input)); err != nil {
			return &ConversionError{Op: "render DOCX", Err: fmt.Errorf("%w: %v", ErrRenderFailure, err)}
		}
	case FormatPDF:
		if err := render.RenderPdfToFileWithBaseDir(doc, output, filepath.Dir(input)); err != nil {
			return &ConversionError{Op: "render PDF", Err: fmt.Errorf("%w: %v", ErrRenderFailure, err)}
		}
	default:
		return &ConversionError{Op: "render", Err: ErrInvalidFormat}
	}

	return nil
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
