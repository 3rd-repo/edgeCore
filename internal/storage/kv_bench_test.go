package storage

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/anviod/edgeCore/internal/model"
)

// 性能对比基准：bbolt vs modernc SQLite（同一 KVDB 接口、同一负载）。
// 运行：go test ./internal/storage/ -bench BenchmarkStore -benchmem -run '^$'

func benchStorage(b *testing.B, backend string) *Storage {
	b.Helper()
	s, err := NewStorageWithBackend(b.TempDir(), backend)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	return s
}

func benchChannels(n int) []model.Channel {
	channels := make([]model.Channel, 0, n)
	for i := 0; i < n; i++ {
		points := make([]model.Point, 0, 20)
		for j := 0; j < 20; j++ {
			points = append(points, model.Point{
				ID: fmt.Sprintf("pt-%d-%d", i, j), Name: fmt.Sprintf("point_%d_%d", i, j),
				Address: fmt.Sprintf("4%05d", j), DataType: "float", ReadWrite: "rw",
			})
		}
		channels = append(channels, model.Channel{
			ID: fmt.Sprintf("ch-%03d", i), Name: fmt.Sprintf("channel_%d", i),
			Protocol: "modbus-tcp", Enable: true,
			Config:  map[string]any{"url": "192.168.1.100:502", "timeout": "3s", "retry": 3},
			Devices: []model.Device{{ID: fmt.Sprintf("dev-%03d", i), Name: fmt.Sprintf("dev_%d", i), Points: points}},
		})
	}
	return channels
}

// BenchmarkStoreConfigSave: 配置落库（50 通道整批 JSON，对齐 saveChannels 负载）
func BenchmarkStoreConfigSave(b *testing.B) {
	channels := benchChannels(50)
	for _, backend := range backendNames() {
		b.Run(backend, func(b *testing.B) {
			s := benchStorage(b, backend)
			cs, err := NewConfigStore(s.GetConfigDB())
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := cs.SaveChannels(channels); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStoreConfigLoad: 配置加载（50 通道 JSON 反序列化）
func BenchmarkStoreConfigLoad(b *testing.B) {
	channels := benchChannels(50)
	for _, backend := range backendNames() {
		b.Run(backend, func(b *testing.B) {
			s := benchStorage(b, backend)
			cs, err := NewConfigStore(s.GetConfigDB())
			if err != nil {
				b.Fatal(err)
			}
			if err := cs.SaveChannels(channels); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := cs.LoadChannels(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStorePointWrite: 运行时点位值随机写（1 点位高频覆盖，对齐 SaveValue）
func BenchmarkStorePointWrite(b *testing.B) {
	for _, backend := range backendNames() {
		b.Run(backend, func(b *testing.B) {
			s := benchStorage(b, backend)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				val := model.Value{PointID: fmt.Sprintf("pt-%d", i%500), Value: float64(i)}
				if err := s.SaveValue(val); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStoreRangeScan: 运行时范围扫描（写入 1000 条后反复 LoadRange 500 条）
func BenchmarkStoreRangeScan(b *testing.B) {
	for _, backend := range backendNames() {
		b.Run(backend, func(b *testing.B) {
			s := benchStorage(b, backend)
			for i := 0; i < 1000; i++ {
				if err := s.SaveData(BucketDataCache, fmt.Sprintf("hist-%06d", i), map[string]any{"v": i}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				n := 0
				err := s.LoadRange(BucketDataCache, "hist-000100", "hist-000599", func(k, v []byte) error {
					n++
					return nil
				})
				if err != nil {
					b.Fatal(err)
				}
				if n != 500 {
					b.Fatalf("got %d rows, want 500", n)
				}
			}
		})
	}
}

// BenchmarkStoreOfflineCache: 北向离线缓存追加写（对齐 SaveOfflineMessage）
func BenchmarkStoreOfflineCache(b *testing.B) {
	payload, _ := json.Marshal(map[string]any{"device": "dev-1", "point": "pt-1", "value": 3.14, "ts": 1728800000000})
	for _, backend := range backendNames() {
		b.Run(backend, func(b *testing.B) {
			s := benchStorage(b, backend)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := s.SaveOfflineMessage("mqtt-1", payload, 5000); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStoreCompact: Compact 成本（bbolt 整文件重写 vs sqlite VACUUM）
func BenchmarkStoreCompact(b *testing.B) {
	for _, backend := range backendNames() {
		b.Run(backend, func(b *testing.B) {
			s := benchStorage(b, backend)
			for i := 0; i < 2000; i++ {
				_ = s.SaveData(BucketDataCache, fmt.Sprintf("k%06d", i), map[string]any{"v": i, "pad": "xxxxxxxxxxxxxxxxxxxx"})
			}
			for i := 0; i < 1000; i++ {
				_ = s.DeleteData(BucketDataCache, fmt.Sprintf("k%06d", i))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := s.CompactRuntimeDB(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
