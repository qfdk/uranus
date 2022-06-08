FROM golang:1.17-alpine as builder
# Set destination for COPY
WORKDIR /app
COPY . .
RUN apk update && apk add git musl-dev gcc make
RUN make

# 运行阶段指定scratch作为基础镜像
FROM alpine
WORKDIR /app
RUN apk update && apk add nginx bash
RUN mkdir -p /etc/nginx/sites-enabled
RUN nginx
# 将上一个阶段publish文件夹下的所有文件复制进来
COPY --from=builder /app/uranus .
# 指定运行时环境变量
ENV GIN_MODE release
EXPOSE 7777
CMD [ "/app/uranus" ]