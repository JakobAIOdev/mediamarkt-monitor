# Running on a server

This guide assumes Linux with systemd and a checkout at
`~/mediamarkt-monitor`. The service runs as your regular server user.
You need Git, Go **1.24.1 or later**, and the system CA certificates
(`ca-certificates` on Ubuntu/Debian). For a private repository, the server
must have access to GitHub.

## Commit and push locally

```sh
git add .env.example .gitignore README.md SERVER.md go.mod go.sum main.go internal scripts deploy tasks.example.csv proxies.example.txt
git commit -m "Add MediaMarkt stock monitor and server setup"
git push origin main
```

`.env`, `tasks.csv`, `proxies.txt`, logs, and `bin/` are excluded from Git.
Set up the actual configuration files on the server. The templates contain
no credentials.

## Initial setup

On the server:

```sh
git clone git@github.com:JakobAIOdev/mediamarkt-monitor.git ~/mediamarkt-monitor
cd ~/mediamarkt-monitor
cp .env.example .env
cp tasks.example.csv tasks.csv
cp proxies.example.txt proxies.txt
chmod 600 .env tasks.csv proxies.txt
```

Edit these files before starting:

- `.env`: set `CHECK_INTERVAL=30s` and optionally `DISCORD_WEBHOOK_URL`.
- `tasks.csv`: add your PIDs and regions, with an optional `webhook_url` per task.
- `proxies.txt`: add your actual proxies and remove the example addresses.

If you already have a checkout, run `git pull --ff-only` there and copy only
the configuration files that are missing. The `cp` commands above are for
initial setup and will overwrite existing files.

```sh
./scripts/build.sh
./scripts/start.sh
```

`build.sh` builds the binary for the server's operating system and architecture.
A new build replaces the previous binary only after compilation succeeds.
`start.sh` starts watch mode with `tasks.csv` and **`proxies.txt`**, loads
`.env` from the checkout, and writes JSON events to the terminal. A missing,
empty, or invalid proxy file prevents startup.

To override the delay, use `./scripts/start.sh -interval 1m`.
Press Ctrl+C to stop the monitor. Install the service below to keep it running
after you disconnect from SSH.

## Install the systemd service

The template expects `~/mediamarkt-monitor`. If your checkout is elsewhere,
update `WorkingDirectory` and `ExecStart` in the installed service file.

```sh
mkdir -p ~/.config/systemd/user
cp deploy/mediamarkt-monitor.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now mediamarkt-monitor.service
systemctl --user status mediamarkt-monitor.service
```

To run the user service after SSH logout and at server boot:

```sh
sudo loginctl enable-linger "$USER"
```

Lingering keeps the user service manager running without an active login
session. See the [systemd loginctl documentation](https://github.com/systemd/systemd/blob/main/man/loginctl.xml).

View logs and control the service:

```sh
journalctl --user -u mediamarkt-monitor.service -f
systemctl --user stop mediamarkt-monitor.service
systemctl --user start mediamarkt-monitor.service
```

Run the control commands individually as needed. After changing `.env`, tasks,
or proxies, restart the service with
`systemctl --user restart mediamarkt-monitor.service`. These files are read
at startup.

The service restarts after a failure, with a limit of five starts within ten
minutes. It stays stopped after all tasks finish successfully;
`Restart=on-failure` does not restart a process that exits normally. See
[systemd.service](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml).
Restarting the monitor can notify again for products that are already available,
because notification state is currently stored only for the active run.
If the start limit has been reached, fix the underlying error, then run:

```sh
systemctl --user reset-failed mediamarkt-monitor.service
systemctl --user start mediamarkt-monitor.service
```

## Deploy updates

Commit and push your changes locally. Then run on the server:

```sh
cd ~/mediamarkt-monitor
git pull --ff-only
./scripts/build.sh
systemctl --user restart mediamarkt-monitor.service
systemctl --user status mediamarkt-monitor.service
```

Restart only after the pull and build succeed. The running process continues
using the previous binary until you restart it. Local configuration files
are preserved during the pull.

If you updated the service template, copy it to `~/.config/systemd/user/` again
and run `systemctl --user daemon-reload` before restarting the service.
