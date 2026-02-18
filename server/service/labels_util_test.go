package service

import (
	"context"
	"testing"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/mock"
	"github.com/fleetdm/fleet/v4/server/ptr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoLabelName(t *testing.T) {
	tests := []struct {
		entityType string
		entityID   uint
		expected   string
	}{
		{"policy", 1, "__fleet_host_target_policy_1"},
		{"policy", 999, "__fleet_host_target_policy_999"},
		{"query", 42, "__fleet_host_target_query_42"},
		{"query", 0, "__fleet_host_target_query_0"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, autoLabelName(tt.entityType, tt.entityID))
	}
}

func TestCreateOrUpdateAutoLabel_NewLabel(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	// LabelByName returns not found → triggers creation
	ds.LabelByNameFunc = func(ctx context.Context, name string, filter fleet.TeamFilter) (*fleet.Label, error) {
		return nil, &notFoundError{}
	}
	ds.NewLabelFunc = func(ctx context.Context, label *fleet.Label, opts ...fleet.OptionalArg) (*fleet.Label, error) {
		assert.Equal(t, "__fleet_host_target_policy_5", label.Name)
		assert.True(t, label.Hidden)
		assert.Equal(t, fleet.LabelMembershipTypeManual, label.LabelMembershipType)
		assert.Equal(t, fleet.LabelTypeRegular, label.LabelType)
		label.ID = 10
		return label, nil
	}
	ds.UpdateLabelMembershipByHostIDsFunc = func(ctx context.Context, label fleet.Label, hostIDs []uint, filter fleet.TeamFilter) (*fleet.Label, []uint, error) {
		assert.Equal(t, uint(10), label.ID)
		assert.Equal(t, []uint{1, 2, 3}, hostIDs)
		return &label, hostIDs, nil
	}

	name, id, err := createOrUpdateAutoLabel(ctx, ds, "policy", 5, []uint{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, "__fleet_host_target_policy_5", name)
	assert.Equal(t, uint(10), id)
	assert.True(t, ds.NewLabelFuncInvoked)
	assert.True(t, ds.UpdateLabelMembershipByHostIDsFuncInvoked)
}

func TestCreateOrUpdateAutoLabel_ExistingLabel(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	// LabelByName finds an existing label
	ds.LabelByNameFunc = func(ctx context.Context, name string, filter fleet.TeamFilter) (*fleet.Label, error) {
		return &fleet.Label{ID: 20, Name: name}, nil
	}
	ds.UpdateLabelMembershipByHostIDsFunc = func(ctx context.Context, label fleet.Label, hostIDs []uint, filter fleet.TeamFilter) (*fleet.Label, []uint, error) {
		assert.Equal(t, uint(20), label.ID)
		assert.Equal(t, []uint{4, 5}, hostIDs)
		return &label, hostIDs, nil
	}

	name, id, err := createOrUpdateAutoLabel(ctx, ds, "query", 7, []uint{4, 5})
	require.NoError(t, err)
	assert.Equal(t, "__fleet_host_target_query_7", name)
	assert.Equal(t, uint(20), id)
	// NewLabel should NOT have been called
	assert.False(t, ds.NewLabelFuncInvoked)
}

func TestCreateOrUpdateAutoLabel_EmptyHostIDs(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	ds.LabelByNameFunc = func(ctx context.Context, name string, filter fleet.TeamFilter) (*fleet.Label, error) {
		return nil, &notFoundError{}
	}
	ds.NewLabelFunc = func(ctx context.Context, label *fleet.Label, opts ...fleet.OptionalArg) (*fleet.Label, error) {
		label.ID = 30
		return label, nil
	}

	// With empty hostIDs, UpdateLabelMembershipByHostIDs should not be called
	name, id, err := createOrUpdateAutoLabel(ctx, ds, "policy", 1, []uint{})
	require.NoError(t, err)
	assert.Equal(t, "__fleet_host_target_policy_1", name)
	assert.Equal(t, uint(30), id)
	assert.False(t, ds.UpdateLabelMembershipByHostIDsFuncInvoked)
}

func TestDeleteAutoLabel_NilLabelID(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	// Should be a no-op
	err := deleteAutoLabel(ctx, ds, nil)
	require.NoError(t, err)
	assert.False(t, ds.LabelFuncInvoked)
}

func TestDeleteAutoLabel_Success(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	ds.LabelFunc = func(ctx context.Context, lid uint, filter fleet.TeamFilter) (*fleet.LabelWithTeamName, []uint, error) {
		return &fleet.LabelWithTeamName{Label: fleet.Label{ID: lid, Name: "__fleet_host_target_policy_5"}}, nil, nil
	}
	ds.DeleteLabelFunc = func(ctx context.Context, name string, filter fleet.TeamFilter) error {
		assert.Equal(t, "__fleet_host_target_policy_5", name)
		return nil
	}

	err := deleteAutoLabel(ctx, ds, ptr.Uint(5))
	require.NoError(t, err)
	assert.True(t, ds.LabelFuncInvoked)
	assert.True(t, ds.DeleteLabelFuncInvoked)
}

func TestDeleteAutoLabel_AlreadyDeleted(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	ds.LabelFunc = func(ctx context.Context, lid uint, filter fleet.TeamFilter) (*fleet.LabelWithTeamName, []uint, error) {
		return nil, nil, &notFoundError{}
	}

	// Should not error if label is already gone
	err := deleteAutoLabel(ctx, ds, ptr.Uint(99))
	require.NoError(t, err)
}

func TestPopulatePolicyHostIDs_NilPolicy(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	err := populatePolicyHostIDs(ctx, ds, nil)
	require.NoError(t, err)
}

func TestPopulatePolicyHostIDs_NoAutoLabel(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	policy := &fleet.Policy{PolicyData: fleet.PolicyData{ID: 1}}
	err := populatePolicyHostIDs(ctx, ds, policy)
	require.NoError(t, err)
	assert.Nil(t, policy.HostIDs)
}

func TestPopulatePolicyHostIDs_WithAutoLabel(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	ds.HostIDsInLabelFunc = func(ctx context.Context, labelID uint) ([]uint, error) {
		assert.Equal(t, uint(10), labelID)
		return []uint{1, 2, 3}, nil
	}

	policy := &fleet.Policy{PolicyData: fleet.PolicyData{
		ID:                 1,
		AutoHostIDsLabelID: ptr.Uint(10),
	}}
	err := populatePolicyHostIDs(ctx, ds, policy)
	require.NoError(t, err)
	assert.Equal(t, []uint{1, 2, 3}, policy.HostIDs)
}

func TestPopulateQueryHostIDs_NilQuery(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	err := populateQueryHostIDs(ctx, ds, nil)
	require.NoError(t, err)
}

func TestPopulateQueryHostIDs_NoAutoLabel(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	query := &fleet.Query{ID: 1}
	err := populateQueryHostIDs(ctx, ds, query)
	require.NoError(t, err)
	assert.Nil(t, query.HostIDs)
}

func TestPopulateQueryHostIDs_WithAutoLabel(t *testing.T) {
	ds := new(mock.Store)
	ctx := context.Background()

	ds.HostIDsInLabelFunc = func(ctx context.Context, labelID uint) ([]uint, error) {
		assert.Equal(t, uint(20), labelID)
		return []uint{5, 6}, nil
	}

	query := &fleet.Query{
		ID:                 1,
		AutoHostIDsLabelID: ptr.Uint(20),
	}
	err := populateQueryHostIDs(ctx, ds, query)
	require.NoError(t, err)
	assert.Equal(t, []uint{5, 6}, query.HostIDs)
}
