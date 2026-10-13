package config

import (
	"path/filepath"
	"testing"

	"github.com/anviod/edgeCore/internal/model"
	"github.com/anviod/edgeCore/internal/storage"
)

// TestLoadConfigFromDBDedupesDuplicateDevices 回归：持久化数据中若存在通道内
// 重复设备 ID（历史版本缺陷：设备改名未同步 ID / 复制设备 ID 冲突），
// LoadConfigFromDB 必须"保留首个"去重，而不是把带脏数据的通道原样交给
// AddChannel 的 Fail-Fast 校验拒收整条通道（会导致重启后该通道采集全部消失）。
func TestLoadConfigFromDBDedupesDuplicateDevices(t *testing.T) {
	tempDir := testOutputDir(t)
	dbPath := filepath.Join(tempDir, "data")
	store, err := storage.NewStorage(dbPath)
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}
	defer store.Close()

	ch := model.Channel{
		ID:       "ch_dup_test",
		Name:     "BACNET",
		Protocol: "bacnet-ip",
		Enable:   true,
		Devices: []model.Device{
			{ID: "bacnet-100", Name: "RoomController A", Enable: true, Config: map[string]interface{}{"bacnet_device_id": 100}},
			// 模拟历史脏数据：同 ID 重复出现（内容指向另一台物理设备）
			{ID: "bacnet-200", Name: "RoomController B", Enable: true, Config: map[string]interface{}{"bacnet_device_id": 200}},
			{ID: "bacnet-200", Name: "RoomController B duplicate", Enable: true, Config: map[string]interface{}{"bacnet_device_id": 200}},
			{ID: "bacnet-300", Name: "RoomController C", Enable: true, Config: map[string]interface{}{"bacnet_device_id": 300}},
		},
	}
	configStore, err := storage.NewConfigStore(store.GetConfigDB())
	if err != nil {
		t.Fatalf("NewConfigStore failed: %v", err)
	}
	if err := configStore.SaveChannels([]model.Channel{ch}); err != nil {
		t.Fatalf("SaveChannels failed: %v", err)
	}

	cfg, err := LoadConfigFromDB(store.GetConfigDB())
	if err != nil {
		t.Fatalf("LoadConfigFromDB failed: %v", err)
	}

	if len(cfg.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(cfg.Channels))
	}
	got := cfg.Channels[0].Devices
	if len(got) != 3 {
		t.Fatalf("expected 3 devices after dedupe, got %d: %+v", len(got), deviceIDs(got))
	}
	wantOrder := []string{"bacnet-100", "bacnet-200", "bacnet-300"}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Fatalf("device[%d] = %s, want %s (keep-first order violated)", i, got[i].ID, id)
		}
	}
	if got[1].Name != "RoomController B" {
		t.Fatalf("duplicate kept wrong entry: kept %q, want first occurrence %q", got[1].Name, "RoomController B")
	}
}

func deviceIDs(devices []model.Device) []string {
	ids := make([]string, 0, len(devices))
	for _, d := range devices {
		ids = append(ids, d.ID)
	}
	return ids
}
