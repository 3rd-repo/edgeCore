package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anviod/edgeCore/internal/model"

	"go.etcd.io/bbolt"
	"go.uber.org/zap"
)

type Storage struct {
	configDB  *bbolt.DB
	runtimeDB *bbolt.DB
	// runtimeMu 保护 runtimeDB 句柄的换绑（compact 关库→换文件→重开）。
	// 读路径持 RLock 执行事务，compact 持 Lock 完成换绑：换绑窗口内的
	// 写入会短暂阻塞而不是拿到 "database not open" 被上层吞掉
	// （P0 数据丢失防护，2026-10-13 审查）。
	runtimeMu sync.RWMutex
	// compactMu 串行化 compact（定时循环与手动 API 可能并发触发）。
	compactMu sync.Mutex
	dataDir   string
}

// runtimeUpdate / runtimeView：RLock 保护下的 runtime.db 事务。
func (s *Storage) runtimeUpdate(fn func(tx *bbolt.Tx) error) error {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.runtimeDB.Update(fn)
}

func (s *Storage) runtimeView(fn func(tx *bbolt.Tx) error) error {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.runtimeDB.View(fn)
}

func (s *Storage) GetPath() string {
	return s.GetRuntimePath()
}

// GetRuntimePath returns the on-disk path of runtime.db.
func (s *Storage) GetRuntimePath() string {
	return s.runtimeDB.Path()
}

func (s *Storage) GetConfigPath() string {
	return s.configDB.Path()
}

func (s *Storage) GetDB() *bbolt.DB {
	return s.runtimeDB
}

func (s *Storage) GetConfigDB() *bbolt.DB {
	return s.configDB
}

func (s *Storage) getDBByBucket(bucketName string) *bbolt.DB {
	if IsConfigBucket(bucketName) {
		return s.configDB
	}
	return s.runtimeDB
}

func (s *Storage) SaveOfflineMessage(configID string, data []byte, maxCount int) error {
	return s.runtimeUpdate(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketNorthboundCache))
		if b == nil {
			return fmt.Errorf("bucket %s not found", BucketNorthboundCache)
		}

		key := fmt.Sprintf("%s_%d", configID, time.Now().UnixNano())

		if err := b.Put([]byte(key), data); err != nil {
			return err
		}

		c := b.Cursor()
		prefix := []byte(configID + "_")
		count := 0
		var keysToDelete [][]byte

		for k, _ := c.Seek(prefix); k != nil && len(k) > len(prefix) && string(k[:len(prefix)]) == string(prefix); k, _ = c.Next() {
			count++
		}

		if count > maxCount {
			toDelete := count - maxCount
			for k, _ := c.Seek(prefix); k != nil && len(k) > len(prefix) && string(k[:len(prefix)]) == string(prefix); k, _ = c.Next() {
				if toDelete <= 0 {
					break
				}
				keysToDelete = append(keysToDelete, append([]byte{}, k...))
				toDelete--
			}
		}

		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}

		return nil
	})
}

func (s *Storage) GetOfflineMessages(configID string, limit int) ([]OfflineMessage, error) {
	var messages []OfflineMessage
	err := s.runtimeView(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketNorthboundCache))
		if b == nil {
			return nil
		}

		c := b.Cursor()
		prefix := []byte(configID + "_")

		for k, v := c.Seek(prefix); k != nil && len(k) > len(prefix) && string(k[:len(prefix)]) == string(prefix); k, v = c.Next() {
			if len(messages) >= limit {
				break
			}
			dataCopy := make([]byte, len(v))
			copy(dataCopy, v)

			messages = append(messages, OfflineMessage{
				Key:  string(k),
				Data: dataCopy,
			})
		}
		return nil
	})
	return messages, err
}

func (s *Storage) RemoveOfflineMessage(key string) error {
	return s.runtimeUpdate(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketNorthboundCache))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

const (
	BucketValues          = "values"
	BucketRuleState       = "RuleState"
	BucketDataCache       = "DataCache"
	BucketWindow          = "WindowData"
	BucketNorthboundCache = "NorthboundCache"

	// legacyShadowWALBucket is a removed ShadowCore WAL bucket; dropped on startup.
	legacyShadowWALBucket = "shadow_wal"
)

type OfflineMessage struct {
	Key  string
	Data []byte
}

var configBucketNames = []string{
	BucketConfigVersion,
	BucketChannels,
	BucketDevices,
	BucketNorthbound,
	BucketEdgeRules,
	BucketSystem,
	BucketUsers,
	BucketServer,
	BucketVirtualShadows,
}

var runtimeBucketNames = []string{
	BucketValues,
	BucketRuleState,
	BucketDataCache,
	BucketWindow,
	BucketNorthboundCache,
}

var edgeLogBucketNames = []string{
	"edge_events",
	"edge_failures",
	"bblot",
}

var knownRuntimeBucketNames = append(append([]string(nil), runtimeBucketNames...), edgeLogBucketNames...)

func NewStorage(dataDir string) (*Storage, error) {
	if dataDir == "" {
		dataDir = "data"
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	configPath := filepath.Join(dataDir, "config.db")
	runtimePath := filepath.Join(dataDir, "runtime.db")

	// 打开配置数据库（强一致写入）
	configDB, err := openBoltDB(configPath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to open config database %s: %w", configPath, err)
	}

	// 初始化配置 bucket
	err = configDB.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range configBucketNames {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return err
			}
		}
		b := tx.Bucket([]byte(BucketConfigVersion))
		if b != nil {
			currentVersion := b.Get([]byte(ConfigVersionKey))
			if currentVersion == nil {
				return b.Put([]byte(ConfigVersionKey), []byte(ConfigVersionValue))
			}
		}
		return nil
	})
	if err != nil {
		configDB.Close()
		return nil, fmt.Errorf("failed to init config buckets: %w", err)
	}

	// 打开运行时数据库（允许清理、compact、重建）
	// P0 自愈（2026-10-13 审查）：CompactRuntimeDB 的两步 rename 之间若进程
	// 崩溃/断电，runtime.db 会缺失但 .pre-compact.bak 完整存在——此时必须恢复，
	// 否则网关 Fatal 无限重启（数据明明还在磁盘上）。
	if _, statErr := os.Stat(runtimePath); os.IsNotExist(statErr) {
		backupPath := runtimePath + ".pre-compact.bak"
		if _, bakErr := os.Stat(backupPath); bakErr == nil {
			if renameErr := os.Rename(backupPath, runtimePath); renameErr == nil {
				zap.L().Warn("runtime.db missing after interrupted compact, restored from .pre-compact.bak",
					zap.String("path", runtimePath))
			}
		}
	}
	runtimeDB, err := openBoltDB(runtimePath, true)
	if err != nil {
		configDB.Close()
		return nil, fmt.Errorf("failed to open runtime database %s: %w", runtimePath, err)
	}

	// 初始化运行时 bucket
	err = runtimeDB.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range runtimeBucketNames {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		runtimeDB.Close()
		configDB.Close()
		return nil, fmt.Errorf("failed to init runtime buckets: %w", err)
	}

	storage := &Storage{
		configDB:  configDB,
		runtimeDB: runtimeDB,
		dataDir:   dataDir,
	}
	if err := storage.dropLegacyShadowWALBucket(); err != nil {
		runtimeDB.Close()
		configDB.Close()
		return nil, fmt.Errorf("failed to drop legacy shadow WAL bucket: %w", err)
	}

	return storage, nil
}

func (s *Storage) Close() error {
	err1 := s.configDB.Close()
	s.runtimeMu.Lock()
	err2 := s.runtimeDB.Close()
	s.runtimeMu.Unlock()
	if err1 != nil {
		return err1
	}
	return err2
}

func (s *Storage) SaveData(bucketName string, key string, data interface{}) error {
	if !IsConfigBucket(bucketName) {
		return s.runtimeUpdate(func(tx *bbolt.Tx) error {
			b, err := tx.CreateBucketIfNotExists([]byte(bucketName))
			if err != nil {
				return err
			}
			bytes, err := json.Marshal(data)
			if err != nil {
				return err
			}
			return b.Put([]byte(key), bytes)
		})
	}
	db := s.getDBByBucket(bucketName)
	return db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return err
		}
		bytes, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return b.Put([]byte(key), bytes)
	})
}

func (s *Storage) GetData(bucketName string, key string, result interface{}) error {
	if !IsConfigBucket(bucketName) {
		return s.runtimeView(func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(bucketName))
			if b == nil {
				return fmt.Errorf("bucket %s not found", bucketName)
			}
			data := b.Get([]byte(key))
			if data == nil {
				return fmt.Errorf("key %s not found in bucket %s", key, bucketName)
			}
			return json.Unmarshal(data, result)
		})
	}
	db := s.getDBByBucket(bucketName)
	return db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucketName)
		}
		data := b.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("key %s not found in bucket %s", key, bucketName)
		}
		return json.Unmarshal(data, result)
	})
}

func (s *Storage) DeleteData(bucketName string, key string) error {
	db := s.getDBByBucket(bucketName)
	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

func (s *Storage) PruneOldest(bucketName string, maxRecords int) error {
	db := s.getDBByBucket(bucketName)
	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}

		count := b.Stats().KeyN
		if count <= maxRecords {
			return nil
		}

		deleteCount := count - maxRecords
		c := b.Cursor()
		for i := 0; i < deleteCount; i++ {
			k, _ := c.First()
			if k == nil {
				break
			}
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Storage) LoadLatest(bucketName string, limit int, callback func(k, v []byte) error) error {
	db := s.getDBByBucket(bucketName)
	return db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}

		c := b.Cursor()
		count := 0
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			if limit > 0 && count >= limit {
				break
			}
			if err := callback(k, v); err != nil {
				return err
			}
			count++
		}
		return nil
	})
}

func (s *Storage) LoadAll(bucketName string, callback func(k, v []byte) error) error {
	if !IsConfigBucket(bucketName) {
		return s.runtimeView(func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(bucketName))
			if b == nil {
				return nil
			}
			return b.ForEach(callback)
		})
	}
	db := s.getDBByBucket(bucketName)
	return db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}
		return b.ForEach(callback)
	})
}

func (s *Storage) LoadRange(bucketName string, minKey, maxKey string, callback func(k, v []byte) error) error {
	if !IsConfigBucket(bucketName) {
		return s.runtimeView(func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(bucketName))
			if b == nil {
				return nil
			}
			c := b.Cursor()
			for k, v := c.Seek([]byte(minKey)); k != nil && string(k) <= maxKey; k, v = c.Next() {
				if err := callback(k, v); err != nil {
					return err
				}
			}
			return nil
		})
	}
	db := s.getDBByBucket(bucketName)
	return db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.Seek([]byte(minKey)); k != nil && string(k) <= maxKey; k, v = c.Next() {
			if err := callback(k, v); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Storage) SaveValue(val model.Value) error {
	return s.runtimeUpdate(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketValues))

		data, err := json.Marshal(val)
		if err != nil {
			return err
		}

		return b.Put([]byte(val.PointID), data)
	})
}

func (s *Storage) GetLastValue(pointID string) (*model.Value, error) {
	var val model.Value
	err := s.runtimeView(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketValues))
		data := b.Get([]byte(pointID))
		if data == nil {
			return fmt.Errorf("not found")
		}
		return json.Unmarshal(data, &val)
	})
	return &val, err
}

func (s *Storage) GetAllValues() (map[string]model.Value, error) {
	result := make(map[string]model.Value)
	err := s.runtimeView(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketValues))
		return b.ForEach(func(k, v []byte) error {
			var val model.Value
			if err := json.Unmarshal(v, &val); err == nil {
				result[string(k)] = val
			}
			return nil
		})
	})
	return result, err
}

type BucketStats struct {
	Name        string `json:"name"`
	RecordCount int    `json:"record_count"`
	TotalSize   int64  `json:"total_size"`
	Category    string `json:"category"`
	Clearable   bool   `json:"clearable"`
	Database    string `json:"database"`
}

var (
	configBucketMap = map[string]bool{
		"ConfigVersion":  true,
		"Channels":       true,
		"Devices":        true,
		"Northbound":     true,
		"EdgeRules":      true,
		"System":         true,
		"Users":          true,
		"Server":         true,
		"VirtualShadows": true,
		"ai_copilot":     true,
	}

	cacheBucketMap = map[string]bool{
		"DataCache":       true,
		"WindowData":      true,
		"NorthboundCache": true,
		"RuleState":       true,
	}

	runtimeBucketMap = map[string]bool{
		"values":        true,
		"shadow_values": true,
	}

	edgeLogBucketMap = map[string]bool{
		"edge_events":   true,
		"edge_failures": true,
		"bblot":         true,
	}
)

func ClassifyBucket(name string) (category string, clearable bool) {
	return classifyBucket(name)
}

func classifyBucket(name string) (category string, clearable bool) {
	if configBucketMap[name] {
		return "config", false
	}
	if cacheBucketMap[name] {
		return "cache", true
	}
	if runtimeBucketMap[name] {
		return "runtime", true
	}
	if edgeLogBucketMap[name] {
		return "edge_log", true
	}
	if strings.HasPrefix(name, "device_history_") {
		return "history", true
	}
	if name == legacyShadowWALBucket {
		return "legacy", true
	}
	if name == "WAL" {
		return "cache", true
	}
	return "unknown", false
}

// dropLegacyShadowWALBucket removes the obsolete ShadowCore WAL bucket.
// Shadow devices are memory-only; leftover data is reclaimed after CompactRuntimeDB.
func (s *Storage) dropLegacyShadowWALBucket() error {
	return s.runtimeUpdate(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(legacyShadowWALBucket)) == nil {
			return nil
		}
		return tx.DeleteBucket([]byte(legacyShadowWALBucket))
	})
}

func IsConfigBucket(name string) bool {
	return configBucketMap[name]
}

func (s *Storage) GetBucketStats() ([]BucketStats, int64, error) {
	var stats []BucketStats
	var totalSize int64

	configFileInfo, err := os.Stat(s.configDB.Path())
	if err != nil {
		return nil, 0, err
	}
	totalSize += configFileInfo.Size()

	runtimeFileInfo, err := os.Stat(s.runtimeDB.Path())
	if err != nil {
		return nil, 0, err
	}
	totalSize += runtimeFileInfo.Size()

	err = s.configDB.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			count := 0
			size := int64(0)
			b.ForEach(func(k, v []byte) error {
				count++
				size += int64(len(k) + len(v))
				return nil
			})
			category, clearable := classifyBucket(string(name))
			stats = append(stats, BucketStats{
				Name:        string(name),
				RecordCount: count,
				TotalSize:   size,
				Category:    category,
				Clearable:   clearable,
				Database:    "config",
			})
			return nil
		})
	})
	if err != nil {
		return nil, 0, err
	}

	err = s.runtimeView(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			count := 0
			size := int64(0)
			b.ForEach(func(k, v []byte) error {
				count++
				size += int64(len(k) + len(v))
				return nil
			})
			category, clearable := classifyBucket(string(name))
			stats = append(stats, BucketStats{
				Name:        string(name),
				RecordCount: count,
				TotalSize:   size,
				Category:    category,
				Clearable:   clearable,
				Database:    "runtime",
			})
			return nil
		})
	})
	if err != nil {
		return nil, 0, err
	}

	stats = ensureKnownRuntimeBuckets(stats)
	sortBucketStats(stats)

	return stats, totalSize, err
}

func ensureKnownRuntimeBuckets(stats []BucketStats) []BucketStats {
	existing := make(map[string]struct{}, len(stats))
	for _, st := range stats {
		if st.Database == "runtime" {
			existing[st.Name] = struct{}{}
		}
	}
	for _, name := range knownRuntimeBucketNames {
		if _, ok := existing[name]; ok {
			continue
		}
		category, clearable := classifyBucket(name)
		stats = append(stats, BucketStats{
			Name:        name,
			RecordCount: 0,
			TotalSize:   0,
			Category:    category,
			Clearable:   clearable,
			Database:    "runtime",
		})
	}
	return stats
}

func sortBucketStats(stats []BucketStats) {
	sort.SliceStable(stats, func(i, j int) bool {
		if stats[i].Database != stats[j].Database {
			return stats[i].Database == "config"
		}
		return stats[i].Name < stats[j].Name
	})
}

func (s *Storage) ClearBucket(bucketName string) error {
	if IsConfigBucket(bucketName) {
		return fmt.Errorf("config bucket %s cannot be cleared", bucketName)
	}
	return s.runtimeUpdate(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucketName)
		}

		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			if err := c.Delete(); err != nil {
				return err
			}
		}
		return nil
	})
}

// ClearAllRuntimeBuckets 清空 runtime.db 中的全部 bucket（含 values、缓存、历史、遗留 WAL 等）。
// 配置 bucket 不应出现在 runtime.db；若检测到则报错。
func (s *Storage) ClearAllRuntimeBuckets() ([]string, error) {
	var cleared []string

	err := s.runtimeUpdate(func(tx *bbolt.Tx) error {
		var bucketNames []string
		if err := tx.ForEach(func(name []byte, _ *bbolt.Bucket) error {
			bucketNames = append(bucketNames, string(name))
			return nil
		}); err != nil {
			return err
		}

		for _, bucketName := range bucketNames {
			if IsConfigBucket(bucketName) {
				return fmt.Errorf("config bucket %s found in runtime db", bucketName)
			}

			if bucketName == legacyShadowWALBucket {
				if err := tx.DeleteBucket([]byte(bucketName)); err != nil {
					return err
				}
				cleared = append(cleared, bucketName)
				continue
			}

			b := tx.Bucket([]byte(bucketName))
			if b == nil {
				continue
			}
			c := b.Cursor()
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				if err := c.Delete(); err != nil {
					return err
				}
			}
			cleared = append(cleared, bucketName)
		}
		return nil
	})

	return cleared, err
}

const compactTxMaxSize = 65536 // matches bbolt CLI default; avoids OOM during large compacts

// CompactRuntimeDB 压缩运行时数据库文件，回收已删除数据的空间。
// P0 数据丢失防护（2026-10-13 审查）重构：
//   - 压缩源直接使用存活的 runtimeDB 句柄（bbolt.Compact 只开读事务，
//     与写事务互不阻塞）——不再先关库再做整个压缩，把"database not open"
//     的丢数据窗口从秒级缩到毫秒级；
//   - 关库→换文件→重开的毫秒级窗口内持有 runtimeMu 写锁，数据面写入
//     短暂排队而不是报错被吞；
//   - 两步 rename 之间进程崩溃会在磁盘上留下 .pre-compact.bak，
//     NewStorage 启动时检测并自愈恢复。
func (s *Storage) CompactRuntimeDB() error {
	s.compactMu.Lock()
	defer s.compactMu.Unlock()

	// 快照当前句柄（reopenRuntimeDB 换绑期间指针竞态防护）
	s.runtimeMu.RLock()
	srcDB := s.runtimeDB
	s.runtimeMu.RUnlock()

	runtimePath := srcDB.Path()
	tmpPath := runtimePath + ".compact.tmp"
	_ = os.Remove(tmpPath)

	dst, err := openBoltDB(tmpPath, true)
	if err != nil {
		return fmt.Errorf("failed to create compact temp db: %w", err)
	}

	if err := bbolt.Compact(dst, srcDB, compactTxMaxSize); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to compact runtime db: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close compact temp db: %w", err)
	}

	tmpInfo, err := os.Stat(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to stat compacted file: %w", err)
	}
	if tmpInfo.Size() == 0 {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("compacted file is empty, aborting")
	}

	// 毫秒级换绑窗口：写入在 runtimeMu 上排队，不丢数据
	backupPath := runtimePath + ".pre-compact.bak"
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()

	if err := s.runtimeDB.Close(); err != nil {
		return fmt.Errorf("failed to close runtime db: %w", err)
	}
	if err := os.Rename(runtimePath, backupPath); err != nil {
		_ = os.Remove(tmpPath)
		if reopenErr := s.reopenRuntimeDB(runtimePath); reopenErr != nil {
			return fmt.Errorf("failed to backup original runtime db: %w (reopen: %v)", err, reopenErr)
		}
		return fmt.Errorf("failed to backup original runtime db: %w", err)
	}
	if err := os.Rename(tmpPath, runtimePath); err != nil {
		_ = os.Rename(backupPath, runtimePath)
		if reopenErr := s.reopenRuntimeDB(runtimePath); reopenErr != nil {
			return fmt.Errorf("failed to replace runtime db with compacted file: %w (reopen: %v)", err, reopenErr)
		}
		return fmt.Errorf("failed to replace runtime db with compacted file: %w", err)
	}
	if err := s.reopenRuntimeDB(runtimePath); err != nil {
		return fmt.Errorf("failed to reopen compacted runtime db: %w", err)
	}

	if err := s.runtimeDB.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range runtimeBucketNames {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to reinit runtime buckets after compact: %w", err)
	}

	_ = os.Remove(backupPath)
	return nil
}

func (s *Storage) reopenRuntimeDB(path string) error {
	db, err := openBoltDB(path, true)
	if err != nil {
		return err
	}
	s.runtimeDB = db
	return nil
}

// BackupConfigDB 备份配置数据库到指定目录。
// 配置库优先备份，不包含运行时数据。
func (s *Storage) BackupConfigDB(backupDir string) (*model.BackupInfo, error) {
	return BackupDB(s.configDB.Path(), backupDir)
}

// BackupRuntimeDB 备份运行时数据库到指定目录（可选诊断用途）。
func (s *Storage) BackupRuntimeDB(backupDir string) (*model.BackupInfo, error) {
	runtimeBackupDir := filepath.Join(backupDir, "runtime")
	return BackupDB(s.runtimeDB.Path(), runtimeBackupDir)
}

// SyncConfigDB 对配置数据库执行强制同步，确保持久化到磁盘。
// 配置库使用强一致写入策略。
func (s *Storage) SyncConfigDB() error {
	return s.configDB.Sync()
}
