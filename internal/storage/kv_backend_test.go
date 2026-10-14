package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/anviod/edgeCore/internal/model"
)

// 本文件对 bbolt / SQLite 两个后端跑同一套语义断言，保证 KVDB 抽象行为一致。

func backendNames() []string {
	return []string{BackendBbolt, BackendSQLite}
}

func newBackendStorage(t *testing.T, backend string) *Storage {
	t.Helper()
	s, err := NewStorageWithBackend(t.TempDir(), backend)
	if err != nil {
		t.Fatalf("[%s] NewStorageWithBackend: %v", backend, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestKVBackends_Match(t *testing.T) {
	for _, backend := range backendNames() {
		t.Run(backend, func(t *testing.T) {
			s := newBackendStorage(t, backend)

			if got := s.Backend(); got != backend {
				t.Fatalf("Backend() = %q, want %q", got, backend)
			}
			if got := s.GetConfigDB().Backend(); got != backend {
				t.Fatalf("configDB backend = %q, want %q", got, backend)
			}
			if got := s.GetDB().Backend(); got != backend {
				t.Fatalf("runtimeDB backend = %q, want %q", got, backend)
			}

			// 通用 KV：SaveData / GetData / DeleteData（runtime bucket）
			if err := s.SaveData(BucketDataCache, "k1", map[string]any{"v": 1}); err != nil {
				t.Fatalf("SaveData: %v", err)
			}
			var got map[string]any
			if err := s.GetData(BucketDataCache, "k1", &got); err != nil {
				t.Fatalf("GetData: %v", err)
			}
			if got["v"].(float64) != 1 {
				t.Fatalf("roundtrip mismatch: %v", got)
			}
			if err := s.DeleteData(BucketDataCache, "k1"); err != nil {
				t.Fatalf("DeleteData: %v", err)
			}
			if err := s.GetData(BucketDataCache, "k1", &got); err == nil {
				t.Fatal("GetData after delete should fail")
			}

			// 配置 bucket 走 configDB
			if err := s.SaveData(BucketServer, "server", model.ServerConfig{Port: 8080, LogLevel: "info"}); err != nil {
				t.Fatalf("SaveData config bucket: %v", err)
			}
			var srv model.ServerConfig
			if err := s.GetData(BucketServer, "server", &srv); err != nil || srv.Port != 8080 {
				t.Fatalf("config bucket roundtrip: %v %+v", err, srv)
			}

			// LoadAll / LoadRange / LoadLatest
			for i := 0; i < 10; i++ {
				if err := s.SaveData(BucketDataCache, fmt.Sprintf("key-%03d", i), i); err != nil {
					t.Fatal(err)
				}
			}
			seen := map[string]int{}
			if err := s.LoadAll(BucketDataCache, func(k, v []byte) error {
				seen[string(k)] = 1
				return nil
			}); err != nil {
				t.Fatalf("LoadAll: %v", err)
			}
			if len(seen) != 10 {
				t.Fatalf("LoadAll got %d keys, want 10", len(seen))
			}

			ranged := 0
			if err := s.LoadRange(BucketDataCache, "key-002", "key-005", func(k, v []byte) error {
				ranged++
				return nil
			}); err != nil {
				t.Fatalf("LoadRange: %v", err)
			}
			if ranged != 4 {
				t.Fatalf("LoadRange got %d, want 4", ranged)
			}

			latest := 0
			if err := s.LoadLatest(BucketDataCache, 3, func(k, v []byte) error {
				latest++
				return nil
			}); err != nil {
				t.Fatalf("LoadLatest: %v", err)
			}
			if latest != 3 {
				t.Fatalf("LoadLatest got %d, want 3", latest)
			}

			// ClearBucket
			if err := s.ClearBucket(BucketDataCache); err != nil {
				t.Fatalf("ClearBucket: %v", err)
			}
			if err := s.LoadAll(BucketDataCache, func(k, v []byte) error {
				t.Fatalf("ClearBucket left key %s", k)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKVBackends_ConfigStore(t *testing.T) {
	for _, backend := range backendNames() {
		t.Run(backend, func(t *testing.T) {
			s := newBackendStorage(t, backend)
			cs, err := NewConfigStore(s.GetConfigDB())
			if err != nil {
				t.Fatalf("NewConfigStore: %v", err)
			}

			channels := []model.Channel{{ID: "ch-1", Name: "Channel 1", Protocol: "modbus-tcp"}}
			if err := cs.SaveChannels(channels); err != nil {
				t.Fatalf("SaveChannels: %v", err)
			}
			loaded, err := cs.LoadChannels()
			if err != nil || len(loaded) != 1 || loaded[0].ID != "ch-1" {
				t.Fatalf("LoadChannels: %v %+v", err, loaded)
			}

			users := []model.UserConfig{{Username: "admin", Password: "x"}}
			if err := cs.SaveUsers(users); err != nil {
				t.Fatalf("SaveUsers: %v", err)
			}
			if ok, err := cs.HasUsers(); err != nil || !ok {
				t.Fatalf("HasUsers: %v %v", ok, err)
			}

			// 导入守卫：缺失 section 保留现值（P0-2 语义在两个后端一致）
			if err := cs.ImportConfig(&ConfigExport{}); err != nil {
				t.Fatalf("ImportConfig empty: %v", err)
			}
			after, err := cs.LoadChannels()
			if err != nil || len(after) != 1 {
				t.Fatalf("ImportConfig must not wipe channels: %v %+v", err, after)
			}

			// 导出→归档→导入（跨后端：bbolt 导出可在 sqlite 端导入，反之亦然）
			export, err := cs.ExportAllConfig()
			if err != nil {
				t.Fatalf("ExportAllConfig: %v", err)
			}

			other := BackendSQLite
			if backend == BackendSQLite {
				other = BackendBbolt
			}
			s2 := newBackendStorage(t, other)
			cs2, err := NewConfigStore(s2.GetConfigDB())
			if err != nil {
				t.Fatal(err)
			}
			if err := cs2.ImportConfig(export); err != nil {
				t.Fatalf("cross-backend ImportConfig %s->%s: %v", backend, other, err)
			}
			cross, err := cs2.LoadChannels()
			if err != nil || len(cross) != 1 || cross[0].ID != "ch-1" {
				t.Fatalf("cross-backend channels: %v %+v", err, cross)
			}
		})
	}
}

func TestKVBackends_BackupAndCompact(t *testing.T) {
	for _, backend := range backendNames() {
		t.Run(backend, func(t *testing.T) {
			s := newBackendStorage(t, backend)

			// 配置库备份：写入 config bucket 后备份，恢复快照验证
			if err := s.SaveData(BucketServer, "server", model.ServerConfig{Port: 9099, LogLevel: "debug"}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 100; i++ {
				_ = s.SaveData(BucketDataCache, fmt.Sprintf("k%03d", i), i)
				_ = s.DeleteData(BucketDataCache, fmt.Sprintf("k%03d", i))
			}

			// 备份快照可被同后端重新打开并读出
			backupDir := filepath.Join(t.TempDir(), "backup")
			info, err := s.BackupConfigDB(backupDir)
			if err != nil {
				t.Fatalf("BackupConfigDB: %v", err)
			}
			if info.FileSizeBytes <= 0 {
				t.Fatal("backup file is empty")
			}

			restoreDir := t.TempDir()
			restorePath := filepath.Join(restoreDir, "restored"+filepath.Ext(info.BackupPath))
			// BackupTo 输出即完整数据库文件：复制到新位置验证可打开
			data, err := os.ReadFile(info.BackupPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(restorePath, data, 0600); err != nil {
				t.Fatal(err)
			}
			kv, err := openKV(restorePath, false, backend)
			if err != nil {
				t.Fatalf("reopen backup: %v", err)
			}
			defer kv.Close()
			var srv model.ServerConfig
			err = kv.View(func(tx KVTx) error {
				raw := tx.Get(BucketServer, []byte("server"))
				if raw == nil {
					return fmt.Errorf("server config missing in backup")
				}
				return json.Unmarshal(raw, &srv)
			})
			if err != nil {
				t.Fatalf("read backup: %v", err)
			}
			if srv.Port != 9099 {
				t.Fatalf("backup value mismatch: %+v", srv)
			}

			// 运行时数据写入后 Compact，数据仍在
			if err := s.SaveValue(model.Value{PointID: "p1", Value: 42}); err != nil {
				t.Fatal(err)
			}
			if err := s.CompactRuntimeDB(); err != nil {
				t.Fatalf("CompactRuntimeDB: %v", err)
			}
			if _, err := s.GetLastValue("p1"); err != nil {
				t.Fatalf("GetLastValue after compact: %v", err)
			}

			// meta 文件记录了后端选择
			if got := ReadBackendChoice(s.dataDir); got != backend {
				t.Fatalf("ReadBackendChoice = %q, want %q", got, backend)
			}
		})
	}
}
