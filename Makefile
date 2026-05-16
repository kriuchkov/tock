
linter:
	docker run -t --rm -v $$(pwd):/app -w /app \
	-v $$(go env GOCACHE):/.cache/go-build -e GOCACHE=/.cache/go-build \
	-v $$(go env GOMODCACHE):/.cache/mod -e GOMODCACHE=/.cache/mod \
	-v ~/.cache/golangci-lint:/.cache/golangci-lint -e GOLANGCI_LINT_CACHE=/.cache/golangci-lint \
	-e CGO_CFLAGS="-D_LARGEFILE64_SOURCE" \
	golangci/golangci-lint:v2.12.2-alpine golangci-lint run --fix --config .golangci.yaml --timeout 5m --concurrency 4

test:
	docker run -t --rm -v $$(pwd):/app -w /app \
	-v $$(go env GOCACHE):/.cache/go-build -e GOCACHE=/.cache/go-build \
	-v $$(go env GOMODCACHE):/.cache/mod -e GOMODCACHE=/.cache/mod \
	--entrypoint "" golang:1.26.3 sh -c "go test -v -count=1 -p 4 -coverprofile=coverage.out ./... && go tool cover -func=coverage.out && go tool cover -html=coverage.out -o coverage.html"

build:
	docker run -t --rm -v $$(pwd):/app -w /app \
	-v $$(go env GOCACHE):/.cache/go-build -e GOCACHE=/.cache/go-build \
	-v $$(go env GOMODCACHE):/.cache/mod -e GOMODCACHE=/.cache/mod \
	--entrypoint "" golang:1.26.3 sh -c "go build -o tock ./cmd/tock"

# Refresh test data by running the script that generates it. 
# By default, it refreshes data for the last 1 day, 
refresh-test-data:
	python3 scripts/refresh_test_data.py --days $(or $(DAYS),1)
