# Builds, installs, and tests tack, the Tack compiler.

# BIN is where `make build` writes the compiler binary.
BIN := bin/tack

# BUILTIN_DOC and RUNTIME_HEADER are the files built into the compiler that
# std keeps copies of: the builtins' documentation and the runtime header.
BUILTIN_DOC := builtin/builtin.tck
RUNTIME_HEADER := builtin/runtime/tack/tack.h

.PHONY: build install install-std std-copies unicode-tables test port-test port-fuzz fuzz-progen fmt lint

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

# unicode-tables rewrites std/unicode's tables, src/std/unicode/tables.tck,
# from the unicode package of the Go that runs it. A test of
# internal/unicodegen fails while the committed file differs from what
# that Go would write.
unicode-tables:
	go run ./internal/unicodegen src/std/unicode/tables.tck

# test runs every test in the module, including the golden tests and the tests
# that compile and run generated C, which need clang on PATH.
test:
	go test -race ./...

# port-test checks the Tack compiler written in Tack, the port, against
# this one, stage by stage, with the tests of porttest. The port's source
# tree is $TACK_PORT, ../tack by default; TACK_PORT_BIN names a built
# port instead, and TACK_GO a built Go compiler, relative paths taken
# from this directory. Stages the port does not implement yet skip unless
# porttest/required.txt names them. TestPort runs only with
# TACK_PORT_TEST=1 and -count=1: go test cannot see a change to the port
# or to the binaries, so a cached result could pass a broken port.
port-test:
	TACK_PORT_TEST=1 go test -count=1 ./porttest

# FUZZTIME is how long `make fuzz-progen` and `make port-fuzz` fuzz for.
FUZZTIME := 10m

# PORT_FUZZ_BIN is where `make port-fuzz` builds the two compilers, under
# the ignored bin/.
PORT_FUZZ_BIN := bin/port-fuzz

# PORT_FUZZ_PARALLEL is how many fuzz workers `make port-fuzz` runs, each
# running both compilers once per input.
PORT_FUZZ_PARALLEL := 4

# port-fuzz fuzzes FuzzLexDiff, which compares the port's --lex with this
# compiler's on generated inputs, for $(FUZZTIME). It builds both
# compilers once, into $(PORT_FUZZ_BIN), and passes them as TACK_GO and
# TACK_PORT_BIN, so the fuzz workers, each a process of its own, build
# neither. The port's source tree is $TACK_PORT, ../tack by default, as
# for port-test.
port-fuzz:
	mkdir -p $(PORT_FUZZ_BIN)
	go build -o $(PORT_FUZZ_BIN)/tack-go ./tack
	cd "$${TACK_PORT:-../tack}" && TACK_HOME="$(CURDIR)" \
		TACK_CACHE="$(CURDIR)/$(PORT_FUZZ_BIN)/cache" \
		"$(CURDIR)/$(PORT_FUZZ_BIN)/tack-go" --out "$(CURDIR)/$(PORT_FUZZ_BIN)/tack-port" ./driver
	TACK_GO=$(PORT_FUZZ_BIN)/tack-go TACK_PORT_BIN=$(PORT_FUZZ_BIN)/tack-port TACK_PORT_TEST=1 \
		go test -count=1 ./porttest -run '^$$' -fuzz '^FuzzLexDiff$$' \
		-fuzztime $(FUZZTIME) -parallel $(PORT_FUZZ_PARALLEL)

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
