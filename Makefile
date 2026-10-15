# Builds, installs, and tests tack, the Tack compiler.

# BIN is where `make build` writes the compiler binary.
BIN := bin/tack

.PHONY: build install install-std test fmt lint

# build compiles the compiler into $(BIN).
build:
	go build -o $(BIN) ./tack

# install compiles the compiler and puts it in Go's install directory: $GOBIN
# if set, otherwise $GOPATH/bin. That directory needs to be on PATH for
# `tack` to be runnable by name. It installs the standard library too, which
# the compiler reads its builtins from.
install: install-std
	go install ./tack

# install-std copies the standard library, src/std, to $TACK_HOME/src/std, or
# to ~/.tack/src/std when TACK_HOME is unset or empty, where the compiler
# looks for it. What is there is removed first, so a file deleted from
# src/std does not linger in the installed copy.
install-std:
	@home="$${TACK_HOME:-}"; \
	if [ -z "$$home" ]; then \
		if [ -z "$${HOME:-}" ]; then echo "install-std: neither TACK_HOME nor HOME is set" >&2; exit 1; fi; \
		home="$$HOME/.tack"; \
	fi; \
	echo "installing std to $$home/src/std"; \
	rm -rf "$$home/src/std" && mkdir -p "$$home/src" && cp -R src/std "$$home/src/std"

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
