// OPENFRAME(migrations-idempotency): static guard for the fork's "all migrations are idempotent"
// invariant — openframe/docs/migrations.md
//
// The fork rewrote the upstream tables/ and data/ migrations in place to be idempotent, because
// OpenFrame tenants are provisioned against databases that may be partially migrated (restored
// snapshots, re-run Helm migration jobs). Every upstream sync imports brand-new migrations that
// arrive non-idempotent, and a new file is never a merge conflict — so nothing forces a reviewer
// to notice. The upstream-sync runbook's semantic-conflict watchlist asks a human to run a git
// diff and re-patch each new migration by hand.
//
// This test does that check mechanically, with no MySQL or Docker: it scans the migration sources
// for the SQL forms the fork's convention rewrites, so a sync that forgets the sweep fails here
// instead of failing at `fleet prepare db` against a tenant.
//
// It intentionally checks only the three unambiguous, purely textual rules. Guarding an
// ALTER ... ADD COLUMN needs a columnExists()-style check whose correctness depends on the
// statement, which no regex can judge; those stay a review item.
package tables

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// knownNonIdempotentMigrations are pre-existing gaps in the fork's original bulk idempotency pass,
// inherited before this guard existed. They are recorded rather than silently skipped so the list
// can be burned down. Do NOT add to this list to make a new migration pass — patch the migration.
var knownNonIdempotentMigrations = map[string]struct{}{
	"20210818151828_AddJSONKeyValueTable.go":                           {},
	"20211202092042_RemovePolicyHistory.go":                            {},
	"20230602111827_RemoveQueryParamsFromMDMServerURL.go":              {},
	"20230721161508_QueriesDataMigrator.go":                            {},
	"20231215122713_InsertPolicyStatsData.go":                          {},
	"20240222073518_AddCertInfoToNanoCertAssociations.go":              {},
	"20240302111134_AddScriptContentsTableAndRelationships.go":         {},
	"20240607133721_ReconcileSoftwareTitles.go":                        {},
	"20240905200000_UninstallPackages.go":                              {},
	"20241002104104_UpdateUninstallScript.go":                          {},
	"20250217093329_MigratePendingUpcomingActivities.go":               {},
	"20250422095806_AddFleetVariablesTable.go":                         {},
	"20250430112622_CollectFleetVariablesFromExistingAppleProfiles.go": {},
	"20250502222222_AddMdmEnrollTables.go":                             {},
	"20250609102714_AddHostCertificateSourcesTable.go":                 {},
	"20251028140000_CreateTableOSVersionVulnerabilities.go":            {},
	"20260108200708_AddHostLocationSupport.go":                         {},
	"20260316120002_FixMismatchedSoftwareTitles.go":                    {},
	"20260409153714_AddApiEndpointPermissionsTables.go":                {},
	"20260522195226_CreateTableAppConfigurations.go":                   {},
	"20260528201143_AddMDMAndroidCommands.go":                          {},
}

var idempotencyRules = []struct {
	name string
	// want describes the rewrite the fork's convention calls for.
	want string
	re   *regexp.Regexp
}{
	{"CREATE TABLE", "CREATE TABLE IF NOT EXISTS", regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS)?`)},
	{"DROP TABLE", "DROP TABLE IF EXISTS", regexp.MustCompile(`(?i)DROP\s+TABLE\s+(?:IF\s+EXISTS)?`)},
	{"INSERT INTO", "INSERT IGNORE INTO", regexp.MustCompile(`(?i)INSERT\s+(?:IGNORE\s+)?INTO\s`)},
}

// stripComments removes Go line comments and SQL line comments so prose that merely mentions
// "CREATE TABLE" is not mistaken for a statement.
func stripComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, ln := range lines {
		for _, marker := range []string{"//", "--"} {
			if idx := strings.Index(ln, marker); idx >= 0 {
				ln = ln[:idx]
			}
		}
		lines[i] = ln
	}
	return strings.Join(lines, "\n")
}

func TestOpenframeMigrationsAreIdempotent(t *testing.T) {
	var files []string
	for _, dir := range []string{".", filepath.Join("..", "data")} {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err, "reading %s", dir)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			// Only timestamped migration files, not the package's shared helpers.
			if len(name) < 14 || !regexp.MustCompile(`^\d{14}_`).MatchString(name) {
				continue
			}
			files = append(files, filepath.Join(dir, name))
		}
	}
	require.Greater(t, len(files), 500, "expected to find the migration corpus")

	var offenders, staleAllowlist []string
	seen := map[string]struct{}{}

	for _, path := range files {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		src := stripComments(string(raw))
		base := filepath.Base(path)

		var bad []string
		for _, rule := range idempotencyRules {
			for _, m := range rule.re.FindAllString(src, -1) {
				if !strings.EqualFold(strings.Join(strings.Fields(m), " "), rule.want) {
					bad = append(bad, rule.name+" -> use "+rule.want)
					break
				}
			}
		}
		if _, allowed := knownNonIdempotentMigrations[base]; allowed {
			seen[base] = struct{}{}
			if len(bad) == 0 {
				staleAllowlist = append(staleAllowlist, base)
			}
			continue
		}
		if len(bad) > 0 {
			offenders = append(offenders, base+": "+strings.Join(bad, ", "))
		}
	}

	sort.Strings(offenders)
	assert.Empty(t, offenders,
		"non-idempotent migration(s) found. New upstream migrations arrive non-idempotent and produce "+
			"no merge conflict; re-run the idempotency sweep from "+
			"openframe/docs/upstream-sync-conflict-resolution.md and add `// Idempotent migration.`")

	// Keep the allowlist honest: an entry that no longer offends should be deleted.
	sort.Strings(staleAllowlist)
	assert.Empty(t, staleAllowlist, "knownNonIdempotentMigrations entries are now idempotent — remove them")

	for base := range knownNonIdempotentMigrations {
		if _, ok := seen[base]; !ok {
			assert.Fail(t, "allowlisted migration no longer exists", "remove %q from knownNonIdempotentMigrations", base)
		}
	}
}
