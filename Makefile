SHELL := /bin/bash
PORT ?= 8781
BASE_URL := http://127.0.0.1:$(PORT)

.PHONY: build serve status test vet smoke clean

## build — compile the standalone bridge binary
build:
	go build -o wraith-bridge ./cmd/wraith-bridge/

## serve — build and start the bridge
serve: build
	./wraith-bridge

## status — check WRAITH runtime status
status:
	@curl -sf $(BASE_URL)/wraith/status | python3 -m json.tool 2>/dev/null || \
		echo "Bridge not reachable at $(BASE_URL). Is it running?"

## test — run all Go tests
test:
	go test ./internal/... -v -count=1

## vet — run go vet and gofmt check
vet:
	go vet ./...
	@UNFORMATTED=$$(gofmt -l internal/ cmd/); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "gofmt violations:"; echo "$$UNFORMATTED"; exit 1; \
	fi

## smoke — run fixture-based smoke test (offline, no server needed)
smoke:
	./scripts/smoke.sh

## clean — remove build artifacts
clean:
	rm -f wraith-bridge
