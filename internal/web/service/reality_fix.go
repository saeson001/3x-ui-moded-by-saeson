package service

import (
	"encoding/json"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// preserveRealitySettings prevents External Proxy from clearing REALITY
// security parameters (publicKey, fingerprint) in stream_settings.
//
// Background:
// When a VLESS+REALITY inbound has External Proxy enabled, the call to
// database.CreateHostsFromExternalProxy() may clear the realitySettings
// in the inbound's stream_settings. The generated Xray config.json will
// then contain publicKey: null and fingerprint: null, breaking clients
// like Clash Meta / Mihomo / Clash Party that rely solely on config.json
// (unlike v2rayN which can re-derive parameters from the share link).
//
// This function reloads the original stream_settings from the database
// and restores the full realitySettings before the inbound is saved.
func (s *InboundService) preserveRealitySettings(tx *gorm.DB, inbound *model.Inbound) {
	if inbound == nil || inbound.Protocol != model.VLESS {
		return
	}

	var ss map[string]interface{}
	if err := json.Unmarshal([]byte(inbound.StreamSettings), &ss); err != nil {
		return
	}

	security, _ := ss["security"].(string)
	if security != "reality" {
		return
	}

	realitySettings, ok := ss["realitySettings"].(map[string]interface{})
	if !ok {
		return
	}

	settings, ok := realitySettings["settings"].(map[string]interface{})
	if !ok {
		return
	}

	pubKey, _ := settings["publicKey"].(string)
	fingerprint, _ := settings["fingerprint"].(string)

	// If both are already non-empty, nothing to fix
	if pubKey != "" && pubKey != "null" && fingerprint != "" && fingerprint != "null" {
		return
	}

	logger.Debug("preserveRealitySettings: fixing cleared realitySettings for inbound",
		inbound.Id, inbound.Tag)

	// Reload the raw stream_settings from the database (before externalProxy
	// cleared the reality fields)
	var dbSS string
	if err := tx.Model(&model.Inbound{}).Where("id = ?", inbound.Id).
		Select("stream_settings").Scan(&dbSS).Error; err != nil {
		logger.Warning("preserveRealitySettings: failed to reload from DB:", err)
		return
	}

	var dbSSMap map[string]interface{}
	if err := json.Unmarshal([]byte(dbSS), &dbSSMap); err != nil {
		logger.Warning("preserveRealitySettings: failed to parse DB stream_settings:", err)
		return
	}

	dbReality, ok := dbSSMap["realitySettings"].(map[string]interface{})
	if !ok {
		return
	}

	// Replace the cleared realitySettings with the database's preserved copy
	ss["realitySettings"] = dbReality

	fixed, err := json.Marshal(ss)
	if err != nil {
		logger.Warning("preserveRealitySettings: failed to marshal:", err)
		return
	}

	inbound.StreamSettings = string(fixed)
	logger.Debug("preserveRealitySettings: fixed realitySettings for inbound",
		inbound.Id, inbound.Tag)
}
