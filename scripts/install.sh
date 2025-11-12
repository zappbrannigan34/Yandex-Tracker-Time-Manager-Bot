#!/bin/bash
# Installation script for time-tracker-bot

set -e

VERSION="1.0.0"
INSTALL_DIR="/opt/time-tracker-bot"
CONFIG_DIR="/etc/time-tracker-bot"
USER="tracker"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}╔════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  Time Tracker Bot Installation v${VERSION}  ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════╝${NC}"
echo

# Check if running as root
if [ "$EUID" -ne 0 ]; then
  echo -e "${RED}✗ Please run as root (sudo)${NC}"
  exit 1
fi

# Check dependencies
echo -e "${YELLOW}→${NC} Checking dependencies..."

if ! command -v yc &> /dev/null; then
  echo -e "${RED}✗ yc CLI not found${NC}"
  echo "  Install: curl -sSL https://storage.yandexcloud.net/yandexcloud-yc/install.sh | bash"
  exit 1
fi

echo -e "${GREEN}✓${NC} yc CLI found"

# Check if yc is authenticated
if ! yc config list &> /dev/null; then
  echo -e "${YELLOW}⚠${NC} yc CLI not authenticated"
  echo "  Run: yc init"
  read -p "Continue anyway? (y/n) " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    exit 1
  fi
fi

# Build binary
echo -e "${YELLOW}→${NC} Building binary..."

if [ ! -f "time-tracker-bot" ]; then
  if command -v go &> /dev/null; then
    echo "  Compiling from source..."
    go build -o time-tracker-bot ./cmd/time-tracker-bot
  else
    echo -e "${RED}✗ Binary not found and Go not installed${NC}"
    echo "  Please build manually or download release binary"
    exit 1
  fi
fi

echo -e "${GREEN}✓${NC} Binary ready"

# Create user
echo -e "${YELLOW}→${NC} Creating user ${USER}..."

if id "$USER" &>/dev/null; then
  echo -e "${GREEN}✓${NC} User already exists"
else
  useradd -r -s /bin/false "$USER"
  echo -e "${GREEN}✓${NC} User created"
fi

# Create directories
echo -e "${YELLOW}→${NC} Creating directories..."

mkdir -p "$INSTALL_DIR"
mkdir -p "$INSTALL_DIR/state"
mkdir -p "$INSTALL_DIR/logs"
mkdir -p "$CONFIG_DIR"

echo -e "${GREEN}✓${NC} Directories created"

# Copy files
echo -e "${YELLOW}→${NC} Installing files..."

cp time-tracker-bot "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/time-tracker-bot"

# Copy config if not exists
if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
  if [ -f "config.yaml" ]; then
    cp config.yaml "$CONFIG_DIR/"
  else
    cp config.yaml.example "$CONFIG_DIR/config.yaml"
  fi
  echo -e "${YELLOW}⚠${NC} Config copied to $CONFIG_DIR/config.yaml - please edit it"
fi

# Copy data
if [ -d "data" ]; then
  cp -r data "$INSTALL_DIR/"
fi

echo -e "${GREEN}✓${NC} Files installed"

# Set permissions
echo -e "${YELLOW}→${NC} Setting permissions..."

chown -R "$USER:$USER" "$INSTALL_DIR"
chown -R "$USER:$USER" "$CONFIG_DIR"

echo -e "${GREEN}✓${NC} Permissions set"

# Install systemd service
if command -v systemctl &> /dev/null; then
  echo -e "${YELLOW}→${NC} Installing systemd service..."

  if [ -f "deployments/systemd/time-tracker-bot.service" ]; then
    cp deployments/systemd/time-tracker-bot.service /etc/systemd/system/

    # Update service file with actual paths
    sed -i "s|/opt/time-tracker-bot|$INSTALL_DIR|g" /etc/systemd/system/time-tracker-bot.service
    sed -i "s|--config /opt/time-tracker-bot/config.yaml|--config $CONFIG_DIR/config.yaml|g" /etc/systemd/system/time-tracker-bot.service

    systemctl daemon-reload
    echo -e "${GREEN}✓${NC} Systemd service installed"
  fi
fi

# Create symlink
echo -e "${YELLOW}→${NC} Creating symlink..."

ln -sf "$INSTALL_DIR/time-tracker-bot" /usr/local/bin/time-tracker-bot

echo -e "${GREEN}✓${NC} Symlink created"

echo
echo -e "${GREEN}╔════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  Installation Complete!                ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════╝${NC}"
echo
echo -e "${YELLOW}Next steps:${NC}"
echo -e "  1. Edit config:    sudo nano $CONFIG_DIR/config.yaml"
echo -e "  2. Set TRACKER_ORG_ID in config"
echo -e "  3. Test:           time-tracker-bot sync --dry-run"
echo
echo -e "${YELLOW}Systemd service:${NC}"
echo -e "  Enable:  sudo systemctl enable time-tracker-bot"
echo -e "  Start:   sudo systemctl start time-tracker-bot"
echo -e "  Status:  sudo systemctl status time-tracker-bot"
echo -e "  Logs:    sudo journalctl -u time-tracker-bot -f"
echo
