# 阶段一：编译 Golang 程序
FROM golang:1.25.4-alpine AS builder

# 设置国内代理加速依赖下载
ENV GO111MODULE=on \
    GOPROXY=https://goproxy.cn,direct

WORKDIR /app
# 拷贝所有源码
COPY . .

# 禁用 CGO 进行静态编译，加上 -s -w 减小编译后的体积，确保在 alpine 中稳定运行
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o camkeep main.go

# 阶段二：构建最终运行环境
FROM alpine:latest

# 安装 ffmpeg 和时区数据 (非常重要，否则定时任务时间会乱)
RUN apk add --no-cache ffmpeg tzdata
# 设置你的默认时区
ENV TZ=Asia/Shanghai

WORKDIR /app

# 1. 从 builder 阶段拷贝编译好的二进制文件
COPY --from=builder /app/camkeep .

# 2. 【修复关键】拷贝前端静态资源和模板，否则 Gin 框架启动会 panic
COPY --from=builder /app/static ./static
COPY --from=builder /app/template ./template

# 暴露 Web 服务端口（仅作文档声明用，具体映射看 docker-compose）
EXPOSE 9110

# 3. 【修复关键】main.go 中硬编码了读取当前目录的 conf.yaml，无需额外参数
CMD ["./camkeep"]