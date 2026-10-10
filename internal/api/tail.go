package api

import (
	"io"
	"os"
	"strings"
	"time"
)

const restartMarker = "── the log restarted ──"
const trimMarker = "── the start of the log was trimmed ──"

// tailFile: sends the last `backlog` lines, then follows the file every 200 ms.
// If the file was replaced (rotation), it reads from the start. If the same file just
// got shorter (truncation), it doesn't re-read from the start but continues from the
// current end, so it doesn't duplicate lines already sent.
func tailFile(path string, backlog int, emit func(string) error, stop <-chan struct{}) error {
	var (
		prev    os.FileInfo
		offset  int64
		partial string
		first   = true
	)
	for {
		if f, err := os.Open(path); err == nil {
			if fi, err := f.Stat(); err == nil {
				if prev != nil {
					switch {
					case !os.SameFile(prev, fi):
						// the file was replaced (rotation): read from the start, signal the break.
						offset, partial, first = 0, "", true
						if err := emit(restartMarker); err != nil {
							f.Close()
							return err
						}
					case fi.Size() < offset:
						// the same file just got shorter (e.g. TruncateHead) -- this is not a rotation.
						// We don't re-read from the start (that would duplicate lines already sent),
						// but continue from the current end, with a single marker line.
						offset, partial = fi.Size(), ""
						if err := emit(trimMarker); err != nil {
							f.Close()
							return err
						}
					}
				}
				prev = fi
				if fi.Size() > offset {
					if _, err := f.Seek(offset, io.SeekStart); err == nil {
						data, _ := io.ReadAll(f)
						offset += int64(len(data))
						lines := strings.Split(partial+string(data), "\n")
						partial = lines[len(lines)-1]
						lines = lines[:len(lines)-1]
						if first && len(lines) > backlog {
							lines = lines[len(lines)-backlog:]
						}
						for _, l := range lines {
							if err := emit(l); err != nil {
								f.Close()
								return err
							}
						}
					}
				}
				first = false
			}
			f.Close()
		}
		select {
		case <-stop:
			return nil
		case <-time.After(200 * time.Millisecond):
		}
	}
}
