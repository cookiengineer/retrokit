# upstreamcheck

`upstreamcheck` maintains and verifies RetroKit's divergence from upstream
WebKit. It is deliberately read-only: it can generate candidate patches and
reports, but it never edits the main worktree or commits for you.

## Why

RetroKit is not a normal WebKit branch. It is upstream WebKit with a large
removal layer: whole features, individual declarations, build flags and
behaviour fixes. A naive three-way merge cannot reapply that layer reliably
because upstream keeps changing the code around the removals.

The tool therefore tracks the removal policy in a TSV manifest and compares
three trees:

- the upstream baseline (`vendor-base/2.34.1`),
- the fork (`HEAD`),
- the next upstream snapshot (`vendor/2.54.2`, then later vendor tags).

## Vendor snapshots

Build the helper first:

```bash
cd Helpers/upstreamcheck
go build -o upstreamcheck ./cmd/upstreamcheck
```

Import a filtered upstream snapshot as a normal commit:

```bash
./upstreamcheck vendor import --upstream ../../upstream_repo
```

The snapshot contains `CMakeLists.txt`, `Source`, and the minimal `Tools`
closure needed to build MiniBrowser. The vendor commit's parent is the previous
vendor snapshot, so future upstream releases can be merged with ordinary
`git merge`:

```bash
# After updating the upstream checkout to 2.56.0:
./upstreamcheck vendor import --upstream ../../upstream_repo \
  --version 2.56.0 --base-tag vendor/2.54.2 --tag vendor/2.56.0
```

## Manifest

`manifest/divergence.tsv` is the authoritative list of intentional divergence:

```text
feature  kind  symbol  path  disposition  scope  origin  notes
```

- `kind`: `file`, `class`, `struct`, `method`, `function`, `field`, `enum`,
  `interface`, `idl_member`, `build_ref`, `flag`, `pref`, `behavior`, ...
- `disposition`: `removed`, `modified`, `added`.
- `scope`: `source`, `tools`, `vendor-root`, `downstream`, `docs`.
- `origin`: the fork commit that last touched the entry.

`manifest/features.tsv` links features to `SECURITY.md` and contains the tokens
used by leak scans.

## Commands

```bash
# Derive the manifest from the fork history (one-time bootstrap).
./upstreamcheck init --base fbd7f1a9

# Verify that every in-scope change is explained by the manifest and that
# removed files/symbols have not reappeared.
./upstreamcheck lint

# Show coverage and the migration work queue.
./upstreamcheck status
./upstreamcheck plan --feature webgl

# Find remaining references to removed files/symbols.
./upstreamcheck refs --feature webrtc

# Scan for residual feature tokens in non-comment source lines.
./upstreamcheck leaks --feature webgl

# Check a new fork tree against the removal manifest.
./upstreamcheck equivalence --fork <new-branch>
./upstreamcheck equivalence --fork HEAD --feature webgl

# Write the full feature-by-feature migration work queue.
./upstreamcheck report --target vendor/2.54.2 --out manifest/migration-plan.md

# Generate a candidate patch for one feature against the target snapshot.
./upstreamcheck migrate --feature webgl --out manifest/candidates
```

## Indexing

- `git` provides diffs, rename detection and snapshot import.
- tree-sitter C++ extracts declarations and their byte ranges.
- WebIDL has a dedicated lexer/parser.
- CMake, `Sources*.txt`, preferences and `.in` files use line-level parsing.

The tool does not attempt to be a compiler. It is a policy linter and a
candidate-patch generator; every generated patch must be reviewed and built.
