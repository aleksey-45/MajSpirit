.PHONY: help build run test test-db test-cover vet fmt lint tidy clean

BINARY := bin/majspirit
PKGS   := ./...

# ★ 必须 -p 1（包串行），不能只写 go test ./...
#
# 原因：internal/store 的用例会 DROP SCHEMA，而 cmd/server 的集成测试也在
# **同一个库**上跑迁移。两个包并行时，store 的 clean() 会插在别的包的两条
# 迁移之间 —— 实测报「应用 0002_drop_users_updated_at.sql: 关系 users 不存在」。
# 只有一条迁移时这个窗口看起来无害（重建一遍就是了），所以这个坑是加第二条
# 迁移时才露出来的。
#
# 这是测试夹具的性质，不是生产代码的问题：生产里两个实例抢的是同一把
# advisory lock，那套是对的（见 internal/store/migrate.go）。
GOTEST := go test -p 1

help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## 编译服务端到 bin/
	go build -trimpath -o $(BINARY) ./cmd/server

run: ## 本地启动服务端（读 .env）
	@test -f .env || { echo "没有 .env —— 先照 .env.example 建一个"; exit 1; }
	set -a; . ./.env; set +a; go run ./cmd/server


test: ## 运行全部测试（需要数据库的用例会自动跳过）
	$(GOTEST) $(PKGS)

test-db: ## 连上真库跑全部测试（读 .env；-count=1 绕过测试缓存）
	@test -f .env || { echo "没有 .env —— 先照 .env.example 建一个"; exit 1; }
	set -a; . ./.env; set +a; $(GOTEST) -count=1 $(PKGS)

test-cover: ## 运行测试并输出覆盖率
	$(GOTEST) -cover $(PKGS)

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
