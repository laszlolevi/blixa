package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.2.0", "v0.3.0", true},
		{"v0.2.0", "v0.2.1", true},
		{"v0.9.0", "v0.10.0", true},
		{"v0.2.0", "v1.0.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.3.0", "v0.2.9", false},
		{"v1.0.0", "v0.99.0", false},
		{"dev 78b8003", "v0.3.0", false},
		{"v0.2.0+dirty", "v0.3.0", false},
		{"v0.2.0", "v0.3.0-rc.1", false},
		{"v0.2.0", "", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/laszlolevi/blixa/releases/latest":
			w.Write([]byte(`{"tag_name": "v0.3.0", "prerelease": false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u := Updater{APIBase: srv.URL, Repo: "laszlolevi/blixa"}
	tag, err := u.Latest(context.Background())
	if err != nil || tag != "v0.3.0" {
		t.Fatalf("tag=%q err=%v", tag, err)
	}
	u.Repo = "nope/nope"
	if _, err := u.Latest(context.Background()); err == nil {
		t.Fatal("expected an error for a 404")
	}
}

func TestLatestRejectsMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name": "no tag here"}`))
	}))
	defer srv.Close()
	u := Updater{APIBase: srv.URL, Repo: "laszlolevi/blixa"}
	if _, err := u.Latest(context.Background()); err == nil {
		t.Fatal("expected an error for a response without tag_name")
	}
}

// releaseServer serves a release asset of the given binary content, with the checksum of
// checksumOf in checksums.txt.
func releaseServer(t *testing.T, content, checksumOf []byte) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "blixa", Mode: 0o755, Size: int64(len(content))})
	tw.Write(content)
	tw.Close()
	gz.Close()
	asset := buf.Bytes()

	var sumSrc []byte
	if checksumOf == nil {
		sumSrc = asset
	} else {
		sumSrc = checksumOf
	}
	sum := sha256.Sum256(sumSrc)
	name := "blixa_darwin_" + runtime.GOARCH + ".tar.gz"
	checksums := hex.EncodeToString(sum[:]) + "  " + name + "\n" +
		"0000000000000000000000000000000000000000000000000000000000000000  other.tar.gz\n"

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.3.0/" + name:
			w.Write(asset)
		case "/v0.3.0/checksums.txt":
			w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestApplyReplacesBinary(t *testing.T) {
	srv := releaseServer(t, []byte("new binary"), nil)
	defer srv.Close()
	exe := filepath.Join(t.TempDir(), "blixa")
	os.WriteFile(exe, []byte("old binary"), 0o755)

	u := Updater{DownloadBase: srv.URL}
	if err := u.Apply(context.Background(), "v0.3.0", exe); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	fi, _ := os.Stat(exe)
	if string(got) != "new binary" || fi.Mode().Perm() != 0o755 {
		t.Fatalf("content=%q mode=%v", got, fi.Mode())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), ".blixa.new")); err == nil {
		t.Fatal("the temp file was left behind")
	}
}

func TestApplyChecksumMismatchKeepsOldBinary(t *testing.T) {
	srv := releaseServer(t, []byte("new binary"), []byte("something else"))
	defer srv.Close()
	dir := t.TempDir()
	exe := filepath.Join(dir, "blixa")
	os.WriteFile(exe, []byte("old binary"), 0o755)

	u := Updater{DownloadBase: srv.URL}
	if err := u.Apply(context.Background(), "v0.3.0", exe); err == nil {
		t.Fatal("expected a checksum error")
	}
	got, _ := os.ReadFile(exe)
	entries, _ := os.ReadDir(dir)
	if string(got) != "old binary" || len(entries) != 1 {
		t.Fatalf("content=%q entries=%d", got, len(entries))
	}
}

func TestApplyMissingAsset(t *testing.T) {
	srv := releaseServer(t, []byte("new binary"), nil)
	defer srv.Close()
	exe := filepath.Join(t.TempDir(), "blixa")
	os.WriteFile(exe, []byte("old binary"), 0o755)

	u := Updater{DownloadBase: srv.URL}
	if err := u.Apply(context.Background(), "v9.9.9", exe); err == nil {
		t.Fatal("expected a download error")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatalf("content=%q", got)
	}
}
