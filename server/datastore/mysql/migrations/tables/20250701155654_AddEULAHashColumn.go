package tables

import (
	"crypto/sha256"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
)

func init() {
	MigrationClient.AddMigration(Up_20250701155654, Down_20250701155654)
}

func Up_20250701155654(tx *sql.Tx) error {
	// Idempotent migration.
	if !columnExists(tx, "eulas", "sha256") {
		if _, err := tx.Exec("ALTER TABLE eulas ADD COLUMN sha256 binary(32)"); err != nil {
			return fmt.Errorf("adding sha256 to eulas table: %w", err)
		}
	}

	txx := sqlx.Tx{Tx: tx, Mapper: reflectx.NewMapperFunc("db", sqlx.NameMapper)}

	type eula struct {
		ID    int    `db:"id"`
		Bytes []byte `db:"bytes"`
	}

	var eulas []eula
	if err := txx.Select(&eulas, "SELECT id, bytes FROM eulas"); err != nil {
		return fmt.Errorf("selecting existing eulas: %w", err)
	}

	for _, e := range eulas {
		hash := sha256.New()
		_, _ = hash.Write(e.Bytes)
		sha256Hash := hash.Sum(nil)
		if _, err := txx.Exec("UPDATE eulas SET sha256 = ? WHERE id = ?", sha256Hash, e.ID); err != nil {
			return fmt.Errorf("updating eula %d with sha256: %w", e.ID, err)
		}
	}

	return nil
}

func Down_20250701155654(tx *sql.Tx) error {
	return nil
}
