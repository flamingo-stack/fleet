package service

import (
	"context"
	"testing"
	"time"

	"github.com/fleetdm/fleet/v4/server/contexts/viewer"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/mock"
	"github.com/fleetdm/fleet/v4/server/ptr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPolicySpecAuthorization(t *testing.T) {
	t.Run("when team not found", func(t *testing.T) {
		ds := new(mock.Store)
		ds.TeamByNameFunc = func(ctx context.Context, name string) (*fleet.Team, error) {
			return nil, &notFoundError{}
		}

		svc, ctx := newTestService(t, ds, nil, nil)

		req := []*fleet.PolicySpec{
			{
				Team: "some_team",
			},
		}

		user := &fleet.User{GlobalRole: ptr.String(fleet.RoleAdmin)}
		ctx = viewer.NewContext(ctx, viewer.Viewer{User: user})

		actual := svc.ApplyPolicySpecs(ctx, req)
		var expected fleet.NotFoundError

		require.ErrorAs(t, actual, &expected)
	})
}

func TestGlobalPoliciesAuth(t *testing.T) {
	ds := new(mock.Store)
	svc, ctx := newTestService(t, ds, nil, nil)

	ds.NewGlobalPolicyFunc = func(ctx context.Context, authorID *uint, args fleet.PolicyPayload) (*fleet.Policy, error) {
		return &fleet.Policy{}, nil
	}
	ds.ListGlobalPoliciesFunc = func(ctx context.Context, opts fleet.ListOptions) ([]*fleet.Policy, error) {
		return nil, nil
	}
	ds.PoliciesByIDFunc = func(ctx context.Context, ids []uint) (map[uint]*fleet.Policy, error) {
		return nil, nil
	}
	ds.PolicyFunc = func(ctx context.Context, id uint) (*fleet.Policy, error) {
		return &fleet.Policy{
			PolicyData: fleet.PolicyData{
				ID: id,
			},
		}, nil
	}
	ds.DeleteGlobalPoliciesFunc = func(ctx context.Context, ids []uint) ([]uint, error) {
		return nil, nil
	}
	ds.TeamByNameFunc = func(ctx context.Context, name string) (*fleet.Team, error) {
		return &fleet.Team{ID: 1}, nil
	}
	ds.ApplyPolicySpecsFunc = func(ctx context.Context, authorID uint, specs []*fleet.PolicySpec) error {
		return nil
	}
	ds.NewActivityFunc = func(
		ctx context.Context, user *fleet.User, activity fleet.ActivityDetails, details []byte, createdAt time.Time,
	) error {
		return nil
	}
	ds.SavePolicyFunc = func(ctx context.Context, p *fleet.Policy, shouldDeleteAll bool, removePolicyStats bool) error {
		return nil
	}
	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{
			WebhookSettings: fleet.WebhookSettings{
				FailingPoliciesWebhook: fleet.FailingPoliciesWebhookSettings{
					Enable: false,
				},
			},
		}, nil
	}

	testCases := []struct {
		name            string
		user            *fleet.User
		shouldFailWrite bool
		shouldFailRead  bool
	}{
		{
			"global admin",
			&fleet.User{GlobalRole: ptr.String(fleet.RoleAdmin)},
			false,
			false,
		},
		{
			"global maintainer",
			&fleet.User{GlobalRole: ptr.String(fleet.RoleMaintainer)},
			false,
			false,
		},
		{
			"global observer",
			&fleet.User{GlobalRole: ptr.String(fleet.RoleObserver)},
			true,
			false,
		},
		{
			"team admin",
			&fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 1}, Role: fleet.RoleAdmin}}},
			true,
			false,
		},
		{
			"team maintainer",
			&fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 1}, Role: fleet.RoleMaintainer}}},
			true,
			false,
		},
		{
			"team observer",
			&fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 1}, Role: fleet.RoleObserver}}},
			true,
			false,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := viewer.NewContext(ctx, viewer.Viewer{User: tt.user})

			_, err := svc.NewGlobalPolicy(ctx, fleet.PolicyPayload{
				Name:  "query1",
				Query: "select 1;",
			})
			checkAuthErr(t, tt.shouldFailWrite, err)

			_, err = svc.ListGlobalPolicies(ctx, fleet.ListOptions{})
			checkAuthErr(t, tt.shouldFailRead, err)

			_, err = svc.GetPolicyByIDQueries(ctx, 1)
			checkAuthErr(t, tt.shouldFailRead, err)

			_, err = svc.ModifyGlobalPolicy(ctx, 1, fleet.ModifyPolicyPayload{})
			checkAuthErr(t, tt.shouldFailWrite, err)

			_, err = svc.DeleteGlobalPolicies(ctx, []uint{1})
			checkAuthErr(t, tt.shouldFailWrite, err)

			err = svc.ApplyPolicySpecs(ctx, []*fleet.PolicySpec{
				{
					Name:  "query2",
					Query: "select 1;",
				},
			})
			checkAuthErr(t, tt.shouldFailWrite, err)
		})
	}
}

func TestRemoveGlobalPoliciesFromWebhookConfig(t *testing.T) {
	ds := new(mock.Store)
	svc := &Service{ds: ds}

	var storedAppConfig fleet.AppConfig

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &storedAppConfig, nil
	}
	ds.SaveAppConfigFunc = func(ctx context.Context, info *fleet.AppConfig) error {
		storedAppConfig = *info
		return nil
	}

	for _, tc := range []struct {
		name     string
		currCfg  []uint
		toDelete []uint
		expCfg   []uint
	}{
		{
			name:     "delete-one",
			currCfg:  []uint{1},
			toDelete: []uint{1},
			expCfg:   []uint{},
		},
		{
			name:     "delete-all-2",
			currCfg:  []uint{1, 2, 3},
			toDelete: []uint{1, 2, 3},
			expCfg:   []uint{},
		},
		{
			name:     "basic",
			currCfg:  []uint{1, 2, 3},
			toDelete: []uint{1, 2},
			expCfg:   []uint{3},
		},
		{
			name:     "empty-cfg",
			currCfg:  []uint{},
			toDelete: []uint{1},
			expCfg:   []uint{},
		},
		{
			name:     "no-deletion-cfg",
			currCfg:  []uint{1},
			toDelete: []uint{2, 3, 4},
			expCfg:   []uint{1},
		},
		{
			name:     "no-deletion-cfg-2",
			currCfg:  []uint{1},
			toDelete: []uint{},
			expCfg:   []uint{1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storedAppConfig.WebhookSettings.FailingPoliciesWebhook.PolicyIDs = tc.currCfg
			err := svc.removeGlobalPoliciesFromWebhookConfig(context.Background(), tc.toDelete)
			require.NoError(t, err)
			require.Equal(t, tc.expCfg, storedAppConfig.WebhookSettings.FailingPoliciesWebhook.PolicyIDs)
		})
	}
}

// test ApplyPolicySpecsReturnsErrorOnDuplicatePolicyNamesInSpecs
func TestApplyPolicySpecsReturnsErrorOnDuplicatePolicyNamesInSpecs(t *testing.T) {
	ds := new(mock.Store)
	ds.TeamByNameFunc = func(ctx context.Context, name string) (*fleet.Team, error) {
		return nil, &notFoundError{}
	}

	svc, ctx := newTestService(t, ds, nil, nil)

	req := []*fleet.PolicySpec{
		{
			Name:     "query1",
			Query:    "select 1;",
			Platform: "windows",
		},
		{
			Name:     "query1",
			Query:    "select 1;",
			Platform: "windows",
		},
	}

	user := &fleet.User{GlobalRole: ptr.String(fleet.RoleAdmin)}
	ctx = viewer.NewContext(ctx, viewer.Viewer{User: user})

	err := svc.ApplyPolicySpecs(ctx, req)

	badRequestError := &fleet.BadRequestError{}
	require.ErrorAs(t, err, &badRequestError)
	require.Equal(t, "duplicate policy names not allowed", badRequestError.Message)
}

func TestApplyPolicySpecsLabelsValidation(t *testing.T) {
	ds := new(mock.Store)
	ds.NewGlobalPolicyFunc = func(ctx context.Context, authorID *uint, args fleet.PolicyPayload) (*fleet.Policy, error) {
		return &fleet.Policy{}, nil
	}
	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{}, nil
	}
	ds.NewActivityFunc = func(
		ctx context.Context, user *fleet.User, activity fleet.ActivityDetails, details []byte, createdAt time.Time,
	) error {
		return nil
	}
	ds.ApplyPolicySpecsFunc = func(ctx context.Context, authorID uint, specs []*fleet.PolicySpec) error {
		return nil
	}
	ds.LabelsByNameFunc = func(ctx context.Context, names []string, filter fleet.TeamFilter) (map[string]*fleet.Label, error) {
		labels := make(map[string]*fleet.Label, len(names))
		for _, name := range names {
			if name == "foo" {
				labels["foo"] = &fleet.Label{
					Name: "foo",
					ID:   1,
				}
			}
		}
		return labels, nil
	}

	svc, ctx := newTestService(t, ds, nil, nil)

	testAdmin := fleet.User{
		ID:         1,
		Teams:      []fleet.UserTeam{},
		GlobalRole: ptr.String(fleet.RoleAdmin),
	}
	viewerCtx := viewer.NewContext(ctx, viewer.Viewer{User: &testAdmin})

	// Test that a query spec with a label that exists doesn't return an error
	err := svc.ApplyPolicySpecs(viewerCtx, []*fleet.PolicySpec{
		{
			Name:             "test query",
			Query:            "select 1",
			LabelsIncludeAny: []string{"foo"},
			Platform:         "darwin,windows",
		},
	})
	// Check that no error is returned
	require.NoError(t, err)

	// Test that a query spec with a label that doesn't exist returns an error.
	err = svc.ApplyPolicySpecs(viewerCtx, []*fleet.PolicySpec{
		{
			Name:             "test query",
			Query:            "select 1",
			LabelsIncludeAny: []string{"nope"},
			Platform:         "darwin,windows",
		},
	})

	require.Error(t, err)
}

func TestNewGlobalPolicyWithHostIDs(t *testing.T) {
	ds := new(mock.Store)

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{}, nil
	}
	ds.NewActivityFunc = func(ctx context.Context, user *fleet.User, activity fleet.ActivityDetails, details []byte, createdAt time.Time) error {
		return nil
	}

	var savedPolicyAutoLabelID *uint
	ds.NewGlobalPolicyFunc = func(ctx context.Context, authorID *uint, args fleet.PolicyPayload) (*fleet.Policy, error) {
		return &fleet.Policy{PolicyData: fleet.PolicyData{
			ID:    1,
			Name:  args.Name,
			Query: args.Query,
		}}, nil
	}
	ds.LabelByNameFunc = func(ctx context.Context, name string, filter fleet.TeamFilter) (*fleet.Label, error) {
		return nil, &notFoundError{}
	}
	ds.NewLabelFunc = func(ctx context.Context, label *fleet.Label, opts ...fleet.OptionalArg) (*fleet.Label, error) {
		assert.True(t, label.Hidden)
		assert.Contains(t, label.Name, "__fleet_host_target_policy_1")
		label.ID = 100
		return label, nil
	}
	ds.UpdateLabelMembershipByHostIDsFunc = func(ctx context.Context, label fleet.Label, hostIDs []uint, filter fleet.TeamFilter) (*fleet.Label, []uint, error) {
		assert.Equal(t, uint(100), label.ID)
		assert.Equal(t, []uint{10, 20, 30}, hostIDs)
		return &label, hostIDs, nil
	}
	ds.SavePolicyFunc = func(ctx context.Context, p *fleet.Policy, shouldDeleteAll bool, removePolicyStats bool) error {
		savedPolicyAutoLabelID = p.AutoHostIDsLabelID
		assert.NotNil(t, p.AutoHostIDsLabelID)
		assert.Equal(t, uint(100), *p.AutoHostIDsLabelID)
		assert.Len(t, p.LabelsIncludeAny, 1)
		assert.Equal(t, "__fleet_host_target_policy_1", p.LabelsIncludeAny[0].LabelName)
		return nil
	}

	svc, ctx := newTestService(t, ds, nil, nil)
	user := &fleet.User{ID: 1, GlobalRole: ptr.String(fleet.RoleAdmin)}
	ctx = viewer.NewContext(ctx, viewer.Viewer{User: user})

	policy, err := svc.NewGlobalPolicy(ctx, fleet.PolicyPayload{
		Name:    "test policy",
		Query:   "SELECT 1",
		HostIDs: []uint{10, 20, 30},
	})
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, uint(1), policy.ID)
	assert.Equal(t, []uint{10, 20, 30}, policy.HostIDs)
	assert.NotNil(t, savedPolicyAutoLabelID)
	assert.Equal(t, uint(100), *savedPolicyAutoLabelID)
	assert.True(t, ds.NewLabelFuncInvoked)
	assert.True(t, ds.SavePolicyFuncInvoked)
}

func TestNewGlobalPolicyWithHostIDs_ConflictsWithLabels(t *testing.T) {
	ds := new(mock.Store)

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{}, nil
	}

	svc, ctx := newTestService(t, ds, nil, nil)
	user := &fleet.User{ID: 1, GlobalRole: ptr.String(fleet.RoleAdmin)}
	ctx = viewer.NewContext(ctx, viewer.Viewer{User: user})

	// Should fail: host_ids with labels_include_any
	_, err := svc.NewGlobalPolicy(ctx, fleet.PolicyPayload{
		Name:             "test",
		Query:            "SELECT 1",
		HostIDs:          []uint{1},
		LabelsIncludeAny: []string{"label1"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host_ids cannot be used with labels_include_any or labels_exclude_any")
}

func TestDeleteGlobalPoliciesWithHostIDs(t *testing.T) {
	ds := new(mock.Store)

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{
			WebhookSettings: fleet.WebhookSettings{},
		}, nil
	}
	ds.SaveAppConfigFunc = func(ctx context.Context, config *fleet.AppConfig) error {
		return nil
	}
	ds.NewActivityFunc = func(ctx context.Context, user *fleet.User, activity fleet.ActivityDetails, details []byte, createdAt time.Time) error {
		return nil
	}

	autoLabelDeleted := false
	ds.PoliciesByIDFunc = func(ctx context.Context, ids []uint) (map[uint]*fleet.Policy, error) {
		return map[uint]*fleet.Policy{
			1: {PolicyData: fleet.PolicyData{ID: 1, Name: "p1", AutoHostIDsLabelID: ptr.Uint(100)}},
		}, nil
	}
	ds.LabelFunc = func(ctx context.Context, lid uint, filter fleet.TeamFilter) (*fleet.LabelWithTeamName, []uint, error) {
		return &fleet.LabelWithTeamName{Label: fleet.Label{ID: lid, Name: "__fleet_host_target_policy_1"}}, nil, nil
	}
	ds.DeleteLabelFunc = func(ctx context.Context, name string, filter fleet.TeamFilter) error {
		assert.Equal(t, "__fleet_host_target_policy_1", name)
		autoLabelDeleted = true
		return nil
	}
	ds.DeleteGlobalPoliciesFunc = func(ctx context.Context, ids []uint) ([]uint, error) {
		return ids, nil
	}

	svc, ctx := newTestService(t, ds, nil, nil)
	user := &fleet.User{ID: 1, GlobalRole: ptr.String(fleet.RoleAdmin)}
	ctx = viewer.NewContext(ctx, viewer.Viewer{User: user})

	deleted, err := svc.DeleteGlobalPolicies(ctx, []uint{1})
	require.NoError(t, err)
	assert.Equal(t, []uint{1}, deleted)
	assert.True(t, autoLabelDeleted, "auto-label should have been deleted")
}
