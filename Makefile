# ClawMind — 常用构建与开发命令（在仓库根目录执行）

BACKEND_DIR  := backend
FRONTEND_DIR := frontend
BIN_DIR      := bin
SERVER_BIN   := $(BIN_DIR)/clawmind-server

.PHONY: help all deps backend-build frontend-build build clean test \
	run dev dev-backend dev-frontend fmt vet docker-up docker-down frontend-install

help:
	@echo "ClawMind Makefile"
	@echo ""
	@echo "  make deps            安装前端依赖（npm ci，首次或锁文件变更后）"
	@echo "  make frontend-install 安装前端依赖（npm install，无 lock 时可用）"
	@echo "  make backend-build   编译后端 -> $(SERVER_BIN)（纯 Go SQLite，无需 CGO）"
	@echo "  make frontend-build  构建前端 -> frontend/dist"
	@echo "  make build           deps + backend-build + frontend-build（发布用）"
	@echo "  make run             本机启动：并行后端 :8080 + 前端 :5173（同 make dev）"
	@echo "  make dev             同上"
	@echo "  make dev-backend     仅后端: go run ./cmd/server（工作目录 $(BACKEND_DIR)）"
	@echo "  make dev-frontend    仅前端: npm run dev"
	@echo "  make test            后端 go test ./..."
	@echo "  make vet             后端 go vet ./..."
	@echo "  make fmt             后端 gofmt（写入）"
	@echo "  make clean           删除 $(BIN_DIR)/ 与 frontend/dist/"
	@echo "  make docker-up       docker compose up --build -d"
	@echo "  make docker-down     docker compose down"

all: build

deps:
	cd $(FRONTEND_DIR) && npm ci

frontend-install:
	cd $(FRONTEND_DIR) && npm install

backend-build:
	mkdir -p $(BIN_DIR)
	cd $(BACKEND_DIR) && go build -o ../$(SERVER_BIN) ./cmd/server

frontend-build:
	cd $(FRONTEND_DIR) && npm run build

# 完整发布构建：先同步前端 lock 依赖，再编译两端
build: deps backend-build frontend-build

clean:
	rm -rf $(BIN_DIR) $(FRONTEND_DIR)/dist

test:
	cd $(BACKEND_DIR) && go test ./...

vet:
	cd $(BACKEND_DIR) && go vet ./...

fmt:
	cd $(BACKEND_DIR) && gofmt -w .

# 本机开发：后端 :8080，前端 :5173（输出会交错属正常现象）
run: dev

dev:
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	cd $(BACKEND_DIR) && go run ./cmd/server

dev-frontend:
	cd $(FRONTEND_DIR) && npm run dev

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down
