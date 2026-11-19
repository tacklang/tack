# Builds, installs, and tests tack, the Tack compiler.

# BIN is where `make build` writes the compiler binary.
BIN := bin/tack

# BUILTIN_DOC and RUNTIME_HEADER are the files built into the compiler that
# std keeps copies of: the builtins' documentation and the runtime header.
# They, the rest of builtin/runtime and src/std are frozen copies, kept for
# porttest's comparisons: the port, ../tack, owns std and the runtime, and
# a change to either is made there (per the owner's ruling, 2026-11-18).
BUILTIN_DOC := builtin/builtin.tck
RUNTIME_HEADER := builtin/runtime/tack/tack.h

.PHONY: build install install-std std-copies unicode-tables test port-test port-fuzz port-sanitize port-alloc-fail port-measure port-embed port-go-bin port-test-names fuzz-progen fmt lint

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
# port instead, and TACK_GO a built reference Go compiler, as
# port-go-bin builds one, relative paths taken from this directory.
# Stages the port does not implement yet skip unless
# porttest/required.txt names them. TestPort runs only with
# TACK_PORT_TEST=1 and -count=1: go test cannot see a change to the port
# or to the binaries, so a cached result could pass a broken port.
port-test:
	TACK_PORT_TEST=1 go test -count=1 -timeout 60m ./porttest

# PORT_STAGE2_BIN is where `make port-test-stage2` builds the reference
# Go compiler, stage 1 and stage 2, under the ignored bin/.
PORT_STAGE2_BIN := bin/port-stage2

.PHONY: port-test-stage2

# port-test-stage2 runs port-test with stage 2 as the port: it builds
# the reference Go compiler, as port-go builds it, stage 1, the port's
# driver built with it at -O3, and then runs the port's `make bootstrap`
# with stage 1, which builds stage 2 and stage 3 and fails unless they
# are the same bytes, fails unless stage 1 is those bytes too, and runs
# porttest with TACK_GO the reference and
# TACK_PORT_BIN stage 2, copied out of the port's bin/bootstrap. The
# port's source tree is $TACK_PORT, ../tack by default, as for
# port-test, and stage 1 is built with its own std, TACK_HOME that tree,
# as porttest builds it.
port-test-stage2:
	mkdir -p $(PORT_STAGE2_BIN)
	$(call port-go,$(PORT_STAGE2_BIN)/tack-go)
	cd "$${TACK_PORT:-../tack}" && TACK_HOME="$$PWD" \
		TACK_CACHE="$(CURDIR)/$(PORT_STAGE2_BIN)/cache" \
		"$(CURDIR)/$(PORT_STAGE2_BIN)/tack-go" -O3 --out "$(CURDIR)/$(PORT_STAGE2_BIN)/stage1" ./driver
	$(MAKE) -C "$${TACK_PORT:-../tack}" bootstrap TACK="$(CURDIR)/$(PORT_STAGE2_BIN)/stage1"
	cp "$${TACK_PORT:-../tack}/bin/bootstrap/stage2" $(PORT_STAGE2_BIN)/stage2
	cmp $(PORT_STAGE2_BIN)/stage1 $(PORT_STAGE2_BIN)/stage2
	TACK_GO=$(PORT_STAGE2_BIN)/tack-go TACK_PORT_BIN=$(PORT_STAGE2_BIN)/stage2 TACK_PORT_TEST=1 \
		go test -count=1 -timeout 60m ./porttest

# port-go builds, at the path $(1) names, the reference Go compiler
# porttest compares the port with: this compiler's Go code, frozen, with
# the files its package builtin embeds, builtin.tck, the runtime header
# and the runtime's C files, taken from the port's runtime/ through go
# build's -overlay, so that what it builds links the port's runtime,
# which the port owns. porttest's TestMain builds it, run with
# PORTTEST_BUILD_GO, as it builds its own when TACK_GO is unset. The
# port's source tree is $TACK_PORT, ../tack by default.
port-go = PORTTEST_BUILD_GO="$(1)" go test -count=1 -run '^$$' ./porttest

# PORT_GO_BIN is where `make port-go-bin` builds the reference Go
# compiler, under the ignored bin/.
PORT_GO_BIN := bin/port-go/tack

# port-go-bin builds the reference Go compiler, as port-go does, at
# $(PORT_GO_BIN): the compiler to build the port with, so that the port
# links its own runtime, and to pass as TACK_GO.
port-go-bin:
	$(call port-go,$(PORT_GO_BIN))

# port-test-names rewrites porttest/testdata/gotest-names.txt, every test
# and subtest go test runs in the packages whose runtime assertions are
# the port's run records, by running those tests (see porttest/testnames).
# porttest's TestRunRecordsCoverage fails while the list's top-level tests
# differ from those packages' test functions.
port-test-names:
	go generate ./porttest

# FUZZTIME is how long `make fuzz-progen` and `make port-fuzz` fuzz for.
FUZZTIME := 10m

# PORT_FUZZ_BIN is where `make port-fuzz` builds the two compilers, under
# the ignored bin/.
PORT_FUZZ_BIN := bin/port-fuzz

# PORT_FUZZ_PARALLEL is how many fuzz workers `make port-fuzz` runs, each
# running both compilers once per input.
PORT_FUZZ_PARALLEL := 4

# PORT_FUZZ_MINIMIZE is how long `make port-fuzz` lets the fuzz engine
# minimize each input that finds new coverage, go test's default. One
# execution of FuzzEmitDiff runs both compilers three times on a
# program, so at the default it spends most of a run minimizing; run it
# with PORT_FUZZ_MINIMIZE=20x, as make fuzz-progen does FuzzGenerated.
PORT_FUZZ_MINIMIZE := 60s

# PORT_FUZZ is the fuzz test `make port-fuzz` runs: FuzzLexDiff, which
# compares the port's --lex with this compiler's, FuzzParseDiff, which
# compares their --ast, FuzzResDiff, which compares their --res,
# FuzzLoadDiff, which compares their --ast and --res of generated file
# trees, or FuzzEmitDiff, which compares their --emit, --check and
# --emit-header of programs internal/progen generates.
PORT_FUZZ := FuzzLexDiff

# port-fuzz fuzzes $(PORT_FUZZ), which compares the port with this
# compiler on generated inputs, for $(FUZZTIME). It builds both
# compilers once, into $(PORT_FUZZ_BIN), the Go one the reference
# port-go builds, and passes them as TACK_GO and TACK_PORT_BIN, so the
# fuzz workers, each a process of its own, build neither. The port's
# source tree is $TACK_PORT, ../tack by default, as for port-test, and
# the port is built with its own std, TACK_HOME that tree, as porttest
# builds it.
port-fuzz:
	mkdir -p $(PORT_FUZZ_BIN)
	$(call port-go,$(PORT_FUZZ_BIN)/tack-go)
	cd "$${TACK_PORT:-../tack}" && TACK_HOME="$$PWD" \
		TACK_CACHE="$(CURDIR)/$(PORT_FUZZ_BIN)/cache" \
		"$(CURDIR)/$(PORT_FUZZ_BIN)/tack-go" --out "$(CURDIR)/$(PORT_FUZZ_BIN)/tack-port" ./driver
	TACK_GO=$(PORT_FUZZ_BIN)/tack-go TACK_PORT_BIN=$(PORT_FUZZ_BIN)/tack-port TACK_PORT_TEST=1 \
		go test -count=1 ./porttest -run '^$$' -fuzz '^$(PORT_FUZZ)$$' \
		-fuzztime $(FUZZTIME) -fuzzminimizetime $(PORT_FUZZ_MINIMIZE) \
		-parallel $(PORT_FUZZ_PARALLEL)

# PORT_SANITIZE_BIN is where `make port-sanitize` builds the two
# compilers, and the clang it builds the port with, under the ignored bin/.
PORT_SANITIZE_BIN := bin/port-sanitize

# SANITIZE_FLAGS are what `make port-sanitize`'s clang adds to every
# compile and link of the port: the address sanitizer, the leak sanitizer
# with it, and the undefined behavior sanitizer, each ending the run at
# its first report. -fno-sanitize-address-globals-dead-stripping keeps the
# address sanitizer from defining one symbol in every object, which this
# compiler's check of the C files' symbols rejects as defined twice.
# porttest's TestAllocFail adds the same flags (sanitizeFlags).
SANITIZE_FLAGS := -g -fno-omit-frame-pointer -fsanitize=address,undefined \
	-fno-sanitize-recover=all -fno-sanitize-address-globals-dead-stripping

# PORT_SANITIZE_STACK multiplies every stack porttest holds the port to
# under `make port-sanitize` (PORTTEST_STACK_SCALE): the sanitizers'
# frames are larger, so the deep inputs need more than the budgets give
# a port built as the harness builds it.
PORT_SANITIZE_STACK := 4

# PORT_SANITIZE_TIMEOUT multiplies how long porttest lets one run of a
# compiler take under `make port-sanitize` (PORTTEST_TIMEOUT_SCALE): the
# sanitized port is several times slower, and the largest deep inputs
# take it past the minute a port built as the harness builds it gets.
PORT_SANITIZE_TIMEOUT := 8

# PORT_SANITIZE_SKIP are the tests `make port-sanitize` skips: the
# performance tests, whose bounds a sanitized port, several times slower
# and larger, cannot meet; and TestPort's bootstrap stage, whose stage
# 1, the sanitized port, cannot be the same bytes as the stage 2 it
# builds, which clang builds without the sanitizers.
PORT_SANITIZE_SKIP := Perf|TestDeepFactsMemory|^TestPort$$/^bootstrap$$

# PORT_MALFORMED is how many accepted programs `make port-sanitize` has
# porttest's TestMalformed break, truncate and mutate.
PORT_MALFORMED := 100

# PORT_SANITIZE_PARALLEL is how many of porttest's cases `make
# port-sanitize` runs at once (go test -parallel). Each sanitized port
# and its clang take several times the memory of an unsanitized one, so
# go test's default, one per core, ran past 8 GB on a 24-core machine;
# 4 fits in it and took 2 h 44 min there.
PORT_SANITIZE_PARALLEL := 4

# port-sanitize runs porttest against the port built with SANITIZE_FLAGS,
# from the C this compiler, the reference port-go builds, writes for
# it, through a clang on PATH that
# adds them to every command but --version, with every stack
# PORT_SANITIZE_STACK times larger and every run's time limit
# PORT_SANITIZE_TIMEOUT times longer. A report ends the port with exit
# status 86, which fails the case it ran: the sanitizers linked together
# share one exit status, the one the options read last set, so both
# options are given 86. TestMalformed
# runs too, on $(PORT_MALFORMED) programs, and at most
# $(PORT_SANITIZE_PARALLEL) cases run at once. The port's
# source tree is $TACK_PORT, ../tack by default, as for port-test, and
# the port is built with its own std, TACK_HOME that tree, as porttest
# builds it.
port-sanitize:
	mkdir -p $(PORT_SANITIZE_BIN)/clang
	$(call port-go,$(PORT_SANITIZE_BIN)/tack-go)
	clang="$$(command -v clang)" && \
		printf '#!/bin/sh\ncase "$$1" in --version) exec "%s" "$$@";; esac\nexec "%s" "$$@" %s\n' \
		"$$clang" "$$clang" '$(SANITIZE_FLAGS)' > $(PORT_SANITIZE_BIN)/clang/clang && \
		chmod +x $(PORT_SANITIZE_BIN)/clang/clang
	cd "$${TACK_PORT:-../tack}" && TACK_HOME="$$PWD" \
		TACK_CACHE="$(CURDIR)/$(PORT_SANITIZE_BIN)/cache" \
		PATH="$(CURDIR)/$(PORT_SANITIZE_BIN)/clang:$$PATH" \
		"$(CURDIR)/$(PORT_SANITIZE_BIN)/tack-go" --out "$(CURDIR)/$(PORT_SANITIZE_BIN)/tack-port" ./driver
	ASAN_OPTIONS=detect_leaks=1:exitcode=86 UBSAN_OPTIONS=print_stacktrace=1:halt_on_error=1:exitcode=86 \
		PORTTEST_STACK_SCALE=$(PORT_SANITIZE_STACK) PORTTEST_TIMEOUT_SCALE=$(PORT_SANITIZE_TIMEOUT) \
		PORTTEST_MALFORMED=$(PORT_MALFORMED) \
		TACK_GO=$(PORT_SANITIZE_BIN)/tack-go TACK_PORT_BIN=$(PORT_SANITIZE_BIN)/tack-port TACK_PORT_TEST=1 \
		go test -count=1 -timeout 480m -p 1 -parallel $(PORT_SANITIZE_PARALLEL) \
		./porttest -skip '$(PORT_SANITIZE_SKIP)'

# PORT_ALLOC_FAIL is how many points of allocation failure `make
# port-alloc-fail` sweeps each run at.
PORT_ALLOC_FAIL := 40

# port-alloc-fail runs porttest's TestAllocFail, which builds the port
# with the sanitizers and its allocations counted, and runs a sample of
# the corpus failing every allocation from the Nth on, for
# $(PORT_ALLOC_FAIL) values of N each. The port's source tree is
# $TACK_PORT, ../tack by default, as for port-test.
port-alloc-fail:
	PORTTEST_ALLOC_FAIL=$(PORT_ALLOC_FAIL) TACK_PORT_TEST=1 \
		go test -count=1 -timeout 240m ./porttest -run '^TestAllocFail$$'

# PORT_MEASURE_LOG is the file `make port-measure` logs every run of
# both compilers to, measured.
PORT_MEASURE_LOG := bin/port-measure.jsonl

# port-measure runs TestPort with every run of both compilers measured,
# its wall time and peak memory appended to $(PORT_MEASURE_LOG), which it
# empties first (PORTTEST_MEASURE_LOG), and then TestBudget on the log,
# which lists the runs of the port over the budgets against Go's.
port-measure:
	mkdir -p $(dir $(PORT_MEASURE_LOG))
	: > $(PORT_MEASURE_LOG)
	PORTTEST_MEASURE_LOG=$(PORT_MEASURE_LOG) TACK_PORT_TEST=1 \
		go test -count=1 -timeout 120m ./porttest -run '^TestPort$$'
	PORTTEST_BUDGET_LOG=$(PORT_MEASURE_LOG) go test -count=1 ./porttest -run '^TestBudget$$' -v

# PORT_EMBED_BIN is where `make port-embed` builds this compiler, under
# the ignored bin/.
PORT_EMBED_BIN := bin/port-embed

# port-embed writes the port's builtin/embedded.tck, the files the
# compiler carries inside it as Tack string literals, with the port's
# own `make embed`, from the port's own runtime/: builtin.tck, the
# runtime's header and its C files, in the byte order of their names.
# It builds this compiler, the reference port-go builds, and passes it
# as the port's TACK, which builds
# the port's tools/embed with the port's std. porttest's TestEmbedded
# fails while the port's copy differs from what this writes. The port's
# source tree is $TACK_PORT, ../tack by default, as for port-test.
port-embed:
	mkdir -p $(PORT_EMBED_BIN)
	$(call port-go,$(PORT_EMBED_BIN)/tack-go)
	$(MAKE) -C "$${TACK_PORT:-../tack}" embed TACK="$(CURDIR)/$(PORT_EMBED_BIN)/tack-go"

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
