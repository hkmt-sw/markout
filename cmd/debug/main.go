package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hkmt-sw/markout/internal/convert"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <markdown-file> [output.docx|output.pdf]\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]

	// If output file specified, use convert package
	if len(os.Args) >= 3 {
		outputFile := os.Args[2]

		// Handle --docx flag for backwards compatibility
		if os.Args[2] == "--docx" && len(os.Args) >= 4 {
			outputFile = os.Args[3]
		}

		if strings.HasSuffix(outputFile, ".docx") || strings.HasSuffix(outputFile, ".pdf") {
			if err := convert.ConvertFileAuto(filename, outputFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("%s written to: %s\n", strings.ToUpper(strings.TrimPrefix(outputFile[len(outputFile)-4:], ".")), outputFile)
			return
		}
	}

	// Otherwise, output parsed AST as JSON for debugging
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// MARKOUT_FLAVOR selects the flavor to parse as (default: markout)
	fl := flavor.Default()
	if id := os.Getenv("MARKOUT_FLAVOR"); id != "" {
		chosen, ok := flavor.ByID(id)
		if !ok {
			fmt.Fprintf(os.Stderr, "Unknown flavor: %s\n", id)
			os.Exit(1)
		}
		fl = chosen
	}

	doc, err := parse.ParseFlavor(content, fl, filepath.Dir(filename))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing markdown: %v\n", err)
		os.Exit(1)
	}

	jsonBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(jsonBytes))
}
