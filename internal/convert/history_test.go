package convert

import (
	"archive/zip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git in dir, failing the test if it does not succeed.
func gitIn(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitFile writes a file into a repository and commits it with the trailers.
func commitFile(t *testing.T, dir, name, text, date string, trailers ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, nil, "add", "-A")
	args := []string{"commit", "-q", "-m", "Edit " + name}
	for _, tr := range trailers {
		args = append(args, "--trailer", tr)
	}
	gitIn(t, dir, []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}, args...)
	return path
}

// newGitDir makes a repository, or skips the test where there is no git.
func newGitDir(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "none"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	gitIn(t, dir, nil, "init", "-q")
	gitIn(t, dir, nil, "config", "user.name", "Anna Kovács")
	gitIn(t, dir, nil, "config", "user.email", "anna@example.com")
	gitIn(t, dir, nil, "config", "commit.gpgsign", "false")
	return dir
}

func docxBody(t *testing.T, path string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			data, _ := io.ReadAll(rc)
			return string(data)
		}
	}
	t.Fatal("no document.xml")
	return ""
}

const historyDoc = "---\ntitle: Mentési eljárásrend\nlang: hu\nhistory: git\n---\n\n# Szabály\n\nbody text\n"

// A document that asks for it gets the history of the file in its repository
// after the control block, in both formats; one that does not, gets none.
func TestVersionHistoryFromGit(t *testing.T) {
	dir := newGitDir(t)
	commitFile(t, dir, "policy.md", historyDoc, "2026-01-10T09:00:00+01:00", "Changelog: Első változat.", "Version: 1.0")
	path := commitFile(t, dir, "policy.md", historyDoc+"\nmore\n", "2026-03-15T09:00:00+01:00", "Changelog: A mentések megőrzési ideje 90 nap.")

	var warnings []string
	opts := Options{Warn: func(w string) { warnings = append(warnings, w) }}
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := ConvertFileWith(path, out, FormatDOCX, opts); err != nil {
		t.Fatal(err)
	}
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
	body := docxBody(t, out)
	order := []string{">Mentési eljárásrend<", ">Változástörténet<", ">Verzió<", ">2026-03-15<", ">Első változat.<", ">Szabály<"}
	at := 0
	for _, want := range order {
		i := strings.Index(body[at:], want)
		if i < 0 {
			t.Fatalf("%s is missing, or comes before the others, in:\n%s", want, body)
		}
		at += i + len(want)
	}
	if strings.Index(body, ">2026-03-15<") > strings.Index(body, ">2026-01-10<") {
		t.Errorf("the older commit comes first")
	}

	pdf := filepath.Join(t.TempDir(), "out.pdf")
	if err := ConvertFileWith(path, pdf, FormatPDF, opts); err != nil {
		t.Fatal(err)
	}

	// Without the key there is no history
	plain := commitFile(t, dir, "plain.md", strings.Replace(historyDoc, "history: git\n", "", 1), "2026-04-01T09:00:00+01:00", "Changelog: Not shown.")
	out = filepath.Join(t.TempDir(), "plain.docx")
	if err := ConvertFileWith(plain, out, FormatDOCX, opts); err != nil {
		t.Fatal(err)
	}
	if body := docxBody(t, out); strings.Contains(body, "Változástörténet") {
		t.Errorf("a document that did not ask has a history")
	}
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
}

// Nothing that goes wrong with git fails the conversion: the document is
// converted without the table and the warning says why.
func TestVersionHistoryWarnings(t *testing.T) {
	dir := newGitDir(t)
	committed := commitFile(t, dir, "plain.md", historyDoc, "2026-01-10T09:00:00+01:00")
	asked := commitFile(t, dir, "bad.md", strings.Replace(historyDoc, "history: git", "history: svn", 1), "2026-01-11T09:00:00+01:00", "Changelog: Ignored.")
	untracked := filepath.Join(dir, "new.md") // after the last commit, which takes in every file
	if err := os.WriteFile(untracked, []byte(historyDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, path, want string }{
		{"no trailer", committed, "has a Changelog trailer"},
		{"not committed", untracked, "new.md is not committed yet"},
		{"another value", asked, "history in the front matter must be git, not svn; it was ignored."},
	} {
		var warnings []string
		out := filepath.Join(t.TempDir(), "out.docx")
		err := ConvertFileWith(tc.path, out, FormatDOCX, Options{Warn: func(w string) { warnings = append(warnings, w) }})
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], tc.want) || !strings.HasSuffix(warnings[0], ".") {
			t.Errorf("%s: warnings %q, want one with %q", tc.name, warnings, tc.want)
		}
		if strings.Contains(docxBody(t, out), "Változástörténet") {
			t.Errorf("%s: the document has a history", tc.name)
		}
	}

	t.Run("no git", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		var warnings []string
		out := filepath.Join(t.TempDir(), "out.pdf")
		if err := ConvertFileWith(committed, out, FormatPDF, Options{Warn: func(w string) { warnings = append(warnings, w) }}); err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "git was not found") {
			t.Errorf("warnings %q", warnings)
		}
	})
}
