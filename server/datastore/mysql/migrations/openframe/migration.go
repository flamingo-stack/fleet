package openframe

import (
	"github.com/fleetdm/fleet/v4/server/goose"
)

// MigrationClient tracks openframe-specific schema changes independently
// from the upstream Fleet migration pipeline (migration_status_tables).
// This avoids version conflicts when rebasing onto newer upstream releases.
var MigrationClient = goose.New("migration_status_openframe", goose.MySqlDialect{})
