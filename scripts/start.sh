#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root_dir"

if [ ! -x bin/mediamarkt-monitor ]; then
    printf '%s\n' 'Monitor binary missing; run ./scripts/build.sh first.' >&2
    exit 1
fi

# Relative configuration paths resolve from the checkout, including .env.
# exec forwards stop signals directly to the monitor.
exec ./bin/mediamarkt-monitor -watch -tasks tasks.csv -proxies proxies.txt "$@"
