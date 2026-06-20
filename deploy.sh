#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Gin/Standard Rummy — Deployment Script
#
# Builds the server for linux/amd64, copies it and the static
# assets to the VPS, installs a systemd service with TLS, and
# starts it. Modeled on the CharmToolWeb deploy.
#
#   WARNING: richymac.com's :80/:443 are currently served by the
#   `charmtoolweb` service. This script frees those ports, which
#   STOPS CharmToolWeb and serves the Rummy app at the root
#   instead. Run only if you intend to replace the current site.
#
# Required:
#   ADMIN_PASSWORD=...   # admin account password (no insecure default in prod)
# Usage:
#   ADMIN_PASSWORD='strong-pass' ./deploy.sh
# ============================================================

REMOTE="root@srv629042.hstgr.cloud"
TLS_CERT="/etc/letsencrypt/live/richymac.com/fullchain.pem"
TLS_KEY="/etc/letsencrypt/live/richymac.com/privkey.pem"
APP_NAME="ginrummy"
REMOTE_DIR="/opt/ginrummy"
SERVICE_NAME="ginrummy"
LOCAL_DIR="$(cd "$(dirname "$0")" && pwd)"

ADMIN_PASSWORD="${ADMIN_PASSWORD:-}"
if [ -z "$ADMIN_PASSWORD" ]; then
    echo "ERROR: set ADMIN_PASSWORD before deploying, e.g.:"
    echo "  ADMIN_PASSWORD='something-strong' ./deploy.sh"
    exit 1
fi

echo "This will REPLACE the site currently at https://richymac.com (CharmToolWeb)."
read -r -p "Type 'replace' to continue: " confirm
[ "$confirm" = "replace" ] || { echo "Aborted."; exit 1; }

echo "==> Building ${APP_NAME} for linux/amd64..."
cd "$LOCAL_DIR"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "${APP_NAME}-linux" .
echo "    Built ${APP_NAME}-linux"

echo "==> Creating deployment bundle..."
STAGING=$(mktemp -d)
trap 'rm -rf "$STAGING"' EXIT
cp "${APP_NAME}-linux" "${STAGING}/${APP_NAME}"
cp -r static           "${STAGING}/static"

cat > "${STAGING}/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Gin/Standard Rummy Server
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${REMOTE_DIR}
ExecStart=${REMOTE_DIR}/${APP_NAME}
Environment=TLS_CERT=${TLS_CERT}
Environment=TLS_KEY=${TLS_KEY}
Environment=GIN_DB=${REMOTE_DIR}/data/gin.db
Environment=ADMIN_PASSWORD=${ADMIN_PASSWORD}
Environment=ACME_WEBROOT=/var/www/letsencrypt
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

echo "==> Stopping existing services and freeing ports 80/443..."
ssh "$REMOTE" 'bash -s' <<'PORT_CLEANUP'
systemctl stop ginrummy 2>/dev/null || true
systemctl stop charmtoolweb 2>/dev/null || true
systemctl stop nginx 2>/dev/null || true
for port in 80 443; do
    if ss -tlnH "sport = :$port" | grep -q .; then
        fuser -k -TERM "${port}/tcp" 2>/dev/null || true
    fi
done
sleep 1
PORT_CLEANUP

echo "==> Copying files to ${REMOTE}:${REMOTE_DIR} (data/ preserved)..."
ssh "$REMOTE" "mkdir -p ${REMOTE_DIR}/data && rm -rf ${REMOTE_DIR}/static"
scp    "${STAGING}/${APP_NAME}"             "${REMOTE}:${REMOTE_DIR}/${APP_NAME}"
scp -r "${STAGING}/static"                  "${REMOTE}:${REMOTE_DIR}/"
scp    "${STAGING}/${SERVICE_NAME}.service" "${REMOTE}:/etc/systemd/system/${SERVICE_NAME}.service"

echo "==> Installing and starting service..."
ssh "$REMOTE" <<EOF
chmod +x ${REMOTE_DIR}/${APP_NAME}
systemctl daemon-reload
systemctl enable ${SERVICE_NAME}
systemctl start ${SERVICE_NAME}
systemctl status ${SERVICE_NAME} --no-pager
EOF

echo ""
echo "==> Deployed. https://richymac.com/"
echo "    Logs:    ssh ${REMOTE} journalctl -u ${SERVICE_NAME} -f"
echo "    Restart: ssh ${REMOTE} systemctl restart ${SERVICE_NAME}"
echo "    The SQLite DB (users, games, saved game state) lives at ${REMOTE_DIR}/data/gin.db and survives restarts."
