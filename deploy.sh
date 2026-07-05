#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Gin/Standard Rummy — Sidecar Deployment
#
# Runs Gin Rummy as an isolated, loopback-only service behind
# the main richymac.com webserver (CharmToolWeb). The webserver
# reverse-proxies https://richymac.com/gin/ to this process on
# 127.0.0.1:8090. It does NOT bind :80/:443 and does NOT touch
# the charmtoolweb service — both sites run side by side.
#
# After deploying this sidecar, (re)deploy CharmToolWeb so its
# /gin/ proxy route is live:  cd ../CharmToolWeb && ./deploy.sh
#
# Required:
#   ADMIN_PASSWORD=...   # admin account password (no insecure default in prod)
# Usage:
#   ADMIN_PASSWORD='strong-pass' ./deploy.sh
# ============================================================

REMOTE="root@srv629042.hstgr.cloud"
APP_NAME="ginrummy"
REMOTE_DIR="/opt/ginrummy"
SERVICE_NAME="ginrummy"
LISTEN_ADDR="127.0.0.1:8090"   # loopback only; the webserver proxies to here
LOCAL_DIR="$(cd "$(dirname "$0")" && pwd)"

ADMIN_PASSWORD="${ADMIN_PASSWORD:-}"
if [ -z "$ADMIN_PASSWORD" ]; then
    echo "ERROR: set ADMIN_PASSWORD before deploying, e.g.:"
    echo "  ADMIN_PASSWORD='something-strong' ./deploy.sh"
    exit 1
fi

echo "==> Building ${APP_NAME} for linux/amd64..."
cd "$LOCAL_DIR"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "${APP_NAME}-linux" .
echo "    Built ${APP_NAME}-linux"

echo "==> Creating deployment bundle..."
STAGING=$(mktemp -d)
trap 'rm -rf "$STAGING"' EXIT
cp "${APP_NAME}-linux" "${STAGING}/${APP_NAME}"
cp -r static           "${STAGING}/static"

# Plaintext loopback service. No TLS_CERT/TLS_KEY (those would make it grab
# :80/:443 and fight the webserver). GIN_ADDR pins it to loopback.
cat > "${STAGING}/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Gin/Standard Rummy Server (sidecar behind richymac.com/gin)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${REMOTE_DIR}
ExecStart=${REMOTE_DIR}/${APP_NAME}
Environment=GIN_ADDR=${LISTEN_ADDR}
Environment=GIN_DB=${REMOTE_DIR}/data/gin.db
Environment=ADMIN_PASSWORD=${ADMIN_PASSWORD}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

echo "==> Copying files to ${REMOTE}:${REMOTE_DIR} (data/ preserved)..."
ssh "$REMOTE" "systemctl stop ${SERVICE_NAME} 2>/dev/null || true; mkdir -p ${REMOTE_DIR}/data && rm -rf ${REMOTE_DIR}/static"
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
echo "==> Sidecar deployed on ${LISTEN_ADDR}."
echo "    Now (re)deploy CharmToolWeb to activate the /gin/ proxy:"
echo "      cd ../CharmToolWeb && ./deploy.sh"
echo "    Then visit https://richymac.com/gin/"
echo "    Logs:    ssh ${REMOTE} journalctl -u ${SERVICE_NAME} -f"
echo "    The SQLite DB lives at ${REMOTE_DIR}/data/gin.db and survives restarts."
