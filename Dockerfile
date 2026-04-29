# --- 阶段零：根据架构动态下载 go2rtc ---
FROM alpine:latest AS go2rtc-downloader

# 声明引入 Docker 内置的架构变量
ARG TARGETARCH

# 接收从外部传入的版本号参数
ARG VERSION="dev"

# go2rtc 版本
ARG Go2rtcVersion="v1.9.14"

RUN apk add --no-cache wget

# 使用 ${TARGETARCH} 动态拼接下载链接
# Docker 会自动将 TARGETARCH 替换为 amd64, arm64 或 arm
RUN echo "正在为 linux_${TARGETARCH} 下载 go2rtc..." && \
    wget -O /go2rtc https://github.com/AlexxIT/go2rtc/releases/download/${Go2rtcVersion}/go2rtc_linux_${TARGETARCH} && \
    chmod +x /go2rtc

# --- 阶段一：编译 CamKeep ---
# 指定平台，利用交叉编译加快构建速度（可选但推荐）
FROM --platform=$BUILDPLATFORM golang:1.25.4-alpine AS builder

# 声明架构变量给 Go 编译器使用
ARG TARGETARCH

ENV GO111MODULE=on \
    GOPROXY=https://goproxy.cn,direct

WORKDIR /app
COPY . .

# 加入 GOARCH=${TARGETARCH} 让 Go 编译器知道目标架构
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-s -w -X main.Version=${VERSION}" -o camkeep main.go

# --- 阶段二：构建最终运行环境 ---
FROM alpine:latest
RUN apk add --no-cache ffmpeg tzdata
ENV TZ=Asia/Shanghai
WORKDIR /app

# 从前面的阶段拷贝文件
COPY --from=builder /app/camkeep .
COPY --from=builder /app/static ./static
COPY --from=builder /app/template ./template
COPY --from=go2rtc-downloader /go2rtc ./go2rtc

EXPOSE 9110 1984 8554 8555
CMD ["./camkeep"]