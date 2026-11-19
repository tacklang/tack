# Builds the files of the port generated from its own sources.

# TACK is the Tack compiler that builds the tools below, tack on PATH by
# default. tackc's `make port-embed` sets it to the Go compiler it builds.
TACK ?= tack

# BIN is where the tools below are built, with their build cache, under
# the ignored bin/.
BIN := bin

# BUILTIN_DOC and RUNTIME_HEADER are the builtins' documentation and the
# runtime header, files the compiler carries inside it that std keeps
# copies of. RUNTIME_C is the runtime's C files, in the byte order of
# their names, the order builtin/embedded.tck holds them in.
BUILTIN_DOC := runtime/builtin.tck
RUNTIME_HEADER := runtime/tack/tack.h
RUNTIME_C := $(sort $(wildcard runtime/*.c))

.PHONY: embed std-copies test-corpus update-corpus

# embed writes builtin/embedded.tck, the files the compiler carries
# inside it as Tack string literals: the builtins' documentation, the
# runtime header and the runtime's C files. It builds tools/embed with
# $(TACK), with TACK_HOME this tree, so std is the port's own, and runs
# it. tackc's porttest TestEmbedded fails while the committed file
# differs from what this writes.
embed:
	mkdir -p $(BIN)
	TACK_HOME="$(CURDIR)" TACK_CACHE="$(CURDIR)/$(BIN)/cache" \
		"$(TACK)" --out "$(CURDIR)/$(BIN)/embed" ./tools/embed
	"$(CURDIR)/$(BIN)/embed" builtin/embedded.tck builtin \
		doc=$(BUILTIN_DOC) header=$(RUNTIME_HEADER) \
		$(foreach c,$(RUNTIME_C),'runtime[]=$(c)')

# std-copies refreshes std's copies, src/std/builtin/builtin.tck and
# src/std/include/tack/tack.h, of the builtins' documentation and the
# runtime header. tackc's porttest TestPortFiles fails while they differ.
std-copies:
	cp $(BUILTIN_DOC) src/std/builtin/builtin.tck
	cp $(RUNTIME_HEADER) src/std/include/tack/tack.h

# corpus-tool builds tools/corpus, the corpus runner, with $(TACK), with
# TACK_HOME this tree, as embed builds tools/embed, and runs it on
# corpus/ with this tree's std and $(TACK), looked up on PATH when it is
# a bare name, as the compiler it checks, $(1) added to its command line.
define corpus-tool
	mkdir -p $(BIN)
	TACK_HOME="$(CURDIR)" TACK_CACHE="$(CURDIR)/$(BIN)/cache" \
		"$(TACK)" --out "$(CURDIR)/$(BIN)/corpus" ./tools/corpus
	compiler="$$(command -v "$(TACK)")" && \
		"$(CURDIR)/$(BIN)/corpus" $(1) "$$compiler" corpus src/std
endef

# test-corpus checks $(TACK), which must be the port, against the test
# corpus, corpus/: every case of its archives, every golden and every
# committed tack.h (see tools/corpus). It needs clang and coreutils, and
# no Go.
test-corpus:
	$(call corpus-tool,)

# update-corpus is test-corpus with the corpus rewritten where the
# compiler's output differs from it, as go test ./test -update rewrites
# tackc's; read the diff it leaves before keeping it.
update-corpus:
	$(call corpus-tool,--update)
