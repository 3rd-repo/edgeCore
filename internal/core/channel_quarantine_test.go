package core

import (
	"sync"
	"testing"
	"time"

	"github.com/anviod/edgeCore/internal/model"
)

// waitForSaveCount 轮询等待 saveFunc 被调用 n 次（saveChannels 异步落盘）。
// counter 自带加锁，这里不要再包一层 mu（sync.Mutex 不可重入）。
func waitForSaveCount(t *testing.T, counter func() int, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if counter() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("saveFunc was not called %d times within deadline (got %d)", n, counter())
}

// TestQuarantinedChannelSurvivesPersistence 回归（P0，2026-10-13 真机事故）：
// 启动时 AddChannel 校验失败的通道若只是"拒收"，后续任意一次全量保存
// 都会把它从 DB 静默删除。隔离区机制要求：
//  1. QuarantineChannel 后的通道必须出现在持久化视图中；
//  2. 显式 RemoveChannel 能删除隔离通道（用户意图优先）；
//  3. 同 ID AddChannel 成功后隔离解除且不重复出现在持久化视图。
func TestQuarantinedChannelSurvivesPersistence(t *testing.T) {
	var mu sync.Mutex
	saveCount := 0
	lastSnapshot := map[string]struct{}{}
	cm := NewChannelManager(nil, func(chs []model.Channel) error {
		mu.Lock()
		defer mu.Unlock()
		saveCount++
		lastSnapshot = map[string]struct{}{}
		for _, c := range chs {
			lastSnapshot[c.ID] = struct{}{}
		}
		return nil
	})
	defer cm.cancel()
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return saveCount
	}

	// 1) 隔离通道必须保留在持久化视图
	cm.QuarantineChannel(&model.Channel{
		ID: "ch-q1", Name: "Broken", Protocol: addChannelMockProtocol,
		Devices: []model.Device{{ID: "dev-q1"}},
	}, "duplicate device dev-q1")
	waitForSaveCount(t, count, 1)
	mu.Lock()
	_, kept := lastSnapshot["ch-q1"]
	mu.Unlock()
	if !kept {
		t.Fatal("quarantined channel missing from persistence view")
	}

	// 2) 显式删除隔离通道生效
	if err := cm.RemoveChannel("ch-q1"); err != nil {
		t.Fatalf("RemoveChannel(quarantined): %v", err)
	}
	waitForSaveCount(t, count, 2)
	mu.Lock()
	_, still := lastSnapshot["ch-q1"]
	n := len(cm.QuarantinedChannels())
	mu.Unlock()
	if still {
		t.Fatal("removed quarantined channel still in persistence view")
	}
	if n != 0 {
		t.Fatalf("quarantine not cleared after remove: %d entries", n)
	}

	// 3) 同 ID 成功加载后隔离解除、视图不重复
	cm.QuarantineChannel(&model.Channel{
		ID: "ch-q2", Name: "Broken2", Protocol: addChannelMockProtocol,
	}, "dirty data")
	waitForSaveCount(t, count, 3)
	if err := cm.AddChannel(&model.Channel{
		ID: "ch-q2", Name: "Fixed", Protocol: addChannelMockProtocol,
		Config: map[string]any{},
	}); err != nil {
		t.Fatalf("AddChannel after quarantine: %v", err)
	}
	waitForSaveCount(t, count, 4)
	mu.Lock()
	occurrences := 0
	for _, c := range cm.channels {
		if c.ID == "ch-q2" {
			occurrences++
		}
	}
	qn := len(cm.QuarantinedChannels())
	mu.Unlock()
	if occurrences != 1 {
		t.Fatalf("channel ch-q2 registered %d times in cm.channels", occurrences)
	}
	if qn != 0 {
		t.Fatalf("quarantine not cleared after AddChannel: %d entries", qn)
	}
}
