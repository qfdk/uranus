#!/bin/bash
# 在各发行版真实安装的 nginx 上检查 Uranus 找到的位置：可执行文件、主配置、PID 文件、
# 网站目录（以及 nginx.conf 是否引用了它）和证书目录。
# 每种安装方式启动一个一次性的 Docker 容器，安装 nginx 后运行 TestRealNginxLayout，像在页面上一样操作：
# 必要时加上 include，保存一个网站，启动、重载、重启、停止 nginx，网站要能访问，进程号要在检测到的 PID 文件里。
#
# 用法：scripts/test-nginx-layouts.sh [名称...]，不带参数时全部检查
set -euo pipefail
cd "$(dirname "$0")/.."

OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

NGINX_SRC=https://nginx.org/download/nginx-1.28.0.tar.gz
APT='apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nginx'
SOURCE_BUILD="apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq build-essential libpcre2-dev zlib1g-dev curl \
  && curl -fsSL -o /tmp/nginx.tar.gz $NGINX_SRC && tar xzf /tmp/nginx.tar.gz -C /tmp \
  && cd /tmp/nginx-* && ./configure && make -j4 && make install"

# 名称|镜像|平台|安装命令|期望的网站目录|期望 nginx.conf 已引用（字段用 | 分隔，安装命令里不能有 |）
LAYOUTS=(
  "debian|debian:bookworm-slim||$APT|/etc/nginx/conf.d|true"
  "ubuntu-22.04|ubuntu:22.04||$APT|/etc/nginx/conf.d|true"
  "ubuntu-24.04|ubuntu:24.04||$APT|/etc/nginx/conf.d|true"
  "rocky-9|rockylinux/rockylinux:9||dnf install -y -q nginx|/etc/nginx/conf.d|true"
  "alpine|alpine:3||apk add -q --no-cache nginx|/etc/nginx/http.d|true"
  "nginx-official-image|nginx:alpine||true|/etc/nginx/conf.d|true"
  "openresty-image|openresty/openresty:alpine||true|/etc/nginx/conf.d|true"
  # 在 Apple 芯片上模拟 amd64 时，pacman 下载用的沙箱（seccomp）不可用，要关掉
  "arch|archlinux:latest|linux/amd64|pacman -Syu --noconfirm --quiet --disable-sandbox nginx|/etc/nginx/conf.d|false"
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
  case "$want_included" in
    true | false) ;;
    *) echo "$name 的定义有误：字段数不对，安装命令里可能有 |"; exit 1 ;;
  esac
  wanted "$name" "$@" || continue
  arch=$(docker version -f '{{.Server.Arch}}')
  [ -n "$platform" ] && arch=${platform#linux/}
  if [ ! -f "$OUT/e2e-$arch.test" ]; then
    GOOS=linux GOARCH=$arch CGO_ENABLED=0 go test -c -o "$OUT/e2e-$arch.test" ./internal/e2e
  fi
  echo "==> $name ($image)"
  checked=$((checked + 1))
  status=0
  docker run --rm ${platform:+--platform "$platform"} -v "$OUT:/uranus-test:ro" \
    -e URANUS_NGINX_E2E=1 -e URANUS_NGINX_E2E_WRITE=1 \
    -e WANT_SITES="$want_sites" -e WANT_INCLUDED="$want_included" \
    "$image" sh -c "($setup) >/tmp/setup.log 2>&1 || { tail -20 /tmp/setup.log; exit 1; }
      /uranus-test/e2e-$arch.test -test.run TestRealNginxLayout -test.v -test.timeout=3m" || status=$?
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
