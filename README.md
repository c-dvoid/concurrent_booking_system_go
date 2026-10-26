# Concurrent Booking System

A cinema seat booking service in Go that guarantees a seat can't be double-booked, even when thousands of users click it at the same moment. Seat state lives in Redis.

## How it works

1. **Hold** – A user holds a seat. The hold is a Redis `SET NX` with a 2-minute TTL, so only the first request wins. Everyone else gets `409 Conflict`.
2. **Confirm** – The holder confirms within 2 minutes. The TTL is removed and the booking becomes permanent.
3. **Release** – The holder can give the seat back early. If they do nothing, the hold expires on its own.

Confirm and release each run as a single Lua script, so checking who owns the seat and changing it happen in one step. A user can only confirm or release their own hold, and only while it's still active.

## Getting started

Requirements: Go 1.26+ and Docker.

```bash
docker compose up -d   # Redis on :6379, Redis Commander on :8081
go run ./cmd           # API and web page on :8080 (run from the repo root)
```

Then open http://localhost:8080. Each browser tab is a separate user, so you can open two tabs and compete for the same seat.

## API

| Method   | Path                                   | Body                 | Description                    |
| -------- | -------------------------------------- | -------------------- | ------------------------------ |
| `GET`    | `/movies`                              |                      | List movies                    |
| `GET`    | `/movies/{movieID}/seats`              |                      | Seats that are held or booked  |
| `POST`   | `/movies/{movieID}/seats/{seatID}/hold`| `{"user_id": "..."}` | Hold a seat, returns `session_id` |
| `PUT`    | `/sessions/{sessionID}/confirm`        | `{"user_id": "..."}` | Confirm a hold                 |
| `DELETE` | `/sessions/{sessionID}`                | `{"user_id": "..."}` | Release a hold                 |

Errors come back as `{"error": "..."}`:

| Status | Meaning |
| ------ | ------- |
| `400` | Bad request body or missing `user_id` |
| `403` | Not your session |
| `404` | Session not found or expired |
| `409` | Seat already taken, or booking already confirmed |

## Testing

```bash
go test -race -count=1 -v ./internal/booking/
```

The test has 1,000 goroutines race for the same seat and checks that exactly one wins. It needs Redis running; without Redis it's skipped.

## Project layout

```
cmd/                 entry point and routes
internal/booking/    domain, service, HTTP handlers, Redis store
internal/adapters/   Redis client setup
static/              browser UI
```
