#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_NAME="${APP_NAME:-fnmonitor}"
DISPLAY_NAME="${DISPLAY_NAME:-飞牛系统监控}"
VERSION="${VERSION:-0.1.14}"
PLATFORM="${PLATFORM:-x86}"
PORT="${PORT:-39187}"

case "${PLATFORM}" in
    x86)
        GOARCH="${GOARCH:-amd64}"
        ;;
    arm)
        GOARCH="${GOARCH:-arm64}"
        ;;
    *)
        echo "unsupported PLATFORM=${PLATFORM}; use x86 or arm" >&2
        exit 1
        ;;
esac

BUILD_DIR="${ROOT_DIR}/.build/fnos-${PLATFORM}"
APP_ROOT="${BUILD_DIR}/app_root"
PKG_DIR="${BUILD_DIR}/package"
DIST_DIR="${ROOT_DIR}/dist"
APP_TGZ="${BUILD_DIR}/app.tgz"
FPK_NAME="${APP_NAME}_${VERSION}_${PLATFORM}.fpk"
FPK_PATH="${DIST_DIR}/${FPK_NAME}"

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || {
        echo "missing required command: $1" >&2
        exit 1
    }
}

copy_template() {
    local src="$1"
    local dest="$2"
    sed "s/__PORT__/${PORT}/g" "${src}" > "${dest}"
}

render_icon_go() {
    local out="$1"
    cat > "${out}" <<'GO'
package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

func rect(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA) {
	for y := y1; y < y2; y++ {
		for x := x1; x < x2; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func icon(size int, path string) error {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{20, 44, 62, 255}
	panel := color.RGBA{246, 249, 252, 255}
	teal := color.RGBA{34, 155, 132, 255}
	blue := color.RGBA{54, 105, 196, 255}
	warn := color.RGBA{230, 155, 46, 255}
	line := color.RGBA{210, 222, 233, 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, bg)
		}
	}
	m := size / 7
	rect(img, m, m, size-m, size-m, panel)
	rect(img, m, m, size-m, m+size/12, teal)
	rect(img, size/5, size/2, size/5+size/12, size-m-size/10, blue)
	rect(img, size/5+size/6, size/3, size/5+size/4, size-m-size/10, teal)
	rect(img, size/5+size/3, size/2-size/12, size/5+size/3+size/12, size-m-size/10, warn)
	rect(img, size/5, size-m-size/10, size-m-size/6, size-m-size/12, line)
	rect(img, size/5, size/3-size/20, size-m-size/6, size/3, line)
	rect(img, size/5, size/2-size/20, size-m-size/6, size/2, line)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func main() {
	if err := icon(64, os.Args[1]); err != nil {
		panic(err)
	}
	if err := icon(256, os.Args[2]); err != nil {
		panic(err)
	}
}
GO
}

verify_fpk() {
    local fpk="$1"
    local listing
    listing="$(tar -tzf "${fpk}" | sed 's#^\./##')"
    local required=(
        manifest
        app.tgz
        cmd/main
        cmd/common
        cmd/installer
        cmd/install_init
        cmd/install_callback
        cmd/uninstall_init
        cmd/uninstall_callback
        cmd/upgrade_init
        cmd/upgrade_callback
        wizard/config
        ICON.PNG
        ICON_256.PNG
    )
    local entry
    for entry in "${required[@]}"; do
        if ! grep -qxF "${entry}" <<< "${listing}"; then
            echo "fpk missing required entry: ${entry}" >&2
            exit 1
        fi
    done

    local declared computed
    declared="$(tar -xOzf "${fpk}" manifest | awk -F= '/^checksum/ {gsub(/[ \t]/, "", $2); print $2}')"
    computed="$(tar -xOzf "${fpk}" app.tgz | md5sum | awk '{print $1}')"
    if [ "${declared}" != "${computed}" ]; then
        echo "checksum mismatch: manifest=${declared} computed=${computed}" >&2
        exit 1
    fi
}

require_cmd go
require_cmd tar
require_cmd md5sum
require_cmd sed

rm -rf "${BUILD_DIR}"
mkdir -p "${APP_ROOT}/bin" "${PKG_DIR}" "${DIST_DIR}"

echo "==> Building ${APP_NAME} ${VERSION} for linux/${GOARCH}"
(
    cd "${ROOT_DIR}"
    CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" go build -trimpath -ldflags="-s -w" -o "${APP_ROOT}/fnmonitor" ./cmd/fnmonitor
)
chmod +x "${APP_ROOT}/fnmonitor"

cp "${ROOT_DIR}/config.example.yaml" "${APP_ROOT}/config.yaml"
sed -i "s/listen: \":[0-9][0-9]*\"/listen: \":${PORT}\"/" "${APP_ROOT}/config.yaml"

cat > "${APP_ROOT}/bin/fnmonitor-server" <<'EOF'
#!/bin/sh
set -eu

APP_DIR="${TRIM_APPDEST:-$(cd "$(dirname "$0")/.." && pwd)}"
APP_DATA_DIR="${1:-${TRIM_PKGVAR:-${APP_DIR}/data}}"
CONFIG_FILE="${APP_DATA_DIR}/config.yaml"

mkdir -p "${APP_DATA_DIR}"
if [ ! -f "${CONFIG_FILE}" ]; then
    cp "${APP_DIR}/config.yaml" "${CONFIG_FILE}"
fi

export FNMONITOR_TEST_RESULT_FILE="${APP_DATA_DIR}/last-notification-test.log"

cd "${APP_DIR}"
exec "${APP_DIR}/fnmonitor" -config "${CONFIG_FILE}"
EOF
chmod +x "${APP_ROOT}/bin/fnmonitor-server"

echo "==> Creating app.tgz"
tar -czf "${APP_TGZ}" -C "${APP_ROOT}" .
CHECKSUM="$(md5sum "${APP_TGZ}" | awk '{print $1}')"

mkdir -p "${PKG_DIR}/cmd" "${PKG_DIR}/config" "${PKG_DIR}/wizard" "${PKG_DIR}/ui/images"
cp "${APP_TGZ}" "${PKG_DIR}/app.tgz"

for f in "${ROOT_DIR}/packaging/fnos/cmd/"*; do
    copy_template "${f}" "${PKG_DIR}/cmd/$(basename "${f}")"
    chmod +x "${PKG_DIR}/cmd/$(basename "${f}")"
done
cp "${ROOT_DIR}/packaging/fnos/config/privilege" "${PKG_DIR}/config/privilege"
cp "${ROOT_DIR}/packaging/fnos/config/resource" "${PKG_DIR}/config/resource"
copy_template "${ROOT_DIR}/packaging/fnos/wizard/install" "${PKG_DIR}/wizard/install"
copy_template "${ROOT_DIR}/packaging/fnos/wizard/config" "${PKG_DIR}/wizard/config"
cp "${ROOT_DIR}/packaging/fnos/wizard/uninstall" "${PKG_DIR}/wizard/uninstall"
cp "${ROOT_DIR}/packaging/fnos/health.json" "${PKG_DIR}/health.json"

cat > "${PKG_DIR}/manifest" <<EOF
appname         = ${APP_NAME}
version         = ${VERSION}
display_name    = ${DISPLAY_NAME}
platform        = ${PLATFORM}
maintainer      = fnmonitor
maintainer_url  = https://github.com/
distributor     = local
distributor_url = https://github.com/
desktop_uidir   = ui
desktop_applaunchname = ${APP_NAME}.Settings
service_port    = ${PORT}
checkport       = true
ctl_stop        = true
desc            = 飞牛/fnOS 系统监控：采集系统日志、CPU、内存、磁盘、网络、温度、进程等指标，并在异常时发送通知。
source          = thirdparty
checksum        = ${CHECKSUM}
EOF

cat > "${PKG_DIR}/ui/config" <<EOF
{
    ".url": {
        "${APP_NAME}.Settings": {
            "title": "${DISPLAY_NAME}",
            "desc": "设置与测试通知",
            "icon": "images/{0}.png",
            "type": "url",
            "port": "${PORT}",
            "protocol": "http",
            "url": "/settings",
            "allUsers": true
        },
        "${APP_NAME}.Dashboard": {
            "title": "监控面板",
            "desc": "系统日志与指标监控",
            "icon": "images/{0}.png",
            "type": "url",
            "port": "${PORT}",
            "protocol": "http",
            "url": "/",
            "allUsers": true
        }
    }
}
EOF

cat > "${PKG_DIR}/fnmonitor.sc" <<EOF
[fnmonitor]
title="${DISPLAY_NAME}"
desc="系统日志与指标监控"
port_forward="yes"
src.ports="${PORT}/tcp"
dst.ports="${PORT}/tcp"
EOF

ICON_GO="${BUILD_DIR}/make_icon.go"
render_icon_go "${ICON_GO}"
go run "${ICON_GO}" "${PKG_DIR}/ui/images/64.png" "${PKG_DIR}/ICON_256.PNG"
cp "${PKG_DIR}/ui/images/64.png" "${PKG_DIR}/ICON.PNG"
cp "${PKG_DIR}/ICON_256.PNG" "${PKG_DIR}/ui/images/256.png"

echo "==> Creating ${FPK_NAME}"
(
    cd "${PKG_DIR}"
    tar -czf "${FPK_PATH}" *
)

verify_fpk "${FPK_PATH}"
echo "Built ${FPK_PATH}"
