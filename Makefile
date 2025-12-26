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
	@go build -o opensqt .

build: build-frontend build-backend

all: build

clean:
	@rm -rf opensqt webui/dist

