# Concurrent Website Monitor

A small Go service that monitors HTTP/HTTPS sites concurrently, persists check history in SQLite, exposes a JSON REST API, and logs availability transitions.

## Requirements

- Go 1.22+

## Run locally

```powershell
go mod tidy
go run ./cmd/monitor
```

By default, the service listens on `:8080` and creates `monitor.db` in the working directory. Add a site:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:8080/api/sites `
  -ContentType 'application/json' -Body '{"name":"Example","url":"https://example.com"}'
```

The monitor runs a check immediately at startup and then checks at the configured interval.

## API

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Liveness check |
| `GET` | `/api/sites` | List monitored sites |
| `POST` | `/api/sites` | Add a site (`{"name":"Example","url":"https://example.com"}`; name is optional) |
| `GET` | `/api/summary` | Uptime percentage, latest status, and counts per site |
| `GET` | `/api/sites/{id}/history?limit=100` | Recent checks for a site (maximum 1000) |

Availability means the request completed and returned a status code from 200 through 499. Network errors, timeouts, and 5xx responses are failures. The default client follows redirects.

## Configuration

| Environment variable | Default | Description |
|---|---:|---|
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `DATABASE_PATH` | `monitor.db` | SQLite database file |
| `CHECK_INTERVAL` | `60s` | Time between check runs (Go duration) |
| `REQUEST_TIMEOUT` | `10s` | Per-request timeout |
| `WORKERS` | `10` | Maximum simultaneous checks |
| `RETRIES` | `1` | Additional attempts after a failed check |

Availability changes are emitted as structured warning logs. The process handles Ctrl+C/SIGTERM and gracefully shuts down the HTTP server and monitor workers.

## Validate

```powershell
go test ./...
```
