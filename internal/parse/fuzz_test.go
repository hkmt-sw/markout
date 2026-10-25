package parse

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hkmt-sw/markout/internal/flavor"
)

// fuzzTimeLimit is how long one flavor may take to read one input. The inputs
// of the security tests used to take minutes; nothing the fuzzer finds should
// take more than a moment.
const fuzzTimeLimit = 2 * time.Second

// FuzzParse feeds arbitrary bytes to the parser as every flavor. It must not
// panic and must not hang. Errors are fine.
//
//	go test -run='^$' -fuzz=FuzzParse -fuzztime=60s ./internal/parse
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"",
		"# Title\n\ntext **bold** _it_ `code` [link](https://example.com)\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n",
		"- [x] done\n- [ ] open\n  - nested\n    - deeper\n",
		"> [!NOTE] Title\n> body\n",
		"---\ntitle: T\n---\n\n+++\na = 1\n+++\n\n;;;\n{\"a\": 1}\n;;;\n",
		"$$\n\\frac{a}{b}\n$$\n\n```math\nx^2\n```\n\n```mermaid\ngraph TD; A-->B;\n```\n",
		"::: mermaid\ngraph LR\nA-->B\n:::\n",
		"!!! note \"Title\"\n    body\n\n=== \"Tab\"\n    content\n",
		"```{note}\nbody\n```\n\n:::{warning}\nbody\n:::\n\n{role}`x`\n",
		"::include{file=../secret.md}\n\n::include{file=/dev/zero}\n\n--8<-- \"part.md\"\n",
		"Term\n: Definition\n\n[^1]: note\n\ntext[^1] ^[inline]\n",
		"<details><summary>s</summary>\n\n*md*\n\n</details>\n<img src=\"x.png\" width=\"50%\">\n",
		// The shapes of security_test.go, small
		strings.Repeat(":::note\n", 200),
		strings.Repeat(":::note\n", 100) + "core\n" + strings.Repeat(":::\n", 100),
		indented(100),
		"[[_TOC_]]\n[TOC]\n\n% title\n% author\n\nKey: value\n\n# H {#id .class}\n",
	} {
		f.Add([]byte(seed))
	}
	files, _ := filepath.Glob(filepath.Join("..", "..", "fixtures", "flavors", "*.md"))
	for _, file := range files {
		if data, err := os.ReadFile(file); err == nil {
			f.Add(data)
		}
	}

	// Includes may only read inside this directory, which holds nothing.
	dir := f.TempDir()
	flavors := flavor.All()

	f.Fuzz(func(t *testing.T, src []byte) {
		for _, fl := range flavors {
			err := runBounded(fuzzTimeLimit, func() {
				doc, _ := ParseFlavor(src, fl, dir)
				if doc != nil {
					_ = dump(doc)
				}
			})
			if err != nil {
				t.Fatalf("flavor %s: %v\ninput: %q", fl.ID, err, src)
			}
		}
	})
}

// indented is a list that nests one level deeper on every line.
func indented(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(strings.Repeat(" ", 2*i) + "- item\n")
	}
	return b.String()
}

// runBounded runs fn on its own goroutine and reports a panic or a run that
// takes longer than limit as an error.
func runBounded(limit time.Duration, fn func()) error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- fmt.Errorf("panic: %v", p)
			}
		}()
		fn()
		done <- nil
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("still running after %v", limit)
	}
}
