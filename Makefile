.PHONY: build build-windows clean test run daemon sync status help

# Detect OS
ifeq ($(OS),Windows_NT)
    BINARY_NAME := time-tracker-bot.exe
    YC_PATH := yc
    RM := del /Q
    MKDIR := mkdir
else
    BINARY_NAME := time-tracker-bot
    YC_PATH := yc
    RM := rm -f
    MKDIR := mkdir -p
endif

# Build the binary
build:
	go build -o $(BINARY_NAME) ./cmd/time-tracker-bot

# Build for Windows specifically
build-windows:
	GOOS=windows GOARCH=amd64 go build -o time-tracker-bot.exe ./cmd/time-tracker-bot

# Build for Linux specifically
build-linux:
	GOOS=linux GOARCH=amd64 go build -o time-tracker-bot ./cmd/time-tracker-bot

# Build for production with optimizations
build-prod:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/time-tracker-bot

# Clean build artifacts
clean:
	rm -f time-tracker-bot time-tracker-bot.exe
	rm -rf dist/

# Run tests
test:
	go test -v ./internal/... ./pkg/...

# Run with coverage
test-coverage:
	go test -coverprofile=coverage.out ./internal/... ./pkg/...
	go tool cover -html=coverage.out

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Run daemon mode
daemon:
	./$(BINARY_NAME) daemon --config config.yaml

# Sync time for today
sync:
	./$(BINARY_NAME) sync --date today

# Sync with dry-run
sync-dry:
	./$(BINARY_NAME) sync --dry-run --date today

# Show status
status:
	./$(BINARY_NAME) status

# Show weekly schedule
weekly:
	./$(BINARY_NAME) weekly-schedule

# Install dependencies
deps:
	go mod download
	go mod tidy

# Setup project (first time)
setup:
	cp config.yaml.example config.yaml
	mkdir -p data state logs
	touch logs/.gitkeep
	@echo "✅ Project setup complete"
	@echo "📝 Edit config.yaml with your settings"
	@echo "🔑 Run 'yc init' to authenticate"

# Check if yc CLI is installed
check-yc:
ifeq ($(OS),Windows_NT)
	@$(YC_PATH) version > nul 2>&1 || (echo ❌ yc CLI not installed && exit 1)
	@$(YC_PATH) config list > nul 2>&1 || (echo ❌ yc CLI not authenticated. Run: $(YC_PATH) init && exit 1)
	@echo ✅ yc CLI is installed and authenticated
else
	@which yc > /dev/null || (echo "❌ yc CLI not installed. Install: https://cloud.yandex.com/en/docs/cli/quickstart" && exit 1)
	@yc config list > /dev/null 2>&1 || (echo "❌ yc CLI not authenticated. Run: yc init" && exit 1)
	@echo "✅ yc CLI is installed and authenticated"
endif

# Verify IAM token
check-token:
ifeq ($(OS),Windows_NT)
	@$(YC_PATH) iam create-token > nul 2>&1 && echo ✅ IAM token OK || echo ❌ Failed to get IAM token
else
	@yc iam create-token > /dev/null && echo "✅ IAM token OK" || echo "❌ Failed to get IAM token"
endif

# Help
help:
	@echo "Available targets:"
	@echo "  build         - Build the binary"
	@echo "  build-prod    - Build with optimizations"
	@echo "  clean         - Remove build artifacts"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  fmt           - Format code"
	@echo "  lint          - Lint code"
	@echo "  daemon        - Run in daemon mode"
	@echo "  sync          - Sync time for today"
	@echo "  sync-dry      - Dry-run sync"
	@echo "  status        - Show current status"
	@echo "  weekly        - Show weekly schedule"
	@echo "  deps          - Install dependencies"
	@echo "  setup         - Setup project (first time)"
	@echo "  check-yc      - Check yc CLI installation"
	@echo "  check-token   - Verify IAM token"
	@echo "  help          - Show this help"
