// OPENFRAME(managed-policies): verifies that `policies.openframe_managed` keeps a policy out of the list and
// count paths while leaving by-id reads intact — openframe/docs/managed-policies.md
//
// IMPORTANT: the datastore test harness loads server/datastore/mysql/schema.sql, dumped from the
// UPSTREAM tables/ migrations only, so it has no `openframe_managed` column. Every test here calls
// ds.MigrateOpenframe(ctx) first (see migrations_openframe_test.go).
//
// OpenFrame mode is switched on through the context, never through FLEET_OPENFRAME_MODE:
// CreateMySQLDS marks the test parallel, which makes t.Setenv panic, and a process-wide
// os.Setenv would leak the mode into every test running alongside.
package mysql

import (
	"context"
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/stretchr/testify/require"
)

func TestOpenframeManagedPolicies(t *testing.T) {
	ds := CreateMySQLDS(t)
	ctx := context.Background()

	require.NoError(t, ds.MigrateOpenframe(ctx))
	ctx = fleet.NewOpenframeModeContext(ctx, true)

	team, err := ds.NewTeam(ctx, &fleet.Team{Name: "openframe-managed"})
	require.NoError(t, err)

	visible, err := ds.NewTeamPolicy(ctx, team.ID, nil, fleet.PolicyPayload{
		Name:  "openframe-managed-visible",
		Query: "SELECT 1",
	})
	require.NoError(t, err)
	require.False(t, visible.OpenframeManaged)

	managed, err := ds.NewTeamPolicy(ctx, team.ID, nil, fleet.PolicyPayload{
		Name:             "openframe-managed-platform",
		Query:            "SELECT 1",
		OpenframeManaged: true,
	})
	require.NoError(t, err)
	require.True(t, managed.OpenframeManaged, "create must round-trip the openframe_managed flag")

	policyIDs := func(policies []*fleet.Policy) []uint {
		ids := make([]uint, 0, len(policies))
		for _, p := range policies {
			ids = append(ids, p.ID)
		}
		return ids
	}

	t.Run("excluded from listings and counts", func(t *testing.T) {
		teamPolicies, _, err := ds.ListTeamPolicies(ctx, team.ID, fleet.ListOptions{}, fleet.ListOptions{}, "")
		require.NoError(t, err)
		require.Equal(t, []uint{visible.ID}, policyIDs(teamPolicies))

		merged, err := ds.ListMergedTeamPolicies(ctx, team.ID, fleet.ListOptions{}, "")
		require.NoError(t, err)
		require.Equal(t, []uint{visible.ID}, policyIDs(merged))

		count, err := ds.CountPolicies(ctx, &team.ID, "", "")
		require.NoError(t, err)
		require.Equal(t, 1, count, "OpenFrame-managed policies must not inflate the paging count")

		mergedCount, err := ds.CountMergedTeamPolicies(ctx, team.ID, "", "")
		require.NoError(t, err)
		require.Equal(t, 1, mergedCount)
	})

	t.Run("still readable by id", func(t *testing.T) {
		got, err := ds.Policy(ctx, managed.ID)
		require.NoError(t, err)
		require.True(t, got.OpenframeManaged)
	})

	t.Run("unhiding brings it back", func(t *testing.T) {
		got, err := ds.Policy(ctx, managed.ID)
		require.NoError(t, err)

		got.OpenframeManaged = false
		require.NoError(t, ds.SavePolicy(ctx, got, false, false))

		teamPolicies, _, err := ds.ListTeamPolicies(ctx, team.ID, fleet.ListOptions{}, fleet.ListOptions{}, "")
		require.NoError(t, err)
		require.ElementsMatch(t, []uint{visible.ID, managed.ID}, policyIDs(teamPolicies))

		got.OpenframeManaged = true
		require.NoError(t, ds.SavePolicy(ctx, got, false, false))

		teamPolicies, _, err = ds.ListTeamPolicies(ctx, team.ID, fleet.ListOptions{}, fleet.ListOptions{}, "")
		require.NoError(t, err)
		require.Equal(t, []uint{visible.ID}, policyIDs(teamPolicies))
	})

	t.Run("inert when openframe mode is off", func(t *testing.T) {
		offCtx := fleet.NewOpenframeModeContext(ctx, false)

		teamPolicies, _, err := ds.ListTeamPolicies(offCtx, team.ID, fleet.ListOptions{}, fleet.ListOptions{}, "")
		require.NoError(t, err)
		require.ElementsMatch(t, []uint{visible.ID, managed.ID}, policyIDs(teamPolicies),
			"the exclusion must not apply outside OpenFrame mode, where the column may not exist")

		count, err := ds.CountPolicies(offCtx, &team.ID, "", "")
		require.NoError(t, err)
		require.Equal(t, 2, count)
	})
}

func TestOpenframeManagedPoliciesGlobal(t *testing.T) {
	ds := CreateMySQLDS(t)
	ctx := context.Background()

	require.NoError(t, ds.MigrateOpenframe(ctx))
	ctx = fleet.NewOpenframeModeContext(ctx, true)

	visible, err := ds.NewGlobalPolicy(ctx, nil, fleet.PolicyPayload{
		Name:  "openframe-managed-global-visible",
		Query: "SELECT 1",
	})
	require.NoError(t, err)

	_, err = ds.NewGlobalPolicy(ctx, nil, fleet.PolicyPayload{
		Name:             "openframe-managed-global-platform",
		Query:            "SELECT 1",
		OpenframeManaged: true,
	})
	require.NoError(t, err)

	policies, err := ds.ListGlobalPolicies(ctx, fleet.ListOptions{})
	require.NoError(t, err)
	require.Len(t, policies, 1)
	require.Equal(t, visible.ID, policies[0].ID)

	count, err := ds.CountPolicies(ctx, nil, "", "")
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
