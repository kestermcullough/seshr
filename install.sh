#!/usr/bin/env sh
# seshr installer - downloads a prebuilt binary from GitHub Releases.
# Usage: curl -fsSL https://raw.githubusercontent.com/kestermcullough/seshr/main/install.sh | sh
set -eu

REPO="kestermcullough/seshr"
BIN_NAME="seshr"
VERSION="${VERSION:-latest}"

die() {
  echo "error: $*" >&2
  exit 1
}

has() {
  command -v "$1" >/dev/null 2>&1
}

download() {
  url="$1"
  dest="$2"

  if has curl; then
    curl -fsSL "$url" -o "$dest"
  elif has wget; then
    wget -qO "$dest" "$url"
  else
    die "curl or wget is required to download $BIN_NAME"
  fi
}

detect_os() {
  os="$(uname -s)"
  case "$os" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    *) die "unsupported OS: $os" ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64 | amd64) echo "amd64" ;;
    arm64 | aarch64) echo "arm64" ;;
    *) die "unsupported architecture: $arch" ;;
  esac
}

choose_install_dir() {
  if [ -n "${INSTALL_DIR:-}" ]; then
    echo "$INSTALL_DIR"
    return
  fi

  home="${HOME:-}"

  if has "$BIN_NAME"; then
    existing="$(command -v "$BIN_NAME")"
    existing_dir="$(dirname "$existing")"
    if [ -w "$existing_dir" ]; then
      echo "$existing_dir"
      return
    fi
  fi

  old_ifs="$IFS"
  IFS=:
  for path_dir in ${PATH:-}; do
    if [ -n "$path_dir" ] && [ -d "$path_dir" ] && [ -w "$path_dir" ]; then
      if [ -n "$home" ]; then
        case "$path_dir" in
          "$home"/*)
            IFS="$old_ifs"
            echo "$path_dir"
            return
            ;;
        esac
      fi

      case "$path_dir" in
        /usr/local/bin | /opt/homebrew/bin)
          IFS="$old_ifs"
          echo "$path_dir"
          return
          ;;
      esac
    fi
  done
  IFS="$old_ifs"

  if [ -z "$home" ]; then
    die "HOME is not set; pass INSTALL_DIR=/path/to/bin"
  fi

  echo "$home/.local/bin"
}

verify_checksum() {
  archive="$1"
  checksums="$2"
  asset="$3"

  if [ ! -f "$checksums" ]; then
    return
  fi

  expected="$(grep " $asset\$" "$checksums" | awk '{print $1}' || true)"
  if [ -z "$expected" ]; then
    echo "warning: checksum for $asset not found; skipping verification" >&2
    return
  fi

  if has sha256sum; then
    actual="$(sha256sum "$archive" | awk '{print $1}')"
  elif has shasum; then
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  else
    echo "warning: sha256sum or shasum not found; skipping verification" >&2
    return
  fi

  if [ "$expected" != "$actual" ]; then
    die "checksum mismatch for $asset"
  fi
}

OS="$(detect_os)"
ARCH="$(detect_arch)"
ASSET="${BIN_NAME}_${OS}_${ARCH}.tar.gz"

if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
else
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
fi

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t seshr)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

ARCHIVE="$TMP_DIR/$ASSET"
CHECKSUMS="$TMP_DIR/checksums.txt"
INSTALL_DIR_RESOLVED="$(choose_install_dir)"

echo "Installing $BIN_NAME ${VERSION} for ${OS}/${ARCH}..."

has tar || die "tar is required to unpack $ASSET"

if ! download "$BASE_URL/$ASSET" "$ARCHIVE"; then
  die "could not download $BASE_URL/$ASSET"
fi

if download "$BASE_URL/checksums.txt" "$CHECKSUMS"; then
  verify_checksum "$ARCHIVE" "$CHECKSUMS" "$ASSET"
else
  echo "warning: checksums.txt not found; skipping verification" >&2
fi

tar -xzf "$ARCHIVE" -C "$TMP_DIR"

if [ ! -x "$TMP_DIR/$BIN_NAME" ]; then
  die "archive did not contain executable $BIN_NAME"
fi

mkdir -p "$INSTALL_DIR_RESOLVED"

if [ ! -w "$INSTALL_DIR_RESOLVED" ]; then
  die "$INSTALL_DIR_RESOLVED is not writable; rerun with INSTALL_DIR=/path/to/bin"
fi

cp "$TMP_DIR/$BIN_NAME" "$INSTALL_DIR_RESOLVED/$BIN_NAME"
chmod 0755 "$INSTALL_DIR_RESOLVED/$BIN_NAME"

echo
echo "installed: $INSTALL_DIR_RESOLVED/$BIN_NAME"

case ":${PATH:-}:" in
  *":$INSTALL_DIR_RESOLVED:"*) ;;
  *)
    echo
    echo "Note: $INSTALL_DIR_RESOLVED is not on your PATH."
    echo "Add this to your shell profile:"
    echo "  export PATH=\"$INSTALL_DIR_RESOLVED:\$PATH\""
    echo
    echo "For now, run:"
    echo "  $INSTALL_DIR_RESOLVED/$BIN_NAME"
    ;;
esac

echo
echo "Run '$BIN_NAME' to open the picker, or '$BIN_NAME --help' for flags."
