package clash_link

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"gorm.io/gorm"
)

const (
	defaultMixedPort = 7890
	defaultMode      = "rule"
	defaultLogLevel  = "info"
	defaultGroupName = "节点选择"
)

// ConfigRecord represents a stored clash config
type ConfigRecord struct {
	ID         int       `json:"id"`
	Token      string    `json:"token" gorm:"uniqueIndex;not null"`
	ConfigName string    `json:"config_name"`
	InboundIDs string    `json:"inbound_ids"` // JSON array of ints
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// LinkService manages clash config generation and serving
type LinkService struct {
	db       *gorm.DB
	storeDir string
	mu       sync.RWMutex
}

// NewLinkService creates a new LinkService
func NewLinkService(db *gorm.DB, storeDir string) (*LinkService, error) {
	svc := &LinkService{
		db:       db,
		storeDir: storeDir,
	}

	// Ensure store directory exists
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}

	// Auto-migrate config records table
	if err := db.AutoMigrate(&ConfigRecord{}); err != nil {
		return nil, fmt.Errorf("migrate config records: %w", err)
	}

	return svc, nil
}

// GenerateRequest contains parameters for config generation
type GenerateRequest struct {
	InboundIDs []int  `json:"inbound_ids"` // empty = all enabled inbounds
	ConfigName string `json:"config_name"`
	MixedPort  int    `json:"mixed_port"`
	AllowLan   bool   `json:"allow_lan"`
	Mode       string `json:"mode"`
	LogLevel   string `json:"log_level"`
	GroupName  string `json:"group_name"`
	Host       string `json:"host"` // for generating the share URL
}

// GenerateResponse is the result of config generation
type GenerateResponse struct {
	Token    string `json:"token"`
	YAMLURL  string `json:"yaml_url"`
	ProxyNum int    `json:"proxy_num"`
}

// Generate creates a clash config and returns a token URL
func (s *LinkService) Generate(req GenerateRequest) (*GenerateResponse, error) {
	// Apply defaults
	if req.MixedPort <= 0 {
		req.MixedPort = defaultMixedPort
	}
	if req.Mode == "" {
		req.Mode = defaultMode
	}
	if req.LogLevel == "" {
		req.LogLevel = defaultLogLevel
	}
	if req.GroupName == "" {
		req.GroupName = defaultGroupName
	}

	// Get inbounds
	var inbounds []model.Inbound
	if len(req.InboundIDs) == 0 {
		// Get all enabled inbounds
		if err := s.db.Where("enable = ?", true).
			Where("protocol IN ?", []string{"vless", "vmess", "trojan", "shadowsocks"}).
			Order("sub_sort_index ASC, id ASC").
			Find(&inbounds).Error; err != nil {
			return nil, fmt.Errorf("query inbounds: %w", err)
		}
	} else {
		if err := s.db.Where("id IN ?", req.InboundIDs).
			Where("enable = ?", true).
			Find(&inbounds).Error; err != nil {
			return nil, fmt.Errorf("query inbounds by IDs: %w", err)
		}
	}

	if len(inbounds) == 0 {
		return nil, fmt.Errorf("no enabled inbounds found")
	}

	// Build YAML
	yamlStr, err := BuildClashYAML(inbounds, req.ConfigName, req.MixedPort, req.AllowLan, req.Mode, req.LogLevel, req.GroupName)
	if err != nil {
		return nil, fmt.Errorf("build YAML: %w", err)
	}

	// Generate token
	token := generateToken(16)

	// Write YAML file
	yamlPath := filepath.Join(s.storeDir, token+".yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlStr), 0644); err != nil {
		return nil, fmt.Errorf("write YAML: %w", err)
	}

	// Save record to database
	idsJSON, _ := json.Marshal(req.InboundIDs)
	record := ConfigRecord{
		Token:      token,
		ConfigName: req.ConfigName,
		InboundIDs: string(idsJSON),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := s.db.Create(&record).Error; err != nil {
		// Clean up file on DB error
		os.Remove(yamlPath)
		return nil, fmt.Errorf("save record: %w", err)
	}

	// Count actual proxies
	proxyCount := countProxies(yamlStr)

	return &GenerateResponse{
		Token:    token,
		YAMLURL:  fmt.Sprintf("/d/%s", token),
		ProxyNum: proxyCount,
	}, nil
}

// GetYAML returns the YAML content for a token
func (s *LinkService) GetYAML(token string) (string, string, error) {
	yamlPath := filepath.Join(s.storeDir, token+".yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("config not found")
		}
		return "", "", fmt.Errorf("read YAML: %w", err)
	}

	// Get config name from record
	var record ConfigRecord
	configName := token
	if err := s.db.Where("token = ?", token).First(&record).Error; err == nil {
		if record.ConfigName != "" {
			configName = record.ConfigName
		}
	}

	return string(data), configName, nil
}

// ListConfigs returns all generated config records
func (s *LinkService) ListConfigs() ([]ConfigRecord, error) {
	var records []ConfigRecord
	if err := s.db.Order("created_at DESC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list configs: %w", err)
	}
	return records, nil
}

// DeleteConfig removes a config by token
func (s *LinkService) DeleteConfig(token string) error {
	yamlPath := filepath.Join(s.storeDir, token+".yaml")
	os.Remove(yamlPath) // best-effort cleanup

	if err := s.db.Where("token = ?", token).Delete(&ConfigRecord{}).Error; err != nil {
		return fmt.Errorf("delete record: %w", err)
	}
	return nil
}

// Regenerate refreshes the YAML for an existing token
func (s *LinkService) Regenerate(token string, host string) (*GenerateResponse, error) {
	var record ConfigRecord
	if err := s.db.Where("token = ?", token).First(&record).Error; err != nil {
		return nil, fmt.Errorf("config not found: %w", err)
	}

	var inboundIDs []int
	if record.InboundIDs != "" {
		json.Unmarshal([]byte(record.InboundIDs), &inboundIDs)
	}

	return s.Generate(GenerateRequest{
		InboundIDs: inboundIDs,
		ConfigName: record.ConfigName,
		Host:       host,
	})
}

// ---------------------------------------------------------------------------
// Backup / Export
// ---------------------------------------------------------------------------

// FullBackup exports all settings and inbounds as a JSON object
func (s *LinkService) FullBackup() ([]byte, error) {
	backup := make(map[string]any)
	backup["exported_at"] = time.Now().Format(time.RFC3339)
	backup["version"] = "1.0"

	// Export inbounds
	var inbounds []model.Inbound
	if err := s.db.Order("id ASC").Find(&inbounds).Error; err != nil {
		return nil, fmt.Errorf("query inbounds: %w", err)
	}

	// Serialize with proper JSON rendering (model.Inbound has custom MarshalJSON)
	inboundData := make([]map[string]any, 0, len(inbounds))
	for _, ib := range inbounds {
		data, err := json.Marshal(ib)
		if err != nil {
			continue
		}
		var m map[string]any
		json.Unmarshal(data, &m)
		inboundData = append(inboundData, m)
	}
	backup["inbounds"] = inboundData

	// Export clients
	type clientRecord struct {
		ID        int    `json:"id"`
		Email     string `json:"email"`
		SubID     string `json:"subId"`
		UUID      string `json:"uuid"`
		Password  string `json:"password"`
		Flow      string `json:"flow"`
		Enable    bool   `json:"enable"`
		TotalGB   int64  `json:"totalGB"`
		ExpiryTime int64 `json:"expiryTime"`
		LimitIP   int    `json:"limitIp"`
	}
	var clients []clientRecord
	if err := s.db.Table("clients").Order("id ASC").Find(&clients).Error; err == nil {
		backup["clients"] = clients
	}

	// Export client_inbounds
	type clientInbound struct {
		ClientID     int    `json:"client_id"`
		InboundID    int    `json:"inbound_id"`
		FlowOverride string `json:"flow_override"`
	}
	var clientInbounds []clientInbound
	if err := s.db.Table("client_inbounds").Order("client_id ASC").Find(&clientInbounds).Error; err == nil {
		backup["client_inbounds"] = clientInbounds
	}

	// Export hosts
	var hosts []model.Host
	if err := s.db.Order("id ASC").Find(&hosts).Error; err == nil {
		hostData := make([]map[string]any, 0, len(hosts))
		for _, h := range hosts {
			data, _ := json.Marshal(h)
			var m map[string]any
			json.Unmarshal(data, &m)
			hostData = append(hostData, m)
		}
		backup["hosts"] = hostData
	}

	return json.MarshalIndent(backup, "", "  ")
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

func generateToken(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)[:length]
}

func countProxies(yamlStr string) int {
	count := 0
	lines := splitLines(yamlStr)
	inProxy := false
	for _, line := range lines {
		trimmed := trimSpace(line)
		if trimmed == "proxies:" {
			inProxy = true
			continue
		}
		if inProxy {
			if trimmed == "proxy-groups:" || trimmed == "rules:" {
				break
			}
			if hasPrefix(trimmed, "- name:") {
				count++
			}
		}
	}
	return count
}

func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, ch := range s {
		if ch == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func trimSpace(s string) string {
	// Simple trim
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
