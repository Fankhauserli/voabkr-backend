# voabkr-backend

Backend service for the **voabkr** vocabulary flashcard application, built with Go, Gin, PostgreSQL (via pgx and sqlc), and Redis.

For the full endpoint reference, request/response structs, and authentication details, see [API Documentation](API.md).

---

## Environment Variables

The application requires configuration via environment variables for database connectivity, session storage with Redis, and email delivery via SMTP.

### Summary

| Variable | Required | Default | Description | Example |
| :--- | :---: | :---: | :--- | :--- |
| `DB_CONN_STRING` | **Yes** | — | PostgreSQL connection string used by pgx. | `postgres://user:password@localhost:5432/voabkr?sslmode=disable` |
| `REDIS_ADDR` | **Yes** | — | Redis server address (`host:port`) for session management. | `localhost:6379` |
| `REDIS_PASSWORD` | **Yes** | — | Authentication password for Redis. | `secretredispass` |
| `SESSION_SECRET` | **Yes** | — | Secret key used for signing session cookies. | `super-secret-session-key` |
| `VALKEY_ADDRS` | No | `valkey-cluster:6379` | Valkey cluster seed addresses (comma-separated) for K8s. | `valkey-cluster.default.svc.cluster.local:6379` |
| `VALKEY_PASSWORD` | No | — | Optional authentication password for Valkey cluster. | `valkeypassword` |
| `VALKEY_DEFAULT_TTL_SEC` | No | `300` | Default cache TTL in seconds. | `300` |
| `SMTP_ADDR` | **Yes**\* | — | SMTP server host and port (`host:port`) for sending emails. | `smtp.example.com:587` |
| `SMTP_USER` | No | — | Username for SMTP plain authentication (skipped if empty). | `notifications@example.com` |
| `SMTP_PASS` | No | — | Password for SMTP plain authentication (skipped if empty). | `smtp-password` |
| `FROM_EMAIL` | **Yes**\* | — | Sender email address for outgoing verification emails. | `no-reply@example.com` |
| `FRONTEND_URL` | No | `http://localhost:5173` | Base URL of the frontend application for verification links. | `https://app.voabkr.com` |
| `GIN_MODE` | No | `debug` | Gin framework operational mode (`debug`, `release`, `test`). | `release` |

> \*) Required when user registration and email verification flows are triggered.

---

### Detailed Configuration

#### 1. Database (PostgreSQL)

- **`DB_CONN_STRING`**
  - **Description**: Standard PostgreSQL connection URI. Checked during server startup in `cmd/db.go`. If unset, the server will fail to start.
  - **Format**: `postgres://[user]:[password]@[host]:[port]/[database]?[params]`
  - **Example**: `postgres://postgres:postgres@localhost:5432/voabkr?sslmode=disable`

#### 2. Session & Cache (Redis)

- **`REDIS_ADDR`**
  - **Description**: Network address of the Redis instance used by Gin session middleware (`cmd/session.go`).
  - **Example**: `localhost:6379` or `redis:6379`
- **`REDIS_PASSWORD`**
  - **Description**: Redis instance authentication password.
  - **Example**: `your_redis_password`
- **`SESSION_SECRET`**
  - **Description**: Cryptographic salt / secret string used to sign user session cookies (`cmd/session.go`).
  - **Example**: `64-char-random-hex-or-string`

#### 3. Valkey Cluster & Local Memory Cache
The service uses a two-level cache for high throughput and low latency:
- **Tier 1 (L1)**: In-memory local cache with TTL expiration for instant, zero-network-hop reads.
- **Tier 2 (L2)**: Distributed Valkey Cluster deployed in Kubernetes with server-assisted client-side caching (`github.com/valkey-io/valkey-go`).
- **Protobuf Efficiency**: All cached entities (cards, decks, settings, user profiles, and lists) are serialized and deserialized using compact binary Protocol Buffers (`proto.Marshal` / `proto.Unmarshal`) rather than JSON.

- **`VALKEY_ADDRS` / `VALKEY_ADDR`**
  - **Description**: Seed addresses (comma-separated or single) of the Valkey cluster in Kubernetes. Falls back to local in-memory caching if unreachable.
  - **Example**: `valkey-cluster.default.svc.cluster.local:6379` or `valkey-0:6379,valkey-1:6379,valkey-2:6379`
- **`VALKEY_PASSWORD`**
  - **Description**: Authentication password for Valkey cluster nodes.
- **`VALKEY_DEFAULT_TTL_SEC`**
  - **Description**: Default cache TTL in seconds (default: `300`).
- **`VALKEY_LOCAL_TTL_SEC`**
  - **Description**: Local memory cache TTL in seconds (default: `120`).

#### 4. Email Delivery (SMTP)

- **`SMTP_ADDR`**
  - **Description**: Host and port of the outgoing mail server (`helpers/mail.go`).
  - **Example**: `smtp.mailgun.org:587` or `smtp.gmail.com:587`
- **`SMTP_USER`**
  - **Description**: Username for SMTP authentication. If left empty, authentication will not be performed.
  - **Example**: `user@example.com`
- **`SMTP_PASS`**
  - **Description**: Password for SMTP authentication.
  - **Example**: `your-smtp-password`
- **`FROM_EMAIL`**
  - **Description**: Sender email address populated in the `From` header of account verification emails.
  - **Example**: `no-reply@voabkr.com`

#### 4. Web Framework (Gin)

- **`GIN_MODE`**
  - **Description**: Controls Gin framework logging and optimization. Set to `release` for production deployments.
  - **Allowed Values**: `debug`, `release`, `test`

---

## Example `.env` File

Create a `.env` file in the root directory (ignored by `.gitignore`):

```env
# Database
DB_CONN_STRING=postgres://postgres:postgres@localhost:5432/voabkr?sslmode=disable

# Redis Session Storage
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=myredispassword
SESSION_SECRET=a_very_secret_key_used_to_encrypt_sessions

# SMTP / Email Configuration
SMTP_ADDR=smtp.example.com:587
SMTP_USER=smtp_username
SMTP_PASS=smtp_password
FROM_EMAIL=no-reply@example.com

# Framework Mode
GIN_MODE=debug
```

---

## Running the Application

### Local Development

1. Ensure PostgreSQL and Redis are running.
2. Export the required environment variables or source your `.env`:
   ```bash
   export $(grep -v '^#' .env | xargs)
   ```
3. Run the application:
   ```bash
   go run ./cmd/
   ```
   The HTTP server listens on port `8080` (`http://localhost:8080`).

### Docker

Build and run using Docker:

```bash
docker build -t voabkr-backend .
docker run --env-file .env -p 8080:8080 voabkr-backend
```

### Health Check Endpoints

- `GET /healthz` - Basic liveness probe
- `GET /readyz` - Readiness probe

---

## Database Migrations

Database migrations are managed using [Goose](https://github.com/pressly/goose) and embedded directly into the application binary (`sql/migrations/`).

- **Automatic execution**: Migrations are applied automatically on application startup whenever `DB_CONN_STRING` is configured.
- **Adding migrations**: Create a new file in `sql/migrations/` following the naming convention `0000X_description.sql` with `-- +goose Up` and `-- +goose Down` directives.
- **Code generation**: Run `sqlc generate` in the `sql/` directory to regenerate type-safe Go queries from the migration schema.