package supervisor

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"blixa/internal/task"
)

// LogPath: <stateDir>/logs/<project>-<path hash>/<dir:script or @builtin>.log, with '/' encoded.
func LogPath(stateDir string, s task.Spec) string {
	name := "@builtin"
	if s.Kind == task.KindScript {
		name = s.Dir + ":" + s.Script
	}
	name = strings.ReplaceAll(name, "/", "%2F")
	// the folder is named after the project, plus a hash of its path: same-named projects
	// under different roots get their own logs
	sum := sha1.Sum([]byte(s.ProjectPath))
	dir := s.Project + "-" + hex.EncodeToString(sum[:4])
	return filepath.Join(stateDir, "logs", dir, name+".log")
}

// ExistingLogPath: LogPath, or the log's place before the path hash was added to the
// folder name, if only that exists -- a task started by an older blixa still writes there.
func ExistingLogPath(stateDir string, s task.Spec) string {
	p := LogPath(stateDir, s)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	legacy := filepath.Join(stateDir, "logs", s.Project, filepath.Base(p))
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return p
}

// OpenLog: rotates the existing log to .log.1, and opens a new, empty file in append mode.
// Because of append mode, writes after a TruncateHead land at the end of the file.
func OpenLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, 0o644)
}

// TruncateHead: if the file is larger than maxSize, keeps only the last ~maxSize/2 bytes,
// aligned to a line boundary. A deliberate trade-off: output arriving in the few ms between
// the read and the write-back can be lost. The task writes directly to the file (not
// through the daemon), so it survives a daemon restart too, which rules out an atomic truncation.
func TruncateHead(path string, maxSize int64) error {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || fi.Size() <= maxSize {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) <= maxSize {
		return err
	}
	keep := data[len(data)-int(maxSize/2):]
	if i := bytes.IndexByte(keep, '\n'); i >= 0 {
		keep = keep[i+1:]
	}
	return os.WriteFile(path, keep, 0o644)
}
