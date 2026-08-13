package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

// TestOpenframeAppConfigTeamIsolation verifies the OPENFRAME(mysql-multitenancy) change to the
// app_config read/write: when the request is scoped to a tenant team (via context, or
// FLEET_OPENFRAME_TEAM_ID in production), its config is stored/read under id = team id, so
// different tenants do not share the single app_config row. Runs only under MYSQL_TEST=1.
func TestOpenframeAppConfigTeamIsolation(t *testing.T) {
	ds := CreateMySQLDS(t)
	ctx := context.Background()

	ctx5 := fleet.NewOpenframeTeamContext(ctx, 5)
	ctx6 := fleet.NewOpenframeTeamContext(ctx, 6)

	save := func(c context.Context, orgName string) {
		require.NoError(t, ds.SaveAppConfig(c, &fleet.AppConfig{OrgInfo: fleet.OrgInfo{OrgName: orgName}}))
	}
	readOrg := func(c context.Context) string {
		ac, err := ds.AppConfig(c)
		require.NoError(t, err)
		return ac.OrgInfo.OrgName
	}

	// Each team starts with no config of its own.
	require.Empty(t, readOrg(ctx5))
	require.Empty(t, readOrg(ctx6))

	save(ctx5, "team5-org")
	save(ctx6, "team6-org")

	// Each team reads back its own config, not the other's.
	require.Equal(t, "team5-org", readOrg(ctx5))
	require.Equal(t, "team6-org", readOrg(ctx6))

	// Overwriting team 6 does not affect team 5.
	save(ctx6, "team6-org-v2")
	require.Equal(t, "team5-org", readOrg(ctx5))
	require.Equal(t, "team6-org-v2", readOrg(ctx6))
}

// TestOpenframeAppConfigDefaultsForConfiglessTenant verifies that a pinned tenant with no
// app_config_json row yet (team created, config never saved) reads defaults — not zero-value
// config that would silently disable host users. The team id must be > 1 to avoid the legacy
// singleton row (id = 1).
func TestOpenframeAppConfigDefaultsForConfiglessTenant(t *testing.T) {
	ds := CreateMySQLDS(t)
	ctx := context.Background()

	_, err := ds.NewTeam(ctx, &fleet.Team{Name: "configless-filler"}) // consumes team id 1
	require.NoError(t, err)
	team, err := ds.NewTeam(ctx, &fleet.Team{Name: "configless"})
	require.NoError(t, err)
	require.Greater(t, team.ID, uint(1))
	ctxT := fleet.NewOpenframeTeamContext(ctx, team.ID)

	ac, err := ds.AppConfig(ctxT)
	require.NoError(t, err)
	require.True(t, ac.Features.EnableHostUsers, "defaults must be applied for a config-less tenant")
	require.Equal(t, 24*time.Hour, ac.WebhookSettings.Interval.Duration)
}
