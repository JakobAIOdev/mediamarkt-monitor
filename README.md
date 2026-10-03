# MediaMarkt monitor

Fetch product JSON using a PID, or check repeatedly until an offer reports `InStock`.

Tasks can be loaded from CSV with one row per product and region:

```csv
pid,region
2087300,AT
```

Regions are `AT` and `DE` (case-insensitive). Column order does not matter;
additional columns are ignored. Duplicate region/PID pairs are checked once.
Product IDs can refer to different products on different storefronts.

Use `DISCORD_WEBHOOK_URL` as the default notification destination. An optional
`webhook_url` column overrides it for individual tasks:

```csv
pid,region,webhook_url
2087300,AT,
2087301,DE,https://discord.com/api/webhooks/123/REPLACE_WITH_TOKEN
```

A missing or blank `webhook_url` uses the environment variable. If both are
empty, that task runs without Discord notifications. Positional PIDs use the
environment variable. `.env` is loaded automatically from the working directory;
variables already exported in your shell take precedence. Existing CSV files
containing only `pid,region` continue to work.

Configure the delay and default webhook in `.env` (see `.env.example`):

```dotenv
CHECK_INTERVAL=30s
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/ID/TOKEN
```

`CHECK_INTERVAL` accepts durations such as `30s`, `1m`, or `500ms` and defaults
to `30s` when empty. The `-interval` flag overrides it. Zero, negative, or invalid
durations are rejected before requests start. Request error backoff and server
retry delays can still be longer than this interval.

For duplicate region/PID rows, a nonempty override takes precedence over a blank
one. Conflicting nonempty overrides are rejected before monitoring starts.

```sh
cp tasks.example.csv tasks.csv
# Add your products to tasks.csv, your proxies to proxies.txt,
# and CHECK_INTERVAL / DISCORD_WEBHOOK_URL to .env.
go run . -watch -tasks tasks.csv -proxies proxies.txt
```

Omit `-proxies` for direct requests. Notifications are used only in watch mode;
all effective webhook URLs are validated before workers start. Tasks, proxies,
and `.env` files are excluded from Git.

```sh
# One request (Austria by default)
go run . 2087300

# Check immediately, then wait 30 seconds between successful checks
go run . -watch -interval 30s 2087300

# German storefront (product IDs can differ between countries)
go run . -country de -watch -interval 1m <pid>

# Multiple positional PIDs, using the same region
go run . -watch 2087300 2087301

# CSV tasks without proxies or Discord
go run . -watch -tasks tasks.csv
```

Flags go before positional PIDs. CSV tasks and positional PIDs can be combined;
`-country` applies only to positional PIDs. Stop all workers with Ctrl+C.

Each unique region/PID task has a goroutine, TLS client, and cookie jar. A worker
stops after reporting `InStock` and, if configured, delivering its Discord
notification. Other workers continue; the process ends when all workers finish.

Watch mode outputs one JSON object per line. `checked` reports the current
availability; `error` reports a failed request or missing availability;
`in_stock` includes the full product JSON. `notified` confirms Discord delivery.
With multiple tasks in one-shot mode, the output is also JSON lines, with each
`product` event carrying its PID, country, and full product data. Single-PID
one-shot mode keeps the original plain product JSON output.
`PreOrder`, `BackOrder`, and unknown status values do not trigger an `in_stock` event.
For multiple offers, any offer explicitly reporting `InStock` triggers the event.

The checker reuses its TLS client and cookie jar. After consecutive errors it
waits 1, 2, 4, then 5 minutes, or longer if the configured interval or server's
`Retry-After` requires it. A successful check resets that delay.

The proxy file has one entry per line; blank lines and `#` comments are ignored.
Supported formats:

```text
host:port
host:port:user:pass
http://user:pass@host:port
https://user:pass@host:port
socks5://host:port
socks5h://user:pass@host:port
```

Workers start on proxies in list order, wrapping around when there are more
tasks than proxies. After a fetch failure, the client is closed and the next
attempt uses the next proxy with a fresh cookie jar. Normal error delays and
server `Retry-After` still apply; configured proxies never fall back to direct
requests. With one proxy, a failure recreates the client using that same proxy.

Discord embeds contain name, PID, region, price, product link, and an image when
available. Messages use `wait=true` to request delivery confirmation. Sends are
serialized per normalized webhook URL, rate-limit headers and Discord's
`retry_after` are honored, and
temporary failures are retried until delivery or Ctrl+C. Retries are logged as
`notification_retry`; permanently rejected messages produce `notification_error`.
A rejected/deleted webhook (401/403/404) is not requested again during that run;
other webhook destinations continue independently. Tasks targeting the same
normalized URL share a client and rate-limit state.
Discord requests use a separate direct HTTP client. No proxy credentials or
webhook tokens are written to the JSON logs. Implementation references:
[Discord webhooks](https://docs.discord.com/developers/resources/webhook) and
[Discord rate limits](https://docs.discord.com/developers/topics/rate-limits).

Notifications are remembered only for the current run. Restarting can notify
again for already available products, and ambiguous network failures can cause
duplicate notifications if Discord accepted a message before the connection failed.

Product data comes from JSON-LD embedded in the product page resolved from
`https://www.mediamarkt.at/de/product/-<pid>.html`. Requests ask caches to
revalidate, but the published availability can still lag behind checkout.
This does not verify local store stock or reserve a product.

## Project structure

```text
main.go                  CLI entry point and shutdown signals
internal/
  app/                   CLI flags, configuration, dependency wiring
  product/               TLS client, PID lookup, JSON-LD, availability
  checker/               Workers, proxy rotation, polling, delivery retries
  tasks/                 CSV loading, region validation, deduplication
  proxy/                 Proxy formats, file loading, credential redaction
  discord/               Webhook transport, rate limits, embed payloads
  retry/                 Retry-After parsing and cancellable waits
tasks.example.csv        Task template
proxies.example.txt      Proxy template
.env.example             Delay and webhook configuration template
scripts/build.sh         Build the server binary
scripts/start.sh         Start watch mode with tasks.csv and proxies.txt
deploy/                  systemd user-service template
SERVER.md                Server installation and update instructions
```

Tests live alongside their packages. The checker depends on a notification
interface; `app` connects it to Discord. Product and proxy packages do not depend
on the CLI or notification transport. Runtime files `tasks.csv` and `proxies.txt`
stay in the project root, and the start command remains `go run .`.

```sh
go test -race ./...
go vet ./...
go build -o mediamarkt-monitor .
```

## Server deployment

For commit/push, server setup, systemd operation and updates, follow
[SERVER.md](SERVER.md). The server entry point explicitly enables proxies:

```sh
./scripts/build.sh
./scripts/start.sh
```

Keep `.env`, `tasks.csv` and `proxies.txt` on the server. They are ignored by Git
and remain local during updates. Rebuild and restart after each pull.
