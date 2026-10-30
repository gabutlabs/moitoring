#!/bin/sh
set -eu

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' 0
mkdir -p "$TEST_DIR/bin" "$TEST_DIR/releases"

make_release_archive() {
	architecture=$1
	archive_dir="$TEST_DIR/build-$architecture"
	mkdir -p "$archive_dir"
	printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$architecture" > "$archive_dir/moitoring"
	chmod 0755 "$archive_dir/moitoring"
	printf '[server]\nport = 8080\n' > "$archive_dir/config.ini.example"
	tar -czf "$TEST_DIR/releases/moitoring-linux-$architecture.tar.gz" -C "$archive_dir" moitoring config.ini.example
}

make_release_archive amd64
make_release_archive arm64

cat > "$TEST_DIR/bin/curl" <<'EOF'
#!/bin/sh
set -eu
output=
archive=
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o) shift; output=$1 ;;
		https://github.com/gabutlabs/moitoring/releases/latest/download/*) archive=${1##*/} ;;
	esac
	shift
done
[ -n "$output" ] && [ -n "$archive" ]
cp "$TEST_RELEASE_DIR/$archive" "$output"
printf '%s\n' "$archive" >> "$TEST_DOWNLOAD_LOG"
EOF
cat > "$TEST_DIR/bin/uname" <<'EOF'
#!/bin/sh
set -eu
case "$1" in
	-s) echo Linux ;;
	-m) echo "$TEST_UNAME_MACHINE" ;;
	*) exit 1 ;;
esac
EOF
for tool in go npm; do
	cat > "$TEST_DIR/bin/$tool" <<EOF
#!/bin/sh
echo "$tool must not run on the user's machine" >&2
exit 1
EOF
done
chmod 0755 "$TEST_DIR/bin/curl" "$TEST_DIR/bin/uname" "$TEST_DIR/bin/go" "$TEST_DIR/bin/npm"

INSTALL_DIR="$TEST_DIR/opt/moitoring"
PATH="$TEST_DIR/bin:$PATH" \
	TEST_RELEASE_DIR="$TEST_DIR/releases" \
	TEST_DOWNLOAD_LOG="$TEST_DIR/downloads.log" \
	TEST_UNAME_MACHINE=x86_64 \
	MOITORING_INSTALL_DIR="$INSTALL_DIR" \
	"$PROJECT_DIR/install.sh"
[ "$("$INSTALL_DIR/moitoring")" = amd64 ]
[ -f "$INSTALL_DIR/config.ini" ]
[ -f "$INSTALL_DIR/config.ini.example" ]
[ -d "$INSTALL_DIR/data" ]
grep -q 'moitoring-linux-amd64.tar.gz' "$TEST_DIR/downloads.log"

CONFIG_MODE=$(stat -c '%a' "$INSTALL_DIR/config.ini" 2>/dev/null || stat -f '%Lp' "$INSTALL_DIR/config.ini")
[ "$CONFIG_MODE" = 600 ]
printf 'keep this setting\n' > "$INSTALL_DIR/config.ini"
PATH="$TEST_DIR/bin:$PATH" \
	TEST_RELEASE_DIR="$TEST_DIR/releases" \
	TEST_DOWNLOAD_LOG="$TEST_DIR/downloads.log" \
	TEST_UNAME_MACHINE=amd64 \
	MOITORING_INSTALL_DIR="$INSTALL_DIR" \
	"$PROJECT_DIR/install.sh" >/dev/null
grep -q '^keep this setting$' "$INSTALL_DIR/config.ini"

ARM_INSTALL_DIR="$TEST_DIR/opt/moitoring-arm"
PATH="$TEST_DIR/bin:$PATH" \
	TEST_RELEASE_DIR="$TEST_DIR/releases" \
	TEST_DOWNLOAD_LOG="$TEST_DIR/downloads.log" \
	TEST_UNAME_MACHINE=aarch64 \
	MOITORING_INSTALL_DIR="$ARM_INSTALL_DIR" \
	"$PROJECT_DIR/install.sh" >/dev/null
[ "$("$ARM_INSTALL_DIR/moitoring")" = arm64 ]

if PATH="$TEST_DIR/bin:$PATH" TEST_UNAME_MACHINE=mips MOITORING_INSTALL_DIR="$TEST_DIR/unsupported" "$PROJECT_DIR/install.sh" >"$TEST_DIR/unsupported.log" 2>&1; then
	echo "installer accepted an unsupported architecture" >&2
	exit 1
fi
grep -q 'unsupported architecture' "$TEST_DIR/unsupported.log"

echo "installer checks passed"
