package history

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
)

// repo is a git repository in a temporary directory.
type repo struct {
	t   *testing.T
	dir string
}

// newRepo makes an empty repository, or skips the test where there is no git.
func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	// Whatever the machine's git is set up to do stays out of the test
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "none"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &repo{t: t, dir: dir}
	r.git(nil, "init", "-q")
	r.git(nil, "config", "user.name", "Default Author")
	r.git(nil, "config", "user.email", "default@example.com")
	r.git(nil, "config", "commit.gpgsign", "false")
	return r
}

func (r *repo) git(env []string, args ...string) {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r *repo) write(name, text string) string {
	r.t.Helper()
	path := filepath.Join(r.dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
	return path
}

// commit commits everything, as the author on the day (a git date with its
// time zone), with the trailers in the last paragraph of the message.
func (r *repo) commit(author, date, subject string, trailers ...string) {
	r.t.Helper()
	r.git(nil, "add", "-A")
	args := []string{"commit", "-q", "--author", author + " <" + strings.ReplaceAll(strings.ToLower(author), " ", ".") + "@example.com>", "-m", subject}
	for _, tr := range trailers {
		args = append(args, "--trailer", tr)
	}
	r.git([]string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}, args...)
}

func TestOf(t *testing.T) {
	r := newRepo(t)
	r.write("policy.md", "one\n")
	r.commit("Anna Kovács", "2026-01-10T09:00:00+01:00", "First draft", "Changelog: Első változat.", "Version: 1.0")
	r.write("policy.md", "two\n")
	r.commit("Béla Nagy", "2026-02-20T23:30:00-05:00", "Typo")
	r.write("other.md", "other\n")
	r.commit("Cecil Szabó", "2026-03-01T10:00:00+01:00", "Another file", "Changelog: Not about the policy.")
	r.write("policy.md", "three\n")
	r.commit("Béla Nagy", "2026-03-15T08:00:00+01:00", "Retention", "Changelog: Retention is 90 days", "Changelog: Logs are kept", "Version: 1.1")

	// The file is renamed, and changes after that
	r.git(nil, "mv", "policy.md", "procedure.md")
	r.commit("Anna Kovács", "2026-04-01T12:00:00+02:00", "Rename the file", "Changelog: Now a procedure.")
	procedure := filepath.Join(r.dir, "procedure.md")

	got, warning := Of(procedure)
	if warning != "" {
		t.Fatalf("warning: %s", warning)
	}
	// Of two trailers in one commit, the first comes first and "; " joins them
	want := []ast.VersionEntry{
		{Version: "", Date: "2026-04-01", Author: "Anna Kovács", Change: "Now a procedure."},
		{Version: "1.1", Date: "2026-03-15", Author: "Béla Nagy", Change: "Retention is 90 days; Logs are kept"},
		{Version: "1.0", Date: "2026-01-10", Author: "Anna Kovács", Change: "Első változat."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries:\n got %+v\nwant %+v", got, want)
	}
}

func TestOfDayIsTheCommitsOwn(t *testing.T) {
	r := newRepo(t)
	path := r.write("a.md", "x\n")
	// Half past eleven in the evening in New York is the next day in UTC
	r.commit("Anna", "2026-02-20T23:30:00-05:00", "Late", "Changelog: Late one.")
	got, warning := Of(path)
	if warning != "" || len(got) != 1 || got[0].Date != "2026-02-20" {
		t.Errorf("got %+v, %q", got, warning)
	}
}

func TestOfVersionColumn(t *testing.T) {
	r := newRepo(t)
	path := r.write("a.md", "1\n")
	r.commit("Anna", "2026-01-01T10:00:00+01:00", "One", "Changelog: One.")
	r.write("a.md", "2\n")
	r.commit("Anna", "2026-01-02T10:00:00+01:00", "Two", "Changelog: Two.")
	got, _ := Of(path)
	if (ast.VersionHistory{Entries: got}).HasVersions() {
		t.Errorf("no commit names a version, but the history has the column: %+v", got)
	}

	r.write("a.md", "3\n")
	r.commit("Anna", "2026-01-03T10:00:00+01:00", "Three", "Changelog: Three.", "Version: 2.0")
	got, _ = Of(path)
	if !(ast.VersionHistory{Entries: got}).HasVersions() {
		t.Errorf("a commit names a version, but the history has no column: %+v", got)
	}
	if got[0].Version != "2.0" || got[1].Version != "" || got[2].Version != "" {
		t.Errorf("versions: %+v", got)
	}
}

func TestOfWarns(t *testing.T) {
	t.Run("outside a repository", func(t *testing.T) {
		newRepo(t) // for the git check and the clean configuration
		dir := t.TempDir()
		path := filepath.Join(dir, "a.md")
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// A temporary directory is outside any repository unless the machine
		// has one around /tmp
		if out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output(); err == nil {
			t.Skipf("the temporary directory is inside a repository: %s", out)
		}
		got, warning := Of(path)
		if got != nil || !strings.Contains(warning, "a.md is not in a git work tree") {
			t.Errorf("got %+v, %q", got, warning)
		}
	})

	t.Run("never committed", func(t *testing.T) {
		r := newRepo(t)
		r.write("a.md", "x\n")
		r.commit("Anna", "2026-01-01T10:00:00+01:00", "Another file", "Changelog: Not this one.")
		path := r.write("b.md", "y\n")
		got, warning := Of(path)
		if got != nil || !strings.Contains(warning, "b.md is not committed yet") {
			t.Errorf("got %+v, %q", got, warning)
		}
	})

	t.Run("no trailer", func(t *testing.T) {
		r := newRepo(t)
		path := r.write("a.md", "x\n")
		r.commit("Anna", "2026-01-01T10:00:00+01:00", "Nothing to say")
		got, warning := Of(path)
		if got != nil || !strings.Contains(warning, "no commit that touches a.md has a Changelog trailer") {
			t.Errorf("got %+v, %q", got, warning)
		}
	})

	t.Run("no git", func(t *testing.T) {
		r := newRepo(t)
		path := r.write("a.md", "x\n")
		r.commit("Anna", "2026-01-01T10:00:00+01:00", "One", "Changelog: One.")
		t.Setenv("PATH", t.TempDir())
		got, warning := Of(path)
		if got != nil || !strings.Contains(warning, "git was not found") {
			t.Errorf("got %+v, %q", got, warning)
		}
	})

	t.Run("a failure of git", func(t *testing.T) {
		r := newRepo(t)
		path := r.write("a.md", "x\n")
		r.commit("Anna", "2026-01-01T10:00:00+01:00", "One", "Changelog: One.")
		// A repository git cannot read
		if err := os.WriteFile(filepath.Join(r.dir, ".git", "HEAD"), []byte("garbage\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, warning := Of(path)
		if got != nil || !strings.HasPrefix(warning, "version history: ") || strings.Contains(warning, "\n") {
			t.Errorf("got %+v, %q", got, warning)
		}
	})
}

func TestExplain(t *testing.T) {
	for _, tc := range []struct{ name, message, want string }{
		{"ownership", "fatal: detected dubious ownership in repository at '/x'", "dubious ownership"},
		{"no repository", "fatal: not a git repository (or any of the parent directories): .git", "is not in a git work tree"},
		{"bare", "fatal: this operation must be run in a work tree", "is not in a git work tree"},
		{"other", "fatal: bad object HEAD.", "git failed on a.md (fatal: bad object HEAD)"},
	} {
		got := explain("a.md", &gitError{message: tc.message})
		if !strings.Contains(got, tc.want) || !strings.HasSuffix(got, ".") {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
}

func TestAsked(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     map[string]any
		asked   bool
		warning string
	}{
		{"nothing said", map[string]any{"title": "T"}, false, ""},
		{"git", map[string]any{"history": "git"}, true, ""},
		{"key in other case", map[string]any{"History": "git"}, true, ""},
		{"value in other case", map[string]any{"history": " Git "}, true, ""},
		{"another word", map[string]any{"history": "svn"}, false, "history in the front matter must be git, not svn; it was ignored."},
		{"a flag", map[string]any{"history": true}, false, "history in the front matter must be git, not true; it was ignored."},
	} {
		asked, warning := Asked(ast.FrontMatter{Raw: tc.raw})
		if asked != tc.asked || warning != tc.warning {
			t.Errorf("%s: got %v, %q; want %v, %q", tc.name, asked, warning, tc.asked, tc.warning)
		}
	}
}

// The values of a trailer given twice follow each other with a space after a
// sentence, and with "; " otherwise.
func TestJoined(t *testing.T) {
	for in, want := range map[string]string{
		"Retention is 90 days\x1dLogs are kept":   "Retention is 90 days; Logs are kept",
		"Retention is 90 days.\x1dLogs are kept.": "Retention is 90 days. Logs are kept.",
		" one \x1d\x1d two ":                      "one; two",
		"":                                        "",
	} {
		if got := joined(in); got != want {
			t.Errorf("joined(%q) = %q, want %q", in, got, want)
		}
	}
}
