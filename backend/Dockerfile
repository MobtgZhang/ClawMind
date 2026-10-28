FROM golang:1.22-bookworm AS build
WORKDIR /src
# 构建期依赖源：proxy.golang.org 在部分网络下不可达，默认走 goproxy.cn 镜像，必要时用 --build-arg 覆盖。
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn
ENV GOPROXY=${GOPROXY} GOSUMDB=${GOSUMDB}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w" -o /out/server ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/server ./server
COPY config ./config
# 提示词模板：缺失时代码会回退到内置默认，但镜像里应带上仓库版本的那份。
COPY data/prompts ./data/prompts
ENV DB_PATH=/data/clawmind.db
# 设置页保存的模型/密钥等配置落在 /app/.clawmind，单独挂卷以便重建容器后不丢。
ENV CLAWMIND_DIR=/data/.clawmind
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/server"]
