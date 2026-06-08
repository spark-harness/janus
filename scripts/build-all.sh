#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST="$ROOT/dist"
mkdir -p "$DIST"

build() {
  goos=$1
  goarch=$2
  ext=$3
  output="$DIST/janus-$goos-$goarch$ext"
  echo "building $output"
  GOOS=$goos GOARCH=$goarch go build -o "$output" "$ROOT/cmd/janus"
}

build linux amd64 ""
build linux arm64 ""
build darwin amd64 ""
build darwin arm64 ""
build windows amd64 ".exe"
build windows arm64 ".exe"
