package model

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

const shortIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// sanitizeID replaces path-unsafe characters (/, \, ?, #, spaces) with hyphens
// so that the resulting ID can be safely used as a URL path parameter without
// breaking the Fiber radix-tree router (e.g. "c/metrics28319" → "c-metrics28319").
func sanitizeID(id string) string {
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		"?", "-",
		"#", "-",
		" ", "-",
	)
	return replacer.Replace(id)
}

// EnsureChannelID 确保通道具有非空 ID（优先使用 id，其次 name）。
func EnsureChannelID(ch *Channel) error {
	if ch == nil {
		return fmt.Errorf("channel is nil")
	}
	id := strings.TrimSpace(ch.ID)
	if id == "" {
		id = strings.TrimSpace(ch.Name)
	}
	if id == "" {
		return fmt.Errorf("channel ID or name is required")
	}
	ch.ID = sanitizeID(id)
	return nil
}

// GenerateShortID 生成随机小写字母数字 ID（与前端 channel/device ID 风格一致）。
func GenerateShortID(length int) string {
	if length <= 0 {
		length = 16
	}
	out := make([]byte, length)
	max := big.NewInt(int64(len(shortIDAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			out[i] = shortIDAlphabet[i%len(shortIDAlphabet)]
			continue
		}
		out[i] = shortIDAlphabet[n.Int64()]
	}
	return string(out)
}

// IsEndpointLikeDeviceID reports whether id looks like a connection URL rather than a stable device key.
func IsEndpointLikeDeviceID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	lower := strings.ToLower(id)
	return strings.HasPrefix(lower, "opc.tcp://") ||
		strings.HasPrefix(lower, "opc.http://") ||
		strings.Contains(id, "://")
}

// NormalizeOpcUaDeviceID replaces empty or endpoint-like device IDs with a generated short ID.
// Endpoint URLs belong in device config, not as routing identifiers.
func NormalizeOpcUaDeviceID(dev *Device) {
	if dev == nil {
		return
	}
	id := strings.TrimSpace(dev.ID)
	endpoint := ""
	if dev.Config != nil {
		endpoint = configString(dev.Config["endpoint"])
	}
	if id == "" || IsEndpointLikeDeviceID(id) || (endpoint != "" && id == endpoint) {
		dev.ID = GenerateShortID(16)
	}
}

// EnsureDeviceID 确保设备具有非空 ID（优先使用 id，其次 name）。
func EnsureDeviceID(dev *Device) error {
	if dev == nil {
		return fmt.Errorf("device is nil")
	}
	id := strings.TrimSpace(dev.ID)
	if id == "" {
		id = strings.TrimSpace(dev.Name)
	}
	if id == "" {
		return fmt.Errorf("device ID or name is required")
	}
	dev.ID = sanitizeID(id)
	return nil
}

// EnsurePointID 确保点位具有非空 ID（优先使用 id，其次 name）。
func EnsurePointID(p *Point) error {
	if p == nil {
		return fmt.Errorf("point is nil")
	}
	id := strings.TrimSpace(p.ID)
	if id == "" {
		id = strings.TrimSpace(p.Name)
	}
	if id == "" {
		return fmt.Errorf("point ID or name is required")
	}
	p.ID = sanitizeID(id)
	return nil
}

func ensureNamedID(id, name, kind string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = strings.TrimSpace(name)
	}
	if id == "" {
		return "", fmt.Errorf("%s ID or name is required", kind)
	}
	return sanitizeID(id), nil
}

// NormalizeNorthboundForSave 校验北向通道 ID 并剔除运行时 Status 字段，供 edge.db 持久化使用。
func NormalizeNorthboundForSave(cfg NorthboundConfig) (NorthboundConfig, error) {
	out := cfg
	out.Status = nil

	for i := range out.MQTT {
		id, err := ensureNamedID(out.MQTT[i].ID, out.MQTT[i].Name, "MQTT northbound channel")
		if err != nil {
			return NorthboundConfig{}, err
		}
		out.MQTT[i].ID = id
	}
	for i := range out.HTTP {
		id, err := ensureNamedID(out.HTTP[i].ID, out.HTTP[i].Name, "HTTP northbound channel")
		if err != nil {
			return NorthboundConfig{}, err
		}
		out.HTTP[i].ID = id
	}
	for i := range out.OPCUA {
		id, err := ensureNamedID(out.OPCUA[i].ID, out.OPCUA[i].Name, "OPC UA northbound channel")
		if err != nil {
			return NorthboundConfig{}, err
		}
		out.OPCUA[i].ID = id
	}
	for i := range out.SparkplugB {
		id, err := ensureNamedID(out.SparkplugB[i].ID, out.SparkplugB[i].Name, "SparkplugB northbound channel")
		if err != nil {
			return NorthboundConfig{}, err
		}
		out.SparkplugB[i].ID = id
	}
	for i := range out.EdgeOSMQTT {
		id, err := ensureNamedID(out.EdgeOSMQTT[i].ID, out.EdgeOSMQTT[i].Name, "edgeOS(MQTT) northbound channel")
		if err != nil {
			return NorthboundConfig{}, err
		}
		out.EdgeOSMQTT[i].ID = id
	}
	for i := range out.EdgeOSNATS {
		id, err := ensureNamedID(out.EdgeOSNATS[i].ID, out.EdgeOSNATS[i].Name, "edgeOS(NATS) northbound channel")
		if err != nil {
			return NorthboundConfig{}, err
		}
		out.EdgeOSNATS[i].ID = id
	}
	for i := range out.BACnetServer {
		id, err := ensureNamedID(out.BACnetServer[i].ID, out.BACnetServer[i].Name, "BACnet Server northbound channel")
		if err != nil {
			return NorthboundConfig{}, err
		}
		out.BACnetServer[i].ID = id
	}

	return out, nil
}

// EnsureEdgeRuleID 确保边缘规则具有非空 ID（优先使用 id，其次 name）。
func EnsureEdgeRuleID(rule *EdgeRule) error {
	if rule == nil {
		return fmt.Errorf("edge rule is nil")
	}
	id, err := ensureNamedID(rule.ID, rule.Name, "edge rule")
	if err != nil {
		return err
	}
	rule.ID = id
	return nil
}

// NormalizeEdgeRulesForSave 校验并去重边缘规则列表，供 edge.db 持久化使用。
func NormalizeEdgeRulesForSave(rules []EdgeRule) ([]EdgeRule, error) {
	seen := make(map[string]struct{}, len(rules))
	out := make([]EdgeRule, 0, len(rules))
	for _, rule := range rules {
		if err := EnsureEdgeRuleID(&rule); err != nil {
			return nil, err
		}
		if _, ok := seen[rule.ID]; ok {
			continue
		}
		seen[rule.ID] = struct{}{}
		out = append(out, rule)
	}
	return out, nil
}

// NormalizeDevicesForSave 校验并去重设备列表，供配置持久化使用。
func NormalizeDevicesForSave(devices []Device) ([]Device, error) {
	seen := make(map[string]struct{}, len(devices))
	out := make([]Device, 0, len(devices))
	for _, device := range devices {
		if err := EnsureDeviceID(&device); err != nil {
			return nil, err
		}
		if _, ok := seen[device.ID]; ok {
			continue
		}
		seen[device.ID] = struct{}{}
		out = append(out, device)
	}
	return out, nil
}

// SanitizeDevicesForSave 容错版设备校验：剔除无法生成合法 ID 的条目（记录其
// 名称）而不是整体失败。P1 防护（2026-10-13 审查）：所有配置节共用一个全量
// 写入口，Normalize 的 all-or-nothing 语义会让单条脏数据劫持整条持久化管道，
// 导致全系统配置都无法落盘。
func SanitizeDevicesForSave(devices []Device) ([]Device, []string) {
	seen := make(map[string]struct{}, len(devices))
	out := make([]Device, 0, len(devices))
	var dropped []string
	for _, device := range devices {
		if err := EnsureDeviceID(&device); err != nil {
			dropped = append(dropped, fmt.Sprintf("device %q (name %q)", device.ID, device.Name))
			continue
		}
		if _, ok := seen[device.ID]; ok {
			continue
		}
		seen[device.ID] = struct{}{}
		out = append(out, device)
	}
	return out, dropped
}

// filterEntries 泛型过滤：validate 修改并通过则保留，失败则记录剔除。
func filterEntries[T any](items []T, validate func(*T) error, onDrop func(T)) []T {
	kept := items[:0]
	for i := range items {
		if err := validate(&items[i]); err != nil {
			onDrop(items[i])
			continue
		}
		kept = append(kept, items[i])
	}
	return kept
}

// SanitizeNorthboundForSave 容错版北向校验：剔除无法生成合法 ID 的条目而不是整体失败。
func SanitizeNorthboundForSave(cfg NorthboundConfig) (NorthboundConfig, []string) {
	out := cfg
	out.Status = nil
	var dropped []string

	out.MQTT = filterEntries(out.MQTT, func(c *MQTTConfig) error {
		id, err := ensureNamedID(c.ID, c.Name, "MQTT northbound channel")
		if err != nil {
			return err
		}
		c.ID = id
		return nil
	}, func(c MQTTConfig) {
		dropped = append(dropped, fmt.Sprintf("MQTT %q (name %q)", c.ID, c.Name))
	})

	out.HTTP = filterEntries(out.HTTP, func(c *HTTPConfig) error {
		id, err := ensureNamedID(c.ID, c.Name, "HTTP northbound channel")
		if err != nil {
			return err
		}
		c.ID = id
		return nil
	}, func(c HTTPConfig) {
		dropped = append(dropped, fmt.Sprintf("HTTP %q (name %q)", c.ID, c.Name))
	})

	out.OPCUA = filterEntries(out.OPCUA, func(c *OPCUAConfig) error {
		id, err := ensureNamedID(c.ID, c.Name, "OPC UA northbound channel")
		if err != nil {
			return err
		}
		c.ID = id
		return nil
	}, func(c OPCUAConfig) {
		dropped = append(dropped, fmt.Sprintf("OPC UA %q (name %q)", c.ID, c.Name))
	})

	out.SparkplugB = filterEntries(out.SparkplugB, func(c *SparkplugBConfig) error {
		id, err := ensureNamedID(c.ID, c.Name, "SparkplugB northbound channel")
		if err != nil {
			return err
		}
		c.ID = id
		return nil
	}, func(c SparkplugBConfig) {
		dropped = append(dropped, fmt.Sprintf("SparkplugB %q (name %q)", c.ID, c.Name))
	})

	out.EdgeOSMQTT = filterEntries(out.EdgeOSMQTT, func(c *EdgeOSMQTTConfig) error {
		id, err := ensureNamedID(c.ID, c.Name, "edgeOS(MQTT) northbound channel")
		if err != nil {
			return err
		}
		c.ID = id
		return nil
	}, func(c EdgeOSMQTTConfig) {
		dropped = append(dropped, fmt.Sprintf("edgeOS(MQTT) %q (name %q)", c.ID, c.Name))
	})

	out.EdgeOSNATS = filterEntries(out.EdgeOSNATS, func(c *EdgeOSNATSConfig) error {
		id, err := ensureNamedID(c.ID, c.Name, "edgeOS(NATS) northbound channel")
		if err != nil {
			return err
		}
		c.ID = id
		return nil
	}, func(c EdgeOSNATSConfig) {
		dropped = append(dropped, fmt.Sprintf("edgeOS(NATS) %q (name %q)", c.ID, c.Name))
	})

	out.BACnetServer = filterEntries(out.BACnetServer, func(c *BACnetServerConfig) error {
		id, err := ensureNamedID(c.ID, c.Name, "BACnet Server northbound channel")
		if err != nil {
			return err
		}
		c.ID = id
		return nil
	}, func(c BACnetServerConfig) {
		dropped = append(dropped, fmt.Sprintf("BACnet Server %q (name %q)", c.ID, c.Name))
	})

	return out, dropped
}

// SanitizeEdgeRulesForSave 容错版边缘规则校验：剔除无法生成合法 ID 的条目而不是整体失败。
func SanitizeEdgeRulesForSave(rules []EdgeRule) ([]EdgeRule, []string) {
	seen := make(map[string]struct{}, len(rules))
	out := make([]EdgeRule, 0, len(rules))
	var dropped []string
	for _, rule := range rules {
		if err := EnsureEdgeRuleID(&rule); err != nil {
			dropped = append(dropped, fmt.Sprintf("edge rule %q (name %q)", rule.ID, rule.Name))
			continue
		}
		if _, ok := seen[rule.ID]; ok {
			continue
		}
		seen[rule.ID] = struct{}{}
		out = append(out, rule)
	}
	return out, dropped
}
