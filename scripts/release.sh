#!/usr/bin/env bash
# 编译 Linux amd64/arm64 发布版本，并发布到 https://de.qfdk.me/uranus/
#
# 在 Docker 的 Ubuntu 22.04 里执行 make release（cgo + aarch64 交叉编译器），
# 产物需要 glibc >= 2.34，与之前在 Ubuntu 22.04 服务器上编译的版本一致。
#
# 用法：scripts/release.sh               编译并上传
#       scripts/release.sh --build-only  只编译到 dist/
#
# 上传目标只写在本地的 .release.env（已加入 .gitignore，不进公开仓库），例如：
#   URANUS_RELEASE_HOST=user@host
#   URANUS_RELEASE_DIR=/path/to/site/uranus
set -euo pipefail

cd "$(dirname "$0")/.."

PUBLIC_URL="https://de.qfdk.me/uranus"
GO_VERSION="$(sed -n 's/^toolchain go//p' go.mod)"
# archive.ubuntu.com 有时连不上或极慢，默认用法国镜像，可以用 APT_MIRROR 换成别的
APT_MIRROR="${APT_MIRROR:-http://fr.archive.ubuntu.com/ubuntu}"
FILES=(uranus-amd64 uranus-arm64 install.sh upgrade.sh uninstall.sh version.json version.json.sig)
# version.json 用这把私钥签名，节点升级前用编在程序里的公钥验证。私钥只在发布用的机器上，要另外备份：
# 丢了以后，已经部署的节点都不会再接受新版本，只能用 upgrade.sh 手动升级一次
SIGNING_KEY="${URANUS_SIGNING_KEY:-$HOME/.config/uranus/release-signing.key}"

if [ "${1:-}" != "--build-only" ]; then
  if [ -f .release.env ]; then
    . ./.release.env
  fi
  : "${URANUS_RELEASE_HOST:?请在 .release.env 中设置 URANUS_RELEASE_HOST（SSH 登录目标，如 user@host）}"
  : "${URANUS_RELEASE_DIR:?请在 .release.env 中设置 URANUS_RELEASE_DIR（网站下 uranus 目录的路径）}"
  REMOTE_HOST="$URANUS_RELEASE_HOST"
  REMOTE_DIR="$URANUS_RELEASE_DIR"
fi

if [ ! -f "$SIGNING_KEY" ]; then
  echo "没有找到签名私钥 $SIGNING_KEY（可以用 URANUS_SIGNING_KEY 指定）" >&2
  exit 1
fi

if [ -n "$(git status --porcelain)" ]; then
  echo "工作区有未提交的修改：发布版本必须对应一个已提交的版本" >&2
  exit 1
fi

echo "==> 编译 $(git rev-parse --short HEAD)（Ubuntu 22.04，go${GO_VERSION}）"
rm -rf dist
mkdir -p dist
# 从当前提交的干净克隆编译：发布过程中工作区的修改不会混进发布的程序
SRC="$(mktemp -d)"
trap 'rm -rf "$SRC"' EXIT
git clone -q --no-hardlinks . "$SRC/uranus"
docker run --rm --platform linux/amd64 \
  -v "$SRC/uranus":/src:ro -v "$PWD/dist":/out \
  -e GO_VERSION="$GO_VERSION" -e APT_MIRROR="$APT_MIRROR" -e TZ=Europe/Paris -e DEBIAN_FRONTEND=noninteractive \
  ubuntu:22.04 bash -euo pipefail -c '
    sed -i "s|http://archive.ubuntu.com/ubuntu|$APT_MIRROR|g; s|http://security.ubuntu.com/ubuntu|$APT_MIRROR|g" /etc/apt/sources.list
    apt-get update -qq
    apt-get install -y -qq gcc gcc-aarch64-linux-gnu libc6-dev-arm64-cross make git ca-certificates curl tzdata >/dev/null
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
    export PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=local
    cp -a /src /build
    git config --global --add safe.directory /build
    cd /build
    make release >/dev/null
    cp dist/uranus-amd64 dist/uranus-arm64 /out/
    printf "{\"commitId\":\"%s\",\"buildTime\":\"%s\",\"buildVersion\":\"%s\",\"release\":%s}\n" \
      "$(git rev-parse HEAD)" "$(cat /tmp/release_time)" "$(sed -n "s/^BUILD_VERSION:=//p" Makefile)" "$(git rev-list --count HEAD)" > /out/version.json
    chown "$(stat -c %u:%g /out)" /out/*
  '
cp scripts/install.sh scripts/upgrade.sh scripts/uninstall.sh dist/
# 写入各程序的 sha256 和大小，再签名
go run ./cmd/sign-release -key "$SIGNING_KEY" dist/version.json
cat dist/version.json

if [ "${1:-}" = "--build-only" ]; then
  exit 0
fi

echo "==> 上传到 ${REMOTE_HOST}:${REMOTE_DIR}"
# 先传到同目录下的临时目录，再逐个改名到位：下载方不会拿到传了一半的文件；
# 程序先于 version.json 就位，客户端看到新版本号时，新程序已经可以下载。
ssh "$REMOTE_HOST" "mkdir -p '$REMOTE_DIR/.upload'"
(cd dist && scp -q "${FILES[@]}" "$REMOTE_HOST:$REMOTE_DIR/.upload/")
ssh "$REMOTE_HOST" "cd '$REMOTE_DIR' && chmod 755 .upload/uranus-* && chmod 644 .upload/*.sh .upload/version.json* \
  && mv -f .upload/uranus-amd64 .upload/uranus-arm64 .upload/*.sh . && mv -f .upload/version.json.sig .upload/version.json . && rmdir .upload"

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
