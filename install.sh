#!/bin/sh
set -eu

APP_NAME=moitoring
INSTALL_DIR=${MOITORING_INSTALL_DIR:-/opt/moitoring}
SERVICE_USER=${MOITORING_SERVICE_USER:-moitoring}
SYSTEMD_UNIT_DIR=${MOITORING_SYSTEMD_UNIT_DIR:-/etc/systemd/system}
SYSTEMD_RUNTIME_DIR=${MOITORING_SYSTEMD_RUNTIME_DIR:-/run/systemd/system}
RELEASE_BASE=https://github.com/gabutlabs/moitoring/releases/latest/download

fail() {
	echo "install: $*" >&2
	exit 1
}

[ "$(uname -s)" = Linux ] || fail "only Linux is supported"
case "$(uname -m)" in
	x86_64|amd64) ARCH=amd64 ;;
	aarch64|arm64) ARCH=arm64 ;;
	*) fail "unsupported architecture: $(uname -m)" ;;
esac
case "$INSTALL_DIR" in
	/*) ;;
	*) fail "installation directory must be an absolute path" ;;
esac
case "$INSTALL_DIR" in
	*[!A-Za-z0-9_./-]*) fail "installation directory contains unsupported characters" ;;
esac
case "$SERVICE_USER" in
	""|*[!A-Za-z0-9_.-]*) fail "service user contains unsupported characters" ;;
esac

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"
command -v install >/dev/null 2>&1 || fail "install is required"
command -v chown >/dev/null 2>&1 || fail "chown is required"
command -v systemctl >/dev/null 2>&1 || fail "systemctl is required"
[ -d "$SYSTEMD_RUNTIME_DIR" ] || fail "systemd is not running"

WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' 0
trap 'exit 1' HUP INT TERM

ARCHIVE="$APP_NAME-linux-$ARCH.tar.gz"
ARCHIVE_PATH="$WORK_DIR/$ARCHIVE"
RELEASE_DIR="$WORK_DIR/release"
curl -fsSL "$RELEASE_BASE/$ARCHIVE" -o "$ARCHIVE_PATH"
mkdir -p "$RELEASE_DIR"
tar -xzf "$ARCHIVE_PATH" -C "$RELEASE_DIR"
[ -f "$RELEASE_DIR/$APP_NAME" ] || fail "release archive is missing $APP_NAME"
[ -f "$RELEASE_DIR/config.ini.example" ] || fail "release archive is missing config.ini.example"
[ -f "$RELEASE_DIR/moitoring.service.example" ] || fail "release archive is missing moitoring.service.example"
[ -f "$RELEASE_DIR/moitoring-worker.service.example" ] || fail "release archive is missing moitoring-worker.service.example"

if [ -L "$INSTALL_DIR/config.ini" ]; then
	fail "$INSTALL_DIR/config.ini must not be a symbolic link"
fi
if [ -e "$INSTALL_DIR/config.ini" ] && [ ! -f "$INSTALL_DIR/config.ini" ]; then
	fail "$INSTALL_DIR/config.ini is not a regular file"
fi
if [ -L "$INSTALL_DIR/data" ]; then
	fail "$INSTALL_DIR/data must not be a symbolic link"
fi
if [ -e "$INSTALL_DIR/data" ] && [ ! -d "$INSTALL_DIR/data" ]; then
	fail "$INSTALL_DIR/data is not a directory"
fi

if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
	command -v useradd >/dev/null 2>&1 || fail "useradd is required to create the service account"
	NOLOGIN=$(command -v nologin 2>/dev/null || echo /bin/false)
	useradd --system --user-group --home-dir "$INSTALL_DIR" --shell "$NOLOGIN" "$SERVICE_USER"
fi

install -d -m 0755 "$INSTALL_DIR"
install -d -m 0750 "$INSTALL_DIR/data"
install -m 0755 "$RELEASE_DIR/$APP_NAME" "$INSTALL_DIR/$APP_NAME.new"
mv -f "$INSTALL_DIR/$APP_NAME.new" "$INSTALL_DIR/$APP_NAME"
install -m 0644 "$RELEASE_DIR/config.ini.example" "$INSTALL_DIR/config.ini.example"

if [ ! -e "$INSTALL_DIR/config.ini" ]; then
	install -m 0600 "$RELEASE_DIR/config.ini.example" "$INSTALL_DIR/config.ini"
else
	chmod 0600 "$INSTALL_DIR/config.ini"
fi
chown "$SERVICE_USER" "$INSTALL_DIR/config.ini"
chown -R "$SERVICE_USER" "$INSTALL_DIR/data"

sed \
	-e "s|@INSTALL_DIR@|$INSTALL_DIR|g" \
	-e "s|@SERVICE_USER@|$SERVICE_USER|g" \
	"$RELEASE_DIR/moitoring.service.example" > "$WORK_DIR/moitoring.service"
sed \
	-e "s|@INSTALL_DIR@|$INSTALL_DIR|g" \
	-e "s|@SERVICE_USER@|$SERVICE_USER|g" \
	"$RELEASE_DIR/moitoring-worker.service.example" > "$WORK_DIR/moitoring-worker.service"
install -d -m 0755 "$SYSTEMD_UNIT_DIR"
install -m 0644 "$WORK_DIR/moitoring.service" "$SYSTEMD_UNIT_DIR/moitoring.service"
install -m 0644 "$WORK_DIR/moitoring-worker.service" "$SYSTEMD_UNIT_DIR/moitoring-worker.service"

systemctl daemon-reload
systemctl enable moitoring.service
if systemctl is-active --quiet moitoring.service; then
	systemctl restart moitoring.service
else
	systemctl start moitoring.service
fi
if systemctl is-enabled --quiet moitoring-worker.service; then
	systemctl restart moitoring-worker.service
fi

echo "Installed and started moitoring.service in $INSTALL_DIR"
echo "Review $INSTALL_DIR/config.ini before enabling the worker."
echo "Enable the worker after configuring RabbitMQ and SMTP: systemctl enable --now moitoring-worker.service"
