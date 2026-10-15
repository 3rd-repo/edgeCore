#!/bin/bash
# 安装后处理脚本（安全版）
# P1-12 加固（2026-10-13 数据丢失审查）：
#   - 备份目录移至持久化路径 /var/lib/edgeCore-upgrade-backup（原 /tmp 重启即清空）；
#   - 备份缺失时大声告警（此前静默跳过，升级"成功"但数据处于未知状态）；
#   - 兼容旧版 /tmp 备份作为回退。
set -e

DATA_DIR="/usr/local/bin/edgeCore/data"
CONFIG_DIR="/usr/local/bin/edgeCore/config"
BACKUP_DIR="/var/lib/edgeCore-upgrade-backup/current"
LEGACY_BACKUP_DIR="/tmp/edgeCore_backup"

echo "[postinstall] Running postinstall script..."

# 旧版备份在 /tmp（重启即丢失），升级跨版本时作为回退
if [ ! -d "$BACKUP_DIR" ] && [ -d "$LEGACY_BACKUP_DIR" ]; then
    echo "[postinstall] Using legacy /tmp backup as fallback."
    BACKUP_DIR="$LEGACY_BACKUP_DIR"
fi

restore_dir() {
    local src="$1"
    local dst="$2"

    if [ -d "$src" ]; then
        echo "[postinstall] Restoring $dst from backup..."
        mkdir -p "$dst"
        cp -rf "$src/"* "$dst/" 2>/dev/null || true
    else
        echo "[postinstall] No backup found for $dst, ensuring directory exists."
        mkdir -p "$dst"
    fi
}

# 仅在 systemctl 存在时执行 systemd 操作
if command -v systemctl >/dev/null 2>&1; then
    echo "[postinstall] Reloading systemd..."
    systemctl daemon-reload || true

    echo "[postinstall] Enabling service..."
    systemctl enable edgeCore || true

    # 证书轮换 timer（每月触发，脚本内 14 天提前量幂等；非必需，失败不影响安装）
    if [ -f /etc/systemd/system/edgeCore-cert-rotate.timer ]; then
        echo "[postinstall] Enabling certificate rotation timer..."
        chmod +x /usr/local/bin/edgeCore/scripts/cert-rotate.sh 2>/dev/null || true
        systemctl enable --now edgeCore-cert-rotate.timer || true
    fi

    # 关键：通过 Web UI 升级时，dpkg/postinst 由 edgeCore 进程派生，
    # 运行于 edgeCore.service 的 CGroup 内。直接 systemctl restart 会连同
    # postinst/dpkg 一并终止（dpkg 状态卡在 half-configured，升级误报失败）。
    # 用 systemd-run --wait 创建独立 transient unit 执行 systemd 操作：
    # unit 生命周期由 systemd 管理、与调用者解耦（不能用 --scope）。
    if command -v systemd-run >/dev/null 2>&1; then
        # 停止旧服务（preinstall 通常已停，此处幂等兜底）：旧进程持有
        # data/config.db、runtime.db 的 bbolt 文件锁，运行期间覆盖恢复文件
        # 会与旧进程写入竞态、导致数据库损坏。
        systemd-run --wait --quiet --slice=system.slice systemctl stop edgeCore || true

        # 恢复 data 和 config（旧进程已退出，文件处于稳定状态）
        if [ ! -d "$BACKUP_DIR/data" ] && [ -d "$DATA_DIR/config.db" -o -f "$DATA_DIR/config.db" ]; then
            echo "[postinstall] WARNING: upgrade backup missing but existing data present."
            echo "[postinstall] WARNING: skipping restore to avoid destroying existing data; service will start with current data."
        else
            restore_dir "$BACKUP_DIR/data" "$DATA_DIR"
            restore_dir "$BACKUP_DIR/config" "$CONFIG_DIR"
        fi

        # 启动新版本服务
        systemd-run --wait --quiet --slice=system.slice systemctl start edgeCore || true
    else
        # 无 systemd-run 环境：保持先恢复再重启的顺序，避免服务长时间停摆
        if [ ! -d "$BACKUP_DIR/data" ] && [ -f "$DATA_DIR/config.db" ]; then
            echo "[postinstall] WARNING: upgrade backup missing but existing data present; skipping restore."
        else
            restore_dir "$BACKUP_DIR/data" "$DATA_DIR"
            restore_dir "$BACKUP_DIR/config" "$CONFIG_DIR"
        fi
        systemctl restart edgeCore || true
    fi
else
    # 非 systemd 环境：无服务进程持有 bbolt 文件锁，直接恢复配置即可
    if [ ! -d "$BACKUP_DIR/data" ] && [ -f "$DATA_DIR/config.db" ]; then
        echo "[postinstall] WARNING: upgrade backup missing but existing data present; skipping restore."
    else
        restore_dir "$BACKUP_DIR/data" "$DATA_DIR"
        restore_dir "$BACKUP_DIR/config" "$CONFIG_DIR"
    fi
fi

echo "[postinstall] Completed."
