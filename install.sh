#!/bin/sh
set -eu

APP_NAME=moitoring
INSTALL_DIR=${MOITORING_INSTALL_DIR:-/opt/moitoring}
INSTALL_OWNER=${SUDO_USER:-$(id -un)}
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

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"
command -v install >/dev/null 2>&1 || fail "install is required"
command -v chown >/dev/null 2>&1 || fail "chown is required"

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
chown "$INSTALL_OWNER" "$INSTALL_DIR/config.ini"
chown -R "$INSTALL_OWNER" "$INSTALL_DIR/data"

echo "Installed $APP_NAME in $INSTALL_DIR"
echo "Review $INSTALL_DIR/config.ini before starting the application."
echo "Run the API with: cd $INSTALL_DIR && ./$APP_NAME serve"
echo "Run the worker with: cd $INSTALL_DIR && ./$APP_NAME worker"
