package service

import (
	"context"
	"fmt"

	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/ptr"
)

func loadLabelsFromNames(ctx context.Context, ds fleet.Datastore, labelNames []string, filter fleet.TeamFilter) (map[string]*fleet.Label, error) {
	labelsMap, err := ds.LabelsByName(ctx, labelNames, filter)
	if err != nil {
		return nil, ctxerr.Wrap(ctx, err, "get labels by name")
	}
	// Make sure that all labels were found
	for _, labelName := range labelNames {
		if _, ok := labelsMap[labelName]; !ok {
			return nil, ctxerr.Wrap(ctx, badRequestf("label %q not found", labelName))
		}
	}
	return labelsMap, nil
}

func verifyLabelsToAssociate(ctx context.Context, ds fleet.Datastore, entityTeamID *uint, labelNames []string, user *fleet.User) error {
	if len(labelNames) == 0 {
		return nil
	}
	if user == nil {
		return ctxerr.New(ctx, "Authentication required")
	}

	// Remove duplicate names.
	seen := make(map[string]struct{})
	uniqueLabelNames := make([]string, 0, len(labelNames))
	for _, s := range labelNames {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		uniqueLabelNames = append(uniqueLabelNames, s)
	}

	if entityTeamID == nil { // no-team/all-teams entities can only access global labels
		entityTeamID = ptr.Uint(0)
	}

	labels, err := loadLabelsFromNames(ctx, ds, uniqueLabelNames, fleet.TeamFilter{User: user, TeamID: entityTeamID})
	if err != nil {
		return ctxerr.Wrap(ctx, err, "labels by name")
	}

	if len(labels) != len(uniqueLabelNames) {
		return ctxerr.Wrap(ctx, badRequest("one or more labels specified do not exist, or cannot be applied to this entity"))
	}

	return nil
}

// autoLabelName generates the name for a hidden auto-label used for host_ids targeting.
func autoLabelName(entityType string, entityID uint) string {
	return fmt.Sprintf("__fleet_host_target_%s_%d", entityType, entityID)
}

// createOrUpdateAutoLabel creates or updates a hidden manual label with the given host IDs
// for host_ids targeting on policies/queries. It returns the label name and ID.
func createOrUpdateAutoLabel(ctx context.Context, ds fleet.Datastore, entityType string, entityID uint, hostIDs []uint) (string, uint, error) {
	labelName := autoLabelName(entityType, entityID)

	// Try to find existing label by name (using a global team filter since hidden labels are global)
	existingLabel, err := ds.LabelByName(ctx, labelName, fleet.TeamFilter{})
	if err != nil && !fleet.IsNotFound(err) {
		return "", 0, ctxerr.Wrap(ctx, err, "looking up auto label")
	}

	if existingLabel != nil {
		// Update existing label membership
		_, _, err := ds.UpdateLabelMembershipByHostIDs(ctx, *existingLabel, hostIDs, fleet.TeamFilter{})
		if err != nil {
			return "", 0, ctxerr.Wrap(ctx, err, "updating auto label membership")
		}
		return labelName, existingLabel.ID, nil
	}

	// Create new hidden manual label
	newLabel, err := ds.NewLabel(ctx, &fleet.Label{
		Name:                labelName,
		Description:         fmt.Sprintf("Auto-created label for %s %d host targeting", entityType, entityID),
		LabelType:           fleet.LabelTypeRegular,
		LabelMembershipType: fleet.LabelMembershipTypeManual,
		Hidden:              true,
	})
	if err != nil {
		return "", 0, ctxerr.Wrap(ctx, err, "creating auto label")
	}

	// Set label membership
	if len(hostIDs) > 0 {
		_, _, err := ds.UpdateLabelMembershipByHostIDs(ctx, *newLabel, hostIDs, fleet.TeamFilter{})
		if err != nil {
			return "", 0, ctxerr.Wrap(ctx, err, "setting auto label membership")
		}
	}

	return labelName, newLabel.ID, nil
}

// deleteAutoLabel deletes the auto-label if the label ID is non-nil.
func deleteAutoLabel(ctx context.Context, ds fleet.Datastore, labelID *uint) error {
	if labelID == nil {
		return nil
	}

	// Look up the label to get its name for deletion
	label, _, err := ds.Label(ctx, *labelID, fleet.TeamFilter{})
	if err != nil {
		if fleet.IsNotFound(err) {
			return nil // already deleted
		}
		return ctxerr.Wrap(ctx, err, "looking up auto label for deletion")
	}

	if err := ds.DeleteLabel(ctx, label.Name, fleet.TeamFilter{}); err != nil {
		if fleet.IsNotFound(err) {
			return nil // already deleted
		}
		return ctxerr.Wrap(ctx, err, "deleting auto label")
	}
	return nil
}

// populatePolicyHostIDs populates the HostIDs field on the policy by querying
// the label_membership for the auto-label.
func populatePolicyHostIDs(ctx context.Context, ds fleet.Datastore, policy *fleet.Policy) error {
	if policy == nil || policy.AutoHostIDsLabelID == nil {
		return nil
	}
	hostIDs, err := ds.HostIDsInLabel(ctx, *policy.AutoHostIDsLabelID)
	if err != nil {
		return ctxerr.Wrap(ctx, err, "get host IDs for policy auto label")
	}
	policy.HostIDs = hostIDs
	return nil
}

// populateQueryHostIDs populates the HostIDs field on the query by querying
// the label_membership for the auto-label.
func populateQueryHostIDs(ctx context.Context, ds fleet.Datastore, query *fleet.Query) error {
	if query == nil || query.AutoHostIDsLabelID == nil {
		return nil
	}
	hostIDs, err := ds.HostIDsInLabel(ctx, *query.AutoHostIDsLabelID)
	if err != nil {
		return ctxerr.Wrap(ctx, err, "get host IDs for query auto label")
	}
	query.HostIDs = hostIDs
	return nil
}
