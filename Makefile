.PHONY: help build run test test-db test-cover vet fmt lint tidy clean

BINARY := bin/majspirit
PKGS   := ./...

help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## 编译服务端到 bin/
	go build -trimpath -o $(BINARY) ./cmd/server

run: ## 本地启动服务端（读 .env）
	@test -f .env || { echo "没有 .env —— 先照 .env.example 建一个"; exit 1; }
	set -a; . ./.env; set +a; go run ./cmd/server


test: ## 运行全部测试（需要数据库的用例会自动跳过）
	go test $(PKGS)

test-db: ## 连上真库跑全部测试（读 .env；-count=1 绕过测试缓存）
	@test -f .env || { echo "没有 .env —— 先照 .env.example 建一个"; exit 1; }
	set -a; . ./.env; set +a; go test -count=1 $(PKGS)

test-cover: ## 运行测试并输出覆盖率
	go test -cover $(PKGS)

vet: ## 静态检查
	go vet $(PKGS)

fmt: ## 格式化全部 Go 代码
	gofmt -l -w .

lint: ## 运行 staticcheck（需先装：go install honnef.co/go/tools/cmd/staticcheck@latest）
	staticcheck $(PKGS)

tidy: ## 整理 go.mod 依赖
	go mod tidy

clean: ## 清理编译产物
	rm -rf bin
