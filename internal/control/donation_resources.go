package control

import (
	"encoding/json"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/storage/models"
)

func (s *Service) donationCanonicalFingerprint(canonical []byte) string {
	var value struct {
		APIKey string `json:"api_key"`
	}
	if json.Unmarshal(canonical, &value) != nil || strings.TrimSpace(value.APIKey) == "" {
		return ""
	}
	return s.encryption.Hash(donationFingerprintDomain + strings.TrimSpace(value.APIKey))
}

// The unique insert and row lock are shared with normal management imports.
// Local writeMu is only a runtime consistency guard, not the uniqueness fence.
func (s *Service) lockDonationResource(tx *gorm.DB, fingerprint string) (models.DonationResource, error) {
	resource := models.DonationResource{Fingerprint: fingerprint, CreatedAtMS: s.donationNowMS(), UpdatedAtMS: s.donationNowMS()}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&resource).Error; err != nil {
		return resource, app_errors.ParseDBError(err)
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("fingerprint = ?", fingerprint).Take(&resource).Error; err != nil {
		return resource, app_errors.ParseDBError(err)
	}
	return resource, nil
}

func (s *Service) lockInventoryDonationResources(tx *gorm.DB, candidates []credentialCandidate) (map[string]models.DonationResource, error) {
	fingerprints := make([]string, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		fingerprint := s.donationCanonicalFingerprint(candidate.canonical)
		if fingerprint != "" && !seen[fingerprint] {
			fingerprints = append(fingerprints, fingerprint)
			seen[fingerprint] = true
		}
	}
	// Lock in stable order without changing the user's credential insertion order.
	sort.Strings(fingerprints)
	resources := make(map[string]models.DonationResource, len(fingerprints))
	for _, fingerprint := range fingerprints {
		resource, err := s.lockDonationResource(tx, fingerprint)
		if err != nil {
			return nil, err
		}
		resources[fingerprint] = resource
	}
	return resources, nil
}

func (s *Service) markDonationInventory(tx *gorm.DB, fingerprint string, row models.Credential) error {
	if fingerprint == "" {
		return nil
	}
	// Never replace an acquired donation's original source or resurrect it after deletion.
	if err := tx.Model(&models.DonationResource{}).Where("fingerprint = ? AND acquired_at_ms IS NULL", fingerprint).
		Updates(map[string]any{"origin": "inventory", "owner_item_id": nil, "group_id": row.GroupID,
			"credential_id": row.ID, "acquired_at_ms": max(row.CreatedAtMS, 1), "updated_at_ms": s.donationNowMS()}).Error; err != nil {
		return app_errors.ParseDBError(err)
	}
	return nil
}

// Existing inventory is fingerprinted while the encryption key is available,
// before the first integration request. Migrations never need plaintext keys.
func (s *Service) backfillDonationInventory(tx *gorm.DB) error {
	var rows []models.Credential
	if err := tx.Model(&models.Credential{}).
		Where("group_id IN (?)", tx.Model(&models.Group{}).Select("id").
			Where("connection_type = ?", models.ConnectionTypeAPIKey)).
		Order("id ASC").Find(&rows).Error; err != nil {
		return app_errors.ParseDBError(err)
	}
	byFingerprint := make(map[string]models.Credential, len(rows))
	fingerprints := make([]string, 0, len(rows))
	for _, row := range rows {
		plaintext, err := s.encryption.Decrypt(row.Data)
		if err != nil {
			return app_errors.ErrDonationUnavailable
		}
		fingerprint := s.donationCanonicalFingerprint([]byte(plaintext))
		plaintext = ""
		if fingerprint == "" {
			continue
		}
		if _, exists := byFingerprint[fingerprint]; exists {
			continue
		}
		byFingerprint[fingerprint] = row
		fingerprints = append(fingerprints, fingerprint)
	}
	sort.Strings(fingerprints)
	for _, fingerprint := range fingerprints {
		if _, err := s.lockDonationResource(tx, fingerprint); err != nil {
			return err
		}
		if err := s.markDonationInventory(tx, fingerprint, byFingerprint[fingerprint]); err != nil {
			return err
		}
	}
	return nil
}
