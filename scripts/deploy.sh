#!/usr/bin/env bash
#
# deploy.sh — bootstrap a sing-box server (VLESS-REALITY + Hysteria2) and sbx.
#
# Idempotent: re-running will NOT overwrite an existing config (so it never
# clobbers users you added later). Run as root on a fresh Ubuntu 22.04 box:
#
#   sudo bash deploy.sh
#
# Overridable via env: SBX_SNI, SBX_USER, SBX_VERSION
#
set -euo pipefail

SB_DIR=/etc/sing-box
SB_CFG="$SB_DIR/config.json"
SNI="${SBX_SNI:-www.apple.com}"     # REALITY handshake site (TLS1.3 + H2, not blocked)
FIRST_USER="${SBX_USER:-user1}"     # initial user name
SBX_VERSION="${SBX_VERSION:-v0.1.0}"

[ "$(id -u)" = 0 ] || { echo "run as root: sudo bash deploy.sh"; exit 1; }

# Secret-bearing files (config, keys, git blobs) must not be world-readable.
umask 077

# Up front, re-secure any pre-existing secrets so a rerun hardens an older
# (possibly world-readable) deployment even if a later step fails. No-ops on a
# fresh box; new files are created 0600/0700 by the umask above.
chmod 600 "$SB_CFG" 2>/dev/null || true
[ -e "$SB_DIR/key.pem" ] && chmod 600 "$SB_DIR/key.pem"
if [ -d "$SB_DIR/.git" ]; then
  find "$SB_DIR/.git" -type d -exec chmod 700 {} +
  find "$SB_DIR/.git" -type f -exec chmod 600 {} +
fi

echo "==> 0. dependencies"
apt-get update -y
apt-get install -y curl jq git openssl ufw fail2ban ca-certificates

echo "==> 1. BBR + sysctl tuning"
cat >/etc/sysctl.d/99-sbx.conf <<'EOF'
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.core.rmem_max = 67108864
net.core.wmem_max = 67108864
net.ipv4.tcp_rmem = 4096 87380 67108864
net.ipv4.tcp_wmem = 4096 65536 67108864
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_fastopen = 3
EOF
sysctl --system >/dev/null
echo "    bbr = $(sysctl -n net.ipv4.tcp_congestion_control)"

echo "==> 2. install sing-box"
command -v sing-box >/dev/null || bash <(curl -fsSL https://sing-box.app/deb-install.sh)
mkdir -p /var/lib/sing-box
sing-box version | head -1

echo "==> 3. write config (skip if present, to avoid clobbering users)"
if [ -f "$SB_CFG" ]; then
  echo "    $SB_CFG exists — leaving untouched"
else
  mkdir -p "$SB_DIR"
  KP=$(sing-box generate reality-keypair)
  PRIV=$(echo "$KP" | awk '/PrivateKey/{print $2}')
  PUB=$(echo  "$KP" | awk '/PublicKey/{print $2}')
  SID=$(openssl rand -hex 8)
  UUID=$(sing-box generate uuid)
  OBFS=$(openssl rand -base64 18 | tr -d '/+=')
  [ -n "$PRIV" ] && [ -n "$UUID" ] && [ -n "$SID" ] \
    || { echo "    failed to generate secrets (sing-box output changed?)"; exit 1; }

  openssl ecparam -genkey -name prime256v1 -out "$SB_DIR/key.pem" 2>/dev/null
  openssl req -new -x509 -days 36500 -key "$SB_DIR/key.pem" \
    -out "$SB_DIR/cert.pem" -subj "/CN=www.bing.com" 2>/dev/null
  chmod 600 "$SB_DIR/key.pem"

  cat >"$SB_CFG" <<EOF
{
  "log": { "level": "warn", "timestamp": true },
  "experimental": {
    "clash_api": { "external_controller": "127.0.0.1:9090" },
    "cache_file": { "enabled": true, "path": "/var/lib/sing-box/cache.db" }
  },
  "inbounds": [
    {
      "type": "vless", "tag": "reality-in", "listen": "::", "listen_port": 443,
      "users": [ { "name": "$FIRST_USER", "uuid": "$UUID", "flow": "xtls-rprx-vision" } ],
      "tls": {
        "enabled": true, "server_name": "$SNI",
        "reality": {
          "enabled": true,
          "handshake": { "server": "$SNI", "server_port": 443 },
          "private_key": "$PRIV", "short_id": ["$SID"]
        }
      }
    },
    {
      "type": "hysteria2", "tag": "hy2-in", "listen": "::", "listen_port": 443,
      "users": [ { "name": "$FIRST_USER", "password": "$UUID" } ],
      "obfs": { "type": "salamander", "password": "$OBFS" },
      "tls": {
        "enabled": true, "alpn": ["h3"],
        "certificate_path": "$SB_DIR/cert.pem", "key_path": "$SB_DIR/key.pem"
      }
    }
  ],
  "outbounds": [ { "type": "direct", "tag": "direct" } ]
}
EOF
  echo "$PUB" > "$SB_DIR/.reality_pub"
  echo "    secrets generated, config written"
fi

echo "==> 4. validate + start sing-box"
sing-box check -c "$SB_CFG"
systemctl enable sing-box >/dev/null 2>&1 || true
systemctl restart sing-box
sleep 1
if systemctl is-active sing-box >/dev/null; then
  echo "    sing-box active"
else
  journalctl -u sing-box -n 20 --no-pager
  exit 1
fi

echo "==> 5. firewall (detect SSH port first to avoid lockout)"
SSH_PORTS=$(sshd -T 2>/dev/null | awk '/^port /{print $2}' || true)
[ -z "$SSH_PORTS" ] && SSH_PORTS=$(awk '/^[Pp]ort /{print $2}' /etc/ssh/sshd_config 2>/dev/null || true)
[ -z "$SSH_PORTS" ] && SSH_PORTS=22
for p in $SSH_PORTS; do ufw allow "$p"/tcp >/dev/null; done
ufw allow 443/tcp >/dev/null
ufw allow 443/udp >/dev/null
ufw --force enable >/dev/null
systemctl enable --now fail2ban >/dev/null 2>&1 || true
echo "    allowed SSH ports: $SSH_PORTS + 443/tcp + 443/udp"

echo "==> 6. install sbx"
case "$(uname -m)" in
  x86_64) A=amd64 ;;
  aarch64|arm64) A=arm64 ;;
  *) echo "unsupported arch $(uname -m)"; exit 1 ;;
esac
TMP=$(mktemp -d)
curl -fsSL "https://github.com/awsl5714/cdj-sbx/releases/download/${SBX_VERSION}/sbx_linux_${A}.tar.gz" \
  | tar -xz -C "$TMP"
install -m755 "$TMP/sbx" /usr/local/bin/sbx
rm -rf "$TMP"
sbx --help >/dev/null 2>&1 && echo "    sbx installed: $(command -v sbx)"

echo "==> 7. bring under sbx management (git baseline)"
sbx --config "$SB_CFG" init || true

echo "==> 8. connection info"
IP=$(curl -fsS4 https://api.ipify.org 2>/dev/null || echo "<your-server-ip>")
echo "    server IP: $IP"
echo
sbx --config "$SB_CFG" link "$FIRST_USER" --server "$IP" 2>/dev/null \
  || { echo "(initial user name may have changed; current users:)"; sbx --config "$SB_CFG" user list; }
echo
echo "==> done. add more users with:  sbx user add <name>  then  sbx link <name> --server $IP"
