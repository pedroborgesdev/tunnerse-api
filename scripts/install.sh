#!/bin/bash

if [ "$EUID" -ne 0 ]; then 
    echo "Error: This script must be run with sudo"
    echo "Usage: sudo ./install.sh"
    exit 1
fi

echo "Installing tunnerse API..."

SCRIPT_DIR="$(cd -- "$(dirname -- "$0")" && pwd)"
PROJECT_ROOT="$SCRIPT_DIR/.."

cd "$PROJECT_ROOT/bin" || exit 1

BIN_API="tunnerse-api"

if [ ! -f "$BIN_API" ]; then
    echo "Error: $BIN_API not found in bin directory."
    echo "Please compile first with: cd ../scripts && ./build.sh"
    exit 1
fi

echo "Installing binary to /usr/local/bin/..."
mkdir -p /usr/local/bin

cp "$BIN_API" /usr/local/bin/
chmod +x /usr/local/bin/"$BIN_API"

echo "Copying configuration files and static assets..."

cd "$PROJECT_ROOT" || exit 1

if [ -d "certs" ]; then
    echo "  Copying certs/ directory..."
    cp -r certs /usr/local/bin/
else
    echo "  Warning: certs/ directory not found"
fi

if [ -d "static" ]; then
    echo "  Copying static/ directory..."
    cp -r static /usr/local/bin/
else
    echo "  Warning: static/ directory not found"
fi

if [ -f "tunnerse.config" ]; then
    echo "  Copying tunnerse.config..."
    cp tunnerse.config /usr/local/bin/
else
    echo "  Warning: tunnerse.config not found"
fi

if [ -f ".env" ]; then
    echo "  Copying .env..."
    cp .env /usr/local/bin/
else
    echo "  Warning: .env not found (this may be optional)"
fi

REAL_USER="${SUDO_USER:-$USER}"
chown -R $REAL_USER:$REAL_USER /usr/local/bin/certs 2>/dev/null || true
chown -R $REAL_USER:$REAL_USER /usr/local/bin/static 2>/dev/null || true
chown $REAL_USER:$REAL_USER /usr/local/bin/tunnerse.config 2>/dev/null || true
chown $REAL_USER:$REAL_USER /usr/local/bin/.env 2>/dev/null || true

echo "Installing systemd service..."

REAL_HOME=$(eval echo ~$REAL_USER)

WORKING_DIR="/usr/local/bin"

cat > /tmp/tunnerse-api.service << EOF
[Unit]
Description=Tunnerse API - Tunnel server API
After=network.target

[Service]
Type=simple
User=$REAL_USER
Group=$REAL_USER
WorkingDirectory=$WORKING_DIR
ExecStart=/usr/local/bin/tunnerse-api
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

mv /tmp/tunnerse-api.service /etc/systemd/system/
systemctl daemon-reload

echo ""
echo "Systemd service installed successfully for user: $REAL_USER"
echo "Service will run from: $WORKING_DIR"
echo ""
echo "Installed files:"
echo "  - Binary: /usr/local/bin/tunnerse-api"
echo "  - Static: /usr/local/bin/static/"
echo "  - Config: /usr/local/bin/tunnerse.config"
echo "  - Env:    /usr/local/bin/.env"
echo ""
echo "To manage the service:"
echo "  sudo systemctl enable tunnerse-api    # Enable on boot"
echo "  sudo systemctl start tunnerse-api     # Start now"
echo "  sudo systemctl stop tunnerse-api      # Stop"
echo "  sudo systemctl status tunnerse-api    # Check status"
echo "  sudo journalctl -u tunnerse-api -f    # View logs"

echo ""
echo "Successfully installed tunnerse-api."
echo "Use 'tunnerse-api' to start the server manually."
echo "Or use systemd to run as a service (see above)."
echo ""
