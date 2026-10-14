// Package history reads the version history of a document from the git
// repository it is in. Nothing leaves the machine: git is run on the local
// repository and nothing else.
package history

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hkmt-sw/markout/internal/ast"
)

// timeout is how long git is given for everything it is asked.
const timeout = 10 * time.Second

// The fields of a commit in git's output are told apart by these: the unit
// separator between fields, the record separator between commits, and the
// group separator between the values of a trailer that is given twice.
const (
	fieldSep  = "\x1f"
	recordSep = "\x1e"
	valueSep  = "\x1d"
)

// logFormat is what git log is asked to print for each commit: its author
// date with the time zone it was made in, the author, and the values of its
// Changelog and Version trailers. A trailer that runs on to a second line is
// made one line (unfold).
const logFormat = "%H%x1f%aI%x1f%an" +
	"%x1f%(trailers:key=Changelog,valueonly,unfold,separator=%x1d)" +
	"%x1f%(trailers:key=Version,valueonly,unfold,separator=%x1d)%x1e"

// Asked says whether the front matter asks for a version history. The history
// key must be "git"; anything else is ignored, and the warning says so. The
// key is matched whatever its case and with _ for -, as the other keys are.
func Asked(fm ast.FrontMatter) (asked bool, warning string) {
	found := ""
	for key := range fm.Raw {
		if strings.ReplaceAll(strings.ToLower(key), "_", "-") == "history" && (found == "" || key < found) {
			found = key
		}
	}
	if found == "" {
		return false, ""
	}
	if s, ok := fm.Raw[found].(string); ok && strings.EqualFold(strings.TrimSpace(s), "git") {
		return true, ""
	}
	return false, fmt.Sprintf("history in the front matter must be git, not %v; it was ignored.", fm.Raw[found])
}

// Of returns the commits that touched the file and carry a Changelog trailer,
// newest first, following the file through renames. If there is no history to
// show, the entries are empty and the warning says why; a failure of git
// never goes further than a warning.
func Of(path string) (entries []ast.VersionEntry, warning string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fail(path, err.Error())
	}
	name := filepath.Base(abs)
	dir := filepath.Dir(abs)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}

	if _, err := exec.LookPath("git"); err != nil {
		return nil, "version history: git was not found, so the document has no version history table."
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, explain(name, err)
	}
	// The file must be inside the work tree git names
	top := strings.TrimSpace(out)
	if rel, err := filepath.Rel(top, filepath.Join(dir, name)); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, notRepository(name)
	}

	out, err = runGit(ctx, dir, "log", "--follow", "--no-color", "--no-show-signature", "--format="+logFormat, "--", name)
	if err != nil {
		return nil, explain(name, err)
	}
	commits := 0
	for _, record := range strings.Split(out, recordSep) {
		fields := strings.Split(strings.TrimSpace(record), fieldSep)
		if len(fields) != 5 {
			continue // what is left after the last record
		}
		commits++
		change := joined(fields[3])
		if change == "" {
			continue
		}
		date := fields[1]
		if t, err := time.Parse(time.RFC3339, date); err == nil {
			date = t.Format("2006-01-02") // the day where the commit was made
		}
		entries = append(entries, ast.VersionEntry{Version: joined(fields[4]), Date: date, Author: fields[2], Change: change})
	}
	if commits == 0 {
		return nil, fmt.Sprintf("version history: %s is not committed yet, so the document has no version history table.", name)
	}
	if len(entries) == 0 {
		return nil, fmt.Sprintf("version history: no commit that touches %s has a Changelog trailer, so the document has no version history table.", name)
	}
	return entries, ""
}

// joined makes one line of the values of a trailer, which git separates with
// valueSep: after a value that ends a sentence the next follows with a space,
// otherwise with "; ".
func joined(values string) string {
	var b strings.Builder
	for _, v := range strings.Split(values, valueSep) {
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		if b.Len() > 0 {
			if strings.ContainsAny(b.String()[b.Len()-1:], ".!?") {
				b.WriteString(" ")
			} else {
				b.WriteString("; ")
			}
		}
		b.WriteString(v)
	}
	return b.String()
}

// gitError is a failure of git, with the first line it said about it.
type gitError struct {
	message string
}

func (e *gitError) Error() string { return e.message }

// runGit runs git on the repository around dir and returns what it printed.
// Nothing user-controlled is passed as an option, git is told not to ask
// questions, wait for locks, or run anything the repository configures, and
// it speaks English, so that its messages can be told apart.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{
		"--no-pager", "-C", dir,
		"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Env = gitEnv(os.Environ())
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", &gitError{message: fmt.Sprintf("it took more than %d seconds", int(timeout/time.Second))}
		}
		message := err.Error()
		for _, line := range strings.Split(stderr.String(), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				message = line
				break
			}
		}
		return "", &gitError{message: message}
	}
	return stdout.String(), nil
}

// gitEnv is the environment git is run in: the one the user has, without the
// variables that point git at some other repository, and with the ones that
// keep it from prompting or taking locks.
func gitEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		switch strings.SplitN(kv, "=", 2)[0] {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_NAMESPACE",
			"GIT_TERMINAL_PROMPT", "GIT_OPTIONAL_LOCKS", "LC_ALL":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
}

// explain turns a failure of git into the warning for the user.
func explain(name string, err error) string {
	var ge *gitError
	if !errors.As(err, &ge) {
		return fail(name, err.Error())
	}
	switch {
	case strings.Contains(ge.message, "dubious ownership"):
		return fmt.Sprintf("version history: git does not trust the repository %s is in, because it belongs to another user (dubious ownership; see safe.directory in git-config), so the document has no version history table.", name)
	case strings.Contains(ge.message, "not a git repository"), strings.Contains(ge.message, "must be run in a work tree"):
		return notRepository(name)
	}
	return fail(name, ge.message)
}

func notRepository(name string) string {
	return fmt.Sprintf("version history: %s is not in a git work tree, so the document has no version history table.", name)
}

func fail(name, message string) string {
	return fmt.Sprintf("version history: git failed on %s (%s), so the document has no version history table.", name, strings.TrimRight(message, ". "))
}
