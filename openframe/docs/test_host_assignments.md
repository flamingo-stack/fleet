# Scripts

## test_host_assignments.sh

End-to-end test for policy and query host assignment endpoints in openframe mode.

### Usage

```bash
FLEET_URL=http://localhost:8080 bash openframe/scripts/test_host_assignments.sh [options]
```

### Options

| Flag | Description |
|------|-------------|
| `--policy-id ID` | Use specific policy ID (auto-discovers or creates if omitted) |
| `--query-id ID` | Use specific query ID (auto-discovers or creates if omitted) |
| `--host-ids "1,2,3"` | Use specific host IDs (auto-discovers if omitted, needs at least 2) |
| `--url URL` | Fleet server URL (default: `$FLEET_URL` or `https://localhost:8080`) |
| `--token TOKEN` | API token (default: `$FLEET_TOKEN` or built-in dev token) |
| `--no-cleanup` | Skip cleanup steps — leave test data (hosts assigned to policies/queries) in place for inspection |
| `--verbose` | Log every request and response with HTTP method, path, status code, and body |

### Environment variables

- `FLEET_URL` — Fleet server URL
- `FLEET_TOKEN` — API bearer token

### What it tests

**Policy host assignments** (tests 1-10):

1. `PUT /policies/:id/hosts` — replace all assigned hosts
2. `GET /policies/:id/hosts` — list assigned hosts
3. `GET /policies/:id/hosts?per_page=1` — pagination
4. `DELETE /policies/:id/hosts` — remove specific hosts
5. `GET` — verify removal
6. `POST /policies/:id/hosts` — add hosts
7. `POST` — idempotent add (already assigned)
8. `DELETE` — idempotent remove (non-existent host)
9. `PUT` — clear all (empty list) *[skipped with --no-cleanup]*
10. `GET` — verify empty *[skipped with --no-cleanup]*

**Query host assignments** (tests 11-16):

11. `PUT /queries/:id/hosts` — replace
12. `GET /queries/:id/hosts` — list
13. `DELETE /queries/:id/hosts` — remove one
14. `POST /queries/:id/hosts` — add back
15. `PUT` — clear all *[skipped with --no-cleanup]*
16. `GET` — verify empty *[skipped with --no-cleanup]*

### Auto-creation

If no policies or queries exist, the script automatically creates:
- A policy named `test-host-assignment-policy` with query `SELECT 1;`
- A query named `test-host-assignment-query` with query `SELECT 1;`

### Examples

```bash
# Full test run with cleanup
FLEET_URL=http://localhost:8080 bash openframe/scripts/test_host_assignments.sh

# Verbose, keep data for inspection
FLEET_URL=http://localhost:8080 bash openframe/scripts/test_host_assignments.sh --no-cleanup --verbose

# Target specific resources
FLEET_URL=http://localhost:8080 bash openframe/scripts/test_host_assignments.sh \
  --policy-id 5 --query-id 10 --host-ids "1,2,3"
```
