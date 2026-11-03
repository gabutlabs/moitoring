# Moitoring

## Install on Linux

```sh
curl -fsSL https://raw.githubusercontent.com/gabutlabs/moitoring/main/install.sh | sudo sh
```

GitHub Actions builds the Vue frontend and Linux binaries with `CGO_ENABLED=0`, then publishes `amd64` and `arm64` archives for version tags. The installer downloads the matching latest release, so the target machine does not need Go or Node.js. Create a release by pushing a version tag, for example `git tag v0.1.0 && git push origin v0.1.0`; the install command works after the first release is published. The installer requires a running systemd host, places the binary, `config.ini`, and `data/` in `/opt/moitoring`, creates the `moitoring` service account, and enables and starts `moitoring.service`. Existing configuration is preserved on reinstall. The config and database are owned by the service account.

The installer also places `moitoring-worker.service` but leaves it disabled until RabbitMQ and SMTP settings are configured in `/opt/moitoring/config.ini`. Then start it with:

```sh
sudo systemctl enable --now moitoring-worker.service
```

## Open the site from another machine

The frontend and API share the same port, `8080` by default. The default host is `127.0.0.1`, which only accepts connections from the server itself. After installing a release that supports `server.host`, set it to `0.0.0.0` for direct IP access:

```ini
[server]
host = 0.0.0.0
port = 8080
```

Then restart the API and allow TCP port 8080 in the server firewall:

```sh
sudo systemctl restart moitoring.service
sudo ss -ltnp | grep ':8080'
curl -I http://127.0.0.1:8080/
curl -i http://127.0.0.1:8080/api/health
```

If the firewall uses UFW, allow the port with `sudo ufw allow 8080/tcp`. Direct HTTP is unencrypted; for public access, a TLS reverse proxy is safer. Keep `host = 127.0.0.1` when using a reverse proxy on the same server.

## Run locally

The app reads its port and SQLite path from `config.ini`. `config.ini.example` contains the starter settings; if `config.ini` is missing in a fresh checkout, copy the example with `cp config.ini.example config.ini`. Viper is pinned to 1.19 because Viper 1.20 removed INI decoding.

By default, commands search the current directory and its parent directories for `config.ini.example`, then use the `config.ini` beside it. When launched from elsewhere, the installed binary uses the `config.ini` beside itself. Pass `--config /absolute/path/to/config.ini` to a subcommand to select another file, such as from a systemd unit.

Start the Gin API:

```sh
go run ./cmd serve
```

Open the read-only terminal dashboard (running the installed `moitoring` command with no subcommand does the same):

```sh
go run ./cmd tui
```

The installer creates `/usr/local/bin/moitoring` (override the directory with `MOITORING_BIN_DIR`), which is on the standard shell `PATH` in Bash and Zsh. It can be called from any working directory. The installed configuration and database are private to the `moitoring` service account; open the dashboard with `sudo -u moitoring moitoring`.

Use the left and right arrows or `1`–`3` to switch between Overview, RabbitMQ queues, and Systemd workers. In either list, press `/` to search; queue search matches names, while worker search matches host, unit, and state. Press `s` to cycle status filters, `v` to cycle queue vhosts, and `c` to clear filters. Search updates as you type; Enter keeps it and Escape restores the previous search. Press `r` to refresh or `q` to exit. The dashboard refreshes automatically at the configured monitoring poll interval.

Set `server.web_ui_enabled = false` in `config.ini`, then restart `moitoring.service`, to disable the browser UI while leaving the HTTP API and health endpoint available. The terminal dashboard can run without starting the HTTP server.

The first start creates `./data/moitoring.db`, applies the versioned schema, and creates the configured admin user if that username is missing. Its password is stored as a bcrypt hash. Existing usernames are left unchanged. Replace the sample admin password in `config.ini` before using this outside local development. To override the configured port, use `go run ./cmd/moitoring serve -p 8081`.

Start the Vue app in another:

```sh
cd frontend
npm install
npm run dev
```

Open [http://127.0.0.1:5173](http://127.0.0.1:5173). Vite proxies `/api` requests to the Go API at `127.0.0.1:8080`.

The starter screen shows whether the API is reachable and an honest empty state until monitoring sources are configured. Check the API directly at [http://127.0.0.1:8080/api/health](http://127.0.0.1:8080/api/health).

## Worker monitoring and alerts

Run the worker on the server that can reach RabbitMQ's Management API. It polls RabbitMQ queues and configured systemd units on startup and then at the `monitoring.poll_interval` (60 seconds by default), writes observations to the same SQLite database as `serve`, and sends email through the configured SMTP server. To monitor local systemd services, add their unit names under `[systemd]` in the protected configuration file:

```ini
[systemd]
units = jobs-worker.service, email-worker.service
```

For remote Linux hosts, configure `targets` as comma-separated `host|unit.service|expected_state` entries. The expected state defaults to `active` when the last field is empty. For example:

```ini
[systemd]
targets = worker-a|jobs-worker.service|active, ops@worker-b|billing.service|active
ssh_identity_file = /absolute/path/to/monitoring_ed25519
ssh_known_hosts = /absolute/path/to/monitoring_known_hosts
```

The collector pins SSH host keys to the configured known-hosts file, disables interactive/password authentication, agent forwarding, and SSH config files, and runs only a fixed read-only `systemctl show` command. Use a dedicated unprivileged account and an SSH key restricted on each Linux host to the required read-only inspection command. The local `units` setting targets `localhost`; remote targets work from macOS as well as Linux. Leave both `units` and `targets` empty to disable systemd collection.

The worker reads systemd state with the read-only `systemctl show` command. Unit failures include the load state, active/substate, result, main-process exit code/status, and systemd status text when available. If a host or target cannot be collected, it opens a source/collection incident and preserves the last worker result as stale instead of marking the unit itself failed.

Incidents detected in the same polling cycle are combined into one email listing each affected queue or worker and its reason. An unresolved incident gets a reminder after the configured interval (24 hours in the sample); reminders due in the same polling cycle are grouped too. Recovery is emailed, and a later failure after recovery creates a fresh alert. On macOS, unit tests use simulated collector and SSH output and do not require systemd or a remote host:

```sh
go test ./internal/collector/systemd ./internal/services/monitoring
```

The `worker` command requires Linux for local `systemd.units`; remote `systemd.targets` can be collected from macOS when SSH credentials and host keys are configured.

```sh
go run ./cmd worker --config /etc/moitoring/config.ini
```

The installer uses `deploy/moitoring.service.example` and `deploy/moitoring-worker.service.example` to create the systemd units. If the SQLite database is moved outside `/opt/moitoring/data`, update the units' `ReadWritePaths` to allow the service account to write to the new directory.

Set `monitoring.poll_interval` to control how often RabbitMQ and systemd state are collected. Existing configurations using `rabbitmq.poll_interval` remain supported when the new setting is absent. Set `rabbitmq.url`, the read-only RabbitMQ username and password, and the virtual hosts in the protected configuration file. Set SMTP connection details under `[smtp]`, and the email recipients and `reminder_interval` under `[alerts]`. The sample uses a 24-hour reminder interval. Queue `running` and `idle` states clear a queue outage. Zero consumers show as a warning; RabbitMQ state `down`, a missing expected queue, or an unavailable RabbitMQ API creates a critical incident. New incidents found in one polling cycle share one email; repeated down observations do not send another alert. Each active incident is reminded after the configured interval, and recoveries send email. A later outage opens a new incident and alerts again.

To exercise the queue alert safely in staging, set `rabbitmq.expected_queues = monitoring-alert-test` while that disposable queue is absent. The worker should send a critical alert; creating the test queue should send recovery. To exercise a RabbitMQ source outage, point a staging worker at an unreachable Management API endpoint, then restore the endpoint. Do not stop the production broker or delete a production queue to test notifications.

## Build and check

```sh
npm ci --prefix frontend
npm run build --prefix frontend
rm -rf internal/web/dist
mkdir -p internal/web/dist
cp -R frontend/dist/. internal/web/dist/
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./...
```
