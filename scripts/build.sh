#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root_dir"
mkdir -p bin

# Publish only a completed build. Renaming the new file also allows an existing
# Linux process to keep running until the service is restarted.
build_dir=$(mktemp -d "$root_dir/bin/.build.XXXXXX")
trap 'rm -rf "$build_dir"' 0
trap 'exit 130' INT
trap 'exit 143' TERM

CGO_ENABLED=0 go build -trimpath -o "$build_dir/mediamarkt-monitor" .
mv "$build_dir/mediamarkt-monitor" "$root_dir/bin/mediamarkt-monitor"
printf '%s\n' 'Built bin/mediamarkt-monitor'
