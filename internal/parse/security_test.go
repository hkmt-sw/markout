package parse

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hkmt-sw/markout/internal/flavor"
)

// An include may only read regular files inside the document's directory.
func TestIncludeStaysInsideDocumentDir(t *testing.T) {
	base := t.TempDir()
	docs := filepath.Join(base, "docs")
	if err := os.MkdirAll(filepath.Join(docs, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(base, "secret.md")
	write(secret, "TOPSECRET\n")
	write(filepath.Join(docs, "part.md"), "PART-OK\n\n::include{file=sub/nested.md}\n")
	write(filepath.Join(docs, "sub", "nested.md"), "NESTED-OK\n\n::include{file=../sibling.md}\n\n::include{file=../../secret.md}\n")
	write(filepath.Join(docs, "sibling.md"), "SIBLING-OK\n")

	src := "::include{file=part.md}\n\n" +
		"::include{file=../secret.md}\n\n" +
		"::include{file=" + secret + "}\n\n" +
		"::include{file=sub}\n\n"
	if runtime.GOOS != "windows" {
		if err := os.Symlink(secret, filepath.Join(docs, "link.md")); err != nil {
			t.Fatal(err)
		}
		src += "::include{file=link.md}\n\n::include{file=/dev/zero}\n"
	}

	fl, _ := flavor.ByID("gitlab")
	doc, err := ParseFlavor([]byte(src), fl, docs)
	if err != nil {
		t.Fatal(err)
	}
	got := dump(doc)
	for _, want := range []string{"PART-OK", "NESTED-OK", "SIBLING-OK"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "TOPSECRET") {
		t.Errorf("an include escaped the document directory:\n%s", got)
	}
}

// within fails the test if fn takes longer than limit: these inputs used to
// take minutes.
func within(t *testing.T, limit time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("still running after %v", limit)
	}
}

func TestUnclosedColonFencesAreLinearAndLiteral(t *testing.T) {
	src := []byte(strings.Repeat(":::note\n", 60000))
	within(t, 20*time.Second, func() {
		if _, err := ParseFlavor(src, flavor.Default(), ""); err != nil {
			t.Error(err)
		}
	})

	got := parseAs(t, "docusaurus", ":::note\nnever closed\n")
	if strings.Contains(got, "ALERT") || !strings.Contains(got, ":::note") {
		t.Errorf("an unclosed fence should stay text, got:\n%s", got)
	}
}

func TestDeeplyNestedContainersAreBounded(t *testing.T) {
	const depth = 5000
	src := []byte(strings.Repeat(":::note\n", depth) + "core\n" + strings.Repeat(":::\n", depth))
	within(t, 20*time.Second, func() {
		doc, err := ParseFlavor(src, flavor.Default(), "")
		if err != nil {
			t.Error(err)
		} else if !strings.Contains(dump(doc), "core") {
			t.Error("content inside the nested containers was lost")
		}
	})
}

func TestExtremeIndentationIsCapped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		b.WriteString(strings.Repeat(" ", 2*i) + "- item\n")
	}
	within(t, 20*time.Second, func() {
		doc, err := ParseFlavor([]byte(b.String()), flavor.Default(), "")
		if err != nil {
			t.Error(err)
		} else if n := strings.Count(dump(doc), "item"); n != 3000 {
			t.Errorf("expected all 3000 items to survive, got %d", n)
		}
	})
}
