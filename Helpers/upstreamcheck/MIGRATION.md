# Migration Plan: WebKitGTK 2.34.1 → 2.54.2

## Current state

- Fork baseline: `vendor-base/2.34.1` (the fork's original snapshot, verified
  against the official 2.34.1 release tarball).
- Current legacy branch: `legacy/2.34` (formerly `main`).
- Target vendor snapshot: `vendor/2.54.2`.
- Migration branch: `migrate/2.54` (starts at `vendor/2.54.2`).
- Generated manifest: `manifest/divergence.tsv` (6,748 rows, 38 features).
- Current target work queue: 4,172 pending, 1,725 already gone in 2.54.2,
  851 manual review entries.

The vendor snapshots contain only `CMakeLists.txt`, `Source`, and the minimal
`Tools` closure needed for the build and MiniBrowser:

```text
CMakeLists.txt
Source/
Tools/CMakeLists.txt
Tools/PlatformGTK.cmake
Tools/MiniBrowser/gtk/
Tools/glib/
```

## Why not merge?

The fork is upstream minus features, not a set of independent patches. A naive
three-way merge of the 2.34.1 fork against 2.54.2 conflicts in 572 of 697
modified files (3,034 conflict hunks). Resolving those conflicts by hand is
where removed APIs would silently return.

The migration therefore starts from the clean vendor snapshot and re-applies
the removal layer feature by feature. The old fork is used as a reference, not
as a merge parent.

## Workflow

```bash
cd Helpers/upstreamcheck
go build -o upstreamcheck ./cmd/upstreamcheck

# Inspect coverage and target status.
./upstreamcheck status
./upstreamcheck report --target vendor/2.54.2 --out manifest/migration-plan.md

# For each feature: generate, review, apply, compile, lint.
./upstreamcheck migrate --feature geolocation --target vendor/2.54.2 --out manifest/candidates
git apply --check manifest/candidates/geolocation.patch
git apply manifest/candidates/geolocation.patch
./upstreamcheck refs --feature geolocation
./upstreamcheck lint --base vendor-base/2.34.1
./upstreamcheck equivalence --fork HEAD
```

`migrate` never edits the worktree. It writes a patch and a Markdown report
containing deleted files, modified files, removed symbols, removed build lines,
manual entries, symbols that no longer exist in the target, and possible new
upstream files that need review.

## Phase order

1. **Baseline and vendor snapshots** — done.
2. **Manifest bootstrap** — done; manual curation still needed for the
   `needs-review.tsv` items.
3. **Clean target build** — switch to `migrate/2.54` and build before any
   removals. The filtered snapshot configures successfully with:

   ```bash
   cmake -DPORT=GTK -DCMAKE_BUILD_TYPE=Release \
     -DENABLE_MINIBROWSER=ON \
     -DENABLE_DOCUMENTATION=OFF \
     -DENABLE_API_TESTS=OFF \
     -DENABLE_LAYOUT_TESTS=OFF \
     -DENABLE_SPEECH_SYNTHESIS=OFF \
     -DUSE_LIBBACKTRACE=OFF \
     -DUSE_SYSTEM_UNIFDEF=OFF \
     -GNinja
   ```

   Ruby 2.5 or newer is required for the real build (JavaScriptCore uses Ruby
   generators). `USE_SYSTEM_UNIFDEF=OFF` uses the bundled unifdef. Speech
   synthesis and libbacktrace can be re-enabled after installing Flite and
   libbacktrace.
4. **Feature re-application** — platform/dependencies, legacy plugins and
   media, fingerprinting APIs, automation/quirks, behavioural fixes. Commit
   each feature separately with its manifest update.
5. **Cutover** — full build, MiniBrowser smoke test, feature probe,
   `lint`, `equivalence`, then promote the branch.

## Manual-review backlog

See `manifest/needs-review.tsv`. The initial set contains 19 `:bug:` commits
that touch source files without an obvious feature owner. The most important
ones are:

- IndexedDB origin leak fix.
- MEMMEM and X11 `None` fixes.
- GTK/GLib API fixes.
- Context menu and media-control fixes.
- DerivedSources and missing HTML tag fixes.

These should be assigned to a feature or recorded as `maintenance` behaviour
entries before the migration branch is considered complete.

## Known limitations

- Symbol removal is name-based, so overloads are treated as one symbol.
- C++ parsing is declaration-oriented; it does not do semantic analysis.
- New upstream files are reported but not deleted automatically.
- Behavioural changes are reported as manual entries; they require a human
  patch.
- The initial manifest is generated from net history and needs curation for
  commit-level attribution.
