.PHONY: build build-release clean test fmt vet lint lint-fix run-help test-cover \
       sync-benchmark-offline-snapshot \
       bench-readme bench-weak bench-weak-test bench-weak-validate \
       bench-strong bench-strong-test bench-strong-validate

GOLANGCI_LINT ?= golangci-lint
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS  = -s -w -X lazy-tool/internal/version.Version=$(VERSION) -X lazy-tool/internal/version.Commit=$(COMMIT)

build:
	go build -o bin/lazy-tool ./cmd/lazy-tool

build-release:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/lazy-tool ./cmd/lazy-tool

clean:
	rm -rf bin/ coverage.out coverage.html

test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out -o coverage.html

fmt:
	go fmt ./...

vet:
	go vet ./...

# Requires golangci-lint v2+ on PATH:
#   go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
lint: vet
	$(GOLANGCI_LINT) run ./...

lint-fix: vet
	$(GOLANGCI_LINT) run --fix ./...

run-help:
	go run ./cmd/lazy-tool --help

# After editing benchmark/golden/sample_benchmark_rows.jsonl, refresh the JSON array mirror for PR-friendly diffs:
sync-benchmark-offline-snapshot:
	python3 benchmark/scripts/sync_offline_benchmark_snapshot.py

# ── Benchmark targets ──────────────────────────────────────────────────
# Python deps are auto-installed into benchmark/.venv on first run.

BENCH_PYTHON = . benchmark/scripts/ensure-python-deps.sh "$$(pwd)" && "$$PYTHON"

bench-readme:
	./benchmark/run_readme_benchmark_suite.sh

bench-weak-test:
	@bash -c '$(BENCH_PYTHON) benchmark/scripts/test_weak_model_harness.py -v'

bench-weak-validate:
	@bash -c '$(BENCH_PYTHON) benchmark/scripts/validate_weak_model_jsonl.py benchmark/golden/weak_model_sample_rows.jsonl'
	@bash -c '$(BENCH_PYTHON) benchmark/scripts/check_weak_model_golden_invariants.py benchmark/golden/weak_model_sample_rows.jsonl'

bench-weak:
	./benchmark/run_weak_model_suite.sh

bench-strong:
	./benchmark/run_strong_model_suite.sh

bench-strong-test:
	@bash -c '$(BENCH_PYTHON) benchmark/scripts/test_multi_provider_harness.py -v'

bench-strong-validate:
	@bash -c '$(BENCH_PYTHON) benchmark/scripts/validate_multi_provider_jsonl.py benchmark/golden/multi_provider_sample_rows.jsonl'
