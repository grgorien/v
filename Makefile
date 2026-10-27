BIN := v
PREFIX ?= /usr/local
GO ?= go

.PHONY: build vet test test-int install clean

build:
	$(GO) build -o $(BIN) .

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-int:
	$(GO) test -tags integration ./...

install: 
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 0755 $(BIN) $(DESTDIR)$(PREFIX)/bin/$(BIN)

clean:
	rm -f $(BIN)
