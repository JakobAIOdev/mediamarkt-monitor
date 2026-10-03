#!/bin/sh
set -eu

root_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root_dir"

foreground=false
case "${1:-}" in
    --foreground) foreground=true; shift ;;
    --help)
        printf '%s\n' 'Usage: ./scripts/start.sh [--foreground [monitor flags]]' \
            'Linux: start a systemd user service that survives SSH logout and reboot.' \
            'Other systems: run in the foreground. Use scripts/service.sh for service controls.'
        exit 0 ;;
esac

if [ "$foreground" = false ] && [ "$(uname -s)" = Linux ]; then
    if [ "$#" -gt 0 ]; then
        printf '%s\n' 'Set service options in .env and tasks.csv, or use --foreground with monitor flags.' >&2
        exit 1
    fi
    exec "$root_dir/scripts/service.sh" start
fi

if [ ! -x bin/mediamarkt-monitor ]; then
    printf '%s\n' 'Monitor binary missing; run ./scripts/build.sh first.' >&2
    exit 1
fi

# Relative configuration paths resolve from the checkout, including .env.
# exec forwards stop signals directly to the monitor.
exec ./bin/mediamarkt-monitor -watch -tasks tasks.csv -proxies proxies.txt "$@"
