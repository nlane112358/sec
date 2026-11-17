GO ?= go
BINARY ?= ./sec

.DEFAULT_GOAL := help
.PHONY: help build install run clean

help:
	@printf '%s\n' \
	  'Targets:' \
	  '  make build                 Build the sec CLI in the repository root' \
	  '  make install               Install the sec CLI with go install' \
	  '  make run ARGS="summary AAPL" Run a CLI command' \
	  '  make clean                 Remove the built CLI' \
	  '  make help                  Show this help'

build:
	$(GO) build -o "$(BINARY)" ./cmd/sec

install:
	$(GO) install ./cmd/sec

run: build
	"$(BINARY)" $(ARGS)

clean:
	rm -f "$(BINARY)"
