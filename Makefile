# Builds, installs, and tests tack, the Tack compiler.

# BIN is where `make build` writes the compiler binary.
BIN := bin/tack

.PHONY: build install test fmt lint

# build compiles the compiler into $(BIN).
build:
	go build -o $(BIN) ./tack

# install compiles the compiler and puts it in Go's install directory: $GOBIN
# if set, otherwise $GOPATH/bin. That directory needs to be on PATH for
# `tack` to be runnable by name.
install:
	go install ./tack

# test runs every test in the module, including the golden tests and the tests
# that compile and run generated C, which need clang on PATH.
test:
	go test -race ./...

# fmt rewrites Go sources in place with the formatters enabled in
# .golangci.yml.
fmt:
	golangci-lint fmt

# lint runs the linters enabled in .golangci.yml.
lint:
	golangci-lint run
