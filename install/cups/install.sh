#!/usr/bin/env bash
# install.sh – Install the Meta-Printer CUPS virtual printer and systemd
#              user service on a Debian/Ubuntu system.
#
# Usage:
#   ./install.sh (--target-queue <name> | --target-uri <uri>) [--printer-name <name>]
#
# Options:
#   --target-queue CUPS queue name of the physical printer (see `lpstat -v`).
#   --target-uri   Device URI of the physical printer, e.g.
#                    socket://192.168.1.100:9100   (raw TCP/IP)
#                    ipp://printer.local/printers/HP
#                  (--backend-uri is accepted as an alias.)
#   --printer-name Name for the CUPS printer queue. Default: MetaPrinter
#
# Exactly one of --target-queue / --target-uri is required unless
# /etc/meta-printer/target.json already exists. The target is applied by the
# root-run meta-printer-target.service at boot; to change it later edit that
# file and run: sudo systemctl restart meta-printer-target
#
# Requires: cups, make and go are only needed when prebuilt binaries are absent.
# Requires: LibreOffice (soffice) and python3 with UNO bindings (python3-uno)
#           on the print server, for converting office/text jobs into a
#           PDF with a real Writer header.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

PRINTER_NAME="MetaPrinter"
TARGET_QUEUE=""
TARGET_URI=""
TARGET_CONFIG="/etc/meta-printer/target.json"
# Placeholder device until meta-printer-target.service applies the real target.
PLACEHOLDER_URI="socket://127.0.0.1:9"

# ---------- check LibreOffice runtime dependency -----------------------
if ! command -v soffice &>/dev/null; then
	echo "Error: 'soffice' (LibreOffice) not found in PATH." >&2
	echo "  Office/text print jobs cannot get a real document header without it." >&2
	echo "  Install it, e.g.: sudo apt install libreoffice python3-uno" >&2
	exit 1
fi
if ! command -v python3 &>/dev/null; then
	echo "Error: 'python3' not found in PATH." >&2
	echo "  Install it along with the LibreOffice UNO bindings, e.g.: sudo apt install libreoffice python3-uno" >&2
	exit 1
fi
if ! python3 -c "import uno" &>/dev/null; then
	echo "Error: python3 'uno' module not found." >&2
	echo "  Install the LibreOffice UNO bindings, e.g.: sudo apt install python3-uno" >&2
	exit 1
fi

# ---------- parse arguments -------------------------------------------
while [[ $# -gt 0 ]]; do
	case "$1" in
	--target-uri | --backend-uri)
		TARGET_URI="$2"
		shift 2
		;;
	--target-queue)
		TARGET_QUEUE="$2"
		shift 2
		;;
	--printer-name)
		PRINTER_NAME="$2"
		shift 2
		;;
	*)
		echo "Unknown argument: $1" >&2
		exit 1
		;;
	esac
done

if [[ -n "${TARGET_QUEUE}" && -n "${TARGET_URI}" ]]; then
	echo "Error: use only one of --target-queue / --target-uri." >&2
	exit 1
fi
if [[ -z "${TARGET_QUEUE}${TARGET_URI}" && ! -f "${TARGET_CONFIG}" ]]; then
	echo "Error: specify the physical printer with --target-queue or --target-uri." >&2
	exit 1
fi

# ---------- resolve binaries (prefer prebuilt) --------------------------------
METAD_BIN="${REPO_ROOT}/bin/metad"
METAFILTER_BIN="${REPO_ROOT}/bin/metafilter"
METATARGET_BIN="${REPO_ROOT}/bin/metatarget"

if [[ -x "${METAD_BIN}" && -x "${METAFILTER_BIN}" && -x "${METATARGET_BIN}" ]]; then
	echo "==> Using prebuilt binaries in ${REPO_ROOT}/bin"
else
	echo "==> Prebuilt binaries not found – building from source..."
	(cd "${REPO_ROOT}" && make build)
fi

# Verify binaries exist after prebuilt-or-build path.
if [[ ! -x "${METAD_BIN}" || ! -x "${METAFILTER_BIN}" || ! -x "${METATARGET_BIN}" ]]; then
	echo "Error: required binaries are missing:"
	echo "  ${METAD_BIN}"
	echo "  ${METAFILTER_BIN}"
	echo "  ${METATARGET_BIN}"
	exit 1
fi

# ---------- install CUPS filter ---------------------------------------
FILTER_DIR="/usr/lib/cups/filter"
echo "==> Installing CUPS filter to ${FILTER_DIR}/metafilter"
sudo install -o root -g root -m 0755 \
	"${METAFILTER_BIN}" "${FILTER_DIR}/metafilter"

# ---------- register office MIME types --------------------------------
echo "==> Installing office MIME types"
sudo install -o root -g root -m 0644 \
	"${SCRIPT_DIR}/meta-printer.types" /usr/share/cups/mime/meta-printer.types

# ---------- write target printer config -------------------------------
if [[ -n "${TARGET_QUEUE}${TARGET_URI}" ]]; then
	echo "==> Writing ${TARGET_CONFIG}"
	sudo mkdir -p "$(dirname "${TARGET_CONFIG}")"
	if [[ -n "${TARGET_QUEUE}" ]]; then
		TARGET_JSON="$(printf '{"metaQueue":"%s","queue":"%s"}\n' "${PRINTER_NAME}" "${TARGET_QUEUE}")"
	else
		TARGET_JSON="$(printf '{"metaQueue":"%s","uri":"%s"}\n' "${PRINTER_NAME}" "${TARGET_URI}")"
	fi
	printf '%s\n' "${TARGET_JSON}" | sudo tee "${TARGET_CONFIG}" >/dev/null
	sudo chown root:root "${TARGET_CONFIG}"
	sudo chmod 0644 "${TARGET_CONFIG}"
fi

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
	-v "${PLACEHOLDER_URI}" \
	-P "${PPD_PATH}" \
	-D "Meta Printer (metadata injection)" \
	-L "Virtual CUPS printer – prepends document metadata"

echo "==> Enabling and accepting jobs for '${PRINTER_NAME}'"
sudo cupsenable "${PRINTER_NAME}"
sudo cupsaccept "${PRINTER_NAME}"

# ---------- install & start root target-retargeting unit --------------
echo "==> Installing meta-printer-target.service (applies the target printer at boot)"
sudo install -o root -g root -m 0755 "${METATARGET_BIN}" /usr/local/sbin/metatarget
sudo install -o root -g root -m 0644 \
	"${SCRIPT_DIR}/../systemd/meta-printer-target.service" \
	/etc/systemd/system/meta-printer-target.service
sudo systemctl daemon-reload
sudo systemctl enable meta-printer-target.service
sudo systemctl restart meta-printer-target.service
sudo systemctl restart cups

# ---------- install & start systemd user service ----------------------
SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
echo "==> Installing systemd user service to ${SYSTEMD_USER_DIR}/metad.service"
mkdir -p "${SYSTEMD_USER_DIR}"
install -m 0644 \
	"${SCRIPT_DIR}/../systemd/metad.service" \
	"${SYSTEMD_USER_DIR}/metad.service"

# Install the daemon binary system-wide so the service can find it.
sudo install -o root -g root -m 0755 \
	"${METAD_BIN}" "/usr/local/bin/metad"

echo "==> Enabling and starting metad service"
systemctl --user daemon-reload
systemctl --user enable --now metad.service

echo ""
echo "Installation complete."
echo "  CUPS printer : ${PRINTER_NAME}"
echo "  Target config: ${TARGET_CONFIG}  (edit, then: sudo systemctl restart meta-printer-target)"
echo "  Filter       : ${FILTER_DIR}/metafilter"
echo "  Daemon       : /usr/local/bin/metad  (systemd user service)"
echo ""
echo "To set MetaPrinter as your default printer run:"
echo "  lpoptions -d ${PRINTER_NAME}"
