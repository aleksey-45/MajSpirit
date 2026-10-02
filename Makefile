.PHONY: help build run test test-cover vet fmt lint tidy clean

BINARY := bin/majspirit
PKGS   := ./...

help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## 编译服务端到 bin/
	go build -trimpath -o $(BINARY) ./cmd/server

run: ## 本地启动服务端（默认 :8080）
	go run ./cmd/server

test: ## 运行全部测试
	go test $(PKGS)

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
