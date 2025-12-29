.PHONY: build-frontend build-backend build all clean

build-frontend:
	@echo "Building frontend..."
	@if [ -d "webui" ]; then \
		cd webui && npm install && npm run build; \
		if [ -d "dist" ]; then \
			mkdir -p ../web/dist && cp -r dist/* ../web/dist/; \
		fi \
	else \
		echo "Frontend directory not found, skipping..."; \
	fi

build-backend:
	@echo "Building backend..."
	@VERSION=$$(git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo "3.3.2"); \
	echo "Version: $$VERSION"; \
	go build -ldflags="-s -w -X main.Version=$$VERSION" -o quantmesh .

build: build-frontend build-backend

all: build

clean:
	@rm -rf quantmesh webui/dist

