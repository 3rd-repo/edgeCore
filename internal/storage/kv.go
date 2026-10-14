package storage

// KVDB 是配置/运行时存储后端的统一抽象（2026-10-13 引入）。
// 现有两种实现：
//   - bboltKV   — go.etcd.io/bbolt（B+树 mmap，默认后端）
//   - sqliteKV  — modernc.org/sqlite（纯 Go SQLite，安装页可选）
//
// 语义对齐 bbolt：
//   - View 只读事务可并发；Update 可写事务由后端保证全局串行；
//   - key 按字节序（memcmp）排序；
//   - Get 对缺失 key 返回 nil、对缺失 bucket 返回 nil；
//   - 对缺失 bucket 遍历（ForEach/Cursor）视为空集合；
//   - Put 前调用方需保证 bucket 存在（CreateBucketIfNotExists），
//     否则返回 "bucket not found" 类错误。
type KVDB interface {
	// View 执行只读事务。
	View(fn func(tx KVTx) error) error
	// Update 执行可写事务（提交或整体回滚）。
	Update(fn func(tx KVTx) error) error
	Close() error
	Path() string
	// Backend 返回后端标识："bbolt" 或 "sqlite"。
	Backend() string

	// Sync 强制将已提交数据落盘（bbolt: db.Sync；sqlite: WAL checkpoint FULL）。
	Sync() error
	// Compact 回收已删除数据占用的空间。
	// bbolt：整文件重写并原子换绑（内部处理 rename 与重开）；
	// sqlite：VACUUM 原地重建。
	Compact() error
	// BackupTo 将数据库的事务一致性快照写入 dstPath（热备，不阻塞读写）。
	// bbolt: tx.WriteTo；sqlite: VACUUM INTO。
	BackupTo(dstPath string) error
}

// KVTx 是单个事务内的 bucket 型 KV 操作集合。
type KVTx interface {
	// Get 返回 key 对应值的副本；缺失返回 nil。
	Get(bucket string, key []byte) []byte
	Put(bucket string, key []byte, value []byte) error
	Delete(bucket string, key []byte) error
	CreateBucketIfNotExists(bucket string) error
	// DeleteBucket 删除 bucket 及其全部数据（bucket 不存在返回错误）。
	DeleteBucket(bucket string) error
	// Buckets 返回全部 bucket 名（字节序）。
	Buckets() ([]string, error)
	// HasBucket 判断 bucket 是否已创建（空 bucket 也算存在）。
	HasBucket(bucket string) bool
	// ForEach 遍历 bucket 内全部键值（字节序）；bucket 缺失视为空。
	ForEach(bucket string, fn func(k, v []byte) error) error
	// Cursor 获取 bucket 游标（同向迭代：First/Next 或 Last/Prev/Seek）。
	Cursor(bucket string) KVCursor
	// CountKeys 返回 bucket 内 key 数量。
	CountKeys(bucket string) (int, error)
}

// KVCursor 是 bucket 内的字节序游标。
// 约定：迭代方向一致使用（正向 First/Next/Seek，反向 Last/Prev），
// 混用方向时行为由后端定义（bbolt 原生允许，sqlite 实现会重开流）。
type KVCursor interface {
	First() (k, v []byte)
	Last() (k, v []byte)
	Next() (k, v []byte)
	Prev() (k, v []byte)
	// Seek 定位到第一个 >= prefix 的键。
	Seek(prefix []byte) (k, v []byte)
	// Delete 删除当前键（迭代安全）。
	Delete() error
}

// 存储后端常量与 meta 文件名（安装页选择、启动时恢复）。
const (
	BackendBbolt  = "bbolt"
	BackendSQLite = "sqlite"
	BackendMetaFile = "storage.meta"
)
