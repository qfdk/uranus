#!/bin/bash
# 在各发行版真实安装的 nginx 上检查 Uranus 找到的位置：可执行文件、主配置、PID 文件、
# 网站目录（以及 nginx.conf 是否引用了它）和证书目录。
# 每种安装方式启动一个一次性的 Docker 容器，安装 nginx 后运行 TestRealNginxLayout：
# 必要时加上 include，在网站目录里放一个网站，nginx -t 必须通过，启动后 PID 文件要在检测到的位置。
#
# 用法：scripts/test-nginx-layouts.sh [名称...]，不带参数时全部检查
set -euo pipefail
cd "$(dirname "$0")/.."

OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

NGINX_SRC=https://nginx.org/download/nginx-1.28.0.tar.gz
APT='apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nginx'
SOURCE_BUILD="apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq build-essential libpcre2-dev zlib1g-dev curl \
  && curl -fsSL $NGINX_SRC | tar xz -C /tmp && cd /tmp/nginx-* && ./configure && make -j4 && make install"

# 名称|镜像|平台|安装命令|期望的网站目录|期望 nginx.conf 已引用
LAYOUTS=(
  "debian|debian:bookworm-slim||$APT|/etc/nginx/conf.d|true"
  "ubuntu-22.04|ubuntu:22.04||$APT|/etc/nginx/conf.d|true"
  "ubuntu-24.04|ubuntu:24.04||$APT|/etc/nginx/conf.d|true"
  "rocky-9|rockylinux/rockylinux:9||dnf install -y -q nginx|/etc/nginx/conf.d|true"
  "alpine|alpine:3||apk add -q --no-cache nginx|/etc/nginx/http.d|true"
  "nginx-official-image|nginx:alpine||true|/etc/nginx/conf.d|true"
  "openresty-image|openresty/openresty:alpine||true|/etc/nginx/conf.d|true"
  "arch|archlinux:latest|linux/amd64|pacman -Sy --noconfirm --quiet nginx|/etc/nginx/conf.d|false"
  "source-build|debian:bookworm-slim||$SOURCE_BUILD|/usr/local/nginx/conf/conf.d|false"
)

# wanted 名称 [要检查的名称...]：没有指定时全部检查
wanted() {
  local name=$1; shift
  [ $# -eq 0 ] && return 0
  for w in "$@"; do [ "$w" = "$name" ] && return 0; done
  return 1
}

failed=()
checked=0
for layout in "${LAYOUTS[@]}"; do
  IFS='|' read -r name image platform setup want_sites want_included <<<"$layout"
  wanted "$name" "$@" || continue
  arch=$(docker version -f '{{.Server.Arch}}')
  [ -n "$platform" ] && arch=${platform#linux/}
  if [ ! -f "$OUT/config-$arch.test" ]; then
    GOOS=linux GOARCH=$arch CGO_ENABLED=0 go test -c -o "$OUT/config-$arch.test" ./internal/config
  fi
  echo "==> $name ($image)"
  checked=$((checked + 1))
  status=0
  docker run --rm ${platform:+--platform "$platform"} -v "$OUT:/uranus-test:ro" \
    -e URANUS_NGINX_E2E=1 -e URANUS_NGINX_E2E_WRITE=1 \
    -e WANT_SITES="$want_sites" -e WANT_INCLUDED="$want_included" \
    "$image" sh -c "($setup) >/tmp/setup.log 2>&1 || { tail -20 /tmp/setup.log; exit 1; }
      /uranus-test/config-$arch.test -test.run TestRealNginxLayout -test.v" || status=$?
  [ "$status" -eq 0 ] || failed+=("$name")
done

if [ "$checked" -eq 0 ]; then
  echo "没有匹配的名称：$*"
  exit 1
fi
if [ ${#failed[@]} -gt 0 ]; then
  echo "失败：${failed[*]}"
  exit 1
fi
echo "全部 $checked 种安装方式通过"
