#!/usr/bin/env bash
# install.sh – Install the Meta-Printer CUPS virtual printer and systemd
#              user service on a Debian/Ubuntu system.
#
# Usage:
#   ./install.sh [--backend-uri <uri>] [--printer-name <name>]
#
# Options:
#   --backend-uri  URI of the real printer this virtual printer forwards to.
#                  Defaults to "cups-pdf:/" (write output to PDF file).
#                  Examples:
#                    socket://192.168.1.100:9100   (raw TCP/IP)
#                    ipp://printer.local/printers/HP
#                    file:///dev/null               (discard – for testing)
#   --printer-name Name for the CUPS printer queue. Default: MetaPrinter
#
# Requires: cups, make, go (for building).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

PRINTER_NAME="MetaPrinter"
BACKEND_URI="cups-pdf:/"

# ---------- parse arguments -------------------------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --backend-uri)  BACKEND_URI="$2";  shift 2 ;;
        --printer-name) PRINTER_NAME="$2"; shift 2 ;;
        *) echo "Unknown argument: $1" >&2; exit 1 ;;
    esac
done

# ---------- build binaries --------------------------------------------
echo "==> Building meta-printer binaries…"
(cd "${REPO_ROOT}" && make build)

# ---------- install CUPS filter ---------------------------------------
FILTER_DIR="/usr/lib/cups/filter"
echo "==> Installing CUPS filter to ${FILTER_DIR}/metafilter"
sudo install -o root -g root -m 0755 \
    "${REPO_ROOT}/bin/metafilter" "${FILTER_DIR}/metafilter"

# ---------- install PPD -----------------------------------------------
PPD_DIR="/usr/share/ppd/meta-printer"
PPD_PATH="${PPD_DIR}/MetaPrinter.ppd"
echo "==> Installing PPD to ${PPD_PATH}"
sudo mkdir -p "${PPD_DIR}"
sudo install -o root -g root -m 0644 \
    "${SCRIPT_DIR}/MetaPrinter.ppd" "${PPD_PATH}"

# ---------- create shared metadata directory --------------------------
echo "==> Creating /var/lib/meta-printer (writable by lp group)"
sudo mkdir -p /var/lib/meta-printer
sudo chown root:lp /var/lib/meta-printer
sudo chmod 0775 /var/lib/meta-printer

# ---------- register CUPS printer -------------------------------------
echo "==> Registering printer '${PRINTER_NAME}' in CUPS"
if lpstat -p "${PRINTER_NAME}" &>/dev/null; then
    echo "    Printer already exists – removing old queue first"
    sudo lpadmin -x "${PRINTER_NAME}"
fi
sudo lpadmin \
    -p "${PRINTER_NAME}" \
    -E \
    -v "${BACKEND_URI}" \
    -P "${PPD_PATH}" \
    -D "Meta Printer (metadata injection)" \
    -L "Virtual CUPS printer – prepends document metadata"

echo "==> Enabling and accepting jobs for '${PRINTER_NAME}'"
sudo cupsenable  "${PRINTER_NAME}"
sudo cupsaccept  "${PRINTER_NAME}"

# ---------- install & start systemd user service ----------------------
SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
echo "==> Installing systemd user service to ${SYSTEMD_USER_DIR}/metad.service"
mkdir -p "${SYSTEMD_USER_DIR}"
install -m 0644 \
    "${SCRIPT_DIR}/../systemd/metad.service" \
    "${SYSTEMD_USER_DIR}/metad.service"

# Install the daemon binary system-wide so the service can find it.
sudo install -o root -g root -m 0755 \
    "${REPO_ROOT}/bin/metad" "/usr/local/bin/metad"

echo "==> Enabling and starting metad service"
systemctl --user daemon-reload
systemctl --user enable --now metad.service

echo ""
echo "Installation complete."
echo "  CUPS printer : ${PRINTER_NAME}"
echo "  Backend URI  : ${BACKEND_URI}"
echo "  Filter       : ${FILTER_DIR}/metafilter"
echo "  Daemon       : /usr/local/bin/metad  (systemd user service)"
echo ""
echo "To set MetaPrinter as your default printer run:"
echo "  lpoptions -d ${PRINTER_NAME}"
