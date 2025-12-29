.PHONY: build-frontend build-backend build all clean

build-frontend:
	@echo "Building frontend..."
	@if [ -d "webui" ]; then \
		cd webui && npm install && npm run build; \
	else \
		echo "Frontend directory not found, skipping..."; \
	fi

build-backend:
	@echo "Building backend..."
	@VERSION=$$(git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo "3.3.2"); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo "unknown"); \
	echo "Version: $$VERSION"; \
	echo "Commit: $$COMMIT"; \
	go build -ldflags="-s -w -X main.Version=$$VERSION -X main.BuildCommit=$$COMMIT" -o quantmesh .

build: build-frontend build-backend

all: build

clean:
	@rm -rf quantmesh webui/dist

