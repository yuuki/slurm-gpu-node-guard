.PHONY: all build test vet lint clean install

BINARIES := guardd guardctl
PLUGINS  := guard-plugin-gpu guard-plugin-gpu-errors guard-plugin-rdma guard-plugin-filesystem guard-plugin-service
ALL_TARGETS := $(BINARIES) $(PLUGINS)

PREFIX       ?= /usr/local
BINDIR       ?= $(PREFIX)/bin
LIBEXECDIR   ?= $(PREFIX)/libexec/slurm-gpu-node-guard

all: build

build: $(ALL_TARGETS)

$(ALL_TARGETS):
	go build -o $@ ./cmd/$@

test:
	go test ./...

vet:
	go vet ./...

lint: vet

clean:
	rm -f $(ALL_TARGETS)

install: build
	install -d $(DESTDIR)$(BINDIR) $(DESTDIR)$(LIBEXECDIR)
	install -m 755 $(BINARIES) $(DESTDIR)$(BINDIR)/
	install -m 755 $(PLUGINS) $(DESTDIR)$(LIBEXECDIR)/
