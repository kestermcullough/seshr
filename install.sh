#!/usr/bin/env bash
# seshr installer — builds from source via `go install`.
# Usage: curl -fsSL https://raw.githubusercontent.com/kestermcullough/seshr/main/install.sh | sh
set -e

if ! command -v go >/dev/null 2>&1; then
  echo "error: 'go' is not on PATH."
  echo "Install Go 1.21+ from https://go.dev/dl/ and re-run this script."
  exit 1
fi

echo "installing seshr from github.com/kestermcullough/seshr ..."
go install github.com/kestermcullough/seshr@latest

GOBIN="$(go env GOBIN)"
if [ -z "$GOBIN" ]; then
  GOBIN="$(go env GOPATH)/bin"
fi
BIN="$GOBIN/seshr"

if [ ! -x "$BIN" ]; then
  echo "error: install failed; expected binary at $BIN"
  exit 1
fi

echo
echo "✓ installed: $BIN"

case ":$PATH:" in
  *":$GOBIN:"*) : ;;
  *)
    echo
    echo "Note: $GOBIN is not on your PATH."
    echo "Add this to your shell profile (~/.bashrc or ~/.zshrc):"
    echo "    export PATH=\"$GOBIN:\$PATH\""
    ;;
esac

echo
echo "Run 'seshr' to open the picker, or 'seshr --help' for flags."
