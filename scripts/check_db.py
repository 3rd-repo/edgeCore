#!/usr/bin/env python3
"""check_db.py - 检查 edgeCore 配置数据库（bbolt 格式）。

P2 修复（2026-10-13 数据丢失审查）：旧版误用 sqlite3 打开 bbolt 文件——
config.db 是 bbolt 而非 sqlite，旧脚本必然失败并误导运维。

bbolt 是二进制格式，Python 无官方驱动。本脚本只做只读诊断：
  1. 校验文件魔数（bbolt magic 0xED0CDAED，小端存储于文件头 meta page）
  2. 用字符串扫描粗略列出 bucket 键名提示
如需完整读取，请使用 Go 侧工具或 edgeCore 的备份/导出 API：
  GET /api/data/export-config-db（tar.gz 归档）
  GET /api/config/export（JSON）
"""
import struct
import sys
import json

BBOLT_MAGIC = 0xED0CDAED


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else '/usr/local/bin/edgeCore/data/config.db'
    try:
        with open(path, 'rb') as f:
            meta = f.read(4096)  # bbolt 两个 4KB meta page 在文件头
    except OSError as e:
        print(f'ERROR: cannot open {path}: {e}')
        sys.exit(1)

    if len(meta) < 4096:
        print(f'ERROR: file too small ({len(meta)} bytes), not a valid bbolt db: {path}')
        sys.exit(1)

    # bbolt meta page: magic 位于 page header (16B) 之后的 meta 结构体开头
    magic = struct.unpack('<I', meta[16:20])[0]
    if magic == BBOLT_MAGIC:
        version = struct.unpack('<I', meta[20:24])[0]
        pagesize = struct.unpack('<I', meta[24:28])[0]
        print(f'OK: {path} is a bbolt database (magic=0x{magic:08X}, version={version}, pagesize={pagesize})')
    else:
        print(f'WARNING: {path} does not look like a bbolt database (magic=0x{magic:08X}).')
        print('If it is a sqlite file, use sqlite3 instead. Otherwise the file may be corrupted.')

    # 粗略提取可读 bucket 名（仅诊断提示，非完整枚举）
    with open(path, 'rb') as f:
        data = f.read()
    known_buckets = [
        b'ConfigVersion', b'channels', b'Devices', b'northbound',
        b'edge_rules', b'system', b'users', b'virtual_shadows', b'server',
    ]
    found = [b.decode() for b in known_buckets if b in data]
    print('Buckets found (hint scan):', json.dumps(found, ensure_ascii=False))
    print('For full inspection use: GET /api/config/export (JSON) or /api/data/export-config-db (tar.gz)')


if __name__ == '__main__':
    main()
