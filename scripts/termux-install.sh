#!/data/data/com.termux/files/usr/bin/bash
# Builds and installs pebblewa inside Termux.
# Usage (from a clone of this repo, inside Termux):  bash scripts/termux-install.sh
set -euo pipefail

cd "$(dirname "$0")/../server"

if ! command -v go >/dev/null; then
  echo "Installing Go..."
  pkg install -y golang
fi

echo "Building pebblewa..."
CGO_ENABLED=0 go build -trimpath -o "$PREFIX/bin/pebblewa" ./cmd/pebblewa

mkdir -p ~/.termux/boot
cp ../scripts/termux-boot/start-pebblewa ~/.termux/boot/
chmod +x ~/.termux/boot/start-pebblewa

echo
echo "Installed: $(command -v pebblewa)"
echo "Token for the watchapp settings: $(pebblewa token)"
echo
echo "Next:"
echo "  1. Run:  termux-wake-lock && pebblewa -pair +<your number>"
echo "  2. In WhatsApp: Linked devices > Link a device > Link with phone number instead, enter the code."
echo "  3. Install Termux:Boot from F-Droid to start pebblewa automatically after a reboot."
