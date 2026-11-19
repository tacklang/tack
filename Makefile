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

# UCD is the copy of the Unicode Character Database std/unicode's tables
# are written from, UnicodeData.txt and PropList.txt of the Unicode
# version it is named for, with Unicode's license, and SHA256SUMS, the
# SHA-256 of each of the three as unicode.org publishes them.
# UNICODE_TABLES is the file they are written to, and the one
# check-unicode-tables checks.
UCD := tools/unicodegen/ucd/18.0.0
UNICODE_TABLES := src/std/unicode/tables.tck

.PHONY: unicode-tables check-unicode-tables

# unicodegen builds tools/unicodegen with $(TACK), with TACK_HOME this
# tree, as embed builds tools/embed.
define unicodegen
	mkdir -p $(BIN)
	TACK_HOME="$(CURDIR)" TACK_CACHE="$(CURDIR)/$(BIN)/cache" \
		"$(TACK)" --out "$(CURDIR)/$(BIN)/unicodegen" ./tools/unicodegen
endef

# ucd-check fails unless each file of $(UCD) has the SHA-256
# $(UCD)/SHA256SUMS pins, every file there but SHA256SUMS is pinned, and
# the version PropList.txt's first line names, # PropList-<version>.txt,
# is the one $(UCD) is named for.
define ucd-check
	cd $(UCD) && sha256sum --check --strict --quiet SHA256SUMS
	for f in $(UCD)/*; do \
		n="$${f##*/}"; [ "$$n" = SHA256SUMS ] || grep -q "  $$n\$$" $(UCD)/SHA256SUMS || \
			{ echo "$(UCD)/$$n has no SHA-256 in $(UCD)/SHA256SUMS" >&2; exit 1; }; \
	done
	v="$$(sed -n '1s/^# PropList-\(.*\)\.txt$$/\1/p' $(UCD)/PropList.txt)"; \
		[ "$$v" = "$(notdir $(UCD))" ] || \
		{ echo "$(UCD)/PropList.txt names Unicode '$$v', not $(notdir $(UCD))" >&2; exit 1; }
endef

# unicode-tables rewrites std/unicode's tables, $(UNICODE_TABLES), from
# $(UCD) with tools/unicodegen, once ucd-check passes. It needs
# sha256sum, and no Go.
unicode-tables:
	$(ucd-check)
	$(unicodegen)
	"$(CURDIR)/$(BIN)/unicodegen" $(UCD) $(UNICODE_TABLES)

# check-unicode-tables fails when ucd-check does, and while
# $(UNICODE_TABLES) differs from what tools/unicodegen writes from
# $(UCD), cmp naming the first byte that differs; it writes only into
# $(BIN). tackc's porttest unicode-tables stage makes the same checks.
# It needs sha256sum and cmp, and no Go.
check-unicode-tables:
	$(ucd-check)
	$(unicodegen)
	"$(CURDIR)/$(BIN)/unicodegen" $(UCD) $(BIN)/unicode-tables.tck
	cmp $(UNICODE_TABLES) $(BIN)/unicode-tables.tck || \
		{ echo "$(UNICODE_TABLES) is not what tools/unicodegen writes from $(UCD); run make unicode-tables" >&2; exit 1; }

# STD_TESTS are the packages of std that hold a test file, *_test.tck,
# each as the directory tack test is given.
STD_TESTS := $(sort $(patsubst %/,./%,$(dir $(wildcard src/std/*/*_test.tck))))

.PHONY: test-std

# test-std runs tack test, with $(TACK), on each of $(STD_TESTS), with
# TACK_HOME this tree, and fails when any of them fails, after running
# them all. It needs no Go.
test-std:
	@failed=0; \
	for p in $(STD_TESTS); do \
		TACK_HOME="$(CURDIR)" TACK_CACHE="$(CURDIR)/$(BIN)/cache" "$(TACK)" test "$$p" || failed=$$((failed + 1)); \
	done; \
	echo "std: $(words $(STD_TESTS)) packages tested, $$failed failed"; \
	[ "$$failed" -eq 0 ]

# UNICODEGEN_CASES are the directories of tools/unicodegen's test cases,
# each a small UCD, UnicodeData.txt and PropList.txt, either of them
# missing or malformed in most.
UNICODEGEN_CASES := $(sort $(patsubst %/,%,$(dir $(wildcard tools/unicodegen/testdata/*/want))))

.PHONY: test-unicodegen

# test-unicodegen runs tools/unicodegen on each of $(UNICODEGEN_CASES),
# writing into $(BIN), and fails unless what it writes on standard error,
# followed by a line "exit <status>", is the case's file want, byte for
# byte, and, for a case with a want.tck, the tables it writes are that
# file. Each failed case is named, with the diff. It needs diff, and no
# Go.
test-unicodegen:
	$(unicodegen)
	@failed=0; \
	for c in $(UNICODEGEN_CASES); do \
		rm -f $(BIN)/unicodegen-case.tck; \
		"$(CURDIR)/$(BIN)/unicodegen" "$$c" $(BIN)/unicodegen-case.tck 2> $(BIN)/unicodegen-case.err; \
		echo "exit $$?" >> $(BIN)/unicodegen-case.err; \
		if ! diff -u "$$c/want" $(BIN)/unicodegen-case.err; then \
			echo "FAIL: $$c: standard error and exit status"; failed=$$((failed + 1)); \
		elif [ -f "$$c/want.tck" ] && ! diff -u "$$c/want.tck" $(BIN)/unicodegen-case.tck; then \
			echo "FAIL: $$c: tables"; failed=$$((failed + 1)); \
		fi; \
	done; \
	echo "unicodegen: $(words $(UNICODEGEN_CASES)) cases, $$failed failed"; \
	[ "$$failed" -eq 0 ]

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

# BOOTSTRAP is where `make bootstrap` builds stages 2 and 3 of the
# compiler, with a cache for each, under the ignored bin/.
BOOTSTRAP := $(BIN)/bootstrap

.PHONY: bootstrap

# bootstrap builds the compiler from ./driver twice, at -O3, the level
# the compiler is built at: stage 2 with $(TACK), stage 1, and stage 3
# with stage 2, each with TACK_HOME this tree, so std is the port's own,
# and a cache of its own, removed first, so every object is compiled
# anew. It prints both SHA-256 sums and fails unless the two are the
# same bytes, cmp naming the first byte that differs, counted from 1.
# With $(TACK) the port, built by the reference Go compiler as the
# README says, stage 2 is also stage 1, byte for byte; tackc's porttest
# bootstrap stage checks that. It needs clang, sha256sum and cmp, and
# no Go.
bootstrap:
	rm -rf $(BOOTSTRAP)
	mkdir -p $(BOOTSTRAP)
	TACK_HOME="$(CURDIR)" TACK_CACHE="$(CURDIR)/$(BOOTSTRAP)/cache2" \
		"$(TACK)" -O3 --out "$(CURDIR)/$(BOOTSTRAP)/stage2" ./driver
	TACK_HOME="$(CURDIR)" TACK_CACHE="$(CURDIR)/$(BOOTSTRAP)/cache3" \
		"$(CURDIR)/$(BOOTSTRAP)/stage2" -O3 --out "$(CURDIR)/$(BOOTSTRAP)/stage3" ./driver
	sha256sum $(BOOTSTRAP)/stage2 $(BOOTSTRAP)/stage3
	cmp $(BOOTSTRAP)/stage2 $(BOOTSTRAP)/stage3

# BINDIR is the directory `make install` installs the compiler to, and
# PREFIX the directory whose bin/ it is when BINDIR is unset or empty;
# with both unset or empty it is ~/.local/bin, the home directory
# $HOME, which must then be set, rather than install to /.local/bin.
# Neither is defined here: tools/install.sh reads them, and TACK_HOME
# and HOME, from the environment, where make puts them whether they are
# set there or on its command line, so no path is pasted into a command.
# INSTALL_STAGE1 is where `make install` builds stage 1 with $(TACK),
# and INSTALL_BOOTSTRAP where it runs the bootstrap with stage 1, apart
# from `make bootstrap`'s, both under the ignored bin/.
INSTALL_STAGE1 := $(BIN)/install-stage1
INSTALL_BOOTSTRAP := $(BIN)/install

.PHONY: install install-std test-install

# install builds the compiler in three stages: $(TACK), an existing
# Tack compiler, tack on PATH by default, builds stage 1 from ./driver
# at -O3, with TACK_HOME this tree and a fresh cache, and stage 1 runs
# the bootstrap, as `make bootstrap` runs it but in $(INSTALL_BOOTSTRAP),
# building stage 2 and stage 3, so it fails unless the two are the same
# bytes, even when $(TACK) is an older compiler whose output differs.
# It then installs stage 3 to $(BINDIR) as tack, and std as install-std
# does, both or neither (see tools/install.sh). It runs install.sh's
# checks first, so a destination that is missing, cannot be written, or
# would put std in this tree fails before anything is built, and
# replaces nothing until the compiler is built, so a failed build leaves
# a previous install as it was. stage 1 is written under another name
# and renamed, so $(TACK) may be a previous stage 1. That directory
# needs to be on PATH for `tack` to be runnable by name. It needs
# clang, coreutils and cmp, and no Go.
install:
	@sh tools/install.sh check
	rm -rf $(INSTALL_STAGE1)/cache
	mkdir -p $(INSTALL_STAGE1)
	TACK_HOME="$(CURDIR)" TACK_CACHE="$(CURDIR)/$(INSTALL_STAGE1)/cache" \
		"$(TACK)" -O3 --out "$(CURDIR)/$(INSTALL_STAGE1)/stage1.new" ./driver
	mv -f $(INSTALL_STAGE1)/stage1.new $(INSTALL_STAGE1)/stage1
	$(MAKE) bootstrap BOOTSTRAP=$(INSTALL_BOOTSTRAP) TACK="$(CURDIR)/$(INSTALL_STAGE1)/stage1"
	@sh tools/install.sh install $(INSTALL_BOOTSTRAP)/stage3

# install-std copies the standard library, src/std, to $TACK_HOME/src/std,
# or to ~/.tack/src/std when TACK_HOME is unset or empty, where the
# compiler looks for it, with the builtins' documentation,
# std/builtin/builtin.tck, and the runtime header,
# std/include/tack/tack.h, written from runtime/, read-only: they are
# for reading, by people and editors, and no build reads them. What was
# there is replaced whole, so a file deleted from src/std does not
# linger, and only once the copy is complete, so a failed copy leaves it
# as it was. It refuses a TACK_HOME that is not absolute, and a
# destination inside this tree or holding it (see tools/install.sh). It
# needs coreutils, and no Go.
install-std:
	@sh tools/install.sh std

# test-install runs tools/test-install.sh with $(TACK): it installs, with
# install and install-std, into directories of its own below $TMPDIR,
# from a copy of this tree, and checks what they install, what the
# installed compiler does and how they fail (see the script). It never
# writes to this tree or the real home. tackc's porttest install stage
# runs it with the port. It needs clang, coreutils, cmp and diff, and no
# Go.
test-install:
	sh tools/test-install.sh "$(TACK)"
