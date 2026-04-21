GO      := go
GOFLAGS := -v

VERSION := $(shell cat ../VERSION 2>/dev/null || echo "dev")
COMMIT  := $(shell git describe --dirty=+WiP --always 2>/dev/null || echo "unknown")
APPDATE := $(shell date +"%Y-%m-%d-%H:%M")
LDFLAGS := -X main.version=$(VERSION)-$(COMMIT) -X main.buildTime=$(APPDATE)

BINARIES := chimporter fetch-netnod-uni

# fetch-netnod-any is parked: the v1 Anycast API is being retired in favour
# of the v2 Unicast API, which returns a superset of sites. The binary is
# still buildable on demand with `make fetch-netnod-any`.
PARKED := fetch-netnod-any

.PHONY: all clean test lint help $(BINARIES) $(PARKED)

all: $(BINARIES)

chimporter:
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/chimporter

fetch-netnod-any:
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/fetch-netnod-any

fetch-netnod-uni:
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/fetch-netnod-uni

clean:
	rm -f $(BINARIES) $(PARKED)

test:
	$(GO) test -v -cover ./...

lint:
	$(GO) fmt ./...
	$(GO) vet ./...

help:
	@echo "Targets:"
	@echo "  all (default)     - build all binaries"
	@echo "  chimporter        - build chimporter only"
	@echo "  fetch-netnod-uni  - build fetch-netnod-uni only"
	@echo "  fetch-netnod-any  - build fetch-netnod-any (parked; not in 'all')"
	@echo "  clean             - remove built binaries"
	@echo "  test              - run tests"
	@echo "  lint              - go fmt + go vet"
