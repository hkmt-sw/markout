package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
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
	_ = os.RemoveAll(dir)
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
	var exit *exec.ExitError
	if errors.As(err, &exit) {
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

// An option markout does not know is reported as such instead of being taken
// for a file name, and the file names are either none or two.
func TestCLIRejectsUnknownOptionsAndWrongFileCounts(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "doc.md", "# Title\n")

	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--lan", "hu", "doc.md", "out.pdf"}, "unknown option --lan"},
		{[]string{"doc.md", "out.pdf", "--bogus"}, "unknown option --bogus"},
		{[]string{"--bogus=1", "doc.md", "out.pdf"}, "unknown option --bogus=1"},
		{[]string{"-x", "doc.md", "out.pdf"}, "unknown option -x"},
		{[]string{"-", "doc.md", "out.pdf"}, "unknown option -"},
		{[]string{"--pdfa=true", "doc.md", "out.pdf"}, "unknown option --pdfa=true"},
		{[]string{"doc.md"}, "but got 1 file name"},
		{[]string{"--flavor", "gitlab", "doc.md"}, "but got 1 file name"},
		{[]string{"doc.md", "out.pdf", "third.pdf"}, "but got 3 file name"},
	}
	for _, tt := range tests {
		r := markout(t, dir, tt.args...)
		if r.code != 1 || !strings.Contains(r.stderr, tt.want) || !strings.Contains(r.stderr, "markout --help") {
			t.Errorf("%v: exit %d, stderr %q; want exit 1 mentioning %q and the help", tt.args, r.code, r.stderr, tt.want)
		}
		if r.stdout != "" {
			t.Errorf("%v: an error should print nothing to stdout, got %q", tt.args, r.stdout)
		}
	}

	// Nothing was converted along the way.
	if _, err := os.Stat(filepath.Join(dir, "out.pdf")); err == nil {
		t.Error("a rejected command line still made out.pdf")
	}

	// Every option still works in both spellings.
	writeDoc(t, dir, "config.json", "{}")
	for _, args := range [][]string{
		{"--flavor=gitlab", "--lang=hu", "--theme=default", "--remote-images=deny", "--pdfa", "doc.md", "a.pdf"},
		{"-f", "gitlab", "--lang", "hu", "-t", "default", "--remote-images", "deny", "doc.md", "b.pdf"},
	} {
		if r := markout(t, dir, args...); r.code != 0 {
			t.Errorf("%v: %+v", args, r)
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

func TestCLIThemes(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "doc.md", "# Title\n\ntext\n")
	// The user's themes live next to the settings file.
	if err := os.Mkdir(filepath.Join(dir, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, filepath.Join(dir, "themes"), "crimson.toml", "description = \"Red headings\"\n\n[heading]\ncolor = \"#AA0000\"\n")

	r := markout(t, dir, "--list-themes")
	if r.code != 0 || !strings.Contains(r.stdout, "* default") || !strings.Contains(r.stdout, "crimson") ||
		!strings.Contains(r.stdout, "Red headings") || !strings.Contains(r.stdout, filepath.Join(dir, "themes")) {
		t.Errorf("--list-themes: %+v", r)
	}

	// An exported theme is a complete theme file that loads again.
	r = markout(t, dir, "--export-theme")
	if r.code != 0 || !strings.Contains(r.stdout, "[heading.h1]") || !strings.Contains(r.stdout, "[docx.fonts]") {
		t.Fatalf("--export-theme: exit %d, %d bytes", r.code, len(r.stdout))
	}
	writeDoc(t, dir, "exported.toml", r.stdout)
	if r := markout(t, dir, "--theme", "exported.toml", "doc.md", "exported.pdf"); r.code != 0 {
		t.Errorf("converting with the exported theme: %+v", r)
	}

	// By name from the themes directory, and by file path.
	writeDoc(t, dir, "local.toml", "[heading]\ncolor = \"#00AA00\"\n")
	for args, color := range map[string]string{"--theme crimson": "AA0000", "-t local.toml": "00AA00", "--theme=./local.toml": "00AA00"} {
		out := color + strings.ReplaceAll(args, " ", "") + ".docx"
		out = strings.NewReplacer("/", "", "=", "").Replace(out)
		r := markout(t, dir, append(strings.Fields(args), "doc.md", out)...)
		if r.code != 0 {
			t.Fatalf("%s: %+v", args, r)
		}
		if body := docxDocument(t, filepath.Join(dir, out)); !strings.Contains(body, color) {
			t.Errorf("%s: the theme's heading color %s is not in the output", args, color)
		}
	}

	// The saved theme is the default for conversions.
	writeDoc(t, dir, "config.json", `{"flavor": "markout", "theme": "crimson"}`)
	if r := markout(t, dir, "doc.md", "saved.docx"); r.code != 0 || !strings.Contains(docxDocument(t, filepath.Join(dir, "saved.docx")), "AA0000") {
		t.Errorf("the saved theme was not used: %+v", r)
	}
	if r := markout(t, dir, "--list-themes"); !strings.Contains(r.stdout, "* crimson") {
		t.Errorf("the saved theme is not marked as current:\n%s", r.stdout)
	}

	// Problems are reported, with the setting that is wrong.
	writeDoc(t, dir, "bad.toml", "[text]\nsizee = 12\n")
	failures := []struct {
		args []string
		want string
	}{
		{[]string{"--theme", "nope", "doc.md", "x.pdf"}, `no theme named "nope"`},
		{[]string{"--theme", "bad.toml", "doc.md", "x.pdf"}, "text.sizee is not a setting"},
		{[]string{"--export-theme", "nope"}, "no theme named"},
		{[]string{"--theme"}, "needs a theme name"},
	}
	for _, f := range failures {
		r := markout(t, dir, f.args...)
		if r.code != 1 || !strings.Contains(r.stderr, f.want) {
			t.Errorf("%v: exit %d, stderr %q; want exit 1 mentioning %q", f.args, r.code, r.stderr, f.want)
		}
	}
}

// docxDocument returns the main document XML of a DOCX file, followed by its styles:
// how a heading looks is in its style.
func docxDocument(t *testing.T, path string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	parts := map[string]string{}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" && f.Name != "word/styles.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[f.Name] = string(data)
	}
	if parts["word/document.xml"] == "" {
		t.Fatal("word/document.xml not found")
	}
	return parts["word/document.xml"] + parts["word/styles.xml"]
}

// The language of a document that does not name one comes from --lang, else
// from the saved settings; what the document says goes before both.
func TestCLILanguage(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "plain.md", "# Title\n\ntext\n")
	writeDoc(t, dir, "magyar.md", "---\nlang: hu\n---\n\n# Cím\n\nszöveg\n")

	language := func(args ...string) string {
		t.Helper()
		r := markout(t, dir, append(args, "out.pdf")...)
		if r.code != 0 {
			t.Fatalf("%v: %+v", args, r)
		}
		data, err := os.ReadFile(filepath.Join(dir, "out.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		_, rest, found := bytes.Cut(data, []byte("/Lang ("))
		if !found {
			return ""
		}
		tag, _, _ := bytes.Cut(rest, []byte(")"))
		return string(tag)
	}
	if got := language("plain.md"); got != "" {
		t.Errorf("no language anywhere: the PDF has %q", got)
	}
	if got := language("--lang", "en_gb", "plain.md"); got != "en-GB" {
		t.Errorf("--lang en_gb: the PDF has %q", got)
	}
	if got := language("plain.md", "--lang=hu"); got != "hu" {
		t.Errorf("--lang=hu: the PDF has %q", got)
	}
	if got := language("--lang", "en-GB", "magyar.md"); got != "hu" {
		t.Errorf("a Hungarian document with --lang en-GB: the PDF has %q", got)
	}

	writeDoc(t, dir, "config.json", `{"flavor": "markout", "lang": "en-US"}`)
	if got := language("plain.md"); got != "en-US" {
		t.Errorf("saved language: the PDF has %q", got)
	}
	if got := language("--lang", "hu", "plain.md"); got != "hu" {
		t.Errorf("--lang over the saved language: the PDF has %q", got)
	}

	if r := markout(t, dir, "--lang", "magyar", "plain.md", "out.pdf"); r.code == 0 || !strings.Contains(r.stderr, "language tag") {
		t.Errorf("--lang magyar: %+v", r)
	}
	if r := markout(t, dir, "plain.md", "out.pdf", "--lang"); r.code == 0 || !strings.Contains(r.stderr, "--lang needs") {
		t.Errorf("--lang with nothing after it: %+v", r)
	}
}

func TestCLIPDFA(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "doc.md", "---\ntitle: T\n---\n\n# Title\n\ntext\n")
	writeDoc(t, dir, "refuses.md", "---\npdfa: false\n---\n\n# Title\n\ntext\n")

	isPDFA := func(args ...string) (bool, result) {
		t.Helper()
		r := markout(t, dir, args...)
		if r.code != 0 {
			t.Fatalf("%v: %+v", args, r)
		}
		data, err := os.ReadFile(filepath.Join(dir, "out.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		return bytes.Contains(data, []byte("<pdfaid:conformance>U</pdfaid:conformance>")), r
	}

	if got, r := isPDFA("doc.md", "out.pdf"); got || r.stderr != "" {
		t.Errorf("without --pdfa: PDF/A %v, %+v", got, r)
	}
	for _, args := range [][]string{
		{"--pdfa", "doc.md", "out.pdf"},
		{"doc.md", "out.pdf", "--pdfa"},
	} {
		if got, r := isPDFA(args...); !got || r.stderr != "" {
			t.Errorf("%v: PDF/A %v, %+v", args, got, r)
		}
	}
	if got, _ := isPDFA("--pdfa", "refuses.md", "out.pdf"); got {
		t.Error("a document with pdfa: false was made PDF/A")
	}

	// The saved setting does the same as the flag
	writeDoc(t, dir, "config.json", `{"flavor": "markout", "pdfa": true}`)
	if got, _ := isPDFA("doc.md", "out.pdf"); !got {
		t.Error("the saved setting was not used")
	}

	// A DOCX is not PDF/A: the flag says so, the setting does not
	r := markout(t, dir, "--pdfa", "doc.md", "out.docx")
	if r.code != 0 || !strings.Contains(r.stderr, "Warning: --pdfa applies to PDF only") {
		t.Errorf("--pdfa with DOCX: %+v", r)
	}
	if r := markout(t, dir, "doc.md", "out.docx"); r.code != 0 || r.stderr != "" {
		t.Errorf("the saved setting with DOCX: %+v", r)
	}
	if r := markout(t, dir, "--help"); !strings.Contains(r.stdout, "--pdfa") {
		t.Error("--help does not name --pdfa")
	}
}
