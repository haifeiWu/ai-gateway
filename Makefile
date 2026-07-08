.PHONY: build test test-e2e lint clean run run-gateway run-mock run-mock-bg

build:
	go build ./...

test:
	go test ./...

test-e2e:
	go test -tags e2e -v -timeout 120s ./test/e2e/

lint:
	golangci-lint run

run-gateway:
	go run ./cmd/gateway

run-mock:
	go run ./cmd/mock-provider

run-mock-bg:
	go run ./cmd/mock-provider &

clean:
	rm -f gateway mock-provider

docker-up:
	docker compose up -d

docker-down:
	docker compose down
