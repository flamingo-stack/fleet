package mysql

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenframeEnsureCdcPrivileges covers the grant `fleet prepare db` issues for Debezium: the
// account ends up with exactly the two replication privileges the connector needs and none of the
// ones managed MySQL cannot provide, and re-running is a no-op. This is the in-code replacement
// for the standalone privileged Job, so the grant set is the assertion that matters.
// Runs only under MYSQL_TEST=1.
func TestOpenframeEnsureCdcPrivileges(t *testing.T) {
	ds := CreateMySQLDS(t)
	ctx := context.Background()

	// Stand in for Fleet's own app user: accounts are server-global, so create and drop it here.
	const username = "openframe_cdc_test"
	_, err := ds.writer(ctx).ExecContext(ctx, "CREATE USER IF NOT EXISTS 'openframe_cdc_test'@'%' IDENTIFIED BY 'pw'")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = ds.writer(ctx).ExecContext(ctx, "DROP USER IF EXISTS 'openframe_cdc_test'@'%'")
	})

	require.NoError(t, ds.EnsureOpenframeCdcPrivileges(ctx, username))

	grants := showGrantsFor(t, ds, username)
	require.Contains(t, grants, "REPLICATION CLIENT", "Debezium needs the binlog position")
	require.Contains(t, grants, "REPLICATION SLAVE", "Debezium needs to stream the binlog")
	// Cloud SQL grants neither, so the connector runs with snapshot.locking.mode=none everywhere
	// and must never depend on them being present.
	require.NotContains(t, grants, "RELOAD")
	require.NotContains(t, grants, "FLUSH_TABLES")
	require.NotContains(t, grants, "ALL PRIVILEGES")

	// Idempotent: GRANT re-issues rather than accumulates, so a second run is a no-op.
	require.NoError(t, ds.EnsureOpenframeCdcPrivileges(ctx, username))
	require.Equal(t, grants, showGrantsFor(t, ds, username))
}

// TestOpenframeEnsureCdcPrivilegesRejectsInvalidInput verifies an account name cannot break out of
// the GRANT. The name is operator config (Fleet's own MySQL user), so it is escaped rather than
// constrained to a character set — any legal account name has to work. A hostile name therefore
// reaches MySQL as one literal, matches no account, and fails without granting anything.
func TestOpenframeEnsureCdcPrivilegesRejectsInvalidInput(t *testing.T) {
	ds := CreateMySQLDS(t)
	ctx := context.Background()

	require.Error(t, ds.EnsureOpenframeCdcPrivileges(ctx, ""),
		"an empty username must be refused before a statement is built")

	for name, username := range map[string]string{
		"quote":              `bad'user`,
		"backslash":          `bad\user`,
		"space":              "bad user",
		"statement breakout": `x'@'%'; GRANT ALL PRIVILEGES ON *.* TO 'x'@'%`,
		"too long":           strings.Repeat("u", 33),
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, ds.EnsureOpenframeCdcPrivileges(ctx, username))
		})
	}

	// The decisive assertion: no account the injected string tried to name exists, so nothing was
	// granted to one. (MySQL 8 will not create an account via GRANT, so a name that matches nothing
	// fails loudly.)
	var accounts int
	require.NoError(t, ds.writer(ctx).GetContext(ctx, &accounts,
		"SELECT COUNT(*) FROM mysql.user WHERE user LIKE 'bad%' OR user LIKE 'x%'"))
	require.Zero(t, accounts)
}

// TestOpenframeQuoting pins the escaping the CDC grant depends on. It needs no database: this
// helper is the only thing standing between a values-supplied account name and a statement MySQL
// parses, so it is worth checking on every run, not only under MYSQL_TEST=1.
func TestOpenframeQuoting(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"", "''"},
		{"fleet", "'fleet'"},
		{`p'w`, `'p''w'`},
		{`p\w`, `'p\\w'`},
		// A value ending in a backslash is the classic break-out: unescaped it would consume the
		// closing quote and let the rest be parsed as SQL.
		{`pw\`, `'pw\\'`},
		{`' OR 1=1 -- `, `''' OR 1=1 -- '`},
	} {
		require.Equal(t, tc.want, openframeQuoteString(tc.in), "quoting %q", tc.in)
	}
}

func showGrantsFor(t *testing.T, ds *Datastore, username string) string {
	t.Helper()
	var grants []string
	require.NoError(t, ds.writer(t.Context()).SelectContext(t.Context(), &grants,
		fmt.Sprintf("SHOW GRANTS FOR %s@'%%'", openframeQuoteString(username))))
	return strings.Join(grants, "\n")
}
