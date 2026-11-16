# How to Migrate RetroKit to a New Upstream WebKit

This is the operational runbook for migrating RetroKit from an old WebKitGTK
fork base to a new upstream release without losing the removal layer.

It complements:

- [handover.md](./handover.md) — current state, completed work, next steps.
- [Helpers/upstreamcheck/README.md](./Helpers/upstreamcheck/README.md) — tool reference.
- [Helpers/upstreamcheck/MIGRATION.md](./Helpers/upstreamcheck/MIGRATION.md) — migration plan and progress.
- [SECURITY.md](./SECURITY.md) — what is supposed to be removed and why.
- [TODO.md](./TODO.md) — remaining removal candidates.

This document is intentionally process-oriented. If you are new to the fork,
read `handover.md` first, then this file.

---

## 1. Mental model

Do not try to merge the 2021 fork into 2026 WebKit. That produces hundreds of
conflicts in code the fork intentionally deleted, and it is exactly where
removed APIs silently come back.

RetroKit is maintained as:

```text
upstream release snapshot
        minus
the removal layer (features, APIs, quirks, dependencies, behaviours)
```

The removal layer is stored in two places:

1. **The manifest** — a line-oriented policy describing every intentional
   divergence and why it exists.
2. **Feature commits** — one commit per migrated feature, replayed on top of a
   clean vendor snapshot.

The old fork is a reference, not a merge parent.

---

## 2. Branches, tags, and snapshots

Current branch layout:

```text
main / legacy/2.34   old 2.34.1 fork + current tooling
migrate/2.54         active migration branch (based on vendor/2.54.2)
```

Vendor refs:

```text
vendor-base/2.34.1   filtered 2.34.1 baseline
vendor/2.54.2        filtered 2.54.2 target
```

The vendor path set is deliberately small:

```text
CMakeLists.txt
Source/
Tools/CMakeLists.txt
Tools/PlatformGTK.cmake
Tools/MiniBrowser/gtk/
Tools/glib/
```

Why these paths:

- `Source/` is the actual browser.
- `Tools/glib/` is required by the Source build for generated PDF.js,
  modern-media-controls and inspector resources and the GLib depfile fix.
- `Tools/MiniBrowser/gtk/` is the smoke-test application.
- `CMakeLists.txt` is required to build the tree.

Do not add full upstream history or `LayoutTests`/`JSTests` unless there is a
deliberate decision to grow the repository. The current design keeps the fork
lean and uses filtered vendor snapshots.

---

## 3. Build the maintenance tool

```bash
cd Helpers/upstreamcheck
go build -o upstreamcheck ./cmd/upstreamcheck
```

Commands used during migration:

```bash
./upstreamcheck plan --feature <id> --target vendor/2.54.2
./upstreamcheck migrate --feature <id> --target vendor/2.54.2 \
  --out manifest/candidates
./upstreamcheck refs --feature <id> --limit 100
./upstreamcheck leaks --fork HEAD --feature <id>
./upstreamcheck equivalence --fork HEAD --feature <id>
./upstreamcheck report --target vendor/2.54.2 \
  --out manifest/migration-plan.md
```

Important semantics:

- `plan` is **target-relative**. It shows what still exists in the target
  vendor snapshot, including features already migrated. Use `equivalence`
  against `HEAD` to check the actual migrated branch.
- `refs` and `leaks` use `git grep` against a ref, normally `HEAD`. They do not
  inspect uncommitted working-tree changes. Build and `grep` the working tree
  while iterating; run `equivalence`/`leaks` after committing.
- `migrate` never edits the worktree. It writes a patch and a Markdown report.

---

## 4. Manifest model

Authoritative files:

```text
Helpers/upstreamcheck/manifest/divergence.tsv
Helpers/upstreamcheck/manifest/features.tsv
Helpers/upstreamcheck/manifest/needs-review.tsv
Helpers/upstreamcheck/manifest/out-of-scope.tsv
```

`divergence.tsv` columns:

```text
feature  kind  symbol  path  disposition  scope  origin  notes
```

Kinds include:

```text
file
class
struct
method
function
field
enum
enum_value
interface
idl_member
idl_attribute
idl_operation
build_ref
flag
pref
config_line
behavior
```

Dispositions include:

```text
removed
modified
added
renamed
copied
```

Feature rules:

- One feature ID per removal area (`webgl`, `webrtc`, `media-capture`, ...).
- `features.tsv` holds the human title, status, `SECURITY.md` reference, and
  leak-scan tokens.
- `pending` features are allowed to be present; `removed` features should be
  absent and should be caught by `equivalence` and `leaks`.

The manifest is generated once from the old fork, then curated manually. It is
not a disposable build artifact.

**Do not casually re-run `init`.** It is a bootstrap/recovery command and will
regenerate the old manifest from `legacy/2.34`. If you must run it, use:

```bash
./upstreamcheck init --base fbd7f1a9 --fork legacy/2.34
```

and then re-apply manual curation and migration additions.

---

## 5. End-to-end feature migration checklist

Use this checklist for every feature. Do not skip the build and verification
gates.

### Step 1 — Choose and inspect the feature

```bash
cd Helpers/upstreamcheck

./upstreamcheck plan --feature <id> --target vendor/2.54.2
./upstreamcheck report --target vendor/2.54.2 \
  --out manifest/migration-plan.md
```

Look at:

- `pending` entries that still exist in the target.
- `done` entries that upstream already removed.
- `manual` entries for behavioural or additive changes.
- Any obvious dependency ordering (for example remove a feature before removing
  the library it needs).

### Step 2 — Generate a candidate

```bash
./upstreamcheck migrate --feature <id> --target vendor/2.54.2 \
  --out manifest/candidates
```

Read the generated report, not just the summary:

```text
manifest/candidates/<id>.md
manifest/candidates/<id>.patch
```

The report contains:

- deleted files
- modified files
- removed symbols
- removed build/config lines
- manual entries
- symbols not found in target (often already renamed/removed upstream)
- possible new upstream files added since the old fork

### Step 3 — Review for over-removal

This is critical. The net-diff bootstrap can attribute a symbol to the wrong
feature, especially when one commit bundled several removals.

Check for suspicious entries:

- A feature removing a shared enum value used by an unmigrated feature.
- A utility/library feature removing a symbol used by an unrelated subsystem.
- A file-level `modified` entry that actually belongs to another feature.
- A deletion that removes a **new** upstream file without a manifest row.

Known examples from this migration:

- The WebXR candidate tried to remove `TaskSource::Geolocation`. Geolocation is
  not migrated yet, so this was restored and the manifest row moved to
  `geolocation`.
- The libnotify manifest contained `createRTCProvider`, which belongs to WebRTC.
- The Gamepad commit historically deleted the WebXR modules; path-based
  attribution was added so `Modules/webxr` belongs to `webxr`.

If a manifest row is wrong, move it to the correct feature and add a note. Do
not blindly apply the candidate.

### Step 4 — Apply the patch

First try Git:

```bash
git apply --check manifest/candidates/<id>.patch
git apply manifest/candidates/<id>.patch
```

If upstream context has moved, Git may reject the patch while GNU patch can
still apply with fuzz:

```bash
patch -p1 --no-backup-if-mismatch < manifest/candidates/<id>.patch
```

Inspect the patch log for `Hunk ... succeeded ... with fuzz` and review the
result. Never accept a fuzzy patch without checking the affected file.

### Step 5 — Delete new upstream files

New files added between the old fork and the target are not in the manifest.
The migration report lists them under:

```text
## Possible new upstream files
```

Delete them if they belong to the removed feature, then record them in the
manifest:

```bash
sed -n '/## Possible new upstream files/,$p' \
  manifest/candidates/<id>.md \
  | grep -o '`[^`]*`' | tr -d '`' > /tmp/new-files.txt

xargs -a /tmp/new-files.txt rm -f
```

For a feature that introduced a whole new API surface (WebXR did), this list
can be large. Review the paths; do not delete unrelated files just because a
token matched.

### Step 6 — Handle manual entries

The candidate intentionally leaves these for a human:

- `pref` entries
- `config_line` entries
- `behavior` entries
- `file` entries with `modified`/`added`
- symbols that moved or were renamed upstream

Typical manual changes:

- Remove a preference block from `UnifiedWebPreferences.yaml`.
- Remove a feature object from `features.json`.
- Remove event names from `EventNames.in`/`EventInterfaces.in`.
- Remove event targets from `EventTargetFactory.in`.
- Remove enum values from `TaskSource.h`.
- Remove `ENABLE(...)`/`HAVE(...)` blocks and their includes.
- Remove UI/tooling integration from MiniBrowser or platform code.

Use the old fork commit as the semantic reference, but do not apply its patch
blindly; the surrounding code has changed.

### Step 7 — Remove integration blocks, not just files

Most features are not only files. Search the working tree:

```bash
grep -rIn --exclude-dir=.git --exclude-dir=ThirdParty \
  -iE '<feature tokens>' Source Tools
```

For C/C++/Objective-C/IDL/`.in` files, remove balanced blocks such as:

```cpp
#if ENABLE(WEBXR)
...
#endif
```

or:

```cpp
#if HAVE(TOUCH_BAR)
...
#endif
```

Use a balanced block remover, not a blind line filter. Blindly deleting lines
containing a token can leave orphan `else` bodies, comments, or function
bodies.

After block removal, search again for:

- dangling `#endif` comments
- removed headers still included
- removed types still forward-declared
- feature names in comments/docs
- build-list references

### Step 8 — Remove build and registration references

Search build/config files for deleted names and feature flags:

```bash
grep -rIn -iE '<feature>|<ENABLE_FLAG>|Modules/<feature>' Source \
  --include='*.txt' --include='*.cmake' --include='CMakeLists.txt' \
  --include='*.make' --include='*.xcfilelist' --include='*.in'
```

Remove:

- `Sources*.txt` entries
- `CMakeLists.txt` source/IDL lists
- `Headers.cmake`
- `DerivedSources.make`
- `.xcfilelist` entries
- `ENABLE_*`/`USE_*` options and dependencies
- `PlatformEnable*.h` defaults
- `*.messages.in` IPC declarations
- `*.serialization.in` type mappings
- `webkit.h.in` public API includes
- `Scripts/webkit/messages.py` and opaque IPC tracking entries

Verify no build references remain to deleted files:

```bash
# Example: scan for deleted basenames in build files.
python3 - <<'PY'
import subprocess, pathlib
out = subprocess.check_output(['git', 'status', '--porcelain'], text=True)
deleted = [line[3:] for line in out.splitlines() if line.startswith(' D ')]
basenames = {pathlib.Path(p).name for p in deleted}
for p in pathlib.Path('Source').rglob('*'):
    if not p.is_file() or 'ThirdParty' in p.parts:
        continue
    if p.suffix not in {'.txt', '.cmake', '.make', '.in', '.xcfilelist'}:
        continue
    for i, line in enumerate(p.read_text(errors='ignore').splitlines(), 1):
        if any(b in line for b in basenames):
            print(f'{p}:{i}: {line.strip()}')
PY
```

### Step 9 — Fix preprocessor and structural balance

Check touched C/C++ files for unbalanced `#if`/`#endif`:

```bash
python3 - <<'PY'
import re, pathlib
for p in pathlib.Path('Source').rglob('*'):
    if not p.is_file() or 'ThirdParty' in p.parts:
        continue
    if p.suffix not in {'.h', '.hpp', '.cpp', '.cc', '.c', '.mm', '.m'}:
        continue
    depth = 0
    for line in p.read_text(errors='ignore').splitlines():
        s = line.strip()
        if re.match(r'#\s*(if|ifdef|ifndef)\b', s):
            depth += 1
        elif re.match(r'#\s*endif\b', s):
            depth -= 1
    if depth != 0:
        print(p, depth)
PY
```

Known upstream exception: `Source/bmalloc/bmalloc/valgrind.h` is unbalanced in
upstream and should be ignored.

Also check for dangling includes of deleted headers:

```bash
python3 - <<'PY'
import subprocess, pathlib, re
out = subprocess.check_output(['git', 'status', '--porcelain'], text=True)
deleted = [line[3:] for line in out.splitlines() if line.startswith(' D ')]
headers = {pathlib.Path(p).name for p in deleted
           if pathlib.Path(p).suffix in {'.h', '.hpp', '.in', '.idl'}}
for p in pathlib.Path('Source').rglob('*'):
    if not p.is_file() or 'ThirdParty' in p.parts:
        continue
    if p.suffix not in {'.h', '.hpp', '.cpp', '.cc', '.c', '.mm', '.m', '.in'}:
        continue
    for i, line in enumerate(p.read_text(errors='ignore').splitlines(), 1):
        m = re.match(r'\s*#\s*(include|import)\s*[<"]([^>"]+)[>"]', line)
        if m and pathlib.Path(m.group(2)).name in headers:
            print(f'{p}:{i}: {line.strip()}')
PY
```

### Step 10 — Build in the containers

Use the rootless Podman setup:

```bash
make image DISTRO=debian
make configure DISTRO=debian
make build DISTRO=debian
make smoke DISTRO=debian
```

`make build` already configures + compiles. `make compile` is for incremental
recompiles after a successful configuration.

Both Debian 13 and Arch Linux are validated. Use the distro that matches the
bug or run both when changing build files:

```bash
make build DISTRO=archlinux
make smoke DISTRO=archlinux
```

Build outputs:

```text
build/debian/bin/MiniBrowser
build/archlinux/bin/MiniBrowser
```

Persistent ccache:

```text
build/ccache/debian
build/ccache/archlinux
```

The cache is mounted at `/ccache` and survives separate container runs.
`make clean` keeps it; `make distclean` removes it.

### Step 11 — Debug build fallout

Full builds are the most important verification step. Search the log:

```bash
grep -nE 'error:|FAILED:|fatal error' /tmp/build.log | head -40
```

Common fallout patterns and fixes:

1. **Unrelated enum/member removed**

   Example: WebXR removed `TaskSource::Geolocation` while Geolocation remained.
   Restore the unrelated member and move the manifest row.

2. **Removed function signature left a body**

   Token-based removal can delete a constructor or method signature while
   leaving its initializer list or body.

   Example: `RemoteAdapterProxy` lost its `create`/constructor declarations
   when the `xrCompatible` parameter was removed.

   Fix the signature and constructor, do not delete the body.

3. **Orphan function bodies**

   Example: `WebGPUObjectHeap.cpp` had `convertXR...FromBacking` bodies left
   after their signatures were removed.

   Delete the orphan body blocks.

4. **Trailing comma in a `Variant`/template list**

   Removing the last XR variant left a trailing comma:

   ```cpp
   IPC::ScopedActiveMessageReceiveQueue<RemoteTextureView>,
   >;
   ```

   Remove the comma.

5. **Removed header still included**

   Example: `UIDelegate.mm` still imported `XRGPUProjectionLayerInit.h`.

6. **UI/tooling references removed API**

   Example: MiniBrowser `BrowserTab.c` still handled
   `WebKitXRPermissionRequest`.

   Update `Tools/MiniBrowser/gtk/` as part of the feature.

7. **Container dependency missing**

   Examples from this session:

   - Debian: `xdg-dbus-proxy`, `gettext`, `xauth`
   - Arch: `gobject-introspection`, `ruby-erb`, `glib2-devel`, `xorg-xauth`

   Add the package to the appropriate `containers/*.Containerfile`.

8. **Fuzzy patch applied in the wrong place**

   Review the patch log and `git diff` around the changed files. If in doubt,
   revert the file and apply the change manually.

When the build fails, fix the smallest correct thing, rerun `make compile`, and
keep iterating. `make compile` reuses the existing build directory and ccache.

### Step 12 — Record the feature in the manifest

Add rows for everything the migration changed that is not already recorded:

- new upstream files deleted during the migration
- modified files not already in the manifest
- corrected feature attribution
- new `behavior` entries for non-symbol changes

Rows must be sorted and deduplicated. The tool's writer sorts by:

```text
feature, path, kind, symbol, disposition
```

You can add rows with a small Python script or by editing the TSV manually.
Keep the header and tabs intact.

If a feature was already absent upstream, do not invent source changes. Move
misattributed rows and let `equivalence` confirm the removals.

### Step 13 — Verify before committing

```bash
./Helpers/upstreamcheck/upstreamcheck equivalence \
  --fork HEAD --feature <id>

./Helpers/upstreamcheck/upstreamcheck leaks \
  --fork HEAD --feature <id>
```

`equivalence` must report 0 errors. Warnings about a `modified` file that
upstream already removed are acceptable if reviewed.

Leak scan should report 0 residual occurrences. Historical `NEWS`, Xcode
project metadata, third-party code and `three.js`/Esprima are intentionally
ignored.

Also run `git diff --check` and validate structured files if touched:

```bash
python3 -m json.tool Source/WebCore/features.json > /dev/null
python3 -c 'import yaml; yaml.safe_load(open("Source/WTF/Scripts/Preferences/UnifiedWebPreferences.yaml"))'
python3 -c 'import plistlib; plistlib.load(open("Source/WebKit/Resources/Signposts/SystemTracePoints.plist","rb"))'
```

### Step 14 — Commit

Use the existing commit style:

```bash
git add -A Source Tools Helpers/upstreamcheck/manifest
git commit -m ':unicorn: Remove <Feature>'
```

Use `:bug:` for build fallout and fixes, and `:book:` for documentation-only
changes.

Do not mix unrelated features in one commit. A feature commit should contain:

- source removals
- registration/build removals
- UI/tooling updates
- manifest updates
- any necessary build fixes

### Step 15 — Update the current-state docs

After a feature is committed:

- update [handover.md](./handover.md) with completed work and next steps
- update [Helpers/upstreamcheck/MIGRATION.md](./Helpers/upstreamcheck/MIGRATION.md)
- update [SECURITY.md](./SECURITY.md) checkboxes if the feature corresponds to
  a documented item
- update [TODO.md](./TODO.md) if a remaining item was resolved

---

## 6. Verification gates

A feature is not done until all of these are true:

- [ ] `equivalence --feature <id>` reports 0 errors.
- [ ] `leaks --feature <id>` reports 0 residual occurrences.
- [ ] No dangling includes of deleted headers.
- [ ] No build references to deleted files.
- [ ] Preprocessor blocks are balanced in touched files.
- [ ] CMake configure succeeds.
- [ ] Full container build succeeds with MiniBrowser.
- [ ] MiniBrowser smoke test succeeds.
- [ ] Manifest contains the new files/modifications and corrected attribution.
- [ ] Docs (`handover.md`, `MIGRATION.md`, `SECURITY.md`, `TODO.md`) are in sync.

For features that touch shared platform/build code, run both Debian and Arch
builds.

---

## 7. Container build reference

Build images:

```bash
make image DISTRO=debian
make image DISTRO=archlinux
# or
make images
```

Configure only:

```bash
make configure DISTRO=debian
```

Incremental compile after a configuration exists:

```bash
make compile DISTRO=debian
```

Full configure + build:

```bash
make build DISTRO=debian
make build-all
```

Smoke test:

```bash
make smoke DISTRO=debian
make smoke DISTRO=archlinux
```

Interactive shell:

```bash
make shell DISTRO=debian
```

Cache management:

```bash
make ccache-stats DISTRO=debian
make ccache-clean DISTRO=debian
```

Cleanup:

```bash
make clean DISTRO=debian      # removes build/debian, keeps ccache
make distclean                # removes build/, ccache and both images
```

The Makefile uses rootless Podman and `--userns=keep-id` so build artifacts
are owned by the invoking user. `CONTAINER_UID`/`CONTAINER_GID` are the
**container-side** IDs and default to 1000 to match the image `builder` user.
Do not set them to the host UID; Podman maps the host user to these IDs.

The container images are:

- `docker.io/library/debian:13`
- `docker.io/library/archlinux:latest`

Arch is rolling by nature. For byte-for-byte reproducible images, pin the
base image digest or use an archived Arch Linux image tag.

---

## 8. Handling manifest mistakes

### Misattributed symbols

Move the row to the correct feature:

```text
libnotify method ... createRTCProvider ...  ->  webrtc method ...
```

Add a note:

```text
; misattributed by net-diff bootstrap; belongs to WebRTC
```

### Bundled historical commits

A commit such as `:unicorn: Gamepad API` may have deleted files belonging to
another feature. Path-based attribution in `bootstrap.go` handles known
patterns. If a path is still misattributed:

1. Add or extend `pathFeatureOverride` in
   `Helpers/upstreamcheck/internal/bootstrap/bootstrap.go`.
2. Regenerate the manifest only if you understand the consequences.
3. Prefer moving the individual rows manually on an active migration branch.

### New upstream files

These are not in the old manifest. Delete them if the feature is removed and
add file rows with:

```text
feature  file  -  <path>  removed  source  manual:<feature>-<version>  note
```

### Behaviour changes

Do not force a behaviour change into a symbol row. Use `kind=behavior` and
record the file, origin and rationale. `equivalence` will not remove it
automatically; it must be ported manually.

---

## 9. Future upstream upgrades

Import a new filtered vendor snapshot:

```bash
cd Helpers/upstreamcheck

./upstreamcheck vendor import \
  --upstream ../../upstream_repo \
  --version 2.56.0 \
  --base-tag vendor/2.54.2 \
  --tag vendor/2.56.0
```

Update `upstreamcheck.toml`:

```toml
target_ref = "vendor/2.56.0"
```

Then, on the migration branch:

```bash
git merge vendor/2.56.0
```

Because the vendor commits are chained, the merge base is the previous vendor
release (`vendor/2.54.2`), so only the new upstream delta and the files the fork
touched can conflict.

After merging:

```bash
./upstreamcheck equivalence --fork HEAD
./upstreamcheck leaks --fork HEAD
./upstreamcheck plan --target vendor/2.56.0
```

`equivalence` and `leaks` are the branch-state checks. `plan` remains
target-relative. Then reapply removals or resolve reappeared APIs, and run the
normal feature verification gates.

If a new release adds new files for an already-removed feature (as 2.54 did for
WebXR/WebGPU XR), `plan`/`migrate` will list them. Delete them and record them
in the manifest.

---

## 10. Worked examples from this session

### WebXR

What happened:

- 673 removal entries, 0 equivalence errors.
- Removed `Modules/webxr`, platform XR service, permission requests, testing
  hooks, prefs, flags and the WebGPU XR bridge.
- The candidate over-removed `TaskSource::Geolocation`; restored it because
  Geolocation is a separate feature.
- New upstream files were detected by feature directories and deleted.
- Remaining WebGPU XR internals were removed completely to avoid dangling
  headers.
- MiniBrowser still referenced `WebKitXRPermissionRequest`; its XR permission
  UI was removed.
- The full builds exposed orphan signatures/bodies and a trailing `Variant`
  comma; those were fixed.

### SystemD / journald

What happened:

- Deleted `FindJournald.cmake`.
- Removed `ENABLE_JOURNALD_LOG` and journald logging branches.
- Removed WPE DRM journald integration.
- 19 removal entries, 0 equivalence errors, 0 leaks.

### Touch Bar

What happened:

- Removed the remaining `HAVE(TOUCH_BAR)` IPC/UI surface.
- 26 removal entries, 0 errors, 1 stale warning for an upstream-removed file.
- Beware: a blind line filter over `TouchBar`/`TOUCH_BAR` also modified
  unrelated macOS/legacy code. Always remove balanced `#if HAVE(...)` blocks,
  not individual lines.

### libnotify

What happened:

- Already absent upstream.
- The only pending entry was `createRTCProvider`, misattributed by the net-diff
  bootstrap. It was moved to `webrtc`.
- No source commit was needed.

### FTP

What happened:

- 2.54.2 had already removed the FTP directory documents; what remained was the
  `FTP` scheme, protection-space types and the newer `ftpEnabled` network
  plumbing.
- The manifest still used 2.34.1 symbol names
  (`ProtectionSpaceServerFTP`), so `equivalence` reported the protection-space
  entries as "done" while the enum values were still present. Renaming the rows
  to `ProtectionSpaceBaseServerType::FTP` made the check honest.
- Removing the enum values renumbers the C API protection-space constants.
  Record that in the manifest and docs; it is intentional.
- 82 removal entries, 0 errors, 0 warnings, 0 leaks.
- `URL.h` changed, so this feature triggers a near-full WebCore rebuild even
  though the diff is small.

---

## 11. Next features

Recommended order:

1. **Gamepad** — enabled by default on GTK, exposes hardware identifiers and is
   independent of the media stack. Curate the candidate first; it currently
   contains unrelated `CMakeLists.txt` rows.
2. Geolocation and DeviceMotion/Orientation.
3. Remaining platform and dependency removals (libsecret, libnotify, geoclue).
4. Media stack (capture, streams, recorder, EME, PiP/fullscreen).
5. Automation, WebDriver, quirks and user-agent quirks.
6. Remaining build/behaviour fixes.

Also curate `needs-review.tsv`:

- IndexedDB origin leak fix.
- MEMMEM and X11 `None` fixes.
- GTK/GLib API fixes.
- Context menu and media-control fixes.
- DerivedSources and missing HTML tag fixes.

---

## 12. Branch synchronization and cutover

The tooling and manifest must stay available on both branches while the
migration is in progress.

Sync tooling/manifest changes from `migrate/2.54` to `main`:

```bash
git switch main
git checkout migrate/2.54 -- Helpers/upstreamcheck
git add Helpers/upstreamcheck
git commit -m "Sync upstreamcheck with migration progress"
git branch -f legacy/2.34 main
git switch migrate/2.54
```

At cutover:

1. Confirm both container builds and smoke tests pass.
2. Run `equivalence` for every completed feature.
3. Review `needs-review.tsv`.
4. Promote `migrate/2.54` to `main`.
5. Keep `legacy/2.34` as the historical branch.
6. Update `handover.md` and `MIGRATION.md`.

Do not delete the old branch until the new branch has a full build and a
successful smoke test.

---

## 13. Quick command reference

```bash
# Tool
cd Helpers/upstreamcheck && go build -o upstreamcheck ./cmd/upstreamcheck

# Inspect
./upstreamcheck status
./upstreamcheck plan --feature webxr --target vendor/2.54.2
./upstreamcheck report --target vendor/2.54.2 --out manifest/migration-plan.md

# Migrate
./upstreamcheck migrate --feature gamepad --target vendor/2.54.2 \
  --out manifest/candidates
git apply --check manifest/candidates/gamepad.patch
git apply manifest/candidates/gamepad.patch
# or, if context moved:
patch -p1 --no-backup-if-mismatch < manifest/candidates/gamepad.patch

# Inspect fallout
./upstreamcheck refs --feature gamepad --limit 100
./upstreamcheck leaks --fork HEAD --feature gamepad

# Build
make image DISTRO=debian
make build DISTRO=debian
make smoke DISTRO=debian
make build DISTRO=archlinux
make smoke DISTRO=archlinux

# Cache
make ccache-stats DISTRO=debian
make ccache-clean DISTRO=debian

# Verify and commit
./upstreamcheck equivalence --fork HEAD --feature gamepad
git add -A Source Tools Helpers/upstreamcheck/manifest
git commit -m ':unicorn: Remove Gamepad API'
```

---

## 14. Things that cost time and should not be repeated

- Do not merge the old fork directly into upstream.
- Do not blindly apply a candidate that removes symbols used by an
  unmigrated feature.
- Do not run `init` on the migration branch without `--fork legacy/2.34`.
- Do not remove individual lines containing `#if`/`#endif` tokens; remove
  balanced blocks.
- Do not delete a header while retained code still includes it; either remove
  the retained integration too or keep the internal layer intentionally.
- Do not trust `git apply` failure as “the patch is wrong”; inspect the reject
  and try GNU `patch` with fuzz.
- Do not skip the full container build; configure-only misses missing headers,
  orphan bodies and UI/tooling API changes.
- Do not forget MiniBrowser; it is part of the tracked build closure and often
  uses the APIs being removed.
- Do not lose ccache; use the Makefile cache mount instead of compiling inside
  a disposable container HOME.
- Do not mix docs-only, build-fix and feature-removal commits.

---

## 15. Key references

- [handover.md](./handover.md) — current state and next actions.
- [Helpers/upstreamcheck/README.md](./Helpers/upstreamcheck/README.md) — tool usage.
- [Helpers/upstreamcheck/MIGRATION.md](./Helpers/upstreamcheck/MIGRATION.md) — migration plan/progress.
- [Helpers/upstreamcheck/upstreamcheck.toml](./Helpers/upstreamcheck/upstreamcheck.toml) — vendor paths and refs.
- [Helpers/upstreamcheck/manifest/divergence.tsv](./Helpers/upstreamcheck/manifest/divergence.tsv) — authoritative divergence list.
- [Helpers/upstreamcheck/manifest/features.tsv](./Helpers/upstreamcheck/manifest/features.tsv) — feature metadata and leak tokens.
- [Helpers/upstreamcheck/manifest/needs-review.tsv](./Helpers/upstreamcheck/manifest/needs-review.tsv) — ambiguous legacy commits.
- [SECURITY.md](./SECURITY.md) — removal policy and status.
- [TODO.md](./TODO.md) — remaining candidates.
- [NOTES.md](./NOTES.md) — generated bindings and architecture notes.
- [Makefile](./Makefile) — Podman build/cache/smoke entry points.
- [containers/debian.Containerfile](./containers/debian.Containerfile) — Debian 13 build environment.
- [containers/archlinux.Containerfile](./containers/archlinux.Containerfile) — Arch Linux build environment.
