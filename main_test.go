package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests run the real binary, so they cover what the unit tests cannot:
// argument handling, exit codes, and what is printed where.

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "markout-cli")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "markout")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-ldflags", "-X main.version=v0.0.0-test", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building the binary: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	stdout, stderr string
	code           int
}

// markout runs the binary in dir with no terminal on stdin and its own,
// empty settings file.
func markout(t *testing.T, dir string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "MARKOUT_CONFIG="+filepath.Join(dir, "config.json"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running markout %v: %v", args, err)
	}
	return result{stdout.String(), stderr.String(), code}
}

func writeDoc(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCLIInformationFlags(t *testing.T) {
	dir := t.TempDir()

	if r := markout(t, dir, "--version"); r.code != 0 || strings.TrimSpace(r.stdout) != "markout v0.0.0-test" {
		t.Errorf("--version: %+v", r)
	}
	if r := markout(t, dir, "--help"); r.code != 0 || !strings.Contains(r.stdout, "--flavor") || !strings.Contains(r.stdout, "--remote-images") {
		t.Errorf("--help: %+v", r)
	}

	r := markout(t, dir, "--list-flavors")
	if r.code != 0 {
		t.Fatalf("--list-flavors: %+v", r)
	}
	for _, id := range []string{"markout", "commonmark", "github", "gitlab", "youtrack", "obsidian", "docusaurus"} {
		if !strings.Contains(r.stdout, id) {
			t.Errorf("--list-flavors does not list %q", id)
		}
	}
	if !strings.Contains(r.stdout, "* markout") {
		t.Errorf("the default flavor is not marked as current:\n%s", r.stdout)
	}
}

func TestCLIConverts(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "doc.md", "# Title\n\nSome **bold** text.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")

	for out, magic := range map[string]string{"doc.pdf": "%PDF", "doc.docx": "PK"} {
		r := markout(t, dir, "doc.md", out)
		if r.code != 0 || !strings.Contains(r.stdout, "Saved to: "+out) {
			t.Fatalf("%s: %+v", out, r)
		}
		data, err := os.ReadFile(filepath.Join(dir, out))
		if err != nil || !bytes.HasPrefix(data, []byte(magic)) {
			t.Errorf("%s is not a valid file (%v)", out, err)
		}
		if r.stderr != "" {
			t.Errorf("%s: unexpected output on stderr: %s", out, r.stderr)
		}
	}

	// Flags may come before or after the file names, in either spelling.
	for _, args := range [][]string{
		{"--flavor", "gitlab", "doc.md", "a.pdf"},
		{"doc.md", "b.pdf", "--flavor=gfm"},
		{"-f", "obsidian", "doc.md", "c.pdf"},
	} {
		if r := markout(t, dir, args...); r.code != 0 {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

func TestCLIErrors(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "doc.md", "# Title\n")

	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--flavor", "nope", "doc.md", "out.pdf"}, `unknown flavor "nope"`},
		{[]string{"--flavor"}, "needs a flavor name"},
		{[]string{"missing.md", "out.pdf"}, "input file not found"},
		{[]string{"doc.md", "out.txt"}, "could not determine output format"},
		{[]string{"--remote-images", "maybe", "doc.md", "out.pdf"}, "must be ask, allow or deny"},
		{[]string{"--remote-images=ask", "doc.md", "out.pdf"}, "needs a terminal"},
	}
	for _, tt := range tests {
		r := markout(t, dir, tt.args...)
		if r.code != 1 || !strings.Contains(r.stderr, tt.want) {
			t.Errorf("%v: exit %d, stderr %q; want exit 1 mentioning %q", tt.args, r.code, r.stderr, tt.want)
		}
		if r.stdout != "" {
			t.Errorf("%v: an error should print nothing to stdout, got %q", tt.args, r.stdout)
		}
	}
}

// Without a terminal nobody can be asked, so remote images are skipped and
// named on stderr; the conversion itself still succeeds.
func TestCLISkipsRemoteImagesWhenItCannotAsk(t *testing.T) {
	dir := t.TempDir()
	// Port 9 (discard) on loopback: if this were fetched it would fail fast,
	// but it must not be attempted at all.
	writeDoc(t, dir, "doc.md", "# Doc\n\n![a](http://127.0.0.1:9/a.png)\n\n![b](http://127.0.0.1:9/b.png)\n\n![c](https://img.example.com/c.png)\n")

	for _, args := range [][]string{{"doc.md", "out.pdf"}, {"--remote-images", "deny", "doc.md", "out.docx"}} {
		r := markout(t, dir, args...)
		if r.code != 0 {
			t.Fatalf("%v: %+v", args, r)
		}
		for _, want := range []string{"127.0.0.1:9", "2 images", "local network address", "img.example.com", "1 image", "Skipped"} {
			if !strings.Contains(r.stderr, want) {
				t.Errorf("%v: stderr does not mention %q:\n%s", args, want, r.stderr)
			}
		}
		if strings.Contains(r.stderr, "[y/N]") {
			t.Errorf("%v: asked a question although there is no terminal", args)
		}
	}

	// A document without remote images converts silently.
	writeDoc(t, dir, "plain.md", "# Plain\n\n![local](pic.png)\n")
	if r := markout(t, dir, "plain.md", "plain.pdf"); r.code != 0 || r.stderr != "" {
		t.Errorf("plain document: %+v", r)
	}
}

// The flavor saved in the settings file is the default for conversions, and
// --flavor overrides it.
func TestCLIUsesSavedFlavor(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "config.json", `{"flavor": "gitlab"}`)

	r := markout(t, dir, "--list-flavors")
	if !strings.Contains(r.stdout, "* gitlab") || strings.Contains(r.stdout, "* markout") {
		t.Errorf("saved flavor is not the current one:\n%s", r.stdout)
	}
}
