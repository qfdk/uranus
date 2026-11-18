#!/usr/bin/env bash
# 编译 Linux amd64/arm64 发布版本，并发布到 https://de.qfdk.me/uranus/
#
# 在 Docker 的 Ubuntu 22.04 里执行 make release（cgo + aarch64 交叉编译器），
# 产物需要 glibc >= 2.34，与之前在 Ubuntu 22.04 服务器上编译的版本一致。
#
# 用法：scripts/release.sh               编译并上传
#       scripts/release.sh --build-only  只编译到 dist/
# 上传位置可用环境变量覆盖：URANUS_RELEASE_HOST、URANUS_RELEASE_DIR
set -euo pipefail

cd "$(dirname "$0")/.."

PUBLIC_URL="https://de.qfdk.me/uranus"
REMOTE_HOST="${URANUS_RELEASE_HOST:-root@de.qfdk.me}"
REMOTE_DIR="${URANUS_RELEASE_DIR:-/opt/1panel/www/sites/de.qfdk.me/index/uranus}"
GO_VERSION="$(sed -n 's/^toolchain go//p' go.mod)"
FILES=(uranus-amd64 uranus-arm64 install.sh upgrade.sh uninstall.sh version.json)

if [ -n "$(git status --porcelain)" ]; then
  echo "工作区有未提交的修改：发布版本必须对应一个已提交的版本" >&2
  exit 1
fi

echo "==> 编译 $(git rev-parse --short HEAD)（Ubuntu 22.04，go${GO_VERSION}）"
rm -rf dist
mkdir -p dist
docker run --rm --platform linux/amd64 \
  -v "$PWD":/src:ro -v "$PWD/dist":/out \
  -e GO_VERSION="$GO_VERSION" -e TZ=Europe/Paris -e DEBIAN_FRONTEND=noninteractive \
  ubuntu:22.04 bash -euo pipefail -c '
    apt-get update -qq
    apt-get install -y -qq gcc gcc-aarch64-linux-gnu libc6-dev-arm64-cross make git ca-certificates curl tzdata >/dev/null
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
    export PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=local
    cp -a /src /build
    git config --global --add safe.directory /build
    cd /build
    make release >/dev/null
    cp dist/uranus-amd64 dist/uranus-arm64 /out/
    printf "{\"commitId\":\"%s\",\"buildTime\":\"%s\",\"buildVersion\":\"%s\"}\n" \
      "$(git rev-parse HEAD)" "$(cat /tmp/release_time)" "$(sed -n "s/^BUILD_VERSION:=//p" Makefile)" > /out/version.json
    chown "$(stat -c %u:%g /out)" /out/*
  '
cp scripts/install.sh scripts/upgrade.sh scripts/uninstall.sh dist/
cat dist/version.json

if [ "${1:-}" = "--build-only" ]; then
  exit 0
fi

echo "==> 上传到 ${REMOTE_HOST}:${REMOTE_DIR}"
# 先传到同目录下的临时目录，再逐个改名到位：下载方不会拿到传了一半的文件；
# 程序先于 version.json 就位，客户端看到新版本号时，新程序已经可以下载。
ssh "$REMOTE_HOST" "mkdir -p '$REMOTE_DIR/.upload'"
(cd dist && scp -q "${FILES[@]}" "$REMOTE_HOST:$REMOTE_DIR/.upload/")
ssh "$REMOTE_HOST" "cd '$REMOTE_DIR' && chmod 755 .upload/uranus-* && chmod 644 .upload/*.sh .upload/version.json \
  && mv -f .upload/uranus-amd64 .upload/uranus-arm64 .upload/*.sh . && mv -f .upload/version.json . && rmdir .upload"

echo "==> 校验 ${PUBLIC_URL}"
sha256() {
  if command -v sha256sum >/dev/null; then sha256sum | cut -d' ' -f1; else shasum -a 256 | cut -d' ' -f1; fi
}
for f in "${FILES[@]}"; do
  if [ "$(curl -fsSL "$PUBLIC_URL/$f" | sha256)" != "$(sha256 < "dist/$f")" ]; then
    echo "校验失败：$PUBLIC_URL/$f 与本地 dist/$f 不一致" >&2
    exit 1
  fi
  echo "    $f OK"
done
echo "==> 发布完成"
