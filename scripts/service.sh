#!/bin/sh
set -eu

root_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root_dir"
service_name=mediamarkt-monitor.service
action=${1:-help}

case "$action" in
    help|--help|-h)
        printf '%s\n' 'Usage: ./scripts/service.sh {install|start|stop|restart|status|logs}' \
            'install/start/restart validate configuration and install the service for this checkout.' \
            'logs follows the journal; Ctrl+C closes the viewer without stopping the monitor.'
        exit 0 ;;
    install|start|stop|restart|status|logs) ;;
    *) printf '%s\n' 'Unknown action. Use ./scripts/service.sh --help.' >&2; exit 1 ;;
esac
if [ "$#" -gt 1 ]; then
    printf '%s\n' 'Service commands take one action. Configure the monitor in .env and tasks.csv.' >&2
    exit 1
fi

if [ "$(uname -s)" != Linux ] || ! command -v systemctl >/dev/null 2>&1; then
    printf '%s\n' 'Background mode requires Linux with systemd. Use ./scripts/start.sh --foreground here.' >&2
    exit 1
fi
if ! systemctl --user show-environment >/dev/null; then
    printf '%s\n' 'Cannot connect to the systemd user manager. Log in as your regular server user over SSH.' >&2
    exit 1
fi

if [ "$action" = start ] && systemctl --user is-active --quiet "$service_name"; then
    printf '%s\n' 'Monitor is already running. Use ./scripts/service.sh restart to apply changes.'
    exit 0
fi

case "$action" in
    install|start|restart)
        if [ ! -x bin/mediamarkt-monitor ]; then
            printf '%s\n' 'Monitor binary missing; run ./scripts/build.sh first.' >&2
            exit 1
        fi
        ./bin/mediamarkt-monitor -watch -tasks tasks.csv -proxies proxies.txt -validate
        # Quotes protect spaces in unit paths; reject characters that need
        # systemd specifier/environment expansion rather than shell escaping.
        case "$root_dir" in
            *'%'*|*'$'*|*'"'*|*\\*|*'
'*) printf '%s\n' 'Service checkout path cannot contain %, $, quotes, backslashes or newlines.' >&2; exit 1 ;;
        esac
        service_user=$(id -un)
        if [ "$(loginctl show-user "$service_user" -p Linger --value)" != yes ]; then
            printf '%s\n' 'Enabling the user service after SSH logout and at boot (sudo may ask for your password).'
            if [ "$(id -u)" -eq 0 ]; then
                loginctl enable-linger "$service_user"
            else
                sudo loginctl enable-linger "$service_user"
            fi
        fi
        unit_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
        mkdir -p "$unit_dir"
        unit_tmp=$(mktemp "$unit_dir/.mediamarkt-monitor.XXXXXX")
        trap 'rm -f "$unit_tmp"' 0
        trap 'exit 130' INT
        trap 'exit 143' TERM
        replacement=$(printf '%s' "$root_dir" | sed 's/[&|]/\\&/g')
        sed "s|@MONITOR_ROOT@|$replacement|g" deploy/mediamarkt-monitor.service > "$unit_tmp"
        chmod 644 "$unit_tmp"
        mv "$unit_tmp" "$unit_dir/$service_name"
        systemctl --user daemon-reload
        systemctl --user enable "$service_name"
        ;;
esac

case "$action" in
    install) printf '%s\n' 'Service installed. Run ./scripts/start.sh to start monitoring.' ;;
    start|restart)
        systemctl --user "$action" "$service_name"
        printf '%s\n' 'Monitor launched as a background service. You can disconnect from SSH.' \
            'Logs:   ./scripts/service.sh logs' \
            'Status: ./scripts/service.sh status' \
            'Stop:   ./scripts/service.sh stop'
        ;;
    logs) exec journalctl --user -u "$service_name" -n 50 -f -o cat ;;
    status) exec systemctl --user status "$service_name" --no-pager ;;
    stop) exec systemctl --user stop "$service_name" ;;
esac
