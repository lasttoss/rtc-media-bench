SHELL := /usr/bin/env bash
WS_PORT ?= 17880
MUX_PORT ?= 17882

.PHONY: help
help: ## List the targets
	@grep -E '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: test
test: ## go test -race (no cluster, no network, no credentials)
	go test -race ./internal/...

.PHONY: test-integration
test-integration: build ## Unit tests plus two real participants through the vendored SFU
	@RTC_BENCH_SFU=$(PWD)/bin/sfu go test -race -cover -run . ./internal/rtc/ -v 2>&1 | grep -E "^(ok|FAIL|---|    client)" | sed 's/^/  /'

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

.PHONY: demo
demo: build chart-check verify-sfu ## One command, no cluster and no credentials: build, validate, measure
	@echo
	@echo "== measuring on loopback: every interface advertised vs one =="
	@SFU_BIN=$(PWD)/bin/sfu BENCH_BIN=$(PWD)/bin/bench ./lab/measure.sh mux-advertised 3 6s -udp-mux-port $(MUX_PORT) -advertise-ip 127.0.0.1
	@echo "  (the chart and the vendor patch were checked above; lab-up brings up the cluster)"

.PHONY: chart-check
chart-check: ## Lint and render every branch of the chart, then validate it
	@./lab/chart-check.sh

.PHONY: diagram
diagram: ## Regenerate the figure and fail if the export is older than its sources
	@python3 docs/figures/rtc-media-bench-flow.generator.py
	@bash ~/.pi/agent/scripts/diagram-freshness.sh 2>/dev/null | grep -E "FRESH|STALE|TOUCHED|NO-META" | sed 's/^/  /'

.PHONY: lab-up
lab-up: build ## kind cluster + Calico + images + chart
	@./lab/lab-up.sh

.PHONY: lab-down
lab-down: ## Delete the kind cluster
	@kind delete cluster --name rtc-bench

.PHONY: lab-bench
lab-bench: ## Measure against the cluster
	@./lab/run-cluster.sh cluster-baseline

.PHONY: fault1 fault2 fault3 fault4
fault1: ## Fault 1: egress policy does not let the pod answer
	@./lab/faults/01-egress-networkpolicy.sh
fault2: ## Fault 2: media exposed through a Service
	@./lab/faults/02-media-through-a-service.sh
fault3: ## Fault 3: the SFU advertises the pod's own address
	@./lab/faults/03-advertise-pod-ip.sh
fault4: ## Fault 4: pod deleted, with and without a grace period
	@./lab/faults/04-restart-without-grace.sh

.PHONY: cpu
cpu: ## SFU CPU per participant, from the pod's cgroup
	@./lab/measure-sfu-cpu.sh 3 15s

.PHONY: sample
sample: ## Regenerate testdata/sample.ivf (needs ffmpeg)
	@ffmpeg -v error -y -f lavfi -i "testsrc2=size=160x120:rate=15:duration=1" -c:v libvpx -b:v 120k -an -f ivf testdata/sample.ivf
	@ls -l testdata/sample.ivf

.PHONY: fmt
fmt: ## gofmt
	@gofmt -l . | grep -v '^lab/sfu' | tee /dev/stderr | wc -l | grep -q '^0$$' && echo "gofmt clean"

.PHONY: vet
vet: ## go vet
	@go vet ./...
