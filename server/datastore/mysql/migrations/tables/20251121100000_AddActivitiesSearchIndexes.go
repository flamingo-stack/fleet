package tables

import (
	"database/sql"
)

func init() {
	MigrationClient.AddMigration(Up_20251121100000, Down_20251121100000)
}

func Up_20251121100000(tx *sql.Tx) error {
	// Idempotent migration.
	if !indexExistsTx(tx, "activities", "idx_activities_user_name") {
		if _, err := tx.Exec(`CREATE INDEX idx_activities_user_name ON activities (user_name)`); err != nil {
			return err
		}
	}
	if !indexExistsTx(tx, "activities", "idx_activities_user_email") {
		if _, err := tx.Exec(`CREATE INDEX idx_activities_user_email ON activities (user_email)`); err != nil {
			return err
		}
	}
	if !indexExistsTx(tx, "activities", "idx_activities_activity_type") {
		if _, err := tx.Exec(`CREATE INDEX idx_activities_activity_type ON activities (activity_type)`); err != nil {
			return err
		}
	}
	if !indexExistsTx(tx, "activities", "idx_activities_type_created") {
		if _, err := tx.Exec(`CREATE INDEX idx_activities_type_created ON activities (activity_type, created_at)`); err != nil {
			return err
		}
	}
	if !indexExistsTx(tx, "users", "idx_users_name") {
		if _, err := tx.Exec(`CREATE INDEX idx_users_name ON users (name)`); err != nil {
			return err
		}
	}
	return nil
}

func Down_20251121100000(tx *sql.Tx) error {
	return nil
}
