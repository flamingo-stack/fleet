# Local Setup

## Prerequisites

- Go toolchain
- Node.js (^24.10.0) + yarn
- `kubectl` with access to the tenant cluster
- `jq` for test scripts

## 1. Port-forward MySQL and Redis

```bash
kubectl -n integrated-tools port-forward fleetmdm-mysql-0 3307:3306 &
kubectl -n integrated-tools port-forward fleetmdm-redis-0 6380:6379 &
```

## 2. Build Fleet

Generate frontend assets and compile the binary with embedded assets:

```bash
yarn install --ignore-engines
yarn run --ignore-engines webpack --progress
make generate-go
go build -tags full -o build/fleet ./cmd/fleet
```

## 3. Run database migrations

```bash
./build/fleet prepare db \
  --mysql_address=127.0.0.1:3307 \
  --mysql_database=fleet-mdm-database \
  --mysql_username=fleet-mdm-user \
  --mysql_password=fleet-mdm-password-1234
```

## 4. Start Fleet

```bash
FLEET_OPENFRAME_MODE=1 ./build/fleet serve \
  --dev \
  --mysql_address=127.0.0.1:3307 \
  --mysql_database=fleet-mdm-database \
  --mysql_username=fleet-mdm-user \
  --mysql_password=fleet-mdm-password-1234 \
  --redis_address=127.0.0.1:6380 \
  --server_address=0.0.0.0:8080 \
  --server_tls=false
```

Fleet UI will be available at `http://localhost:8080/login`.

> **Note:** `FLEET_OPENFRAME_MODE=1` is required for openframe-specific endpoints (policy/query host assignments).

## Troubleshooting

### `--dev` flag overrides MySQL credentials

`applyDevFlags` in `cmd/fleet/main.go` sets default MySQL username/password/database. If your credentials differ from the defaults, pass them explicitly via flags or env vars (`FLEET_MYSQL_USERNAME`, etc.) — the patched version only applies defaults when values are empty.

### gcloud auth expired

If port-forward fails with `Reauthentication failed`, run:

```bash
gcloud auth login
```

### S3 bucket errors

The `failed to create test software installer bucket` / `failed to create test carve bucket` warnings are harmless in local dev — they require a local MinIO instance on port 9000.
