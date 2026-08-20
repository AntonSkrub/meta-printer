# Meta-Printer Makefile
#
# Targets:
#   make build    – compile metad and metafilter into ./bin/
#   make test     – run all unit tests
#   make install  – build + run install/cups/install.sh
#   make clean    – remove ./bin/

.PHONY: all build test install clean

GOFLAGS  ?=
BIN_DIR   = bin
METAD     = $(BIN_DIR)/metad
METAFILTER = $(BIN_DIR)/metafilter

all: build

build: $(METAD) $(METAFILTER)

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(METAD): $(BIN_DIR) $(shell find cmd/metad pkg -name '*.go')
	go build $(GOFLAGS) -o $@ ./cmd/metad

$(METAFILTER): $(BIN_DIR) $(shell find cmd/metafilter pkg -name '*.go')
	go build $(GOFLAGS) -o $@ ./cmd/metafilter

test:
	go test $(GOFLAGS) ./...

install: build
	bash install/cups/install.sh

clean:
	rm -rf $(BIN_DIR)
