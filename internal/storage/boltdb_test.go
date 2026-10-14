package storage

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/anviod/edgeCore/internal/model"
)

func TestIsConfigBucket(t *testing.T) {
	testCases := []struct {
		name     string
		bucket   string
		expected bool
	}{
		{"ConfigVersion is config", "ConfigVersion", true},
		{"Channels is config", "Channels", true},
		{"Devices is config", "Devices", true},
		{"Northbound is config", "Northbound", true},
		{"EdgeRules is config", "EdgeRules", true},
		{"System is config", "System", true},
		{"Users is config", "Users", true},
		{"Server is config", "Server", true},
		{"DataCache is not config", "DataCache", false},
		{"WindowData is not config", "WindowData", false},
		{"NorthboundCache is not config", "NorthboundCache", false},
		{"RuleState is not config", "RuleState", false},
		{"values is not config", "values", false},
		{"device_history_xxx is not config", "device_history_device1", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsConfigBucket(tc.bucket)
			if result != tc.expected {
				t.Errorf("IsConfigBucket(%q) = %v, want %v", tc.bucket, result, tc.expected)
			}
		})
	}
}

func TestClassifyBucket(t *testing.T) {
	testCases := []struct {
		name          string
		bucket        string
		expectedCat   string
		expectedClear bool
	}{
		{"ConfigVersion", "ConfigVersion", "config", false},
		{"Channels", "Channels", "config", false},
		{"Devices", "Devices", "config", false},
		{"Northbound", "Northbound", "config", false},
		{"EdgeRules", "EdgeRules", "config", false},
		{"System", "System", "config", false},
		{"Users", "Users", "config", false},
		{"Server", "Server", "config", false},
		{"DataCache", "DataCache", "cache", true},
		{"WindowData", "WindowData", "cache", true},
		{"NorthboundCache", "NorthboundCache", "cache", true},
		{"RuleState", "RuleState", "cache", true},
		{"WAL", "WAL", "cache", true},
		{"values", "values", "runtime", true},
		{"shadow_values", "shadow_values", "runtime", true},
		{"edge_events", "edge_events", "edge_log", true},
		{"edge_failures", "edge_failures", "edge_log", true},
		{"bblot", "bblot", "edge_log", true},
		{"device_history_device1", "device_history_device1", "history", true},
		{"shadow_wal legacy", legacyShadowWALBucket, "legacy", true},
		{"unknown", "unknown", "unknown", false},
		{"custom", "custom", "unknown", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cat, clearable := classifyBucket(tc.bucket)
			if cat != tc.expectedCat {
				t.Errorf("classifyBucket(%q).category = %q, want %q", tc.bucket, cat, tc.expectedCat)
			}
			if clearable != tc.expectedClear {
				t.Errorf("classifyBucket(%q).clearable = %v, want %v", tc.bucket, clearable, tc.expectedClear)
			}
		})
	}
}

func TestClearBucket_Functionality(t *testing.T) {
	tmpDir := testOutputDir(t)

	s, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.GetDB().Update(func(tx KVTx) error {
		if err := tx.CreateBucketIfNotExists("DataCache"); err != nil {
			return err
		}
		if err := tx.Put("DataCache", []byte("key1"), []byte("value1")); err != nil {
			return err
		}
		return tx.Put("DataCache", []byte("key2"), []byte("value2"))
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ClearBucket("DataCache"); err != nil {
		t.Errorf("ClearBucket(DataCache) should succeed: %v", err)
	}

	var count int
	err = s.GetDB().View(func(tx KVTx) error {
		n, err := tx.CountKeys("DataCache")
		if err != nil {
			return err
		}
		count = n
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("DataCache should be empty, got %d records", count)
	}

	if err := s.ClearBucket("NonExistent"); err == nil {
		t.Error("ClearBucket(NonExistent) should fail")
	}
}

func TestClearAllRuntimeBuckets(t *testing.T) {
	tmpDir := testOutputDir(t)

	s, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	populate := func() error {
		return s.GetDB().Update(func(tx KVTx) error {
			if err := tx.CreateBucketIfNotExists("WAL"); err != nil {
				return err
			} else if err := tx.Put("WAL", []byte("w1"), []byte("v1")); err != nil {
				return err
			}
			if err := tx.CreateBucketIfNotExists("device_history_dev1"); err != nil {
				return err
			} else if err := tx.Put("device_history_dev1", []byte("t1"), []byte("h1")); err != nil {
				return err
			}
			if err := tx.CreateBucketIfNotExists(legacyShadowWALBucket); err != nil {
				return err
			} else if err := tx.Put(legacyShadowWALBucket, []byte("wal-1"), []byte("payload")); err != nil {
				return err
			}
			return nil
		})
	}
	if err := populate(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveData(BucketDataCache, "k1", map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveValue(model.Value{PointID: "p1", Value: 42}); err != nil {
		t.Fatal(err)
	}

	cleared, err := s.ClearAllRuntimeBuckets()
	if err != nil {
		t.Fatalf("ClearAllRuntimeBuckets failed: %v", err)
	}
	if len(cleared) == 0 {
		t.Fatal("expected at least one bucket cleared")
	}

	err = s.GetDB().View(func(tx KVTx) error {
		names, err := tx.Buckets()
		if err != nil {
			return err
		}
		for _, name := range names {
			if name == legacyShadowWALBucket {
				t.Error("legacy shadow_wal bucket should be removed")
				continue
			}
			count, err := tx.CountKeys(name)
			if err != nil {
				return err
			}
			if count != 0 {
				t.Errorf("bucket %s should be empty, got %d records", name, count)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	stats, _, err := s.GetBucketStats()
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range stats {
		if st.Database != "runtime" {
			continue
		}
		if st.RecordCount != 0 {
			t.Errorf("runtime bucket %s should have 0 records after clear, got %d", st.Name, st.RecordCount)
		}
	}
}

func runtimeDBFileSize(t *testing.T, s *Storage) int64 {
	t.Helper()
	info, err := os.Stat(s.runtimeDB.Path())
	if err != nil {
		t.Fatalf("stat runtime db: %v", err)
	}
	return info.Size()
}

func TestClearAllRuntimeBucketsShrinksFileAfterCompact(t *testing.T) {
	tmpDir := testOutputDir(t)

	s, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	payload := strings.Repeat("x", 8192)
	for i := 0; i < 256; i++ {
		key := fmt.Sprintf("key-%04d", i)
		if err := s.SaveData(BucketDataCache, key, map[string]string{"data": payload}); err != nil {
			t.Fatalf("SaveData(%s): %v", key, err)
		}
	}
	if err := s.SaveValue(model.Value{PointID: "p1", Value: 42}); err != nil {
		t.Fatal(err)
	}

	beforeSize := runtimeDBFileSize(t, s)
	if beforeSize < 512*1024 {
		t.Fatalf("expected seeded runtime db to be at least 512KB, got %d", beforeSize)
	}

	if _, err := s.ClearAllRuntimeBuckets(); err != nil {
		t.Fatalf("ClearAllRuntimeBuckets: %v", err)
	}

	afterClearSize := runtimeDBFileSize(t, s)
	if afterClearSize < beforeSize/2 {
		t.Fatalf("expected bbolt file to remain bloated after delete-only clear: before=%d afterClear=%d", beforeSize, afterClearSize)
	}

	if err := s.CompactRuntimeDB(); err != nil {
		t.Fatalf("CompactRuntimeDB: %v", err)
	}

	afterCompactSize := runtimeDBFileSize(t, s)
	if afterCompactSize >= afterClearSize {
		t.Fatalf("expected compact to shrink runtime db: afterClear=%d afterCompact=%d", afterClearSize, afterCompactSize)
	}
	if afterCompactSize >= beforeSize/4 {
		t.Fatalf("expected compact to reclaim most disk space: before=%d afterCompact=%d", beforeSize, afterCompactSize)
	}
}

func TestDropLegacyShadowWALBucket(t *testing.T) {
	tmpDir := testOutputDir(t)

	s, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.GetDB().Update(func(tx KVTx) error {
		if err := tx.CreateBucketIfNotExists(legacyShadowWALBucket); err != nil {
			return err
		}
		return tx.Put(legacyShadowWALBucket, []byte("wal-1"), []byte("payload"))
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.dropLegacyShadowWALBucket(); err != nil {
		t.Fatalf("dropLegacyShadowWALBucket failed: %v", err)
	}

	err = s.GetDB().View(func(tx KVTx) error {
		names, _ := tx.Buckets()
		for _, name := range names {
			if name == legacyShadowWALBucket {
				t.Error("legacy shadow_wal bucket should be removed")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	err = s2.GetDB().View(func(tx KVTx) error {
		names, _ := tx.Buckets()
		for _, name := range names {
			if name == legacyShadowWALBucket {
				t.Error("NewStorage should drop legacy shadow_wal on open")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
