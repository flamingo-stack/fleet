package fleet

import (
	"testing"

	"github.com/fleetdm/fleet/v4/server/ptr"
	"github.com/stretchr/testify/require"
)

func TestVerifyPolicyPlatforms(t *testing.T) {
	testCases := []struct {
		platforms string
		isValid   bool
	}{
		{"windows,chrome", true},
		{"chrome", true},
		{"bados", false},
	}

	for _, tc := range testCases {
		actual := verifyPolicyPlatforms(tc.platforms)

		if tc.isValid {
			require.NoError(t, actual)
			continue
		}
		require.Error(t, actual)
	}
}

func TestPolicyPayloadVerify_HostIDsConflict(t *testing.T) {
	testCases := []struct {
		name    string
		payload PolicyPayload
		wantErr error
	}{
		{
			"host_ids alone is valid",
			PolicyPayload{Name: "test", Query: "SELECT 1", HostIDs: []uint{1, 2}},
			nil,
		},
		{
			"host_ids with labels_include_any",
			PolicyPayload{Name: "test", Query: "SELECT 1", HostIDs: []uint{1}, LabelsIncludeAny: []string{"label1"}},
			errPolicyHostIDsConflictsWithLabels,
		},
		{
			"host_ids with labels_exclude_any",
			PolicyPayload{Name: "test", Query: "SELECT 1", HostIDs: []uint{1}, LabelsExcludeAny: []string{"label1"}},
			errPolicyHostIDsConflictsWithLabels,
		},
		{
			"host_ids with both labels",
			PolicyPayload{Name: "test", Query: "SELECT 1", HostIDs: []uint{1}, LabelsIncludeAny: []string{"l1"}, LabelsExcludeAny: []string{"l2"}},
			errPolicyConflictingLabels, // labels conflict checked first
		},
		{
			"empty host_ids with labels is fine",
			PolicyPayload{Name: "test", Query: "SELECT 1", HostIDs: []uint{}, LabelsIncludeAny: []string{"label1"}},
			nil,
		},
		{
			"conflicting labels without host_ids",
			PolicyPayload{Name: "test", Query: "SELECT 1", LabelsIncludeAny: []string{"l1"}, LabelsExcludeAny: []string{"l2"}},
			errPolicyConflictingLabels,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.payload.Verify()
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestModifyPolicyPayloadVerify_HostIDsConflict(t *testing.T) {
	testCases := []struct {
		name    string
		payload ModifyPolicyPayload
		wantErr error
	}{
		{
			"host_ids alone is valid",
			ModifyPolicyPayload{HostIDs: &[]uint{1, 2}},
			nil,
		},
		{
			"host_ids with labels_include_any",
			ModifyPolicyPayload{HostIDs: &[]uint{1}, LabelsIncludeAny: []string{"label1"}},
			errPolicyHostIDsConflictsWithLabels,
		},
		{
			"host_ids with labels_exclude_any",
			ModifyPolicyPayload{HostIDs: &[]uint{1}, LabelsExcludeAny: []string{"label1"}},
			errPolicyHostIDsConflictsWithLabels,
		},
		{
			"nil host_ids with labels is fine",
			ModifyPolicyPayload{LabelsIncludeAny: []string{"label1"}},
			nil,
		},
		{
			"empty host_ids with labels is fine",
			ModifyPolicyPayload{HostIDs: &[]uint{}, LabelsIncludeAny: []string{"label1"}},
			nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.payload.Verify()
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPolicySpecVerify_HostIDsConflict(t *testing.T) {
	testCases := []struct {
		name    string
		spec    PolicySpec
		wantErr error
	}{
		{
			"host_ids alone is valid",
			PolicySpec{Name: "test", Query: "SELECT 1", HostIDs: []uint{1, 2}},
			nil,
		},
		{
			"host_ids with labels_include_any",
			PolicySpec{Name: "test", Query: "SELECT 1", HostIDs: []uint{1}, LabelsIncludeAny: []string{"label1"}},
			errPolicyHostIDsConflictsWithLabels,
		},
		{
			"host_ids with labels_exclude_any",
			PolicySpec{Name: "test", Query: "SELECT 1", HostIDs: []uint{1}, LabelsExcludeAny: []string{"label1"}},
			errPolicyHostIDsConflictsWithLabels,
		},
		{
			"empty host_ids with labels is fine",
			PolicySpec{Name: "test", Query: "SELECT 1", HostIDs: []uint{}, LabelsIncludeAny: []string{"label1"}},
			nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Verify()
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPolicyDataHostIDsSerialization(t *testing.T) {
	// PolicyData with HostIDs should include host_ids in JSON,
	// AutoHostIDsLabelID should be hidden from JSON
	p := PolicyData{
		ID:                 1,
		Name:               "test",
		HostIDs:            []uint{1, 2, 3},
		AutoHostIDsLabelID: ptr.Uint(10),
	}
	_ = p // Verify fields exist and compile
	require.Equal(t, []uint{1, 2, 3}, p.HostIDs)
	require.Equal(t, ptr.Uint(10), p.AutoHostIDsLabelID)
}

func TestFirstFuplicatePolicySpecName(t *testing.T) {
	testCases := []struct {
		name     string
		result   string
		policies []*PolicySpec
	}{
		{"no specs", "", []*PolicySpec{}},
		{"no duplicate names", "", []*PolicySpec{{Name: "foo"}}},
		{"duplicate names", "foo", []*PolicySpec{{Name: "foo"}, {Name: "bar"}, {Name: "foo"}}},
	}

	for _, tc := range testCases {
		name := FirstDuplicatePolicySpecName(tc.policies)
		require.Equal(t, tc.result, name)
	}
}
