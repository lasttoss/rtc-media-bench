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
	# pipefail, because `go test | grep | sed` reports sed's exit status: a failing test joined a
	# pipeline whose last command succeeded, and this target printed a red test as a green run.
	@set -o pipefail; RTC_BENCH_SFU=$(PWD)/bin/sfu go test -race -cover -run . ./internal/rtc/ -v 2>&1 | grep -E "^(ok|FAIL|---|    client)" | sed 's/^/  /'

.PHONY: coverage
coverage: ## Print per-package coverage
	@set -o pipefail; go test -cover ./internal/... | sed 's/^/  /'
	@set -o pipefail; go test -coverprofile=/tmp/rtc-cover.out ./internal/... >/dev/null
	@set -o pipefail; go tool cover -func=/tmp/rtc-cover.out | tail -1 | sed 's/^/  total: /'

.PHONY: build
build: ## Build the harness and the SFU
	go build -o bin/bench ./cmd/bench
	go build -o bin/sfu ./lab/sfu

.PHONY: bench
bench: build ## Join 3 participants and print the table (host, no container)
	# A participant needs something to join: this target used to dial ws://127.0.0.1:17880 with
	# nothing listening, and whatever else held that port answered the WebSocket upgrade with a plain
	# HTTP response. So the SFU starts here, the target waits for its signalling port, and it is
	# stopped on the way out through a pid file - a pattern match at cleanup time also matches the
	# shell running it.
	@rm -f /tmp/rtc-bench-sfu.pid
	@cd lab/sfu && ( exec $(PWD)/bin/sfu -addr :$(WS_PORT) -udp-mux-port $(MUX_PORT) -advertise-ip 127.0.0.1 >/tmp/rtc-bench-sfu.log 2>&1 </dev/null & echo $$! > /tmp/rtc-bench-sfu.pid )
	@for i in $$(seq 1 40); do (exec 3<>/dev/tcp/127.0.0.1/$(WS_PORT)) 2>/dev/null && break || sleep 0.25; done; \
	timeout 120 ./bin/bench -url ws://127.0.0.1:$(WS_PORT)/websocket -clients 3 -duration 10s; rc=$$?; \
	kill "$$(cat /tmp/rtc-bench-sfu.pid)" 2>/dev/null; rm -f /tmp/rtc-bench-sfu.pid; exit $$rc

.PHONY: sfu
sfu: build ## Run the SFU on the host, in the foreground, on one multiplexed UDP port
	# $(PWD), not ../bin/sfu: the old target ran `cd lab/sfu && ../bin/sfu`, which is lab/bin/sfu and
	# has never existed. The harness and the SFU are both built into bin/.
	@cd lab/sfu && exec $(PWD)/bin/sfu -addr :$(WS_PORT) -udp-mux-port $(MUX_PORT) -advertise-ip 127.0.0.1

.PHONY: measure
measure: build ## Run the ICE comparison, into lab/evidence/
	# Each run starts its own SFU and refuses to report on one it did not start, so the two
	# configurations cannot borrow each other's server.
	@SFU_BIN=$(PWD)/bin/sfu BENCH_BIN=$(PWD)/bin/bench WS_PORT=$(WS_PORT) MUX_PORT=$(MUX_PORT) \
	  ./lab/measure.sh mux-all-interfaces 3 10s -udp-mux-port $(MUX_PORT)
	@SFU_BIN=$(PWD)/bin/sfu BENCH_BIN=$(PWD)/bin/bench WS_PORT=$(WS_PORT) MUX_PORT=$(MUX_PORT) \
	  ./lab/measure.sh mux-advertised 3 10s -udp-mux-port $(MUX_PORT) -advertise-ip 127.0.0.1

.PHONY: verify-sfu
verify-sfu: build ## Assert the vendored SFU's ICE flags reach the peer connection
	@SFU_BIN=$(PWD)/bin/sfu WS_PORT=$(WS_PORT) MUX_PORT=$(MUX_PORT) ./lab/check-ice-config.sh

.PHONY: verify-patch
verify-patch: ## Rebuild lab/sfu from upstream at the pinned commit and check it matches
	@./lab/verify-patch.sh

.PHONY: demo
demo: build chart-check verify-sfu ## One command, no cluster and no credentials: build, validate, measure
	@echo
	@echo "== measuring on loopback: every interface advertised vs one =="
	@SFU_BIN=$(PWD)/bin/sfu BENCH_BIN=$(PWD)/bin/bench WS_PORT=$(WS_PORT) MUX_PORT=$(MUX_PORT) \
	  ./lab/measure.sh mux-advertised 3 6s -udp-mux-port $(MUX_PORT) -advertise-ip 127.0.0.1
	@echo "  (the chart and the vendored patch were checked above; lab-up brings up the cluster)"

COMPOSE ?= docker compose
IMAGE_NAME ?= rtc-media-bench:local

.PHONY: up
up: ## Start the SFU in a container, on the host's network, and wait until it answers
	@$(COMPOSE) up -d --build >/dev/null
	@ok=0; for i in $$(seq 1 40); do \
	  if curl -fsS http://127.0.0.1:$(WS_PORT)/ >/dev/null 2>&1; then ok=1; break; fi; \
	  if [ "$$($(COMPOSE) ps -q sfu | xargs -r docker inspect -f '{{.State.Running}}' 2>/dev/null)" != "true" ]; then break; fi; \
	  sleep 0.5; \
	done; \
	if [ $$ok -eq 1 ]; then echo "  the SFU answers on :$(WS_PORT)"; else \
	  echo "  the SFU did not come up:"; $(COMPOSE) logs sfu 2>&1 | tail -6 | sed 's/^/    /'; $(COMPOSE) down --remove-orphans >/dev/null 2>&1; exit 1; fi

.PHONY: down
down: ## Stop the containers and remove them
	@$(COMPOSE) down --remove-orphans

.PHONY: logs
logs: ## Follow the container logs
	@$(COMPOSE) logs -f

.PHONY: compose-bench
compose-bench: up ## Measure through the containerised SFU, then stop it
	@$(COMPOSE) --profile bench run --rm bench 2>&1 | tee /tmp/compose-bench.txt | sed 's/^/  /'
	@$(COMPOSE) down --remove-orphans
	@grep -qE "join p95|p95" /tmp/compose-bench.txt || { echo "  the containerised harness printed no measurement"; exit 1; }

.PHONY: image
image: ## Build the image, then assert it runs and carries no shell
	@docker build -q -t $(IMAGE_NAME) . >/dev/null && echo "  built $(IMAGE_NAME)"
	@set +e; out=$$(docker run --rm $(IMAGE_NAME) -help 2>&1); code=$$?; set -e; \
	  printf '%s\n' "$$out" | tail -4 | sed 's/^/  /'; \
	  printf '%s\n' "$$out" | grep -q -- "-clients" || { echo "  the harness did not print its own usage"; exit 1; }; \
	  echo "  the harness binary ran (exit $$code, usage printed)"
	@if docker run --rm --entrypoint /bin/sh $(IMAGE_NAME) -c true >/dev/null 2>&1; then \
	  echo "  the image has /bin/sh: it is not distroless"; exit 1; \
	else echo "  no /bin/sh in the image: as intended"; fi
	@echo "== the SFU in the image serves its page (it is the half that needs an asset) =="
	@docker rm -f rtc-image-check >/dev/null 2>&1 || true
	@set +e; out=$$(docker run -d --name rtc-image-check --network host --entrypoint /usr/local/bin/sfu $(IMAGE_NAME) -addr :17890 -udp-mux-port 17892 -advertise-ip 127.0.0.1); set -e; \
	  ok=0; for i in $$(seq 1 30); do \
	    if curl -fsS http://127.0.0.1:17890/ 2>/dev/null | grep -qi "rtc\|bench\|websocket"; then ok=1; break; fi; \
	    if [ "$$(docker inspect -f '{{.State.Running}}' rtc-image-check 2>/dev/null)" != "true" ]; then break; fi; \
	    sleep 0.5; \
	  done; \
	  if [ $$ok -eq 1 ]; then echo "  the SFU served its page on :17890"; else echo "  the SFU did not serve its page"; docker logs rtc-image-check 2>&1 | tail -5 | sed 's/^/    /'; docker rm -f rtc-image-check >/dev/null 2>&1; exit 1; fi; \
	  docker rm -f rtc-image-check >/dev/null 2>&1 || true

.PHONY: chart-check
chart-check: ## Lint and render every branch of the chart, then validate it
	@./lab/chart-check.sh

.PHONY: diagram
diagram: ## Regenerate the figure, re-export it, and check it against the sources it names
	# The checker is in this repository, not on the machine of whoever drew the figure. It used to be
	# ~/.pi/agent/scripts/diagram-freshness.sh, piped through grep, which meant the gate the README
	# describes could not run in a clean clone - and a pipe would have hidden its exit code anyway.
	@python3 docs/figures/rtc-media-bench-flow.generator.py
	@python3 scripts/export-figure.py docs/figures/rtc-media-bench-flow.html
	@python3 scripts/diagram-freshness.py .

# Every command this README names has to resolve, and the ones that need no cluster are run. Entries
# are ';'-separated; a leading '!' means the command must fail. What cannot run here is printed with
# the reason rather than skipped in silence.
README_RUN ?= make diagram; make test; make demo; make measure
README_SKIP ?= make lab-up | a kind cluster, Calico and two images; make lab-bench | measures against that cluster; make up | the same two binaries as containers; make down | stops them; make compose-bench | measures through the containerised SFU; make image | builds the image and runs it; make test-integration | two real participants through the vendored SFU: the harness job does it

.PHONY: readme-check
readme-check: build ## Every command this README names resolves, and the offline ones are run
	@README_RUN='$(README_RUN)' README_SKIP='$(README_SKIP)' python3 scripts/readme-check.py .


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
