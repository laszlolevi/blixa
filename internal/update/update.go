// Package update checks GitHub for a newer blixa release and replaces the running binary
// with it (the same download + checksum + rename scheme as install.sh).
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// maxDownload caps a release asset, so a broken server can't fill the disk.
const maxDownload = 200 << 20

type Updater struct {
	APIBase      string // GitHub API, e.g. https://api.github.com
	Repo         string // owner/name
	DownloadBase string // release assets are at <DownloadBase>/<tag>/<asset>
}

func Default() Updater {
	return Updater{
		APIBase:      "https://api.github.com",
		Repo:         "laszlolevi/blixa",
		DownloadBase: "https://github.com/laszlolevi/blixa/releases/download",
	}
}

// Latest: the tag of the latest release. GitHub's "latest" never is a pre-release or a draft.
func (u Updater) Latest(ctx context.Context) (string, error) {
	body, err := get(ctx, u.APIBase+"/repos/"+u.Repo+"/releases/latest")
	if err != nil {
		return "", err
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", errors.New("no tag_name in the latest release")
	}
	return rel.TagName, nil
}

var releaseTag = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// Newer: whether latest is a later vX.Y.Z than current; false if either isn't of that form.
func Newer(current, latest string) bool {
	c, l := releaseTag.FindStringSubmatch(current), releaseTag.FindStringSubmatch(latest)
	if c == nil || l == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		cn, _ := strconv.Atoi(c[i])
		ln, _ := strconv.Atoi(l[i])
		if ln != cn {
			return ln > cn
		}
	}
	return false
}

// Apply downloads the release's binary for this architecture, verifies its checksum and
// renames it over exe. On any error exe is left untouched.
func (u Updater) Apply(ctx context.Context, tag, exe string) error {
	asset := "blixa_darwin_" + runtime.GOARCH + ".tar.gz"
	base := u.DownloadBase + "/" + tag + "/"
	archive, err := get(ctx, base+asset)
	if err != nil {
		return err
	}
	sums, err := get(ctx, base+"checksums.txt")
	if err != nil {
		return err
	}
	if err := verify(archive, sums, asset); err != nil {
		return err
	}
	bin, err := extract(archive, "blixa")
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(exe), ".blixa.new")
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	// WriteFile keeps the mode of an existing file and is subject to the umask.
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
}

// verify checks data against the "<sha256>  <name>" line of checksums.txt.
func verify(data, sums []byte, name string) error {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[1] == name {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != f[0] {
				return fmt.Errorf("checksum mismatch for %s", name)
			}
			return nil
		}
	}
	return fmt.Errorf("no checksum for %s", name)
}

func extract(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in the release archive", name)
		}
		if err != nil {
			return nil, err
		}
		if h.Name == name && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, maxDownload))
		}
	}
}
