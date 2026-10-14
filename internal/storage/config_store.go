package storage

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anviod/edgeCore/internal/model"
)

const (
	BucketConfigVersion  = "ConfigVersion"
	BucketChannels       = "Channels"
	BucketDevices        = "Devices"
	BucketNorthbound     = "Northbound"
	BucketEdgeRules      = "EdgeRules"
	BucketSystem         = "System"
	BucketUsers          = "Users"
	BucketServer         = "Server"
	BucketVirtualShadows = "VirtualShadows"
	BucketAICopilot      = "ai_copilot"
	ConfigVersionKey     = "version"
	ConfigVersionValue   = "1.0"
)

type ConfigStore struct {
	db KVDB
}

// NewConfigStore 基于 KVDB 后端构建配置仓库（bbolt / SQLite 双后端共用）。
func NewConfigStore(db KVDB) (*ConfigStore, error) {
	err := db.Update(func(tx KVTx) error {
		buckets := []string{
			BucketConfigVersion,
			BucketChannels,
			BucketDevices,
			BucketNorthbound,
			BucketEdgeRules,
			BucketSystem,
			BucketUsers,
			BucketServer,
			BucketVirtualShadows,
			BucketAICopilot,
		}
		for _, bucket := range buckets {
			if err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", bucket, err)
			}
		}

		currentVersion := tx.Get(BucketConfigVersion, []byte(ConfigVersionKey))
		if currentVersion == nil {
			return tx.Put(BucketConfigVersion, []byte(ConfigVersionKey), []byte(ConfigVersionValue))
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &ConfigStore{db: db}, nil
}

func (cs *ConfigStore) SaveServerConfig(config model.ServerConfig) error {
	return cs.saveJSON(BucketServer, "server", config)
}

func (cs *ConfigStore) LoadServerConfig() (*model.ServerConfig, error) {
	var config model.ServerConfig
	err := cs.loadJSON(BucketServer, "server", &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (cs *ConfigStore) SaveChannels(channels []model.Channel) error {
	return cs.saveJSON(BucketChannels, "channels", channels)
}

func (cs *ConfigStore) LoadChannels() ([]model.Channel, error) {
	var channels []model.Channel
	err := cs.loadJSON(BucketChannels, "channels", &channels)
	if err != nil {
		return nil, err
	}
	return channels, nil
}

func (cs *ConfigStore) SaveDevice(device model.Device) error {
	if err := model.EnsureDeviceID(&device); err != nil {
		return err
	}
	return cs.saveJSON(BucketDevices, device.ID, device)
}

func (cs *ConfigStore) LoadDevice(deviceID string) (*model.Device, error) {
	var device model.Device
	err := cs.loadJSON(BucketDevices, deviceID, &device)
	if err != nil {
		return nil, err
	}
	return &device, nil
}

func (cs *ConfigStore) LoadAllDevices() (map[string]model.Device, error) {
	result := make(map[string]model.Device)
	err := cs.db.View(func(tx KVTx) error {
		return tx.ForEach(BucketDevices, func(k, v []byte) error {
			var device model.Device
			if err := json.Unmarshal(v, &device); err == nil {
				result[string(k)] = device
			}
			return nil
		})
	})
	return result, err
}

func (cs *ConfigStore) DeleteDevice(deviceID string) error {
	return cs.db.Update(func(tx KVTx) error {
		return tx.Delete(BucketDevices, []byte(deviceID))
	})
}

func (cs *ConfigStore) SaveNorthbound(config model.NorthboundConfig) error {
	return cs.saveJSON(BucketNorthbound, "northbound", config)
}

func (cs *ConfigStore) LoadNorthbound() (*model.NorthboundConfig, error) {
	var config model.NorthboundConfig
	err := cs.loadJSON(BucketNorthbound, "northbound", &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (cs *ConfigStore) SaveVirtualShadows(devices []model.VirtualShadowDeviceConfig) error {
	return cs.saveJSON(BucketVirtualShadows, "virtual_shadows", devices)
}

func (cs *ConfigStore) LoadVirtualShadows() ([]model.VirtualShadowDeviceConfig, error) {
	var devices []model.VirtualShadowDeviceConfig
	err := cs.loadJSON(BucketVirtualShadows, "virtual_shadows", &devices)
	if err != nil {
		return nil, err
	}
	if devices == nil {
		return []model.VirtualShadowDeviceConfig{}, nil
	}
	return devices, nil
}

func (cs *ConfigStore) SaveEdgeRules(rules []model.EdgeRule) error {
	return cs.saveJSON(BucketEdgeRules, "edge_rules", rules)
}

func (cs *ConfigStore) LoadEdgeRules() ([]model.EdgeRule, error) {
	var rules []model.EdgeRule
	err := cs.loadJSON(BucketEdgeRules, "edge_rules", &rules)
	if err != nil {
		return nil, err
	}
	return rules, nil
}

func (cs *ConfigStore) SaveSystem(config model.SystemConfig) error {
	return cs.saveJSON(BucketSystem, "system", config)
}

func (cs *ConfigStore) LoadSystem() (*model.SystemConfig, error) {
	var config model.SystemConfig
	err := cs.loadJSON(BucketSystem, "system", &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (cs *ConfigStore) SaveAICopilotSettings(settings model.AICopilotSettings) error {
	return cs.saveJSON(BucketAICopilot, "settings", settings)
}

func (cs *ConfigStore) LoadAICopilotSettings() (*model.AICopilotSettings, error) {
	var settings model.AICopilotSettings
	err := cs.loadJSON(BucketAICopilot, "settings", &settings)
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (cs *ConfigStore) SaveUsers(users []model.UserConfig) error {
	return cs.saveJSON(BucketUsers, "users", users)
}

func (cs *ConfigStore) LoadUsers() ([]model.UserConfig, error) {
	var users []model.UserConfig
	err := cs.loadJSON(BucketUsers, "users", &users)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (cs *ConfigStore) HasConfigData() (bool, error) {
	var hasData bool
	err := cs.db.View(func(tx KVTx) error {
		data := tx.Get(BucketChannels, []byte("channels"))
		if len(data) > 0 {
			hasData = true
			return nil
		}

		cursor := tx.Cursor(BucketDevices)
		if k, _ := cursor.First(); k != nil {
			hasData = true
			return nil
		}

		if data = tx.Get(BucketNorthbound, []byte("northbound")); len(data) > 0 {
			hasData = true
			return nil
		}

		if data = tx.Get(BucketEdgeRules, []byte("edge_rules")); len(data) > 0 {
			hasData = true
		}

		return nil
	})
	return hasData, err
}

func (cs *ConfigStore) HasUsers() (bool, error) {
	var hasUsers bool
	err := cs.db.View(func(tx KVTx) error {
		data := tx.Get(BucketUsers, []byte("users"))
		if len(data) > 0 {
			var users []model.UserConfig
			if err := json.Unmarshal(data, &users); err == nil && len(users) > 0 {
				hasUsers = true
			}
		}
		return nil
	})
	return hasUsers, err
}

func (cs *ConfigStore) IsSystemInitialized() (bool, error) {
	hasUsers, err := cs.HasUsers()
	if err != nil {
		return false, err
	}
	if !hasUsers {
		return false, nil
	}
	return true, nil
}

func (cs *ConfigStore) ExportAllConfig() (*ConfigExport, error) {
	export := &ConfigExport{}
	err := cs.db.View(func(tx KVTx) error {
		if err := loadFromBucket(tx, BucketServer, "server", &export.Server); err != nil {
			return err
		}
		if err := loadFromBucket(tx, BucketChannels, "channels", &export.Channels); err != nil {
			return err
		}
		if err := loadFromBucket(tx, BucketNorthbound, "northbound", &export.Northbound); err != nil {
			return err
		}
		if err := loadFromBucket(tx, BucketEdgeRules, "edge_rules", &export.EdgeRules); err != nil {
			return err
		}
		if err := loadFromBucket(tx, BucketSystem, "system", &export.System); err != nil {
			return err
		}
		if err := loadFromBucket(tx, BucketUsers, "users", &export.Users); err != nil {
			return err
		}
		if err := loadFromBucket(tx, BucketVirtualShadows, "virtual_shadows", &export.VirtualShadows); err != nil {
			return err
		}

		export.Devices = make(map[string]model.Device)
		err := tx.ForEach(BucketDevices, func(k, v []byte) error {
			var device model.Device
			if err := json.Unmarshal(v, &device); err == nil {
				export.Devices[string(k)] = device
			}
			return nil
		})
		return err
	})
	return export, err
}

// ImportConfigReplace replaces configuration buckets from export data.
// When preserve options are set, current users and/or server port are kept.
func (cs *ConfigStore) ImportConfigReplace(export *ConfigExport, opts ImportPreserveOptions) error {
	if export == nil {
		return fmt.Errorf("import data is required")
	}

	if opts.PreserveUsers {
		users, err := cs.LoadUsers()
		if err != nil {
			return fmt.Errorf("failed to load current users: %w", err)
		}
		export.Users = users
	}

	if opts.PreserveServerPort {
		serverConfig, err := cs.LoadServerConfig()
		if err != nil {
			return fmt.Errorf("failed to load current server config: %w", err)
		}
		if serverConfig != nil {
			export.Server.Port = serverConfig.Port
		}
	}

	return cs.db.Update(func(tx KVTx) error {
		if err := saveToBucket(tx, BucketServer, "server", export.Server); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketChannels, "channels", export.Channels); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketNorthbound, "northbound", export.Northbound); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketEdgeRules, "edge_rules", export.EdgeRules); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketSystem, "system", export.System); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketUsers, "users", export.Users); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketVirtualShadows, "virtual_shadows", export.VirtualShadows); err != nil {
			return err
		}

		if err := replaceDevicesInTx(tx, export.Devices); err != nil {
			return err
		}

		return tx.Put(BucketConfigVersion, []byte(ConfigVersionKey), []byte(ConfigVersionValue))
	})
}

func (cs *ConfigStore) ImportConfig(export *ConfigExport) error {
	return cs.db.Update(func(tx KVTx) error {
		// P0 数据丢失防护（2026-10-13 审查）：nil 字段 = 导入数据未提供该 section，
		// 保留 DB 现值，绝不写成 null/空覆盖。缺 channels 字段的导入文件
		// 曾经把整批通道清空；缺 users 则登录直接失效。
		if export.Channels == nil {
			if cur, err := cs.loadChannelsFrom(tx); err == nil {
				export.Channels = cur
			}
		}
		if export.Users == nil {
			if data := tx.Get(BucketUsers, []byte("users")); data != nil {
				var cur []model.UserConfig
				if json.Unmarshal(data, &cur) == nil {
					export.Users = cur
				}
			}
		}
		if export.EdgeRules == nil {
			if data := tx.Get(BucketEdgeRules, []byte("edge_rules")); data != nil {
				var cur []model.EdgeRule
				if json.Unmarshal(data, &cur) == nil {
					export.EdgeRules = cur
				}
			}
		}
		if export.VirtualShadows == nil {
			if data := tx.Get(BucketVirtualShadows, []byte("virtual_shadows")); data != nil {
				var cur []model.VirtualShadowDeviceConfig
				if json.Unmarshal(data, &cur) == nil {
					export.VirtualShadows = cur
				}
			}
		}
		if export.Devices == nil {
			existing := make(map[string]model.Device)
			_ = tx.ForEach(BucketDevices, func(k, v []byte) error {
				var dev model.Device
				if json.Unmarshal(v, &dev) == nil {
					existing[string(k)] = dev
				}
				return nil
			})
			export.Devices = existing
		}

		if err := saveToBucket(tx, BucketServer, "server", export.Server); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketChannels, "channels", export.Channels); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketNorthbound, "northbound", export.Northbound); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketEdgeRules, "edge_rules", export.EdgeRules); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketSystem, "system", export.System); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketUsers, "users", export.Users); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketVirtualShadows, "virtual_shadows", export.VirtualShadows); err != nil {
			return err
		}

		if export.Devices != nil {
			if err := upsertDevicesInTx(tx, export.Devices); err != nil {
				return err
			}
		}

		return tx.Put(BucketConfigVersion, []byte(ConfigVersionKey), []byte(ConfigVersionValue))
	})
}

func (cs *ConfigStore) SaveAllConfig(server model.ServerConfig, channels []model.Channel, devices []model.Device,
	northbound model.NorthboundConfig, edgeRules []model.EdgeRule, system model.SystemConfig, users []model.UserConfig) error {
	return cs.db.Update(func(tx KVTx) error {
		if err := saveToBucket(tx, BucketServer, "server", server); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketChannels, "channels", channels); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketNorthbound, "northbound", northbound); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketEdgeRules, "edge_rules", edgeRules); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketSystem, "system", system); err != nil {
			return err
		}
		if err := saveToBucket(tx, BucketUsers, "users", users); err != nil {
			return err
		}

		normalized, err := model.NormalizeDevicesForSave(devices)
		if err != nil {
			return err
		}

		for _, device := range normalized {
			data, err := json.Marshal(device)
			if err != nil {
				return err
			}
			if err := tx.Put(BucketDevices, []byte(device.ID), data); err != nil {
				return err
			}
		}

		return nil
	})
}

type ConfigExport struct {
	Server         model.ServerConfig                `json:"server"`
	Channels       []model.Channel                   `json:"channels"`
	Devices        map[string]model.Device           `json:"devices"`
	Northbound     model.NorthboundConfig            `json:"northbound"`
	EdgeRules      []model.EdgeRule                  `json:"edge_rules"`
	System         model.SystemConfig                `json:"system"`
	Users          []model.UserConfig                `json:"users"`
	VirtualShadows []model.VirtualShadowDeviceConfig `json:"virtual_shadows"`
}

func (cs *ConfigStore) saveJSON(bucketName, key string, data interface{}) error {
	return cs.db.Update(func(tx KVTx) error {
		return saveToBucket(tx, bucketName, key, data)
	})
}

func (cs *ConfigStore) loadJSON(bucketName, key string, result interface{}) error {
	return cs.db.View(func(tx KVTx) error {
		return loadFromBucket(tx, bucketName, key, result)
	})
}

// loadChannelsFrom 在指定事务内读取通道列表（nil-safe）。
func (cs *ConfigStore) loadChannelsFrom(tx KVTx) ([]model.Channel, error) {
	var channels []model.Channel
	if err := loadFromBucket(tx, BucketChannels, "channels", &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

func saveToBucket(tx KVTx, bucketName, key string, data interface{}) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("config key is required for bucket %s", bucketName)
	}
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return tx.Put(bucketName, []byte(key), bytes)
}

func loadFromBucket(tx KVTx, bucketName, key string, result interface{}) error {
	data := tx.Get(bucketName, []byte(key))
	if data == nil {
		return nil
	}
	return json.Unmarshal(data, result)
}

// upsertDevicesInTx 把 export.Devices 原样写入 Devices bucket（不清理旧键）。
func upsertDevicesInTx(tx KVTx, devices map[string]model.Device) error {
	if devices == nil {
		return nil
	}
	for id, device := range devices {
		key := strings.TrimSpace(id)
		if key == "" {
			if err := model.EnsureDeviceID(&device); err != nil {
				return err
			}
			key = device.ID
		} else {
			device.ID = key
		}
		data, err := json.Marshal(device)
		if err != nil {
			return err
		}
		if err := tx.Put(BucketDevices, []byte(key), data); err != nil {
			return err
		}
	}
	return nil
}

// replaceDevicesInTx 清空 Devices bucket 后整体写入（导入 replace 语义）。
func replaceDevicesInTx(tx KVTx, devices map[string]model.Device) error {
	var existingKeys [][]byte
	if err := tx.ForEach(BucketDevices, func(k, _ []byte) error {
		existingKeys = append(existingKeys, append([]byte{}, k...))
		return nil
	}); err != nil {
		return err
	}
	for _, key := range existingKeys {
		if err := tx.Delete(BucketDevices, key); err != nil {
			return err
		}
	}
	return upsertDevicesInTx(tx, devices)
}
