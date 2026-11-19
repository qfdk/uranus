FROM golang:1.27-alpine AS builder
# Set destination for COPY
WORKDIR /app
COPY . .
RUN apk update && apk add git musl-dev gcc make build-base
# 设置CGO环境变量解决SQLite编译问题
ENV CGO_ENABLED=1
ENV GOOS=linux
ENV CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
RUN make

# 运行阶段指定alpine作为基础镜像
FROM alpine
WORKDIR /app
# 安装必要的运行时依赖：nginx、bash、ca-certificates
RUN apk update && apk add nginx bash ca-certificates
# 网站配置放在 Alpine 的 nginx 在 http 块里引用的 /etc/nginx/http.d，证书目录在首次签发时创建
RUN mkdir -p /var/log/nginx
RUN mkdir -p /var/lib/nginx/tmp
# 不在构建时启动nginx，改为运行时启动
# 将上一个阶段构建的二进制文件复制进来
COPY --from=builder /app/uranus .
# 创建示例配置文件
RUN echo 'controlcenter = "http://localhost:3000"\nemail = "admin@example.com"\nip = "127.0.0.1"\nmqttbroker = ""\npassword = "change-me-strong-password"\ntoken = "yourtoken"\nusername = "admin"\nuuid = "generate-new-uuid"' > config.toml.example
# 指定运行时环境变量
ENV GIN_MODE=release
EXPOSE 7777
# 启动nginx和uranus
CMD ["sh", "-c", "nginx && /app/uranus"]