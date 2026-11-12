# Builds, installs, and tests tack, the Tack compiler.

# BIN is where `make build` writes the compiler binary.
BIN := bin/tack

# BUILTIN_DOC and RUNTIME_HEADER are the files built into the compiler that
# std keeps copies of: the builtins' documentation and the runtime header.
BUILTIN_DOC := builtin/builtin.tck
RUNTIME_HEADER := builtin/runtime/tack/tack.h

.PHONY: build install install-std std-copies test fuzz-progen fmt lint

# build compiles the compiler into $(BIN).
build:
	go build -o $(BIN) ./tack

# install compiles the compiler and puts it in Go's install directory: $GOBIN
# if set, otherwise $GOPATH/bin. That directory needs to be on PATH for
# `tack` to be runnable by name. It installs the standard library too, whose
# packages a program imports.
install: install-std
	go install ./tack

# install-std copies the standard library, src/std, to $TACK_HOME/src/std, or
# to ~/.tack/src/std when TACK_HOME is unset or empty, where the compiler
# looks for it. What is there is removed first, so a file deleted from
# src/std does not linger in the installed copy. It then writes the files
# built into the compiler that std keeps copies of, the builtins'
# documentation, std/builtin/builtin.tck, and the runtime header,
# std/include/tack/tack.h, from the compiler's tree, read-only: they are
# for reading, by people and editors, and no build reads them.
install-std:
	@home="$${TACK_HOME:-}"; \
	if [ -z "$$home" ]; then \
		if [ -z "$${HOME:-}" ]; then echo "install-std: neither TACK_HOME nor HOME is set" >&2; exit 1; fi; \
		home="$$HOME/.tack"; \
	fi; \
	echo "installing std to $$home/src/std"; \
	rm -rf "$$home/src/std" && mkdir -p "$$home/src" && cp -R src/std "$$home/src/std" && \
	mkdir -p "$$home/src/std/builtin" "$$home/src/std/include/tack" && \
	cp $(BUILTIN_DOC) "$$home/src/std/builtin/builtin.tck" && \
	cp $(RUNTIME_HEADER) "$$home/src/std/include/tack/tack.h" && \
	chmod a-w "$$home/src/std/builtin/builtin.tck" "$$home/src/std/include/tack/tack.h"

# std-copies refreshes the repository's copies, in src/std, of the files
# built into the compiler: the builtins' documentation and the runtime
# header. A test of package builtin fails while they differ.
std-copies:
	cp $(BUILTIN_DOC) src/std/builtin/builtin.tck
	cp $(RUNTIME_HEADER) src/std/include/tack/tack.h

# test runs every test in the module, including the golden tests and the tests
# that compile and run generated C, which need clang on PATH.
test:
	go test -race ./...

# FUZZTIME is how long `make fuzz-progen` fuzzes for.
FUZZTIME := 10m

# fuzz-progen fuzzes FuzzGenerated, which builds each generated program
# with clang under the sanitizers and runs it, for $(FUZZTIME). It caps
# minimizing a new input at 20 executions: by default the fuzz engine
# spends up to 60 seconds minimizing each input that finds new coverage,
# and an execution here is two sanitizer builds and up to 42 runs, so
# the run would spend most of its time minimizing, showing 0 execs/sec.
fuzz-progen:
	go test ./internal/progen -run '^$$' -fuzz '^FuzzGenerated$$' -fuzztime $(FUZZTIME) \
		-fuzzminimizetime=20x

# fmt rewrites Go sources in place with the formatters enabled in
# .golangci.yml.
fmt:
	golangci-lint fmt

# lint runs the linters enabled in .golangci.yml.
lint:
	golangci-lint run
