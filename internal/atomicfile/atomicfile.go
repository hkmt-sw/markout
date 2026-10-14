// Package atomicfile writes files so that a failure never leaves a half
// written file behind: the old content stays until the new content is
// complete.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
)

// maxTries is how many random names are tried for the temporary file before
// giving up. Collisions are practically impossible, so reaching it means
// something else is wrong with the directory.
const maxTries = 100

// WriteFile writes data to the file at path. See Write.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	return Write(path, perm, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// Write calls fill to write the content of the file at path. The content goes
// to a temporary file in the same directory, which replaces the file at path
// only after fill, the flush to disk and the close all succeeded. On any
// failure the file at path is left as it was and the temporary file is
// removed.
//
// A new file gets the permissions perm, less the process's umask, as with
// os.OpenFile. A file that is replaced gets perm as well, not its old
// permissions. If path is a symbolic link, the file it points to is replaced
// and the link stays.
func Write(path string, perm fs.FileMode, fill func(w io.Writer) error) error {
	// The temporary file must be on the same file system as the target for the
	// rename to be atomic, so it cannot live in os.TempDir. The error is
	// ignored on purpose: a path that does not exist yet has nothing to
	// resolve, and a real problem shows up when the file is created.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	// The file is created with os.OpenFile instead of os.CreateTemp, which
	// always asks for 0o600: this way the system applies the umask, and there
	// is no need to read it, which cannot be done in a portable way.
	f, name, err := create(filepath.Dir(path), filepath.Base(path), perm)
	if err != nil {
		return err
	}
	// Tells the deferred cleanup whether the file is in place already
	done := false
	defer func() {
		if !done {
			f.Close() // the error that matters is being returned already
			os.Remove(name)
		}
	}()

	if err := fill(f); err != nil {
		return err
	}
	// Without the sync, a crash after the rename could leave the new name on
	// an empty file
	if err := f.Sync(); err != nil {
		return fmt.Errorf("flushing %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	done = true
	return nil
}

// create opens a new temporary file in dir, named after the target.
func create(dir, base string, perm fs.FileMode) (*os.File, string, error) {
	for range maxTries {
		name := filepath.Join(dir, "."+base+"."+strconv.FormatUint(rand.Uint64(), 36)+".tmp")
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, name, err
	}
	return nil, "", fmt.Errorf("no free temporary file name in %s", dir)
}
