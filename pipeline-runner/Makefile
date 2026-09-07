.PHONY: build test lint clean run-local

build:
	go build -o bin/runner ./cmd/runner

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/runner-linux ./cmd/runner

test:
	go test -v ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	gofumpt -l -w .
	go vet ./...

clean:
	rm -rf bin/ coverage.out

run-local:
	EXECUTION_MODE=batch \
	WORKFLOW_ID=test-wf \
	EXECUTION_ID=test-exec \
	PIPELINES_CONFIG='[{"id":"p1","name":"test"}]' \
	CONTROL_PLANE_URL=http://localhost:8080 \
	go run ./cmd/runner
