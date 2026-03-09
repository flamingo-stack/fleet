# Host Assignment API

Host assignment endpoints allow targeting individual hosts for policies and queries.
All endpoints require `FLEET_OPENFRAME_MODE=1` and return `400 Bad Request` when the mode is disabled.

## Authentication

All requests require a valid API token passed via the `Authorization` header:

```
Authorization: Bearer <token>
```

## Common error responses

| Status | Meaning |
|--------|---------|
| `400` | Openframe mode is not enabled, or invalid request body |
| `401` | Unauthorized — missing or invalid token |
| `403` | Forbidden — insufficient privileges |
| `404` | Policy or query not found |

---

## Policy host assignments

Base path: `/api/v1/fleet/policies/{policy_id}/hosts`

### Add hosts to a policy

Atomically adds hosts to the assignment list. Already-assigned hosts are silently skipped (`INSERT IGNORE`).

```
POST /api/v1/fleet/policies/{policy_id}/hosts
```

**Request body**

```json
{
  "host_ids": [1, 2, 3]
}
```

**Response** `200 OK`

```json
{
  "added": 2
}
```

`added` reflects the number of newly inserted rows (duplicates are not counted).

---

### Remove hosts from a policy

Atomically removes specific hosts from the assignment list. IDs that are not currently assigned are silently ignored.

```
DELETE /api/v1/fleet/policies/{policy_id}/hosts
```

**Request body**

```json
{
  "host_ids": [2, 3]
}
```

**Response** `200 OK`

```json
{
  "removed": 2
}
```

---

### Replace all hosts on a policy

Replaces the entire assignment list in a single transaction (delete all + insert).
Pass an empty array to clear all assignments.

This endpoint is intended for GitOps and bulk-sync workflows where the caller owns the full list.
For interactive / concurrent usage prefer the atomic `POST` (add) and `DELETE` (remove) endpoints.

```
PUT /api/v1/fleet/policies/{policy_id}/hosts
```

**Request body**

```json
{
  "host_ids": [10, 20, 30]
}
```

**Response** `200 OK`

```json
{}
```

To clear all assignments:

```json
{
  "host_ids": []
}
```

---

### List hosts assigned to a policy

Returns hosts assigned to a policy with pagination support.

```
GET /api/v1/fleet/policies/{policy_id}/hosts
```

**Query parameters**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | 0 | Page number (zero-based) |
| `per_page` | int | 20 | Results per page |

**Response** `200 OK`

```json
{
  "hosts": [
    { "id": 1, "hostname": "host-a.local" },
    { "id": 2, "hostname": "host-b.local" }
  ],
  "meta": {
    "has_next_results": true,
    "has_previous_results": false
  }
}
```

When no hosts are assigned, `hosts` is an empty array.

---

## Query host assignments

Base path: `/api/v1/fleet/queries/{query_id}/hosts`

All endpoints mirror the policy host assignment API above.

### Add hosts to a query

```
POST /api/v1/fleet/queries/{query_id}/hosts
```

**Request body**

```json
{
  "host_ids": [1, 2, 3]
}
```

**Response** `200 OK`

```json
{
  "added": 3
}
```

---

### Remove hosts from a query

```
DELETE /api/v1/fleet/queries/{query_id}/hosts
```

**Request body**

```json
{
  "host_ids": [2]
}
```

**Response** `200 OK`

```json
{
  "removed": 1
}
```

---

### Replace all hosts on a query

```
PUT /api/v1/fleet/queries/{query_id}/hosts
```

**Request body**

```json
{
  "host_ids": [10, 20]
}
```

**Response** `200 OK`

```json
{}
```

---

### List hosts assigned to a query

```
GET /api/v1/fleet/queries/{query_id}/hosts
```

**Query parameters**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | 0 | Page number (zero-based) |
| `per_page` | int | 20 | Results per page |

**Response** `200 OK`

```json
{
  "hosts": [
    { "id": 10, "hostname": "server-01.corp" },
    { "id": 20, "hostname": "server-02.corp" }
  ],
  "meta": {
    "has_next_results": false,
    "has_previous_results": false
  }
}
```

---

## Read-only field on parent objects

When openframe mode is enabled, `GET /api/v1/fleet/policies/{id}` and `GET /api/v1/fleet/queries/{id}` include a read-only `hosts_include_any` field in the response:

```json
{
  "id": 5,
  "name": "My policy",
  "hosts_include_any": [
    { "id": 1, "hostname": "host-a.local" },
    { "id": 2, "hostname": "host-b.local" }
  ]
}
```

This field is populated from the `policy_hosts` / `query_hosts` join tables and cannot be modified through the parent create/modify endpoints. Use the dedicated host assignment endpoints to manage assignments.

---

## Concurrency considerations

| Endpoint | Safe for concurrent use? | Notes |
|----------|--------------------------|-------|
| `POST` (add) | Yes | Uses `INSERT IGNORE`; duplicates are no-ops |
| `DELETE` (remove) | Yes | Deletes only specified IDs; missing IDs are no-ops |
| `PUT` (replace) | No | Full delete + insert in a transaction; concurrent replace or add/remove may conflict |
| `GET` (list) | Yes | Read-only |

For UI and multi-user environments, prefer `POST` + `DELETE` for incremental changes.
Reserve `PUT` for single-writer scenarios (GitOps, bulk import, scripts).
