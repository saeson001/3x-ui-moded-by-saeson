package service

import (
	"encoding/json"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// normalizeRealityShortIds ensures every non-empty REALITY shortId stored on a
// VLESS+reality inbound is exactly 8 bytes (16 hex characters).
//
// Why this matters:
// Xray-core treats REALITY shortIds as opaque hex blobs of (almost) any length,
// so a 2-byte value like "0222" works fine with v2rayN / NekoBox / PC mihomo,
// which relay the value straight to the core. But Clash Meta (mihomo) validates
// the `short-id` field in a Clash YAML subscription against a strict 8-byte
// requirement at config-load time. A shorter value makes the whole subscription
// fail to import on Android Clash Meta with "invalid REALITY short ID", even
// though the very same inbound works everywhere else.
//
// By normalizing on every inbound save — Add and Update both call
// normalizeStreamSettings, where this runs — we guarantee the stored shortIds
// are always 8 bytes, so every downstream client (including Android Clash Meta)
// is happy. Empty entries are preserved: an empty shortId is a valid "no shortId"
// auth option.
func (s *InboundService) normalizeRealityShortIds(inbound *model.Inbound) {
	if inbound == nil || inbound.Protocol != model.VLESS {
		return
	}

	ss := map[string]interface{}{}
	if err := json.Unmarshal([]byte(inbound.StreamSettings), &ss); err != nil {
		return
	}
	security, _ := ss["security"].(string)
	if security != "reality" {
		return
	}
	reality, ok := ss["realitySettings"].(map[string]interface{})
	if !ok {
		return
	}
	raw, ok := reality["shortIds"]
	if !ok {
		return
	}
	list, ok := raw.([]interface{})
	if !ok {
		return
	}

	changed := false
	for i, item := range list {
		sid, ok := item.(string)
		if !ok {
			continue
		}
		normalized := normalizeShortId(sid)
		if normalized != sid {
			list[i] = normalized
			changed = true
		}
	}
	if !changed {
		return
	}

	fixed, err := json.Marshal(ss)
	if err != nil {
		logger.Warning("normalizeRealityShortIds: failed to marshal:", err)
		return
	}
	inbound.StreamSettings = string(fixed)
	logger.Debug("normalizeRealityShortIds: normalized shortIds for inbound", inbound.Id, inbound.Tag)
}

// normalizeShortId returns sid padded/truncated to exactly 16 hex chars (8 bytes).
// Non-hex characters are stripped; the result is lowercased. If stripping leaves
// nothing usable, the original value is returned unchanged so a bad value still
// fails loudly at runtime rather than silently becoming an empty shortId.
// An empty input yields an empty output (kept intact as "no shortId" auth).
func normalizeShortId(sid string) string {
	cleaned := make([]byte, 0, len(sid))
	for i := 0; i < len(sid); i++ {
		c := sid[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			if c >= 'A' && c <= 'F' {
				c = c - 'A' + 'a'
			}
			cleaned = append(cleaned, c)
		}
	}
	if len(cleaned) == 0 {
		// nothing usable; keep original so the error surfaces at runtime
		return sid
	}
	const target = 16
	if len(cleaned) >= target {
		return string(cleaned[:target])
	}
	// right-pad with zeros to 8 bytes
	padded := make([]byte, target)
	copy(padded, cleaned)
	for i := len(cleaned); i < target; i++ {
		padded[i] = '0'
	}
	return string(padded)
}
