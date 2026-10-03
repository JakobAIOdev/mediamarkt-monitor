# MediaMarkt monitor

Monitor MediaMarkt products in Austria and Germany using a PID and region.
Each task has its own worker, TLS client and cookie jar. The monitor rotates
proxies after failed requests and sends Discord notifications when a product
reports **InStock**.

## Quick start

Requirements: Go **1.24.1+**, Git, and system CA certificates.

```sh
cp .env.example .env
cp tasks.example.csv tasks.csv
cp proxies.example.txt proxies.txt
chmod 600 .env tasks.csv proxies.txt
```

These commands are for initial setup. Keep existing configuration files when
updating. Replace the example proxies with your real proxies before starting.

`.env`:

```dotenv
CHECK_INTERVAL=30s
ERROR_ALERT_THRESHOLD=5
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/ID/TOKEN
```

`tasks.csv`:

```csv
pid,region,webhook_url
2087300,AT,
```

`proxies.txt` — one proxy per line:

```text
host:port:username:password
```

Build and start:

```sh
./scripts/build.sh
./scripts/start.sh
```

**On Linux with systemd, `start.sh` starts a background service.** It validates
configuration, installs the service using the current checkout path and enables
it at boot. The first start may ask for your sudo password to enable user
lingering. You can then close SSH and the monitor keeps running. An already
running service is left running; use `restart` after changing its configuration.
See [SERVER.md](SERVER.md) for GitHub access and server deployment.

On macOS and other systems, `start.sh` runs in the foreground. To explicitly run
in the foreground on Linux as well:

```sh
./scripts/start.sh --foreground
```

Ctrl+C stops a foreground monitor. Do not run a foreground instance alongside
the service unless you want duplicate checks and notifications.

## Service controls

```sh
./scripts/service.sh logs
./scripts/service.sh status
./scripts/service.sh restart
./scripts/service.sh stop
./scripts/service.sh start
```

Run commands individually as needed. `logs` follows the journal; **Ctrl+C only
closes the log viewer**. `restart` validates configuration before restarting.
The service reads `.env`, `tasks.csv` and `proxies.txt` from the project root.
It automatically supports a checkout such as `~/Tools/mediamarkt-monitor`.

## Terminal output

Watch mode uses compact, readable status lines by default:

```text
  MEDIAMARKT MONITOR
  ──────────────────────────────────────────────────────────────────
  Tasks    1  |  Interval 30s  |  10 proxies
  Discord  1/1 tasks  |  Error alert after 5 failures
  ──────────────────────────────────────────────────────────────────
  12:00:00  AT  2087300    WAITING       Out of stock | #1 | next 30s
  12:00:30  AT  2087300    IN STOCK      Pokémon Box | #2
  12:00:31  AT  2087300    DISCORD       Stock notification delivered
```

The same format works in service logs without cursor controls. For automation,
use `-output json` to receive one complete event per line, including full
product data on stock events:

```sh
go run . -watch -tasks tasks.csv -proxies proxies.txt -output json
```

`-output pretty` explicitly selects readable output. Single-PID snapshots
continue to return product JSON by default.

## Configuration

| Setting | Default | Purpose |
| --- | --- | --- |
| `CHECK_INTERVAL` | `30s` | Delay between successful checks; accepts `30s`, `1m`, `500ms`, etc. |
| `ERROR_ALERT_THRESHOLD` | `5` | Consecutive request/availability failures before a task health alert; positive integer. |
| `DISCORD_WEBHOOK_URL` | Empty | Default destination for stock, task health and process failure alerts. |

The optional `.env` file is loaded from the working directory. Exported process
variables take precedence, including explicitly empty values. `-interval`
overrides `CHECK_INTERVAL`. The background service uses configuration files;
pass additional CLI flags with `--foreground` or invoke the binary directly.

CSV columns `pid` and `region` are required. Regions are `AT` and `DE`,
case-insensitive. An optional `webhook_url` overrides the default **for stock and
health alerts for that task**. A blank override uses `DISCORD_WEBHOOK_URL`.
When both are blank, Discord is disabled for that task. Process failure alerts
always use the default webhook because tasks may be unreadable.

Column order does not matter; additional columns are ignored. Duplicate
region/PID pairs are checked once. A nonempty webhook override takes precedence
over a blank one; conflicting overrides are rejected. IDs can differ between
storefronts: an Austrian PID does not identify the German equivalent reliably.

Supported proxy formats:

```text
host:port
host:port:user:pass
http://user:pass@host:port
https://user:pass@host:port
socks5://host:port
socks5h://user:pass@host:port
```

Blank lines and `#` comments are ignored. Workers start on proxies in list
order, wrapping around as needed. A fetch failure closes the TLS client and
moves that worker to the next proxy with a fresh cookie jar. With one proxy,
the next attempt recreates the client on that proxy. Configured proxies never
fall back to direct requests. Discord uses a separate direct HTTP client.

## Failures and Discord alerts

**Repeated request errors do not stop the monitor.** It waits 1, 2, 4, then
5 minutes between failures, or longer if `CHECK_INTERVAL` or the server's
`Retry-After` requires it. A successful product/availability check resets the
backoff and the consecutive error count.

After `ERROR_ALERT_THRESHOLD` consecutive errors, the task sends one
**Monitor degraded** alert to its effective webhook. Checks continue. Further
errors in the same streak do not send more health alerts. The first successful
check sends **Monitor recovered**; a new failure streak can alert again.
Health delivery is retried within a 15-second budget. A failed health alert is
logged and does not stop product checks. Health alerts are best effort; failed
delivery is not retried again throughout the same error streak.

A missing, empty or invalid proxy file, invalid tasks, or invalid configuration
stops startup with a nonzero exit code. Watch mode attempts a **Monitor failed**
alert through the default webhook. `-validate` performs the same checks without
product requests or stock notifications; failed validation can send a failure
alert. No Discord alert is possible when there is no valid default webhook,
`.env` cannot be loaded, Discord is unreachable, or the process is killed before
it can report. Configure the default webhook for startup failure notifications,
even when all tasks have their own overrides.

Stock notification delivery is retried on temporary failures until confirmed
or cancelled. Permanent stock notification failures end the affected worker;
other workers continue. A rejected/deleted webhook (401/403/404) is not requested
again during that run. Tasks sharing a webhook share its rate-limit state.
See [Discord webhooks](https://docs.discord.com/developers/resources/webhook)
and [rate limits](https://docs.discord.com/developers/topics/rate-limits).

The systemd service restarts after process failures, up to five starts within
ten minutes. It stays stopped when all tasks complete successfully. Error
counts and notification state are held in memory: restarting can send stock
notifications again for products already available. A successful run does not
remove tasks from the CSV. An enabled service runs them again at the next boot.

## CLI examples

```sh
# Fetch product JSON once (Austria by default)
go run . 2087300

# Watch a German PID using a direct connection
go run . -country de -watch YOUR_PID

# Watch CSV tasks with proxies in the foreground
go run . -watch -tasks tasks.csv -proxies proxies.txt

# Override the interval for a foreground run
./scripts/start.sh --foreground -interval 1m

# Check configuration without product requests
./bin/mediamarkt-monitor -watch -tasks tasks.csv -proxies proxies.txt -validate

# Show all flags
go run . -h
```

Flags go before positional PIDs. CSV and positional PIDs can be combined;
`-country` applies only to positional PIDs. Omitting `-proxies` selects direct
requests; both server entry points explicitly pass `-proxies proxies.txt`.

## Stock semantics

Product JSON comes from JSON-LD embedded in the product page, resolved using
`https://www.mediamarkt.at/de/product/-<pid>.html` or the German equivalent.
An offer explicitly reporting `InStock` triggers the notification. `PreOrder`,
`BackOrder` and unknown states do not. With multiple offers, any `InStock`
offer triggers it. A task finishes after stock and its configured notification
are confirmed. The process ends after all tasks finish.

Published availability can lag behind checkout even with cache revalidation.
The monitor does not verify local store stock or reserve products. Ambiguous
Discord transport failures can produce duplicate deliveries.

## Development

```text
main.go                  Entry point and shutdown signals
internal/app/            Flags, environment, output, failure reporting
internal/checker/        Workers, health alerts, polling, notification retries
internal/product/        TLS client, PID lookup, JSON-LD, availability
internal/discord/        Stock/health embeds and webhook rate limits
internal/tasks/          CSV loading and deduplication
internal/proxy/          Proxy loading and credential redaction
internal/retry/          Retry-After parsing and cancellable waits
scripts/                 Build, foreground start and service controls
deploy/                  systemd service template
```

```sh
go test -race ./...
go vet ./...
./scripts/build.sh
```

`.env`, `tasks.csv`, `proxies.txt`, `bin/` and `logs/` are ignored by Git. Never
commit real webhook URLs or proxy credentials. Configuration stays on each
server when pulling updates.
