PROG := chimporter

GO      := go
GOFLAGS := -v
PREFIX  := /usr/local/bin

VERSION := $(shell cat ../VERSION 2>/dev/null || echo "dev")
COMMIT  := $(shell git describe --dirty=+WiP --always 2>/dev/null || echo "unknown")
APPDATE := $(shell date +"%Y-%m-%d-%H:%M")
LDFLAGS := -X main.version=$(VERSION)-$(COMMIT) -X main.buildTime=$(APPDATE)

.PHONY: all clean install test lint help

all: $(PROG)

$(PROG):
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ .

clean:
	rm -f $(PROG)

install: all
	install -b -c -s $(PROG) $(PREFIX)/

test:
	$(GO) test -v -cover ./...

lint:
	$(GO) fmt ./...
	$(GO) vet ./...

help:
	@echo "Targets:"
	@echo "  all (default)  - build $(PROG)"
	@echo "  install        - install to $(PREFIX)/"
	@echo "  test           - run tests"
	@echo "  clean          - remove built binary"
	@echo "  lint           - run go fmt and go vet"
	@echo "  help           - this message"
