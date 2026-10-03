# Running on a server

Use Linux with systemd, Git, Go **1.24.1+**, and CA certificates
(`ca-certificates` on Ubuntu/Debian). Run as your regular server user.
The examples use `~/Tools/mediamarkt-monitor`; the service installer detects
the actual checkout path automatically.

## GitHub SSH access

For a private repository, add a dedicated read-only deploy key. A deploy key
from another repository (for example, Dopesnow) cannot be reused.
See [GitHub deploy keys](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/managing-deploy-keys).

On the server, generate a key if it does not already exist:

```sh
mkdir -p ~/.ssh
chmod 700 ~/.ssh
[ -f ~/.ssh/mediamarkt_github ] || ssh-keygen -t ed25519 -C "server-mediamarkt" -f ~/.ssh/mediamarkt_github -N ""
cat ~/.ssh/mediamarkt_github.pub
```

Add the **public** key to the repository's **Settings → Deploy keys**.
Leave **Allow write access** unchecked. Then clone:

```sh
mkdir -p ~/Tools
cd ~/Tools
GIT_SSH_COMMAND="ssh -i $HOME/.ssh/mediamarkt_github -o IdentitiesOnly=yes" git clone git@github.com:JakobAIOdev/mediamarkt-monitor.git
```

After the clone succeeds, store the SSH key selection for future pulls:

```sh
cd ~/Tools/mediamarkt-monitor
git config core.sshCommand 'ssh -i ~/.ssh/mediamarkt_github -o IdentitiesOnly=yes'
```

## Configure once

Copy only missing files so existing settings are preserved:

```sh
cd ~/Tools/mediamarkt-monitor
[ -f .env ] || cp .env.example .env
[ -f tasks.csv ] || cp tasks.example.csv tasks.csv
[ -f proxies.txt ] || cp proxies.example.txt proxies.txt
chmod 600 .env tasks.csv proxies.txt
nano .env
```

Set:

```dotenv
CHECK_INTERVAL=30s
ERROR_ALERT_THRESHOLD=5
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/ID/TOKEN
```

Edit tasks and replace example proxy addresses with real proxies:

```sh
nano tasks.csv
nano proxies.txt
```

Example task:

```csv
pid,region,webhook_url
2087300,AT,
```

The blank task webhook uses the default in `.env`. Use `AT` or `DE`; product
IDs can differ between regions. See [README.md](README.md) for all formats,
notification behavior and error handling. Config files are ignored by Git.

## Build and start in the background

```sh
./scripts/build.sh
./scripts/start.sh
```

On Linux, `start.sh` validates the task/proxy/webhook configuration, installs the
systemd user service for the current checkout and starts it in the background.
The first start may ask for your sudo password to run
`loginctl enable-linger YOUR_USER`. Lingering keeps the user manager alive after
logout and starts it at boot; see [systemd loginctl](https://github.com/systemd/systemd/blob/main/man/loginctl.xml).
If that step fails, fix it before relying on background operation.

Once the script returns successfully, **you can disconnect from SSH**. The
monitor runs independently. It explicitly uses `tasks.csv` and `proxies.txt`,
and loads `.env` from the checkout. Missing/empty/invalid proxies prevent
startup and can send a failure alert through the default webhook.

The binary is built for the server's architecture with CGO disabled. Builds
replace the existing binary only after compilation succeeds. They do not
restart an already running monitor.

## Logs and controls

```sh
./scripts/service.sh logs
```

Logs use readable status lines. **Ctrl+C exits the log viewer without stopping
the monitor.** Run these controls individually as needed:

```sh
./scripts/service.sh status
./scripts/service.sh restart
./scripts/service.sh stop
./scripts/service.sh start
```

Restart after editing `.env`, tasks or proxies; those files are read at startup.
`restart` validates configuration before touching the running service. `start`
leaves an already active instance running and does not apply configuration
changes. `status` can return a nonzero exit code when the service is inactive
or failed; the journal contains the reason.

To test interactively, stop any background instance first:

```sh
./scripts/service.sh stop
./scripts/start.sh --foreground
```

Ctrl+C stops this foreground instance. A foreground run ends when its SSH
session closes. Return to background operation with `./scripts/start.sh`.

## Failures and completion

Request errors keep retrying with proxy rotation and backoff. A task sends one
Discord warning after `ERROR_ALERT_THRESHOLD` consecutive errors and a recovery
alert after its next successful check. Health alert failures are logged and do
not stop checks. Startup/process failures attempt an alert through the default
webhook. Reporting is best effort and bounded; invalid/unreadable webhook
configuration, Discord outages or abrupt process termination can prevent it.

The service uses `Restart=on-failure` with a five-start limit per ten minutes.
See [systemd.service](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml).
It stays stopped once all tasks finish successfully. Since notification state
is in memory, restarting or booting the enabled service can notify again for
already available products. CSV rows are not removed after completion.

After fixing an error that reached the systemd start limit:

```sh
systemctl --user reset-failed mediamarkt-monitor.service
./scripts/service.sh start
```

To disable automatic startup at boot:

```sh
systemctl --user disable --now mediamarkt-monitor.service
```

## Deploy updates

Commit and push changes on your development machine. On the server:

```sh
cd ~/Tools/mediamarkt-monitor
git pull --ff-only
./scripts/build.sh
./scripts/service.sh restart
./scripts/service.sh status
```

Proceed to the next command only after the previous one succeeds. The running
process keeps its old binary until restart. Real `.env`, tasks and proxies are
preserved during pulls. If you add new environment settings, add them to your
existing `.env`; copying templates would overwrite your configuration.

`start` and `restart` regenerate the installed service from the tracked template
and reload systemd, so unit changes are applied automatically. The installed
unit lives at `~/.config/systemd/user/mediamarkt-monitor.service`. Do not copy the
template directly: its `@MONITOR_ROOT@` placeholders are filled by `service.sh`.
