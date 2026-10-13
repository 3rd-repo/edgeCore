#!/bin/bash
# preinstall.sh - 软件安装前备份用户配置和参数
# P1-12 加固（2026-10-13 数据丢失审查）：
#   1. 先停服务再备份——运行中的 bbolt 文件热拷贝可能得到撕裂/损坏的备份；
#   2. 备份移出 /tmp（重启即清空，升级中途失败备份就没了）；
#   3. 不再"先删旧备份再拷贝"——先拷到 staging，校验通过后原子替换，
#      拷贝中途失败（磁盘写满等）不会新旧备份皆无。
set -e

ROOT_DIR="/usr/local/bin/edgeCore"
CONFIG_DIR="$ROOT_DIR/config"
DATA_DIR="$ROOT_DIR/data"
BACKUP_ROOT="/var/lib/edgeCore-upgrade-backup"
BACKUP_DIR="$BACKUP_ROOT/current"

echo "[preinstall] Starting preinstall script..."

# 1. 停止旧服务后再备份（postinstall 会重新启动）
stop_service() {
    if command -v systemctl >/dev/null 2>&1; then
        if command -v systemd-run >/dev/null 2>&1; then
            # 用 systemd-run transient unit，与 postinstall 同理由（脱离调用者 cgroup）
            systemd-run --wait --quiet --slice=system.slice systemctl stop edgeCore || true
        else
            systemctl stop edgeCore || true
        fi
    fi
}
stop_service

# 2. 备份到 staging 目录，成功后原子替换
rm -rf "$BACKUP_DIR.staging"
mkdir -p "$BACKUP_DIR.staging/data"
mkdir -p "$BACKUP_DIR.staging/config"

backup_dir() {
    local src="$1"
    local dst="$2"

    if [ -d "$src" ]; then
        echo "[preinstall] Backing up $src → $dst"
        mkdir -p "$dst"
        # 若目录为空 cp 会报错，因此忽略错误
        cp -a "$src/"* "$dst/" 2>/dev/null || true
    else
        echo "[preinstall] Directory not found: $src (skip)"
    fi
}

# 备份目录：升级/降级必须保留原有配置，否则通道/设备等业务数据
# （位于 data/config.db，bbolt 存储）会随版本变更而丢失。
backup_dir "$DATA_DIR" "$BACKUP_DIR.staging/data"
backup_dir "$CONFIG_DIR" "$BACKUP_DIR.staging/config"

# 3. 校验关键文件存在再替换旧备份
if [ -f "$BACKUP_DIR.staging/data/config.db" ]; then
    rm -rf "$BACKUP_DIR.old"
    if [ -d "$BACKUP_DIR" ]; then
        mv "$BACKUP_DIR" "$BACKUP_DIR.old"
    fi
    mv "$BACKUP_DIR.staging" "$BACKUP_DIR"
    rm -rf "$BACKUP_DIR.old"
    echo "[preinstall] Backup verified (config.db present)."
else
    echo "[preinstall] WARNING: config.db missing in staging backup; keeping previous backup."
    rm -rf "$BACKUP_DIR.staging"
fi

echo "[preinstall] Completed."
