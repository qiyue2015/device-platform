package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/qiyue2015/device-platform/internal/devicetype"
	"github.com/qiyue2015/device-platform/internal/domain"
)

// ValidateFrozenContracts ensures database release snapshots cannot silently
// become a second runtime source of Device Type semantics.
func ValidateFrozenContracts(ctx context.Context, db *sql.DB) error {
	var revision int
	var profileJSON []byte
	var profileHash []byte
	err := db.QueryRowContext(ctx, `
		SELECT dt.current_revision, p.profile, p.profile_hash
		FROM device_types dt
		JOIN device_type_profiles p
		  ON p.device_type_id = dt.id AND p.revision = dt.current_revision
		WHERE dt.code = $1
	`, domain.DeviceTypeSmartLock).Scan(&revision, &profileJSON, &profileHash)
	if err != nil {
		return fmt.Errorf("load smart-lock frozen profile: %w", err)
	}
	if err := devicetype.ValidateSmartLockSnapshot(revision, profileJSON, profileHash); err != nil {
		return fmt.Errorf("validate frozen Device Type contracts: %w", err)
	}
	return nil
}
