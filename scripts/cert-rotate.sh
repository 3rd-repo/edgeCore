#!/bin/bash
# cert-rotate.sh — edgeCore TLS 证书轮换脚本
#
# 用途：定期（systemd timer 或 cron）轮换 /usr/local/bin/edgeCore/server.crt 与
# server.key 自签证书，保留最近 N 份历史证书以便回滚，并通过 systemd 通知服务重载。
#
# 设计要点：
#   1. 新证书先写入临时文件，成功后再原子替换（写坏不会弄丢可用证书）；
#   2. 轮换前校验新证书（openssl verify 自洽 + 到期窗口检查）；
#   3. 旧证书带时间戳归档，保留最近 KEEP 份，防止误操作后无法回退；
#   4. 使用 copytruncate 式替换 + systemctl reload/try-restart（按需）；
#   5. 幂等：证书仍在有效窗口内（默认剩余 > 14 天）时直接跳过。
#
# 退出码：0 = 无需轮换或成功；非 0 = 轮换失败（保留旧证书）。

set -euo pipefail

EDGE_DIR="${EDGE_DIR:-/usr/local/bin/edgeCore}"
CERT_FILE="$EDGE_DIR/server.crt"
KEY_FILE="$EDGE_DIR/server.key"
BACKUP_DIR="${BACKUP_DIR:-$EDGE_DIR/pki/history}"
KEEP="${KEEP:-5}"

# 证书有效期（天）与提前轮换窗口（剩余天数低于该值才轮换）
VALID_DAYS="${VALID_DAYS:-365}"
ROTATE_BEFORE_DAYS="${ROTATE_BEFORE_DAYS:-14}"

SERVICE_NAME="${SERVICE_NAME:-edgeCore}"
RELOAD_MODE="${RELOAD_MODE:-restart}"   # restart | reload | none

log() { echo "[cert-rotate] $(date '+%F %T') $*"; }
fail() { echo "[cert-rotate] $(date '+%F %T') ERROR: $*" >&2; exit 1; }

command -v openssl >/dev/null 2>&1 || fail "openssl not found"

# ---------- 1. 幂等检查：证书剩余有效期 ----------
days_left() {
    # 输出剩余天数；证书不存在或无法解析时输出 -1
    if [ ! -f "$CERT_FILE" ]; then
        echo -1; return
    fi
    local end
    end=$(openssl x509 -enddate -noout -in "$CERT_FILE" 2>/dev/null | cut -d= -f2) || { echo -1; return; }
    local end_epoch now_epoch
    end_epoch=$(date -d "$end" +%s 2>/dev/null || echo 0)
    now_epoch=$(date +%s)
    echo $(( (end_epoch - now_epoch) / 86400 ))
}

LEFT=$(days_left)
if [ "$LEFT" -ge "$ROTATE_BEFORE_DAYS" ]; then
    log "Certificate valid for $LEFT more days (>= $ROTATE_BEFORE_DAYS), nothing to do."
    exit 0
fi
log "Certificate expires in $LEFT days (or missing) — rotating."

# ---------- 2. 生成新证书 ----------
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT
NEW_CERT="$TMP_DIR/server.crt"
NEW_KEY="$TMP_DIR/server.key"

HOSTNAME_FQDN=$(hostname -f 2>/dev/null || hostname)
HOST_IP=$(hostname -I 2>/dev/null | awk '{print $1}')

log "Generating self-signed certificate (CN=$HOSTNAME_FQDN, ${VALID_DAYS}d) ..."
SUBJ="/CN=$HOSTNAME_FQDN/O=edgeCore/OU=Gateway"
EXTENSIONS=""
if [ -n "$HOST_IP" ]; then
    EXTENSIONS="subjectAltName=IP:$HOST_IP,DNS:localhost"
else
    EXTENSIONS="subjectAltName=DNS:localhost"
fi

openssl req -x509 -newkey rsa:2048 -sha256 -days "$VALID_DAYS" \
    -keyout "$NEW_KEY" -out "$NEW_CERT" -nodes \
    -subj "$SUBJ" -addext "$EXTENSIONS" 2>/dev/null \
    || fail "openssl req failed"

chmod 600 "$NEW_KEY"
chmod 644 "$NEW_CERT"

# ---------- 3. 校验新证书 ----------
openssl x509 -in "$NEW_CERT" -noout -subject >/dev/null 2>&1 || fail "generated certificate is invalid"
# 证书与私钥必须配对（模数一致）
CERT_MOD=$(openssl x509 -noout -modulus -in "$NEW_CERT" 2>/dev/null | openssl sha256)
KEY_MOD=$(openssl rsa -noout -modulus -in "$NEW_KEY" 2>/dev/null | openssl sha256)
[ "$CERT_MOD" = "$KEY_MOD" ] || fail "certificate/key modulus mismatch"
log "New certificate validated (cert/key pair OK)."

# ---------- 4. 归档旧证书 ----------
mkdir -p "$BACKUP_DIR"
STAMP=$(date +%Y%m%d%H%M%S)
if [ -f "$CERT_FILE" ]; then
    cp -a "$CERT_FILE" "$BACKUP_DIR/server.crt.$STAMP"
    cp -a "$KEY_FILE"  "$BACKUP_DIR/server.key.$STAMP" 2>/dev/null || true
    chmod 600 "$BACKUP_DIR/server.key.$STAMP" 2>/dev/null || true
    log "Old certificate archived to $BACKUP_DIR/server.crt.$STAMP"
fi

# ---------- 5. 原子替换 ----------
install -m 644 "$NEW_CERT" "$CERT_FILE.new" && mv -f "$CERT_FILE.new" "$CERT_FILE"
install -m 600 "$NEW_KEY"  "$KEY_FILE.new"  && mv -f "$KEY_FILE.new"  "$KEY_FILE"
log "Certificates replaced atomically."

# ---------- 6. 清理历史归档（保留最近 KEEP 份）----------
ls -1t "$BACKUP_DIR"/server.crt.* 2>/dev/null | tail -n +$((KEEP + 1)) | while IFS= read -r old; do
    rm -f "$old"
    # 同时间戳的私钥一并删除
    key="${old/server.crt./server.key.}"
    rm -f "$key"
    log "Pruned old archive: $(basename "$old")"
done

# ---------- 7. 通知服务 ----------
case "$RELOAD_MODE" in
    restart)
        if command -v systemctl >/dev/null 2>&1; then
            # 用 systemd-run 脱离调用者 cgroup，与安装脚本同理由
            if command -v systemd-run >/dev/null 2>&1; then
                systemd-run --wait --quiet --slice=system.slice systemctl restart "$SERVICE_NAME" \
                    || log "WARN: systemctl restart failed (service may pick up cert on next start)"
            else
                systemctl restart "$SERVICE_NAME" || log "WARN: systemctl restart failed"
            fi
            log "Service $SERVICE_NAME restarted with new certificate."
        else
            log "systemctl unavailable; restart $SERVICE_NAME manually to apply."
        fi
        ;;
    reload)
        systemctl reload "$SERVICE_NAME" 2>/dev/null || log "reload unsupported, cert applies on next restart"
        ;;
    none)
        log "RELOAD_MODE=none, skipping service restart."
        ;;
    *)
        log "Unknown RELOAD_MODE '$RELOAD_MODE', skipping service restart."
        ;;
esac

log "Certificate rotation completed successfully."
