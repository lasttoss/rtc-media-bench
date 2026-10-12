SHELL := /usr/bin/env bash
WS_PORT ?= 17880
MUX_PORT ?= 17882

.PHONY: help
help: ## List the targets
	@grep -E '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: test
test: ## go test -race (no cluster, no network, no credentials)
	go test -race ./internal/...

.PHONY: coverage
coverage: ## Print per-package coverage
	@go test -cover ./internal/... | sed 's/^/  /'
	@go test -coverprofile=/tmp/rtc-cover.out ./internal/... >/dev/null 2>&1 || true
	@go tool cover -func=/tmp/rtc-cover.out 2>/dev/null | tail -1 | sed 's/^/  total: /'

.PHONY: build
build: ## Build the harness and the SFU
	go build -o bin/bench ./cmd/bench
	go build -o bin/sfu ./lab/sfu

.PHONY: bench
bench: build ## Join 3 participants and print the table
	@./bin/bench -clients 3 -duration 10s

.PHONY: sfu
sfu: build ## Run the SFU locally on one multiplexed UDP port
	@cd lab/sfu && ../bin/sfu -addr :$(WS_PORT) -udp-mux-port $(MUX_PORT) -advertise-ip 127.0.0.1

.PHONY: verify-sfu
verify-sfu: build ## Assert the vendored SFU's ICE flags reach the peer connection
	@SFU_BIN=$(PWD)/bin/sfu BENCH_BIN=$(PWD)/bin/bench ./lab/check-ice-config.sh

.PHONY: verify-patch
verify-patch: ## Rebuild lab/sfu from upstream at the pinned commit and check it matches
	@./lab/verify-patch.sh

.PHONY: measure
measure: build ## Run the ICE comparison matrix into lab/evidence/
	@SFU_BIN=$(PWD)/bin/sfu BENCH_BIN=$(PWD)/bin/bench ./lab/measure.sh mux-all-interfaces 3 10s -udp-mux-port $(MUX_PORT)
	@SFU_BIN=$(PWD)/bin/sfu BENCH_BIN=$(PWD)/bin/bench ./lab/measure.sh mux-advertised 3 10s -udp-mux-port $(MUX_PORT) -advertise-ip 127.0.0.1

.PHONY: fmt
fmt: ## gofmt
	@gofmt -l . | grep -v '^lab/sfu' | tee /dev/stderr | wc -l | grep -q '^0$$' && echo "gofmt clean"

.PHONY: vet
vet: ## go vet
	@go vet ./...
