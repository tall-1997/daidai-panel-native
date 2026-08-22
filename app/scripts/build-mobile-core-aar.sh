#!/usr/bin/env bash
set -euo pipefail

GOMOBILE_VERSION="v0.0.0-20260709172247-6129f5bee9d5"
APP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PANEL_SOURCE_DIR="${PANEL_SOURCE_DIR:-$APP_ROOT/../panel}"

if [[ -z "$PANEL_SOURCE_DIR" || ! -d "$PANEL_SOURCE_DIR/server/mobilecore" ]]; then
  printf 'Panel source is missing at %s. Set PANEL_SOURCE_DIR to the monorepo panel directory.\n' \
    "$PANEL_SOURCE_DIR" >&2
  exit 1
fi

OUTPUT_DIR="$APP_ROOT/android/app/libs"
GOMOBILE_BIN="$(go env GOPATH)/bin/gomobile"

go install "golang.org/x/mobile/cmd/gomobile@${GOMOBILE_VERSION}"
"$GOMOBILE_BIN" init

mkdir -p "$OUTPUT_DIR"
(
  cd "$PANEL_SOURCE_DIR/server"
  go test ./mobilecore
  "$GOMOBILE_BIN" bind \
    -target android \
    -androidapi 28 \
    -javapkg mobilecore \
    -o "$OUTPUT_DIR/mobilecore.aar" \
    ./mobilecore
)
