# Orbit node key management

## Overview

Orbit authenticates with the Fleet server using a **node key** — a secret token obtained during enrollment. The node key is stored in two places:

- **In-memory cache** (primary) — authoritative while the process is running
- **On-disk file** (`secret-orbit-node-key.txt`) — persistent cache for surviving restarts

## Enrollment flow

1. Check in-memory cache → return if present
2. Read node key file from disk → cache in memory and return if present
3. Enroll with Fleet server (`POST /api/fleet/orbit/enroll`) → cache in memory, persist to disk

## Re-enrollment (401 handling)

When the server responds with HTTP 401:

1. Clear the in-memory node key
2. Mark the client as unenrolled
3. Best-effort delete of the on-disk file
4. Next request triggers a fresh enrollment (step 3 above)

## Windows file lock resilience

On Windows, `secret-orbit-node-key.txt` may become locked by antivirus, ACLs, or other processes, preventing deletion or overwriting. The following fallback chain handles this:

1. **Direct overwrite** (`WriteFile` with truncate)
2. **Rename fallback** — rename the locked file to `.stale`, create a fresh file, clean up
3. **In-memory only** — if all file operations fail, the key is cached in memory and Orbit continues working. Diagnostic logs are emitted with file/directory stat info and a write probe result.

If Orbit restarts while the file is still locked, it reads the stale key, gets a 401, and re-enrolls — one extra round-trip per restart until the lock is released.

## Diagnostic logs

When file persistence fails, the following diagnostics are logged at WARN/ERROR level:

- OS, architecture, PID
- File stat: size, permissions, modification time
- Directory stat: permissions
- Write probe: whether a temp file can be created in the same directory (distinguishes "file locked" from "directory not writable")

Search logs for `node key file diagnostics start` to find these entries.
