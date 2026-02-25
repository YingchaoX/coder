.PHONY: build test lint coverage clean all fmt vet

BINARY = bin/agent
GOFLAGS = -v

# 默认目标 / Default target
all: lint test build

# 构建 / Build
build:
	go build $(GOFLAGS) -o $(BINARY) ./cmd/agent

# 快速测试 / Quick test
test:
	go test ./... -count=1

# 带竞态检测的测试 / Test with race detection
test-race:
	go test -race ./... -count=1

# 覆盖率报告 / Coverage report
coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo ""
	@echo "Coverage report: coverage.out"
	@echo "View HTML: go tool cover -html=coverage.out"

# 格式化检查 / Format check
fmt:
	@files=$$(gofmt -l .); \
	if [ -n "$$files" ]; then \
		echo "Files not gofmt-formatted:"; \
		echo "$$files"; \
		exit 1; \
	fi
	@echo "gofmt OK"

# 静态分析 / Static analysis
vet:
	go vet ./...

# Lint (fmt + vet)
lint: fmt vet

# 清理 / Clean
clean:
	rm -rf bin/ coverage.out

# 跨平台构建（CGO_ENABLED=0 避免依赖目标机 glibc 版本，兼容旧 Linux）
# Cross-platform build (CGO_ENABLED=0 for glibc-independent binary on older Linux)
build-all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o bin/coder-linux-amd64 ./cmd/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o bin/coder-linux-arm64 ./cmd/agent
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o bin/coder-darwin-amd64 ./cmd/agent
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o bin/coder-darwin-arm64 ./cmd/agent
