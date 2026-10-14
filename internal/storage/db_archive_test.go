package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anviod/edgeCore/internal/model"
)

func TestExportImportConfigDBArchive(t *testing.T) {
	tempDir := testOutputDir(t)

	sourcePath := filepath.Join(tempDir, "source-config.db")
	sourceDB, err := openBoltKV(sourcePath, false)
	if err != nil {
		t.Fatalf("failed to open source db: %v", err)
	}

	sourceStore, err := NewConfigStore(sourceDB)
	if err != nil {
		t.Fatalf("failed to create source config store: %v", err)
	}

	err = sourceStore.SaveServerConfig(model.ServerConfig{Port: 9090, LogLevel: "debug"})
	if err != nil {
		t.Fatalf("failed to save server config: %v", err)
	}
	err = sourceStore.SaveUsers([]model.UserConfig{
		{Username: "imported-user", Password: "imported-hash"},
	})
	if err != nil {
		t.Fatalf("failed to save users: %v", err)
	}
	err = sourceStore.SaveChannels([]model.Channel{
		{ID: "ch-1", Name: "Channel 1", Protocol: "modbus-tcp", Enable: true},
	})
	if err != nil {
		t.Fatalf("failed to save channels: %v", err)
	}
	err = sourceStore.SaveDevice(model.Device{ID: "dev-1", Name: "Device 1", Enable: true})
	if err != nil {
		t.Fatalf("failed to save devices: %v", err)
	}
	sourceDB.Close()

	archiveData, filename, err := ExportDBAsTarGz(sourcePath, ConfigDBEntryName, "config")
	if err != nil {
		t.Fatalf("failed to export archive: %v", err)
	}
	if filename == "" {
		t.Fatal("expected archive filename")
	}
	if len(archiveData) == 0 {
		t.Fatal("expected archive data")
	}

	targetStorage, err := NewStorage(tempDir)
	if err != nil {
		t.Fatalf("failed to create target storage: %v", err)
	}
	defer targetStorage.Close()

	targetStore, err := NewConfigStore(targetStorage.GetConfigDB())
	if err != nil {
		t.Fatalf("failed to create target config store: %v", err)
	}
	err = targetStore.SaveServerConfig(model.ServerConfig{Port: 8080, LogLevel: "info"})
	if err != nil {
		t.Fatalf("failed to save target server config: %v", err)
	}
	err = targetStore.SaveUsers([]model.UserConfig{
		{Username: "local-user", Password: "local-hash"},
	})
	if err != nil {
		t.Fatalf("failed to save target users: %v", err)
	}
	err = targetStore.SaveChannels([]model.Channel{
		{ID: "old-ch", Name: "Old Channel", Protocol: "modbus-tcp", Enable: true},
	})
	if err != nil {
		t.Fatalf("failed to save target channels: %v", err)
	}

	result, err := targetStorage.ImportConfigDBArchive(archiveData, ImportArchiveOptions{})
	if err != nil {
		t.Fatalf("failed to import archive: %v", err)
	}
	if result.PreservedPort != 8080 {
		t.Fatalf("expected preserved port 8080, got %d", result.PreservedPort)
	}
	if result.ChannelCount != 1 || result.DeviceCount != 1 {
		t.Fatalf("unexpected import counts: channels=%d devices=%d", result.ChannelCount, result.DeviceCount)
	}

	loadedStore, err := NewConfigStore(targetStorage.GetConfigDB())
	if err != nil {
		t.Fatalf("failed to reload target config store: %v", err)
	}

	serverConfig, err := loadedStore.LoadServerConfig()
	if err != nil || serverConfig == nil {
		t.Fatalf("failed to load server config: %v", err)
	}
	if serverConfig.Port != 8080 {
		t.Fatalf("expected preserved port 8080, got %d", serverConfig.Port)
	}
	if serverConfig.LogLevel != "debug" {
		t.Fatalf("expected imported log level debug, got %s", serverConfig.LogLevel)
	}

	users, err := loadedStore.LoadUsers()
	if err != nil {
		t.Fatalf("failed to load users: %v", err)
	}
	if len(users) != 1 || users[0].Username != "local-user" || users[0].Password != "local-hash" {
		t.Fatalf("expected local users to be preserved, got %+v", users)
	}

	channels, err := loadedStore.LoadChannels()
	if err != nil {
		t.Fatalf("failed to load channels: %v", err)
	}
	if len(channels) != 1 || channels[0].ID != "ch-1" {
		t.Fatalf("expected imported channel ch-1, got %+v", channels)
	}
}

func TestExportRuntimeDBArchive(t *testing.T) {	tempDir := testOutputDir(t)

	storage, err := NewStorage(tempDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	data, filename, err := storage.ExportRuntimeDBArchive()
	if err != nil {
		storage.Close()
		t.Fatalf("failed to export runtime archive: %v", err)
	}
	storage.Close()
	if filename == "" || len(data) == 0 {
		t.Fatal("expected runtime archive output")
	}

	files, err := extractTarGz(data)
	if err != nil {
		t.Fatalf("failed to extract runtime archive: %v", err)
	}
	if _, err := findArchiveEntry(files, RuntimeDBEntryName); err != nil {
		t.Fatalf("runtime archive missing runtime.db: %v", err)
	}
}

// TestImportConfigDBData_RawBackupCrossBackend 原始备份文件（BackupConfigDB 产物）
// 恢复到任意后端系统：验证魔数识别（含 bbolt 偏移 16 的 meta 魔数）与跨后端导入，
// 且默认保留本地用户与端口。
func TestImportConfigDBData_RawBackupCrossBackend(t *testing.T) {
	for _, srcBackend := range []string{BackendBbolt, BackendSQLite} {
		for _, dstBackend := range []string{BackendBbolt, BackendSQLite} {
			t.Run(srcBackend+"_to_"+dstBackend, func(t *testing.T) {
				srcDir := testOutputDir(t)
				dstDir := testOutputDir(t)

				src, err := NewStorageWithBackend(srcDir, srcBackend)
				if err != nil {
					t.Fatalf("failed to create source storage: %v", err)
				}
				srcStore, err := NewConfigStore(src.GetConfigDB())
				if err != nil {
					t.Fatalf("failed to create source store: %v", err)
				}
				if err := srcStore.SaveServerConfig(model.ServerConfig{Port: 9090, LogLevel: "debug"}); err != nil {
					t.Fatalf("save source server config: %v", err)
				}
				if err := srcStore.SaveUsers([]model.UserConfig{{Username: "remote-user", Password: "remote-hash"}}); err != nil {
					t.Fatalf("save source users: %v", err)
				}
				if err := srcStore.SaveChannels([]model.Channel{{ID: "ch-1", Name: "Channel 1", Protocol: "modbus-tcp", Enable: true}}); err != nil {
					t.Fatalf("save source channels: %v", err)
				}
				if err := srcStore.SaveDevice(model.Device{ID: "dev-1", Name: "Device 1", Enable: true}); err != nil {
					t.Fatalf("save source device: %v", err)
				}

				backupInfo, err := src.BackupConfigDB(filepath.Join(srcDir, "backups"))
				if err != nil {
					t.Fatalf("failed to backup source config db: %v", err)
				}
				src.Close()

				backupBytes, err := os.ReadFile(backupInfo.BackupPath)
				if err != nil {
					t.Fatalf("failed to read backup file: %v", err)
				}

				dst, err := NewStorageWithBackend(dstDir, dstBackend)
				if err != nil {
					t.Fatalf("failed to create target storage: %v", err)
				}
				defer dst.Close()

				dstStore, err := NewConfigStore(dst.GetConfigDB())
				if err != nil {
					t.Fatalf("failed to create target store: %v", err)
				}
				if err := dstStore.SaveServerConfig(model.ServerConfig{Port: 8080, LogLevel: "info"}); err != nil {
					t.Fatalf("save target server config: %v", err)
				}
				if err := dstStore.SaveUsers([]model.UserConfig{{Username: "local-user", Password: "local-hash"}}); err != nil {
					t.Fatalf("save target users: %v", err)
				}

				result, err := dst.ImportConfigDBData(backupBytes, ImportArchiveOptions{})
				if err != nil {
					t.Fatalf("failed to restore backup: %v", err)
				}
				if result.ChannelCount != 1 || result.DeviceCount != 1 {
					t.Fatalf("unexpected restore counts: channels=%d devices=%d", result.ChannelCount, result.DeviceCount)
				}
				if result.PreservedPort != 8080 {
					t.Fatalf("expected preserved port 8080, got %d", result.PreservedPort)
				}

				loadedStore, err := NewConfigStore(dst.GetConfigDB())
				if err != nil {
					t.Fatalf("failed to reload target store: %v", err)
				}
				users, err := loadedStore.LoadUsers()
				if err != nil {
					t.Fatalf("failed to load users: %v", err)
				}
				if len(users) != 1 || users[0].Username != "local-user" || users[0].Password != "local-hash" {
					t.Fatalf("expected local users to be preserved, got %+v", users)
				}
				serverConfig, err := loadedStore.LoadServerConfig()
				if err != nil || serverConfig == nil {
					t.Fatalf("failed to load server config: %v", err)
				}
				if serverConfig.Port != 8080 {
					t.Fatalf("expected preserved port 8080, got %d", serverConfig.Port)
				}
				if serverConfig.LogLevel != "debug" {
					t.Fatalf("expected imported log level debug, got %s", serverConfig.LogLevel)
				}
				channels, err := loadedStore.LoadChannels()
				if err != nil {
					t.Fatalf("failed to load channels: %v", err)
				}
				if len(channels) != 1 || channels[0].ID != "ch-1" {
					t.Fatalf("expected restored channel ch-1, got %+v", channels)
				}
			})
		}
	}
}

// TestImportConfigDBData_RejectsGarbage 非法数据（既非归档也非数据库文件）应报错。
func TestImportConfigDBData_RejectsGarbage(t *testing.T) {
	st, err := NewStorage(testOutputDir(t))
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer st.Close()

	if _, err := st.ImportConfigDBData([]byte("this is not a database"), ImportArchiveOptions{}); err == nil {
		t.Fatal("expected error for unrecognized data")
	}
}

// TestIsBoltDB_RealFileLayout bbolt meta 魔数在偏移 16（page header 之后），
// 且磁盘字节序为 ED DA 0C ED（0xED0CDAED 小端）。
func TestIsBoltDB_RealFileLayout(t *testing.T) {
	realHeader := make([]byte, 24)
	// page header: id=0, flags=metaPageFlag(4), count=0, overflow=0
	copy(realHeader[16:], []byte{0xED, 0xDA, 0x0C, 0xED})
	if !isBoltDB(realHeader) {
		t.Fatal("isBoltDB should match real bbolt file header")
	}
	if isBoltDB(realHeader[:19]) {
		t.Fatal("isBoltDB should require full magic bytes")
	}
	if isBoltDB(make([]byte, 24)) {
		t.Fatal("isBoltDB should not match zeroed header")
	}
}
