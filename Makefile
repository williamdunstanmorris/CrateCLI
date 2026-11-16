BIN     := bin/cratecli
# Plain version until cratecli has its own git repo; then use
# $(shell git describe --tags --always --dirty).
VERSION ?= 0.1.0
PREFIX  ?= /usr/local
LDFLAGS := -s -w -X github.com/williamdunstanmorris/cratecli/internal/cli.Version=$(VERSION)

.PHONY: build install uninstall install-go test vet fmt tidy clean docker up down

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/cratecli

# install puts cratecli (and a short `crate` alias) in $(PREFIX)/bin, which
# is on every macOS user's PATH via /etc/paths. If that folder isn't
# writable, run: sudo make install
install: build
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/cratecli
	ln -sf cratecli $(PREFIX)/bin/crate
	@echo "installed $(PREFIX)/bin/cratecli ($(VERSION)) and $(PREFIX)/bin/crate"

uninstall:
	rm -f $(PREFIX)/bin/cratecli $(PREFIX)/bin/crate

# install-go installs for the current user only, into $(go env GOPATH)/bin.
install-go:
	go install -ldflags "$(LDFLAGS)" ./cmd/cratecli

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -rf bin

# ---- containers (local development) ----
docker:
	docker build --build-arg VERSION=$(VERSION) -t cratecli:$(VERSION) .

up:
	docker compose up --build -d
	@echo "API: http://localhost:8080  ·  Mailpit inbox: http://localhost:8025"

down:
	docker compose down
