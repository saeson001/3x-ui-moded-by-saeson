package clash_link

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// ProxyEntry represents a single proxy node in Clash YAML
type ProxyEntry struct {
	Name              string            `yaml:"name"`
	Type              string            `yaml:"type"`
	Server            string            `yaml:"server"`
	Port              int               `yaml:"port"`
	UUID              string            `yaml:"uuid,omitempty"`
	Password          string            `yaml:"password,omitempty"`
	Cipher            string            `yaml:"cipher,omitempty"`
	AlterID           int               `yaml:"alterId,omitempty"`
	Network           string            `yaml:"network,omitempty"`
	TLS               bool              `yaml:"tls,omitempty"`
	UDP               bool              `yaml:"udp,omitempty"`
	Flow              string            `yaml:"flow,omitempty"`
	ServerName        string            `yaml:"servername,omitempty"`
	SNI               string            `yaml:"sni,omitempty"`
	ClientFingerprint string            `yaml:"client-fingerprint,omitempty"`
	RealityOpts       map[string]string `yaml:"reality-opts,omitempty"`
	SkipCertVerify    bool              `yaml:"skip-cert-verify,omitempty"`
	ALPN              []string          `yaml:"alpn,omitempty"`
	WSOpts            map[string]any    `yaml:"ws-opts,omitempty"`
	GRPCOpts          map[string]string `yaml:"grpc-opts,omitempty"`
	H2Opts            map[string]any    `yaml:"h2-opts,omitempty"`
	Protocol          string            `yaml:"protocol,omitempty"`
	OBFS              string            `yaml:"obfs,omitempty"`
	ProtocolParam     string            `yaml:"protocol-param,omitempty"`
	OBFSParam         string            `yaml:"obfs-param,omitempty"`
}

// ClashConfig represents the full Clash YAML configuration
type ClashConfig struct {
	MixedPort   int          `yaml:"mixed-port"`
	AllowLan    bool         `yaml:"allow-lan"`
	Mode        string       `yaml:"mode"`
	LogLevel    string       `yaml:"log-level"`
	Proxies     []ProxyEntry `yaml:"proxies"`
	ProxyGroups []ProxyGroup `yaml:"proxy-groups"`
	Rules       []string     `yaml:"rules"`
}

// ProxyGroup represents a proxy group in Clash YAML
type ProxyGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

// VLessSettings represents the settings JSON stored in Inbound.Settings for VLESS
type VLessSettings struct {
	Clients    []VLessClient `json:"clients"`
	Decryption string        `json:"decryption"`
}

// VLessClient represents a client in VLESS settings
type VLessClient struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Flow  string `json:"flow"`
}

// VMessSettings represents the settings JSON for VMess
type VMessSettings struct {
	Clients []VMessClient `json:"clients"`
}

// VMessClient represents a client in VMess settings
type VMessClient struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	AlterID  int    `json:"alterId"`
	Security string `json:"security"`
}

// TrojanSettings represents the settings JSON for Trojan
type TrojanSettings struct {
	Clients []TrojanClient `json:"clients"`
}

// TrojanClient represents a client in Trojan settings
type TrojanClient struct {
	Password string `json:"password"`
	Email    string `json:"email"`
}

// SSSettings represents the settings JSON for Shadowsocks
type SSSettings struct {
	Clients []SSClient `json:"clients"`
	Method  string     `json:"method"`
}

// SSClient represents a client in SS settings
type SSClient struct {
	Password string `json:"password"`
	Email    string `json:"email"`
}

// StreamSettings represents the streamSettings JSON
type StreamSettings struct {
	Network         string                 `json:"network"`
	Security        string                 `json:"security"`
	TLSSettings     map[string]any         `json:"tlsSettings"`
	RealitySettings map[string]any         `json:"realitySettings"`
	ExternalProxy   []ExternalProxyEntry   `json:"externalProxy"`
	WSSettings      map[string]any         `json:"wsSettings"`
	GRPCSettings    map[string]any         `json:"grpcSettings"`
	HTTPSettings    map[string]any         `json:"httpSettings"`
	H2Settings      map[string]any         `json:"h2Settings"`
	TCPSettings     map[string]any         `json:"tcpSettings"`
}

// ExternalProxyEntry represents an external proxy configuration
type ExternalProxyEntry struct {
	ForceTLS string `json:"forceTls"`
	Dest     string `json:"dest"`
	Port     int    `json:"port"`
	Remark   string `json:"remark"`
}

// ---------------------------------------------------------------------------
// BuildClashYAML generates Clash Meta YAML from a list of inbounds
// ---------------------------------------------------------------------------
func BuildClashYAML(inbounds []model.Inbound, configName string, mixedPort int, allowLan bool, mode string, logLevel string, groupName string) (string, error) {
	cfg := ClashConfig{
		MixedPort: mixedPort,
		AllowLan:  allowLan,
		Mode:      mode,
		LogLevel:  logLevel,
	}

	seenNames := make(map[string]int)
	for _, inbound := range inbounds {
		proxies, err := buildProxiesFromInbound(inbound)
		if err != nil {
			continue // skip broken inbounds
		}
		for i := range proxies {
			// Deduplicate names
			name := proxies[i].Name
			if name == "" {
				name = fmt.Sprintf("%s-%d", inbound.Protocol, inbound.Port)
			}
			if cnt, ok := seenNames[name]; ok {
				seenNames[name] = cnt + 1
				proxies[i].Name = fmt.Sprintf("%s_%d", name, cnt+1)
			} else {
				seenNames[name] = 0
				proxies[i].Name = name
			}
		}
		cfg.Proxies = append(cfg.Proxies, proxies...)
	}

	// Build proxy group
	proxyNames := make([]string, 0, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		proxyNames = append(proxyNames, p.Name)
	}
	proxyNames = append(proxyNames, "DIRECT")

	cfg.ProxyGroups = []ProxyGroup{
		{
			Name:    groupName,
			Type:    "select",
			Proxies: proxyNames,
		},
	}

	cfg.Rules = []string{fmt.Sprintf("MATCH,%s", groupName)}

	// Serialize to YAML manually (ensures Clash Party compatibility)
	return marshalClashYAML(cfg, configName)
}

// buildProxiesFromInbound creates one or more proxy entries from a single inbound
func buildProxiesFromInbound(inbound model.Inbound) ([]ProxyEntry, error) {
	// Parse stream settings
	var streamSettings StreamSettings
	if err := json.Unmarshal([]byte(inbound.StreamSettings), &streamSettings); err != nil {
		streamSettings = StreamSettings{Network: "tcp", Security: "none"}
	}

	network := streamSettings.Network
	if network == "" {
		network = "tcp"
	}

	// Determine the actual server address
	server := inbound.Listen
	port := inbound.Port

	// Check for externalProxy - if present, use dest address
	externalProxies := streamSettings.ExternalProxy

	var entries []ProxyEntry

	switch inbound.Protocol {
	case model.VLESS:
		proxies, err := buildVLESSProxies(inbound, streamSettings, network, server, port, externalProxies)
		if err != nil {
			return nil, err
		}
		entries = proxies
	case model.VMESS:
		proxies, err := buildVMessProxies(inbound, streamSettings, network, server, port, externalProxies)
		if err != nil {
			return nil, err
		}
		entries = proxies
	case model.Trojan:
		proxies, err := buildTrojanProxies(inbound, streamSettings, network, server, port, externalProxies)
		if err != nil {
			return nil, err
		}
		entries = proxies
	case model.Shadowsocks:
		proxies, err := buildSSProxies(inbound, streamSettings, network, server, port, externalProxies)
		if err != nil {
			return nil, err
		}
		entries = proxies
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", inbound.Protocol)
	}

	return entries, nil
}

// buildVLESSProxies creates VLESS proxy entries
func buildVLESSProxies(inbound model.Inbound, ss StreamSettings, network, server string, port int, externalProxies []ExternalProxyEntry) ([]ProxyEntry, error) {
	var settings VLessSettings
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return nil, fmt.Errorf("failed to parse VLESS settings: %w", err)
	}

	if len(settings.Clients) == 0 {
		return nil, fmt.Errorf("no clients in VLESS inbound")
	}

	security := ss.Security
	if security == "" {
		security = "none"
	}

	useTLS := security == "tls" || security == "reality"

	// Resolve server addresses (handle externalProxy)
	addrs := resolveAddresses(server, port, externalProxies, inbound.Remark)

	var entries []ProxyEntry
	for _, addr := range addrs {
		for _, client := range settings.Clients {
			entry := ProxyEntry{
				Name:    getProxyName(inbound, client.Email, addr.Remark),
				Type:    "vless",
				Server:  addr.Server,
				Port:    addr.Port,
				UUID:    client.ID,
				Network: network,
				TLS:     useTLS,
				UDP:     true,
			}

			// Flow - only set if explicitly configured
			if client.Flow != "" {
				entry.Flow = client.Flow
			}

			// TLS settings
			if security == "tls" {
				applyTLSSettings(&entry, ss.TLSSettings)
			}

			// REALITY settings
			if security == "reality" {
				applyRealitySettings(&entry, ss.RealitySettings)
			}

			// Network options
			applyNetworkOpts(&entry, ss, network)

			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// buildVMessProxies creates VMess proxy entries
func buildVMessProxies(inbound model.Inbound, ss StreamSettings, network, server string, port int, externalProxies []ExternalProxyEntry) ([]ProxyEntry, error) {
	var settings VMessSettings
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return nil, fmt.Errorf("failed to parse VMess settings: %w", err)
	}

	if len(settings.Clients) == 0 {
		return nil, fmt.Errorf("no clients in VMess inbound")
	}

	security := ss.Security
	if security == "" {
		security = "none"
	}
	useTLS := security == "tls"

	addrs := resolveAddresses(server, port, externalProxies, inbound.Remark)

	var entries []ProxyEntry
	for _, addr := range addrs {
		for _, client := range settings.Clients {
			entry := ProxyEntry{
				Name:    getProxyName(inbound, client.Email, addr.Remark),
				Type:    "vmess",
				Server:  addr.Server,
				Port:    addr.Port,
				UUID:    client.ID,
				AlterID: client.AlterID,
				Network: network,
				TLS:     useTLS,
				UDP:     true,
				Cipher:  "auto",
			}

			if security == "tls" {
				applyTLSSettings(&entry, ss.TLSSettings)
			}
			applyNetworkOpts(&entry, ss, network)

			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// buildTrojanProxies creates Trojan proxy entries
func buildTrojanProxies(inbound model.Inbound, ss StreamSettings, network, server string, port int, externalProxies []ExternalProxyEntry) ([]ProxyEntry, error) {
	var settings TrojanSettings
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return nil, fmt.Errorf("failed to parse Trojan settings: %w", err)
	}

	if len(settings.Clients) == 0 {
		return nil, fmt.Errorf("no clients in Trojan inbound")
	}

	addrs := resolveAddresses(server, port, externalProxies, inbound.Remark)

	var entries []ProxyEntry
	for _, addr := range addrs {
		for _, client := range settings.Clients {
			entry := ProxyEntry{
				Name:     getProxyName(inbound, client.Email, addr.Remark),
				Type:     "trojan",
				Server:   addr.Server,
				Port:     addr.Port,
				Password: client.Password,
				Network:  network,
				UDP:      true,
			}

			// SNI defaults to server address
			if ss.TLSSettings != nil {
				if sni, ok := ss.TLSSettings["serverName"].(string); ok && sni != "" {
					entry.SNI = sni
				}
			}
			if entry.SNI == "" {
				entry.SNI = addr.Server
			}

			applyNetworkOpts(&entry, ss, network)

			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// buildSSProxies creates Shadowsocks proxy entries
func buildSSProxies(inbound model.Inbound, ss StreamSettings, network, server string, port int, externalProxies []ExternalProxyEntry) ([]ProxyEntry, error) {
	var settings SSSettings
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return nil, fmt.Errorf("failed to parse SS settings: %w", err)
	}

	if len(settings.Clients) == 0 {
		return nil, fmt.Errorf("no clients in SS inbound")
	}

	addrs := resolveAddresses(server, port, externalProxies, inbound.Remark)

	var entries []ProxyEntry
	for _, addr := range addrs {
		for _, client := range settings.Clients {
			entry := ProxyEntry{
				Name:     getProxyName(inbound, client.Email, addr.Remark),
				Type:     "ss",
				Server:   addr.Server,
				Port:     addr.Port,
				Cipher:   settings.Method,
				Password: client.Password,
				UDP:      true,
				Network:  network,
			}

			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

type addrInfo struct {
	Server string
	Port   int
	Remark string
}

func resolveAddresses(server string, port int, externalProxies []ExternalProxyEntry, fallbackRemark string) []addrInfo {
	if len(externalProxies) == 0 {
		return []addrInfo{{Server: server, Port: port, Remark: fallbackRemark}}
	}

	var addrs []addrInfo
	for _, ep := range externalProxies {
		addr := addrInfo{
			Server: ep.Dest,
			Port:   ep.Port,
			Remark: ep.Remark,
		}
		if addr.Server == "" {
			addr.Server = server
		}
		if addr.Port == 0 {
			addr.Port = port
		}
		if addr.Remark == "" {
			addr.Remark = fmt.Sprintf("%s:%d", addr.Server, addr.Port)
		}
		addrs = append(addrs, addr)
	}
	return addrs
}

func getProxyName(inbound model.Inbound, email, epRemark string) string {
	if epRemark != "" {
		return epRemark
	}
	if inbound.Remark != "" {
		if email != "" {
			return fmt.Sprintf("%s-%s", inbound.Remark, email)
		}
		return inbound.Remark
	}
	if email != "" {
		return fmt.Sprintf("%s-%d-%s", inbound.Protocol, inbound.Port, email)
	}
	return fmt.Sprintf("%s-%d", inbound.Protocol, inbound.Port)
}

func applyTLSSettings(entry *ProxyEntry, tlsSettings map[string]any) {
	if tlsSettings == nil {
		return
	}
	if sni, ok := tlsSettings["serverName"].(string); ok && sni != "" {
		entry.ServerName = sni
	}
	if fp, ok := tlsSettings["fingerprint"].(string); ok && fp != "" {
		entry.ClientFingerprint = fp
	}
	if alpn, ok := tlsSettings["alpn"].([]any); ok {
		for _, a := range alpn {
			if s, ok := a.(string); ok {
				entry.ALPN = append(entry.ALPN, s)
			}
		}
	}
	if allowInsecure, ok := tlsSettings["allowInsecure"].(bool); ok && allowInsecure {
		entry.SkipCertVerify = true
	}
}

func applyRealitySettings(entry *ProxyEntry, realitySettings map[string]any) {
	if realitySettings == nil {
		return
	}
	entry.TLS = true

	// Extract serverName from serverNames array (upstream format)
	if serverNames, ok := realitySettings["serverNames"].([]any); ok && len(serverNames) > 0 {
		if s, ok := serverNames[0].(string); ok {
			entry.ServerName = s
		}
	}
	// Also try direct serverName field
	if entry.ServerName == "" {
		if sni, ok := realitySettings["serverName"].(string); ok && sni != "" {
			entry.ServerName = sni
		}
	}

	// Extract settings from nested "settings" key (upstream format)
	realityClientSettings := realitySettings
	if nested, ok := realitySettings["settings"].(map[string]any); ok {
		realityClientSettings = nested
	}

	realityOpts := make(map[string]string)

	// publicKey
	if pk, ok := realityClientSettings["publicKey"].(string); ok && pk != "" {
		realityOpts["public-key"] = pk
	}

	// shortId (from shortIds array or direct)
	if shortIDs, ok := realitySettings["shortIds"].([]any); ok && len(shortIDs) > 0 {
		if s, ok := shortIDs[0].(string); ok {
			realityOpts["short-id"] = s
		}
	}
	if _, exists := realityOpts["short-id"]; !exists {
		if sid, ok := realityClientSettings["shortId"].(string); ok && sid != "" {
			realityOpts["short-id"] = sid
		}
	}

	if len(realityOpts) > 0 {
		entry.RealityOpts = realityOpts
	}

	// fingerprint
	if fp, ok := realityClientSettings["fingerprint"].(string); ok && fp != "" {
		entry.ClientFingerprint = fp
	}
}

func applyNetworkOpts(entry *ProxyEntry, ss StreamSettings, network string) {
	switch network {
	case "ws":
		wsOpts := make(map[string]any)
		if ss.WSSettings != nil {
			if path, ok := ss.WSSettings["path"].(string); ok && path != "" {
				wsOpts["path"] = path
			}
			if host, ok := ss.WSSettings["headers"].(map[string]any); ok {
				if hostVal, ok := host["Host"].(string); ok && hostVal != "" {
					wsOpts["headers"] = map[string]string{"Host": hostVal}
				}
			}
		}
		if len(wsOpts) > 0 {
			if _, hasPath := wsOpts["path"]; !hasPath {
				wsOpts["path"] = "/"
			}
			entry.WSOpts = wsOpts
		}
	case "grpc":
		grpcOpts := make(map[string]string)
		if ss.GRPCSettings != nil {
			if svcName, ok := ss.GRPCSettings["serviceName"].(string); ok && svcName != "" {
				grpcOpts["grpc-service-name"] = svcName
			}
		}
		if len(grpcOpts) > 0 {
			entry.GRPCOpts = grpcOpts
		}
	case "h2", "http":
		h2Opts := make(map[string]any)
		if ss.HTTPSettings != nil {
			if path, ok := ss.HTTPSettings["path"].(string); ok && path != "" {
				h2Opts["path"] = path
			}
			if host, ok := ss.HTTPSettings["host"].([]any); ok && len(host) > 0 {
				h2Opts["host"] = host
			}
		}
		if len(h2Opts) > 0 {
			entry.H2Opts = h2Opts
		}
	}
}

// ---------------------------------------------------------------------------
// Manual YAML serialization (ensures Clash Party compatibility)
// ---------------------------------------------------------------------------

func marshalClashYAML(cfg ClashConfig, configName string) (string, error) {
	var sb strings.Builder

	// Header
	if configName != "" {
		sb.WriteString(fmt.Sprintf("# Profile: %s\n", configName))
	}
	sb.WriteString(fmt.Sprintf("mixed-port: %d\n", cfg.MixedPort))
	sb.WriteString(fmt.Sprintf("allow-lan: %t\n", cfg.AllowLan))
	sb.WriteString(fmt.Sprintf("mode: %s\n", cfg.Mode))
	sb.WriteString(fmt.Sprintf("log-level: %s\n", cfg.LogLevel))

	// Proxies
	sb.WriteString("\nproxies:\n")
	if len(cfg.Proxies) == 0 {
		sb.WriteString("  []\n")
	} else {
		for _, p := range cfg.Proxies {
			marshalProxy(&sb, p, "  ")
		}
	}

	// Proxy Groups
	sb.WriteString("\nproxy-groups:\n")
	for _, pg := range cfg.ProxyGroups {
		sb.WriteString(fmt.Sprintf("  - name: \"%s\"\n", pg.Name))
		sb.WriteString(fmt.Sprintf("    type: %s\n", pg.Type))
		sb.WriteString("    proxies:\n")
		for _, pn := range pg.Proxies {
			if pn == "DIRECT" {
				sb.WriteString("      - DIRECT\n")
			} else {
				sb.WriteString(fmt.Sprintf("      - \"%s\"\n", pn))
			}
		}
	}

	// Rules
	sb.WriteString("\nrules:\n")
	for _, r := range cfg.Rules {
		sb.WriteString(fmt.Sprintf("  - %s\n", r))
	}

	return sb.String(), nil
}

func marshalProxy(sb *strings.Builder, p ProxyEntry, indent string) {
	sb.WriteString(fmt.Sprintf("%s- name: \"%s\"\n", indent, p.Name))
	sb.WriteString(fmt.Sprintf("%s  type: %s\n", indent, p.Type))
	sb.WriteString(fmt.Sprintf("%s  server: %s\n", indent, p.Server))
	sb.WriteString(fmt.Sprintf("%s  port: %d\n", indent, p.Port))

	if p.UUID != "" {
		sb.WriteString(fmt.Sprintf("%s  uuid: %s\n", indent, p.UUID))
	}
	if p.Password != "" {
		sb.WriteString(fmt.Sprintf("%s  password: \"%s\"\n", indent, p.Password))
	}
	if p.Cipher != "" {
		sb.WriteString(fmt.Sprintf("%s  cipher: %s\n", indent, p.Cipher))
	}
	if p.AlterID > 0 {
		sb.WriteString(fmt.Sprintf("%s  alterId: %d\n", indent, p.AlterID))
	}
	if p.Network != "" && p.Network != "tcp" {
		sb.WriteString(fmt.Sprintf("%s  network: %s\n", indent, p.Network))
	}
	if p.TLS {
		sb.WriteString(fmt.Sprintf("%s  tls: true\n", indent))
	}
	if p.UDP {
		sb.WriteString(fmt.Sprintf("%s  udp: true\n", indent))
	}
	if p.Flow != "" {
		sb.WriteString(fmt.Sprintf("%s  flow: %s\n", indent, p.Flow))
	}
	if p.ServerName != "" {
		sb.WriteString(fmt.Sprintf("%s  servername: %s\n", indent, p.ServerName))
	}
	if p.SNI != "" {
		sb.WriteString(fmt.Sprintf("%s  sni: %s\n", indent, p.SNI))
	}
	if p.ClientFingerprint != "" {
		sb.WriteString(fmt.Sprintf("%s  client-fingerprint: %s\n", indent, p.ClientFingerprint))
	}
	if len(p.RealityOpts) > 0 {
		sb.WriteString(fmt.Sprintf("%s  reality-opts:\n", indent))
		for k, v := range p.RealityOpts {
			sb.WriteString(fmt.Sprintf("%s    %s: \"%s\"\n", indent, k, v))
		}
	}
	if p.SkipCertVerify {
		sb.WriteString(fmt.Sprintf("%s  skip-cert-verify: true\n", indent))
	}
	if len(p.ALPN) > 0 {
		quoted := make([]string, len(p.ALPN))
		for i, a := range p.ALPN {
			quoted[i] = fmt.Sprintf("\"%s\"", a)
		}
		sb.WriteString(fmt.Sprintf("%s  alpn: [%s]\n", indent, strings.Join(quoted, ", ")))
	}
	if p.WSOpts != nil {
		sb.WriteString(fmt.Sprintf("%s  ws-opts:\n", indent))
		for k, v := range p.WSOpts {
			switch val := v.(type) {
			case string:
				sb.WriteString(fmt.Sprintf("%s    %s: \"%s\"\n", indent, k, val))
			case map[string]string:
				sb.WriteString(fmt.Sprintf("%s    %s:\n", indent, k))
				for hk, hv := range val {
					sb.WriteString(fmt.Sprintf("%s      %s: \"%s\"\n", indent, hk, hv))
				}
			default:
				sb.WriteString(fmt.Sprintf("%s    %s: %v\n", indent, k, val))
			}
		}
	}
	if p.GRPCOpts != nil {
		sb.WriteString(fmt.Sprintf("%s  grpc-opts:\n", indent))
		for k, v := range p.GRPCOpts {
			sb.WriteString(fmt.Sprintf("%s    %s: \"%s\"\n", indent, k, v))
		}
	}
	if p.H2Opts != nil {
		sb.WriteString(fmt.Sprintf("%s  h2-opts:\n", indent))
		for k, v := range p.H2Opts {
			switch val := v.(type) {
			case string:
				sb.WriteString(fmt.Sprintf("%s    %s: \"%s\"\n", indent, k, val))
			case []any:
				sb.WriteString(fmt.Sprintf("%s    %s:\n", indent, k))
				for _, hv := range val {
					sb.WriteString(fmt.Sprintf("%s      - \"%v\"\n", indent, hv))
				}
			default:
				sb.WriteString(fmt.Sprintf("%s    %s: %v\n", indent, k, val))
			}
		}
	}
	if p.Protocol != "" {
		sb.WriteString(fmt.Sprintf("%s  protocol: %s\n", indent, p.Protocol))
	}
	if p.OBFS != "" {
		sb.WriteString(fmt.Sprintf("%s  obfs: %s\n", indent, p.OBFS))
	}
	if p.ProtocolParam != "" {
		sb.WriteString(fmt.Sprintf("%s  protocol-param: \"%s\"\n", indent, p.ProtocolParam))
	}
	if p.OBFSParam != "" {
		sb.WriteString(fmt.Sprintf("%s  obfs-param: \"%s\"\n", indent, p.OBFSParam))
	}
}
