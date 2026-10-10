#!/bin/sh
# blixa installer.
#
#   curl -fsSL https://raw.githubusercontent.com/laszlolevi/blixa/main/install.sh | sh
#
# Environment overrides:
#   BLIXA_VERSION      release tag to install (default: the latest release)
#   BLIXA_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
#   BLIXA_REPO         GitHub repository (default: laszlolevi/blixa)
#   BLIXA_BASE_URL     download base URL, e.g. a mirror (default: GitHub releases)
set -eu

repo="${BLIXA_REPO:-laszlolevi/blixa}"
version="${BLIXA_VERSION:-latest}"
install_dir="${BLIXA_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf 'blixa: %s\n' "$*"; }
fail() { printf 'blixa: error: %s\n' "$*" >&2; exit 1; }

os="$(uname -s)"
[ "$os" = "Darwin" ] || fail "only macOS is supported (got $os)"
case "$(uname -m)" in
  arm64 | aarch64) arch="arm64" ;;
  x86_64 | amd64) arch="amd64" ;;
  *) fail "unsupported CPU architecture: $(uname -m)" ;;
esac

if [ -n "${BLIXA_BASE_URL:-}" ]; then
  base="$BLIXA_BASE_URL"
elif [ "$version" = "latest" ]; then
  base="https://github.com/$repo/releases/latest/download"
else
  base="https://github.com/$repo/releases/download/$version"
fi

asset="blixa_darwin_$arch.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "downloading $asset ($version)"
curl -fsSL "$base/$asset" -o "$tmp/$asset" || fail "download failed: $base/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

expected="$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$expected" ] || fail "no checksum for $asset"
actual="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset"

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$install_dir"
# Install via a temp name + rename, so a running blixa (or its daemon) keeps its old binary.
cp "$tmp/blixa" "$install_dir/.blixa.new"
chmod 755 "$install_dir/.blixa.new"
mv -f "$install_dir/.blixa.new" "$install_dir/blixa"
say "installed to $install_dir/blixa"

case ":$PATH:" in
  *":$install_dir:"*) say "run: blixa" ;;
  *)
    say "$install_dir is not on your PATH; add this to your shell profile (e.g. ~/.zshrc):"
    printf '\n  export PATH="%s:$PATH"\n\n' "$install_dir"
    ;;
esac
