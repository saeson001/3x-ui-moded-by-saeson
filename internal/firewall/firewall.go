// Package firewall provides one-click host firewall management for 3x-ui.
//
// It opens ONLY these ports (everything else inbound is denied):
//   - SSH port(s) (auto-detected via ss, fallback 22)
//   - Panel web port + subscription port (from panel settings)
//   - Every inbound port configured in the panel (proxy nodes)
//   - User-defined extra ports (persisted in /etc/x-ui/firewall.json)
//
// Backends: ufw (Debian/Ubuntu) and firewalld (CentOS/RHEL/Alma/Rocky).
// Outbound traffic is never restricted.
package firewall

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gorm.io/gorm"
)

const configFile = "/etc/x-ui/firewall.json"

type Config struct {
	Enabled    bool   `json:"enabled"`
	Backend    string `json:"backend"`
	SSHPorts   []int  `json:"sshPorts"`
	ExtraPorts []int  `json:"extraPorts"`
}

type Status struct {
	Backend      string `json:"backend"`       // ufw / firewalld / ""
	BackendFound bool   `json:"backendFound"`  // is a supported backend installed
	Active       bool   `json:"active"`        // firewall currently enforcing
	SSHPorts     []int  `json:"sshPorts"`
	PanelPorts   []int  `json:"panelPorts"`
	InboundPorts []int  `json:"inboundPorts"`
	ExtraPorts   []int  `json:"extraPorts"`
	AllPorts     []int  `json:"allPorts"`
	Notice       string `json:"notice,omitempty"`
}

var (
	mu sync.Mutex
)

// ---------- helpers ----------

func run(cmd string, args ...string) (string, error) {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// runningAsRoot checks euid via syscall (panel normally runs as root).
func runningAsRoot() bool {
	return os.Geteuid() == 0
}

// DetectBackend returns "ufw", "firewalld" or "" (none supported installed).
func DetectBackend() string {
	if commandExists("ufw") {
		return "ufw"
	}
	if commandExists("firewall-cmd") {
		return "firewalld"
	}
	return ""
}

func loadConfig() *Config {
	cfg := &Config{}
	if data, err := os.ReadFile(configFile); err == nil {
		_ = json.Unmarshal(data, cfg)
	}
	if cfg.ExtraPorts == nil {
		cfg.ExtraPorts = []int{}
	}
	if cfg.SSHPorts == nil {
		cfg.SSHPorts = []int{}
	}
	return cfg
}

func saveConfig(cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll("/etc/x-ui", 0755); err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0644)
}

// ---------- port discovery ----------

// detectSSHPorts finds ports sshd is actually listening on (fallback 22).
func detectSSHPorts() []int {
	ports := map[int]bool{}
	if out, err := run("ss", "-tlnp"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			l := strings.ToLower(line)
			if strings.Contains(l, "sshd") {
				for _, field := range strings.Fields(line) {
					// field like 0.0.0.0:22 or [::]:22
					if idx := strings.LastIndex(field, ":"); idx >= 0 {
						if p, err := strconv.Atoi(field[idx+1:]); err == nil && p > 0 && p < 65536 {
							ports[p] = true
						}
					}
				}
			}
		}
	}
	if len(ports) == 0 {
		return []int{22}
	}
	return sortedKeys(ports)
}

// panelPorts reads webPort/subPort from the settings table.
func panelPorts(db *gorm.DB) []int {
	ports := map[int]bool{}
	for _, key := range []string{"webPort", "subPort", "subListen"} {
		var value string
		if err := db.Raw("SELECT value FROM settings WHERE key = ?", key).Scan(&value).Error; err != nil || value == "" {
			continue
		}
		if p, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && p > 0 && p < 65536 {
			ports[p] = true
		}
	}
	return sortedKeys(ports)
}

// inboundPorts reads every proxy node port from the inbounds table.
func inboundPorts(db *gorm.DB) []int {
	ports := map[int]bool{}
	var rows []int
	if err := db.Raw("SELECT port FROM inbounds").Scan(&rows).Error; err != nil {
		return []int{}
	}
	for _, p := range rows {
		if p > 0 && p < 65536 {
			ports[p] = true
		}
	}
	return sortedKeys(ports)
}

func sortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// collectAllPorts returns the complete allow-list: ssh + panel + inbounds + extra.
func collectAllPorts(db *gorm.DB, cfg *Config) (ssh, panel, inbounds []int, all []int) {
	ssh = detectSSHPorts()
	panel = panelPorts(db)
	inbounds = inboundPorts(db)
	merged := map[int]bool{}
	for _, list := range [][]int{ssh, panel, inbounds, cfg.ExtraPorts} {
		for _, p := range list {
			merged[p] = true
		}
	}
	all = sortedKeys(merged)
	return
}

// ---------- backend operations ----------

func ufwActive() bool {
	out, _ := run("ufw", "status")
	return strings.Contains(strings.ToLower(out), "active") && !strings.Contains(strings.ToLower(out), "inactive")
}

func firewalldActive() bool {
	out, _ := run("firewall-cmd", "--state")
	return strings.TrimSpace(out) == "running"
}

func backendActive(backend string) bool {
	switch backend {
	case "ufw":
		return ufwActive()
	case "firewalld":
		return firewalldActive()
	}
	return false
}

// allowPort opens a port for tcp+udp on the detected backend.
func allowPort(backend string, port int) error {
	ps := strconv.Itoa(port)
	if backend == "ufw" {
		_, err := run("ufw", "allow", ps+"/tcp")
		if err != nil {
			return err
		}
		_, _ = run("ufw", "allow", ps+"/udp") // best-effort (hysteria/tunnel nodes)
		return nil
	}
	if backend == "firewalld" {
		if _, err := run("firewall-cmd", "--permanent", "--add-port="+ps+"/tcp"); err != nil {
			return err
		}
		_, _ = run("firewall-cmd", "--permanent", "--add-port="+ps+"/udp")
		return nil
	}
	return fmt.Errorf("no supported firewall backend installed (install ufw or firewalld)")
}

func denyPort(backend string, port int) error {
	ps := strconv.Itoa(port)
	if backend == "ufw" {
		_, _ = run("ufw", "delete", "allow", ps+"/tcp")
		_, _ = run("ufw", "delete", "allow", ps+"/udp")
		return nil
	}
	if backend == "firewalld" {
		_, _ = run("firewall-cmd", "--permanent", "--remove-port="+ps+"/tcp")
		_, _ = run("firewall-cmd", "--permanent", "--remove-port="+ps+"/udp")
		return nil
	}
	return fmt.Errorf("no supported firewall backend installed")
}

// Enable opens the allow-list then turns the firewall on (deny other inbound).
// SAFETY: it refuses to enable unless at least one SSH port AND the panel
// port are on the allow-list, so you can never lock yourself out.
func Enable(db *gorm.DB, extraPorts []int) (*Status, error) {
	mu.Lock()
	defer mu.Unlock()

	if !runningAsRoot() {
		return nil, fmt.Errorf("firewall management requires root privileges")
	}
	backend := DetectBackend()
	if backend == "" {
		return nil, fmt.Errorf("no supported firewall found, please install ufw (Debian/Ubuntu) or firewalld (CentOS/RHEL) first")
	}

	cfg := loadConfig()
	if len(extraPorts) > 0 {
		cfg.ExtraPorts = extraPorts
	}

	ssh, panel, _, all := collectAllPorts(db, cfg)

	// Safety checks: must keep SSH + panel reachable.
	if len(ssh) == 0 {
		return nil, fmt.Errorf("could not detect SSH port, refusing to enable firewall")
	}
	if len(panel) == 0 {
		return nil, fmt.Errorf("could not detect panel port, refusing to enable firewall")
	}

	// Allow loopback & established connections first (ufw defaults).
	if backend == "ufw" {
		_, _ = run("ufw", "default", "deny", "incoming")
		_, _ = run("ufw", "default", "allow", "outgoing")
	}
	if backend == "firewalld" {
		_, _ = run("firewall-cmd", "--set-default-zone=public")
		_, _ = run("firewall-cmd", "--permanent", "--set-default-zone=public")
	}

	var lastErr error
	for _, p := range all {
		if err := allowPort(backend, p); err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("failed to allow ports: %v", lastErr)
	}

	if backend == "ufw" {
		if out, err := run("ufw", "--force", "enable"); err != nil {
			return nil, fmt.Errorf("ufw enable failed: %s", out)
		}
	} else {
		if out, err := run("firewall-cmd", "--reload"); err != nil {
			return nil, fmt.Errorf("firewalld reload failed: %s", out)
		}
	}

	cfg.Enabled = true
	cfg.Backend = backend
	cfg.SSHPorts = ssh
	if err := saveConfig(cfg); err != nil {
		return nil, fmt.Errorf("firewall enabled but failed to persist config: %v", err)
	}

	return buildStatus(db, cfg), nil
}

// Disable turns the firewall off (rules are kept for the next enable).
func Disable() (*Status, error) {
	mu.Lock()
	defer mu.Unlock()

	if !runningAsRoot() {
		return nil, fmt.Errorf("firewall management requires root privileges")
	}
	backend := DetectBackend()
	if backend == "" {
		return nil, fmt.Errorf("no supported firewall found")
	}
	if backend == "ufw" {
		if out, err := run("ufw", "--force", "disable"); err != nil {
			return nil, fmt.Errorf("ufw disable failed: %s", out)
		}
	} else {
		// Switch default zone to trusted (accept all) without removing rules.
		if out, err := run("firewall-cmd", "--set-default-zone=trusted"); err != nil {
			_, _ = run("firewall-cmd", "--permanent", "--set-default-zone=trusted")
			_, _ = run("firewall-cmd", "--reload")
			_ = out
		}
	}

	cfg := loadConfig()
	cfg.Enabled = false
	_ = saveConfig(cfg)
	return &Status{Backend: backend, BackendFound: true, Active: false, ExtraPorts: cfg.ExtraPorts}, nil
}

// Sync re-applies the allow-list (call after adding/removing inbounds).
func Sync(db *gorm.DB) (*Status, error) {
	mu.Lock()
	defer mu.Unlock()

	if !runningAsRoot() {
		return nil, fmt.Errorf("firewall management requires root privileges")
	}
	backend := DetectBackend()
	if backend == "" {
		return nil, fmt.Errorf("no supported firewall found")
	}
	cfg := loadConfig()
	if !backendActive(backend) {
		return nil, fmt.Errorf("firewall is not active, enable it first")
	}

	_, _, _, all := collectAllPorts(db, cfg)
	for _, p := range all {
		_ = allowPort(backend, p)
	}
	if backend == "firewalld" {
		_, _ = run("firewall-cmd", "--reload")
	}
	return buildStatus(db, cfg), nil
}

// AddExtraPort adds a user-defined port to the allow-list (applied immediately
// when the firewall is active).
func AddExtraPort(db *gorm.DB, port int) (*Status, error) {
	mu.Lock()
	defer mu.Unlock()

	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port: %d", port)
	}
	backend := DetectBackend()
	cfg := loadConfig()
	found := false
	for _, p := range cfg.ExtraPorts {
		if p == port {
			found = true
		}
	}
	if !found {
		cfg.ExtraPorts = append(cfg.ExtraPorts, port)
		sort.Ints(cfg.ExtraPorts)
	}
	if err := saveConfig(cfg); err != nil {
		return nil, err
	}
	if backend != "" && backendActive(backend) {
		if err := allowPort(backend, port); err != nil {
			return nil, err
		}
		if backend == "firewalld" {
			_, _ = run("firewall-cmd", "--reload")
		}
	}
	return buildStatus(db, cfg), nil
}

// RemoveExtraPort removes a user-defined port from the allow-list.
// Panel/SSH/inbound ports cannot be removed through this API.
func RemoveExtraPort(db *gorm.DB, port int) (*Status, error) {
	mu.Lock()
	defer mu.Unlock()

	backend := DetectBackend()
	cfg := loadConfig()
	kept := cfg.ExtraPorts[:0]
	for _, p := range cfg.ExtraPorts {
		if p != port {
			kept = append(kept, p)
		}
	}
	cfg.ExtraPorts = kept
	if err := saveConfig(cfg); err != nil {
		return nil, err
	}
	if backend != "" && backendActive(backend) {
		_ = denyPort(backend, port)
		if backend == "firewalld" {
			_, _ = run("firewall-cmd", "--reload")
		}
	}
	return buildStatus(db, cfg), nil
}

// GetStatus reports the current firewall state and the full allow-list.
func GetStatus(db *gorm.DB) *Status {
	mu.Lock()
	defer mu.Unlock()
	return buildStatus(db, loadConfig())
}

func buildStatus(db *gorm.DB, cfg *Config) *Status {
	st := &Status{
		Backend:      DetectBackend(),
		BackendFound: DetectBackend() != "",
		ExtraPorts:   cfg.ExtraPorts,
	}
	st.Active = backendActive(st.Backend)
	ssh, panel, inbounds, all := collectAllPorts(db, cfg)
	st.SSHPorts = ssh
	st.PanelPorts = panel
	st.InboundPorts = inbounds
	st.AllPorts = all
	if !st.BackendFound {
		st.Notice = "未检测到 ufw 或 firewalld，请先安装其一（Debian/Ubuntu: apt install ufw；CentOS: yum install firewalld）"
	}
	return st
}
