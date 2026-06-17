#!/usr/bin/env bash
#
# verify_fork.sh — assert that the OpenFrame fork's invariants survived an upstream sync.
#
# WHY THIS EXISTS
#   This fork tracks fleetdm/fleet. The weekly merge (or a manual one) can *silently*
#   drop fork-owned code: real losses seen in past syncs include fork frontend
#   components being deleted, the fork's .goreleaser configs being clobbered with
#   upstream's, and ~40 upstream-only workflows being re-introduced. A green build does
#   NOT catch these (they compile fine; they're just the wrong / missing files).
#
#   This script codifies the manual post-sync verification into one fast command. Run it
#   on the sync branch before merging. Non-zero exit == a fork invariant is missing.
#
# USAGE
#   bash openframe/scripts/verify_fork.sh                 # check the working tree (default)
#   bash openframe/scripts/verify_fork.sh --ref <gitref>  # check a ref without checking it out
#   bash openframe/scripts/verify_fork.sh --check-openapi # also smoke-test gen_openapi.py
#   bash openframe/scripts/verify_fork.sh -h
#
# MAINTENANCE
#   The expected files/markers/counts below are the fork's footprint as of the sync that
#   added this script. When the fork legitimately adds or moves a feature, update the
#   corresponding list here. (A future improvement is to drive these from a machine-readable
#   manifest — see openframe/docs and the "Track 5" recommendation in the sync plan.)
#
set -uo pipefail

# ---- args -------------------------------------------------------------------
REF=""
CHECK_OPENAPI=0
while [ $# -gt 0 ]; do
  case "$1" in
    --ref) REF="${2:-}"; shift 2 || { echo "--ref needs a value" >&2; exit 2; } ;;
    --ref=*) REF="${1#*=}"; shift ;;
    --check-openapi) CHECK_OPENAPI=1; shift ;;
    -h|--help) sed -n '2,28p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

# ---- locate repo root (script lives in openframe/scripts/) -------------------
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)" || exit 2
cd "$REPO_ROOT" || exit 2

if [ -n "$REF" ] && ! git rev-parse --verify --quiet "$REF^{commit}" >/dev/null; then
  echo "fatal: --ref '$REF' is not a valid commit" >&2
  exit 2
fi

# ---- output helpers (color only on a tty) -----------------------------------
if [ -t 1 ]; then C_G=$'\033[32m'; C_R=$'\033[31m'; C_Y=$'\033[33m'; C_0=$'\033[0m'; else C_G=; C_R=; C_Y=; C_0=; fi
FAILS=0
pass()    { printf '  %sPASS%s %s\n' "$C_G" "$C_0" "$1"; }
fail()    { printf '  %sFAIL%s %s\n' "$C_R" "$C_0" "$1"; FAILS=$((FAILS + 1)); }
note()    { printf '  %sNOTE%s %s\n' "$C_Y" "$C_0" "$1"; }
section() { printf '\n== %s ==\n' "$1"; }

# ---- primitives that respect $REF (working tree vs git ref) ------------------
# blob_nonempty <path> : true if the path exists and is non-empty
blob_nonempty() {
  if [ -n "$REF" ]; then
    local sz; sz="$(git cat-file -s "$REF:$1" 2>/dev/null)" || return 1
    [ "${sz:-0}" -gt 0 ]
  else
    [ -s "$1" ]
  fi
}
# count_matches <ere> <pathspec...> : number of matching lines across the pathspecs
count_matches() {
  local pat="$1"; shift
  if [ -n "$REF" ]; then
    git grep -hIE "$pat" "$REF" -- "$@" 2>/dev/null | wc -l | tr -d ' '
  else
    grep -rhIE "$pat" "$@" 2>/dev/null | wc -l | tr -d ' '
  fi
}
# list_workflow_basenames : basenames of files under .github/workflows/
list_workflow_basenames() {
  if [ -n "$REF" ]; then
    git ls-tree -r --name-only "$REF" -- .github/workflows/ 2>/dev/null | sed 's#.*/##'
  else
    { ls .github/workflows/ 2>/dev/null; }
  fi | grep -E '\.(yml|yaml)$' || true
}

# ---- check builders ---------------------------------------------------------
require_file() {  # <path>
  if blob_nonempty "$1"; then pass "file present: $1"; else fail "MISSING fork file: $1"; fi
}
require_min() {   # <label> <ere> <min> <pathspec...>
  local label="$1" pat="$2" min="$3"; shift 3
  local n; n="$(count_matches "$pat" "$@")"; n="${n:-0}"
  if [ "$n" -ge "$min" ]; then pass "$label ($n ≥ $min)"; else fail "$label: found $n, expected ≥ $min"; fi
}

printf 'verify_fork.sh — checking %s\n' "${REF:-working tree ($(git rev-parse --short HEAD 2>/dev/null))}"

# =============================================================================
section "1. Fork-owned backend files exist"
require_file server/fleet/openframe.go
require_file pkg/openframe/openframe-encryption-service.go
require_file pkg/openframe/openframe-token-extractor.go
require_file pkg/openframe/openframe_authorization_manager.go
require_file pkg/openframe/openframe_token_refresher.go
require_file server/datastore/mysql/migrations/openframe/migration.go
require_file server/datastore/mysql/migrations/openframe/20260301000001_AddPolicyHostsJoinTable.go
require_file server/datastore/mysql/migrations/openframe/20260301000002_AddQueryHostsJoinTable.go

# =============================================================================
section "2. Fork-owned frontend files exist (these were dropped by a past merge)"
require_file frontend/components/EmptyTable/EmptyTable.tsx
require_file frontend/components/GenericMsgWithNavButton/GenericMsgWithNavButton.tsx
require_file frontend/components/InheritedBadge/InheritedBadge.tsx
require_file frontend/interfaces/empty_table.ts

# =============================================================================
section "3. Flagship feature: host-level targeting (policy_hosts / query_hosts)"
# 8 = Add/Remove/Replace/List x Policy/Query
require_min "datastore host-assignment impls" \
  'func \(ds \*Datastore\) (Add|Remove|Replace|List)(Policy|Query)Hosts\(' 8 server/datastore/mysql
require_min "service host-assignment impls" \
  'func \(svc \*Service\) (Add|Remove|Replace|List)(Policy|Query)Hosts\(' 8 server/service
require_min "datastore interface decls" \
  '(Add|Remove|Replace|List)(Policy|Query)Hosts\(ctx' 8 server/fleet/datastore.go
require_min "service interface decls" \
  '(Add|Remove|Replace|List)(Policy|Query)Hosts\(ctx' 8 server/fleet/service.go
require_min "host-assignment routes registered" \
  '"/api/_version_/fleet/(policies|queries)/\{[^}]+\}/hosts"' 8 server/service/handler.go

# =============================================================================
section "4. Other fork features (presence)"
require_min "IsOpenframeMode gate"          'IsOpenframeMode'               1 server cmd client orbit pkg
require_min "MigrateOpenframe wiring"        'MigrateOpenframe'             1 cmd/fleet/prepare.go
require_min "openframe migration client"     'MigrationClient'             1 server/datastore/mysql/migrations/openframe
require_min "policy_hosts join table"        'policy_hosts'                1 server
require_min "query_hosts join table"         'query_hosts'                 1 server
require_min "query_results TTL cron"          'QueryResultsTTL'            1 server cmd
require_min "per-tenant Redis KeyPrefix"      'KeyPrefix'                  1 server/config/config.go
require_min "orbit openframe-mode flag"       'openframe-mode'             1 orbit/cmd/orbit/orbit.go
require_min "openframe agent auth pkg"        'OpenFrameAuthorizationManager' 1 pkg/openframe

# =============================================================================
section "4b. Fork integration wiring (block-deletion guards)"
# Marker COUNTS miss a deleted block when the marker survives elsewhere in the file.
# Each line below guards a specific wiring that a past merge silently dropped (the orbit
# 'uuid' subcommand, the per-tenant Redis KeyPrefix pool wiring, the query_results TTL
# schedule registration, and Query.Clone's host-targeting deep-copy). Assert the exact wiring.
require_min "orbit 'uuid' subcommand (def + registration)" 'uuidCommand'                    2 orbit/cmd/orbit/orbit.go
require_min "orbit getHostUUID helper"                     'func getHostUUID'               1 orbit/cmd/orbit/orbit.go
require_min "redis pool per-tenant KeyPrefix wiring"       'cfg\.KeyPrefix'                 1 cmd/fleet/redis.go
require_min "query_results TTL schedule (def + register)"  'newQueryResultsTTLCleanupSchedule' 2 cmd/fleet
require_min "Query.Clone copies host targeting"            'clone\.HostsIncludeAny'         1 server/fleet/queries.go

# =============================================================================
section "5. CI/build configs are the fork's (not clobbered by upstream)"
require_file .goreleaser.yml
require_file .goreleaser-snapshot.yml
# The fork templates its image registry; upstream hardcodes fleetdm/fleet. If this token
# vanishes, the merge replaced our goreleaser with upstream's.
require_min "fork goreleaser image template" '\.Env\.(REGISTRY|GITHUB_REPOSITORY)' 1 .goreleaser.yml
require_file charts/fleet/templates/configmap.yaml
require_file charts/fleet/templates/job-migration.yaml
require_file charts/fleet/templates/cron-vulnprocessing.yaml

# =============================================================================
section "6. Only the fork's workflows exist (a merge must not re-introduce upstream's)"
ALLOWED_WORKFLOWS=" changes.yml release.yml sync-upstream.yml test.yml "
for wf in changes.yml release.yml sync-upstream.yml test.yml; do
  require_file ".github/workflows/$wf"
done
extra_wf=0
while IFS= read -r wf; do
  [ -z "$wf" ] && continue
  case "$ALLOWED_WORKFLOWS" in
    *" $wf "*) : ;;
    *) fail "upstream workflow re-introduced by merge: .github/workflows/$wf"; extra_wf=$((extra_wf + 1)) ;;
  esac
done <<EOF
$(list_workflow_basenames)
EOF
[ "$extra_wf" -eq 0 ] && pass "no unexpected (upstream) workflows present"

# =============================================================================
section "7. Migrations (informational — review after a sync)"
if [ -n "$REF" ]; then
  note "duplicate-migration scan skipped in --ref mode (needs working tree)"
else
  # See openframe/docs/migrations.md: collisions appear when an upstream sync adds a
  # migration whose name already exists under a different timestamp. Test files excluded.
  dups="$(ls server/datastore/mysql/migrations/tables/*.go 2>/dev/null \
            | grep -v '_test\.go$' \
            | sed -E 's#.*/[0-9]+_##; s#\.go$##' | sort | uniq -d)"
  if [ -n "$dups" ]; then
    note "duplicate-named migrations in tables/ — confirm these are known/handled, not a new fork/upstream collision:"
    printf '%s\n' "$dups" | sed 's/^/         /'
  else
    pass "no duplicate-named migrations in tables/"
  fi
fi

# =============================================================================
if [ "$CHECK_OPENAPI" -eq 1 ]; then
  section "8. OpenAPI generator still runs against handler.go"
  if [ -n "$REF" ]; then
    note "skipped in --ref mode (generator runs against the working tree)"
  elif ! command -v python3 >/dev/null 2>&1; then
    note "python3 not found — skipping gen_openapi.py smoke test"
  else
    spec="openframe/docs/fleet-openapi.yaml"
    tracked=1; git ls-files --error-unmatch "$spec" >/dev/null 2>&1 || tracked=0
    if python3 openframe/scripts/gen_openapi.py >/dev/null 2>&1; then
      pass "gen_openapi.py ran successfully (route table parses)"
      if [ "$tracked" -eq 1 ]; then
        git diff --quiet -- "$spec" && pass "committed OpenAPI spec is up to date" \
          || fail "$spec is stale — commit the regenerated spec"
      fi
    else
      fail "gen_openapi.py failed — upstream may have changed the handler.go route format"
    fi
    # leave the tree clean if the generated spec is not tracked
    [ "$tracked" -eq 0 ] && rm -f "$spec"
  fi
fi

# =============================================================================
printf '\n'
if [ "$FAILS" -eq 0 ]; then
  printf '%sOK%s — all fork invariants present.\n' "$C_G" "$C_0"
  exit 0
else
  printf '%s%d FORK INVARIANT(S) MISSING%s — see FAIL lines above. Do not merge until resolved.\n' "$C_R" "$FAILS" "$C_0"
  exit 1
fi
