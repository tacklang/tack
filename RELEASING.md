# Releasing

A release is a tag `vMAJOR.MINOR.PATCH`, starting at `v0.1.0`, pushed
to this repository. The release workflow, `.github/workflows/release.yml`,
builds, tests, signs and publishes it as a GitHub release. Its steps are
scripts in `tools/release/` and `tools/get-tack.sh`, which run the same
locally; `make test-release TACK=bin/tack` checks them, with throwaway
keys.

## Before v0.1.0

- [x] The repository is public: the release workflow fetches tackc's
  `go-bootstrap` tag and its own main branch without credentials, and
  GitHub's artifact attestations of a free plan are for public
  repositories alone.
- [ ] The signing key is made and set (see the next section).
- [ ] The `release` environment holds the two secrets, and deploys from
  `v*` tags alone (see the next section).
- [ ] A tag ruleset lets maintainers alone create, update or delete
  `v*` tags: Settings, Rules, Rulesets, a new tag ruleset targeting
  `v*`, restricting creations, updates and deletions, with the
  maintainers as its bypass list.

## Once: the signing key

The release key is a minisign key pair with a password. Make it on a
machine you trust, never in CI:

    minisign -G -p minisign.pub -s minisign.key -c "tack release key"

minisign asks for the password twice. Then:

1. Put the public key, the second line of `minisign.pub`, in three
   places, which `make test-release` keeps the same:
   - `tools/release/minisign.pub`: the whole file, replacing the
     placeholder;
   - `tools/get-tack.sh`: the line `TACK_RELEASE_KEY=UNSET`;
   - `README.md`, Installing: both `minisign -Vm ... -P UNSET` lines
     and the sentence about `UNSET`.

   Until then the workflow's gate refuses every tag, before anything is
   built.
2. Make a GitHub Environment named `release`: Settings, Environments,
   New environment. Under Deployment branches and tags choose Selected
   branches and tags and add the tag rule `v*`, so only a run for a `v*`
   tag can use it. Add two environment secrets to it, not repository
   secrets:
   - `MINISIGN_SECRET_KEY`: the whole text of `minisign.key`;
   - `MINISIGN_PASSPHRASE`: its password.
3. Keep `minisign.key` and its password offline, apart. Losing either
   means a new key (see Rotating the key).

Only the workflow's `publish` job names the `release` environment, so
only it can read the secrets, and only in its Sign step, through `env`.
`tools/release/sign.sh` writes the key to a file made under umask 077,
mode 600, in a directory of its own below `$TMPDIR`, removes it however
the script ends, gives the password to minisign on its standard input,
and verifies each signature with `tools/release/minisign.pub`, so a
secret key that is not that public key's fails the release before
anything is published. Neither secret is written to the log.

## Each release

1. Push the commit to `main` and wait for the ci workflow,
   `.github/workflows/ci.yml`, to pass on it. The release workflow
   refuses a tag whose commit is not on `main`, or has no ci run started
   by that push that succeeded (see the `gate` job below).
2. Run the full porttest, which ci does not, on exactly that commit: in
   tackc at the head of `go-impl`, which holds porttest's checks of the
   port, with `TACK_PORT` a checkout of the commit,

       git -C ../tack checkout --detach <commit>
       make port-test TACK_PORT=../tack 2>&1 | tee port-test-<version>.log

   and keep the log. It must pass.
3. Tag the commit and push the tag:

       git tag -a v0.1.0 -m "Tack v0.1.0" <commit>
       git push origin v0.1.0

4. Watch the workflow. When it ends, the release is published, with its
   four files: the tarball, `SHA256SUMS`, and the `.minisig` of each.
5. Attach the porttest log to the release:

       gh release upload v0.1.0 port-test-v0.1.0.log -R tacklang/tack

A failed run publishes nothing, or leaves a draft release at most,
which is deleted by hand before the run is started again. A tag pushed
before ci passed fails at the gate; once ci passes, re-run the release
workflow's failed jobs from the Actions tab.

## What the workflow does

The `gate` job, with `contents: read` and `actions: read` alone, checks
the tag before anything is built, with `tools/release/gate.sh`. It
requires a passing run of ci, the Go-free suite, not of porttest: that
was decided 2026-11-21, replacing an earlier gate on a porttest
workflow, whose hosted runs took too long. The full porttest is the
manual step 2 of Each release, on the same commit, its log attached to
the release. The gate checks that:

- the release public key in `tools/release/minisign.pub` is set, not
  `UNSET`;
- the tag's commit, peeled from the annotated tag, `git rev-parse
  "refs/tags/<tag>^{commit}"`, is the checkout and is on `main`, an
  ancestor of `origin/main` or `origin/main` itself;
- a run of ci, listed with `gh api` and selected on the workflow's side,
  has that commit as its `head_sha`, was started by a `push`, on the
  branch `main`, and ended in `success`: a run started by hand, on
  another branch, or that failed, was cancelled or is still going never
  counts;
- the tag is above every published release, by SemVer
  (`tools/release/previous.sh`), so a tag at or below the highest
  release is refused, and the first release's Go path can never run
  for a lower tag than a release there is.

It hands the build the previous release, the highest below the tag,
or none for a first release.

The `build` job, with `contents: read`, `attestations: read` and no
secret, runs in `fedora:45`, the image ci runs in, here pinned by digest,
so the release is built and tested with the clang the suite is
developed and checked with. It installs its tools with dnf as root and
logs the versions of clang, llvm, compiler-rt, glibc, tar and xz, and
builds and tests as `ci`, an unprivileged account, since several tests
check that an unwritable path is refused, which root would bypass:

1. installs git, make, clang, compiler-rt (libFuzzer), llvm (for
   `llvm-readelf`), minisign, gh, curl, xz and the base tools, as ci.yml
   does;
2. checks that the tag is `vMAJOR.MINOR.PATCH`, that the checkout is the
   commit the gate passed, and takes `SOURCE_DATE_EPOCH`, the time of
   the commit, which dates every entry of the tarball;
3. with a previous release, fetches it with `tools/get-tack.sh --strict
   --download`, which fails unless its SHA-256, both minisign signatures,
   with the key in the tag's own `get-tack.sh`, and its attestation, by
   the release workflow for its own tag on GitHub's runners, all verify,
   and takes its compiler as stage 1's;
4. with none, the first release, fetches tackc at the tag `go-bootstrap`
   of branch `go-impl`, shallow, and fails unless the tag is the commit
   `FROZEN_REF_SHA`, `b23d723ca6604b07daa2c49d1f00305b40e2d7f1`; installs
   the Go that tackc's `go.mod` names; and builds stage 1 with
   `tools/release/go-stage1.sh`, which builds tackc's reference Go
   compiler with this tree's `runtime/` overlaid, as tackc's `make
   port-go-bin` does, with `-trimpath -buildvcs=false`, and builds
   `./driver` with it. It logs the SHA-256 of both. The reference's bytes
   depend on tackc's files, this tree's runtime and the Go version, not
   on where either is checked out: two checkouts at different paths
   gave the same bytes, and the same stage 1, with the same Go and clang
   (checked 2026-11-21). To build it again, in `fedora:45` with tackc at
   that commit in `../tackc` and a checkout of this repository at the
   tag:

       sh tools/release/go-stage1.sh ../tackc /tmp/stage1/stage1
       sha256sum /tmp/stage1/stage1.go /tmp/stage1/stage1

   That gives the same bytes only with the same Go and the same clang
   as the run, both of which the run logs. The tag `go-frozen` cannot be
   used: the Go compiler there predates `tack test` and `testing.Site`,
   and loads std's tests where the port's std needs them left out
   (decided 2026-11-21);
5. runs `make release VERSION=<tag> TACK=<stage 1>`: a copy of the tree,
   stamped with the version (see below), its `./driver` built by stage
   1, then `make bootstrap` there with that build, which fails unless
   stage 2 and stage 3 are the same bytes; stage 3 checked to print the
   version and, with `tools/release/glibc-check.sh`, to need no glibc
   above `tools/release/glibc-floor`, 2.34, and no library but
   `libc.so.6`; and the tarball, with stage 3 as `bin/tack`, from
   `tools/release/package.sh`;
6. copies the tarball, as root, to a directory the `ci` account cannot
   write, and takes its SHA-256 there, the job's output, before any test
   runs, so nothing the tests run can change what is published;
7. with a previous release, while `GO_CROSSCHECK` is `true`, also builds
   stage 1 with Go from the stamped copy and fails unless the stage 2 it
   builds is the stage 2 the previous release's chain built, byte for
   byte (`tools/release/crosscheck.sh`): a hard failure, never a
   warning. It costs one build of the Go reference and two builds of the
   driver, a few minutes;
8. runs, in the stamped copy with stage 3, `tack --check .` and `make
   test-corpus test-std test-unicodegen check-unicode-tables
   fuzz-regress test-install test-release`;
9. uploads the tarball from the directory of step 6.

The `publish` job, with `contents: write`, `id-token: write` and
`attestations: write`, in the `release` environment, also runs in
`fedora:45`, as root, since it runs no test, and installs minisign and
gh with dnf. It:

1. checks, with `tools/release/check-artifact.sh`, that the downloaded
   artifact is the one tarball, with the SHA-256 the build job took;
2. writes `SHA256SUMS`, `tools/release/sums.sh`;
3. signs the tarball and `SHA256SUMS`, `tools/release/sign.sh`, each
   signature's trusted comment `tack <tag> <file>`;
4. attests the tarball and `SHA256SUMS` with
   `actions/attest-build-provenance`;
5. creates the release as a draft with the four files;
6. lists the published releases again and publishes the draft, marked
   the latest, only while its tag is still above every one of them
   (`tools/release/previous.sh`); otherwise it fails and the draft
   stays a draft.

## The version

`driver/version.tck` holds `var tack_version = "devel"`, committed so,
and `tack --version` prints `tack devel` for every build of a checkout.
The release target copies the tree to `bin/release/tree` and
`tools/release/stamp.sh` rewrites that copy's line to the tag, and the
`stdout "tack devel\n"` lines of its `corpus/runs/port-only.txtar` with
it, so the release's corpus run holds the release's compiler to its own
version. Every stage of the release's bootstrap is built from the
stamped copy, so stage 2 and stage 3 stay the same bytes, and the
committed tree is never stamped. `stamp.sh` refuses any version that is
not `vMAJOR.MINOR.PATCH`, so nothing else reaches Tack source.

## Limits

- The previous release must compile the new source. Its compiler builds
  stage 1 from the tagged tree, so a tree that uses a feature, or needs
  a fix, the previous release's compiler lacks cannot be released from
  it. Then cut an intermediate release first: a commit that the
  previous release compiles and that compiles the new source, typically
  the new feature implemented in the compiler but not yet used in the
  compiler's own source. Release it, then the commit that uses it.
- The Go cross-check runs only while the `go-bootstrap` tag can still
  build the tree. Once the source uses what that Go compiler does not
  have, the check fails; set `GO_CROSSCHECK` to `"false"` in
  `release.yml` in that commit, and from then on releases are checked
  against their previous release alone.
- "Latest" is what GitHub serves as the latest release. `get-tack.sh`
  checks its signatures as any release's, but an older release served
  whole, with its own valid signatures, as the latest cannot be told
  from it: the script says which version it installs, and `--version`
  pins one.
- The compiler needs glibc 2.34 or newer (`tools/release/glibc-floor`).
  Raising the floor is a decision of its own: the release fails, by
  `glibc-check.sh`, while stage 3 needs a newer glibc than the file
  says.
- The `fedora:45` image is pinned by its index digest, resolved from
  Docker Hub on 2026-11-21. Its packages are not pinned: dnf installs
  what Fedora 45's repositories hold on the day, so a later run may get
  a newer clang or xz. The tarball is the same bytes for the same tools,
  which the run logs, not for every run.

## Rotating the key

Each release's build verifies the previous release with the key its own
`tools/get-tack.sh` holds, so a release that brings a new key cannot
verify the previous release, signed with the old one, unaided. Rotate
with an overlap:

1. Make the new key as above, put its public key in the three places,
   and replace the environment's two secrets.
2. In the same commit, set `PREVIOUS_RELEASE_KEY` in `release.yml` to
   the old public key, so the build verifies the previous release with
   it.
3. Release. That release is signed with the new key.
4. In the next commit, set `PREVIOUS_RELEASE_KEY` back to `""`.

Users verifying an older release by hand use the key of its own tag's
README, and `get-tack.sh --version` of an older release needs
`TACK_MINISIGN_KEY` set to the old key.

## The actions

Each action is pinned to a full commit SHA, resolved from its tag on
2026-11-21, with the version in a comment beside it; `make
test-release` fails on a `uses:` line of any workflow without both.
Each pinned version's inputs were checked against its `action.yml` at
that commit:

| action | version | SHA |
| --- | --- | --- |
| actions/checkout | v7.0.1 | 3d3c42e5aac5ba805825da76410c181273ba90b1 |
| actions/setup-go | v7.1.0 | d8cbb8045d7f43706b9177cceb36c1f91a6644d5 |
| actions/upload-artifact | v7.0.2 | cf430e030ddbb5b0abf93d22962f4752f3646cd9 |
| actions/download-artifact | v8.0.2 | 9000827ccba6bdab643e8b6fd33ac0654aef8333 |
| actions/attest-build-provenance | v4.2.2 | 4d101475d8b20a2381f78447822ac1eab6504dd8 |

To move one, resolve the new tag's commit, for example with

    git ls-remote https://github.com/actions/checkout 'refs/tags/v7.0.1*'

taking the `^{}` line for an annotated tag, and read its `action.yml`
for changed inputs. attest-build-provenance is, since v4, a wrapper of
actions/attest; it needs `artifact-metadata: write` only for a storage
record, which it makes only with `push-to-registry`, which this workflow
does not use.

To move the image, resolve `fedora:45`'s index digest again, read-only:

    tok=$(curl -fsSL "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/fedora:pull" |
        sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
    curl -fsSI -H "Authorization: Bearer $tok" \
        -H "Accept: application/vnd.oci.image.index.v1+json" \
        https://registry-1.docker.io/v2/library/fedora/manifests/45 | grep -i docker-content-digest

and put it in both `image:` lines.
