# Auf dem Server betreiben

Diese Anleitung verwendet Linux mit systemd und einen Checkout unter
`~/mediamarkt-monitor`. Der Service läuft als dein normaler Server-Benutzer.
Benötigt werden Git, Go ab **1.24.1** und die CA-Zertifikate des Systems
(`ca-certificates` auf Ubuntu/Debian). Für ein privates Repository muss der
Server über GitHub-Zugriff verfügen.

## Lokal committen und pushen

```sh
git add .env.example .gitignore README.md SERVER.md go.mod go.sum main.go internal scripts deploy tasks.example.csv proxies.example.txt
git commit -m "Add MediaMarkt stock monitor and server setup"
git push origin main
```

`.env`, `tasks.csv`, `proxies.txt`, Logs und `bin/` sind von Git ausgeschlossen.
Die echten Konfigurationsdateien richtest du auf dem Server ein. Die Vorlagen
enthalten keine Zugangsdaten.

## Erster Start

Auf dem Server:

```sh
git clone git@github.com:JakobAIOdev/mediamarkt-monitor.git ~/mediamarkt-monitor
cd ~/mediamarkt-monitor
cp .env.example .env
cp tasks.example.csv tasks.csv
cp proxies.example.txt proxies.txt
chmod 600 .env tasks.csv proxies.txt
```

Bearbeite die Dateien, bevor du startest:

- `.env`: `CHECK_INTERVAL=30s` und optional `DISCORD_WEBHOOK_URL`.
- `tasks.csv`: deine PIDs und Regionen; optional eine `webhook_url` pro Task.
- `proxies.txt`: deine echten Proxies. Entferne die Beispiel-Adressen.

Falls du bereits einen Checkout hast, verwende dort `git pull --ff-only` und
kopiere nur noch fehlende Konfigurationsdateien. Die `cp`-Befehle oben sind für
die Ersteinrichtung und überschreiben vorhandene Dateien.

```sh
./scripts/build.sh
./scripts/start.sh
```

`build.sh` baut das Binary für das Betriebssystem und die Architektur des Servers.
Ein neuer Build ersetzt das alte Binary erst nach erfolgreicher Kompilierung.
`start.sh` startet den Watch-Modus mit `tasks.csv` und **`proxies.txt`**, lädt
`.env` aus dem Checkout und schreibt JSON-Ereignisse ins Terminal. Eine fehlende,
leere oder ungültige Proxy-Datei verhindert den Start.

Für einen anderen Delay kannst du weiterhin
`./scripts/start.sh -interval 1m` verwenden. Mit Ctrl+C beendet sich der Monitor.
Für Betrieb nach dem SSH-Logout installiere den folgenden Service.

## systemd-Service installieren

Die Vorlage erwartet `~/mediamarkt-monitor`. Bei einem anderen Checkout-Pfad
passe `WorkingDirectory` und `ExecStart` in der installierten Service-Datei an.

```sh
mkdir -p ~/.config/systemd/user
cp deploy/mediamarkt-monitor.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now mediamarkt-monitor.service
systemctl --user status mediamarkt-monitor.service
```

Damit der Benutzer-Service auch nach dem SSH-Logout und beim Serverstart läuft:

```sh
sudo loginctl enable-linger "$USER"
```

Linger hält den Benutzer-Service-Manager unabhängig von einer offenen Anmeldung
aktiv. Siehe die [systemd-Dokumentation zu loginctl](https://github.com/systemd/systemd/blob/main/man/loginctl.xml).

Logs und Steuerung:

```sh
journalctl --user -u mediamarkt-monitor.service -f
systemctl --user stop mediamarkt-monitor.service
systemctl --user start mediamarkt-monitor.service
```

Die Befehle für die Steuerung führst du einzeln nach Bedarf aus. Nach Änderungen
an `.env`, Tasks oder Proxies starte den Service mit
`systemctl --user restart mediamarkt-monitor.service` neu. Diese Dateien werden
beim Start eingelesen.

Der Service startet nach einem Fehler erneut, mit maximal fünf Starts innerhalb
von zehn Minuten. Nach einem erfolgreichen Ende aller Tasks bleibt er beendet;
`Restart=on-failure` startet einen normal beendeten Prozess nicht erneut. Siehe
[systemd.service](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml).
Ein Neustart des Monitors kann bereits verfügbare Produkte erneut melden,
weil der Benachrichtigungsstatus derzeit nur für den jeweiligen Lauf gespeichert
wird. Nach dem Beheben eines Fehlers und einer erreichten Startbegrenzung:

```sh
systemctl --user reset-failed mediamarkt-monitor.service
systemctl --user start mediamarkt-monitor.service
```

## Updates deployen

Lokal Änderungen committen und pushen. Anschließend auf dem Server:

```sh
cd ~/mediamarkt-monitor
git pull --ff-only
./scripts/build.sh
systemctl --user restart mediamarkt-monitor.service
systemctl --user status mediamarkt-monitor.service
```

Führe den Neustart nur aus, wenn Pull und Build erfolgreich waren. Der laufende
Prozess verwendet bis zum Neustart das vorherige Binary. Die lokalen
Konfigurationsdateien bleiben beim Pull erhalten.

Wenn du die Service-Vorlage aktualisiert hast, kopiere sie zusätzlich erneut
nach `~/.config/systemd/user/` und führe `systemctl --user daemon-reload` aus,
bevor du den Service neu startest.
