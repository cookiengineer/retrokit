# RetroKit Migration Handover

Last updated: 2026-11-16

## 1. Where we are

RetroKit is being migrated from WebKitGTK **2.34.1** to WebKitGTK **2.54.2**.
The migration does not merge the old fork into upstream. It starts from a clean,
filtered 2.54.2 vendor snapshot and re-applies the removal layer feature by
feature.

Current development branch:

- `migrate/2.54` — active migration branch, based on `vendor/2.54.2`.

Legacy / maintenance branch:

- `main` — the old 2.34.1 fork plus the current `upstreamcheck` tooling.
- `legacy/2.34` — alias of `main` for the old fork.

Vendor refs:

- `vendor-base/2.34.1` — filtered 2.34.1 baseline.
- `vendor/2.54.2` — filtered 2.54.2 target.

Both vendor trees were verified byte-for-byte against the upstream trees for
the tracked path set. The vendor path set is:

```text
CMakeLists.txt
Source/
Tools/CMakeLists.txt
Tools/PlatformGTK.cmake
Tools/MiniBrowser/gtk/
Tools/glib/
```

`Tools/glib` is required by the Source build for generated PDF.js, modern
media controls, inspector resources and the GLib dependency-file workaround.
`Tools/MiniBrowser/gtk` is the smoke-test application.

Recent commits on `migrate/2.54`:

```text
eea8c5a8e Remove Gamepad API
98e4d3fcb Document FTP removal and next steps
aad3b2584 Remove FTP Support
5965e68b9 Update handover commit list
dc4278995 Add end-to-end migration runbook
93a306c15 Note ccache validation on both containers
8087fcb85 Record successful Debian and Arch container builds
e2b68191b Share persistent per-distro ccache across container runs
f62961975 Add glib2-devel to Arch build container
0d47081ff Add ruby-erb to Arch build container
06be042a7 Clean Arch container cache and enable ccache
bb6320d48 Add rootless Podman build containers and handover document
3d9477656 Fix WebXR build fallout and restore Geolocation task source
f549a2c9d Document completed migration features
195673c75 Remove remaining Touch Bar support
a42a5067b Remove SystemD journald integration
a8b4ac338 Skip third-party, Xcode and release notes in leak scans
640489cad Remove WebXR API and WebGPU XR integration
e46e66013 Detect new upstream files by feature directories
f94343291 Improve feature attribution for bundled commits
c75033746 Make manifest generation deterministic
6f4d96f20 Add per-feature equivalence checks
928398df7 Document migration branch and build prerequisites
a9161448f Import downstream RetroKit files and upstreamcheck
53c2ab0d8 Vendor import: WebKitGTK 2.54.2
```

## 2. The maintenance tool

The central tool is `Helpers/upstreamcheck`. See:

- [Helpers/upstreamcheck/README.md](./Helpers/upstreamcheck/README.md)
- [Helpers/upstreamcheck/MIGRATION.md](./Helpers/upstreamcheck/MIGRATION.md)
- [Helpers/upstreamcheck/upstreamcheck.toml](./Helpers/upstreamcheck/upstreamcheck.toml)

Build it with:

```bash
cd Helpers/upstreamcheck
go build -o upstreamcheck ./cmd/upstreamcheck
```

Main commands:

```bash
# Bootstrap/regenerate the manifest from the legacy branch (one-time/recovery).
./upstreamcheck init --base fbd7f1a9 --fork legacy/2.34

# Show current divergence coverage.
./upstreamcheck status

# Verify that every in-scope old-fork change is manifested and that removed
# files/symbols have not reappeared. Run this on the legacy branch.
./upstreamcheck lint

# Per-feature migration status against the target vendor snapshot.
./upstreamcheck plan --feature webxr --target vendor/2.54.2

# Find remaining references to removed files/symbols.
./upstreamcheck refs --feature webxr

# Scan for residual feature tokens outside comments/third-party/Xcode/NEWS.
./upstreamcheck leaks --fork HEAD --feature webxr

# Check a migrated tree against the removal manifest.
./upstreamcheck equivalence --fork HEAD --feature webxr

# Generate a reviewable per-feature candidate patch.
./upstreamcheck migrate --feature ftp --target vendor/2.54.2 \
  --out manifest/candidates

# Write the target-relative work queue.
./upstreamcheck report --target vendor/2.54.2 \
  --out manifest/migration-plan.md
```

Important: `plan` is **target-relative**. It still lists work items for features
that have already been migrated. Use `equivalence --feature <id>` against the
current `HEAD` to verify actual migration state.

## 3. Migration manifest

The source of truth for intentional divergence is:

- [Helpers/upstreamcheck/manifest/divergence.tsv](./Helpers/upstreamcheck/manifest/divergence.tsv)
- [Helpers/upstreamcheck/manifest/features.tsv](./Helpers/upstreamcheck/manifest/features.tsv)
- [Helpers/upstreamcheck/manifest/needs-review.tsv](./Helpers/upstreamcheck/manifest/needs-review.tsv)
- [Helpers/upstreamcheck/manifest/out-of-scope.tsv](./Helpers/upstreamcheck/manifest/out-of-scope.tsv)

`divergence.tsv` uses:

```text
feature  kind  symbol  path  disposition  scope  origin  notes
```

It currently contains **9,849 rows across 39 features**. The manifest covers
the original 2.34.1 fork diff plus the new upstream WebXR/WebGPU-XR files and
the migration edits for completed features.

`needs-review.tsv` currently contains 19 ambiguous legacy `:bug:` commits.
These should be assigned to a feature or recorded as explicit maintenance
behaviour before the migration is considered complete.

## 4. Completed migrations

### WebXR

Commit: `640489cad Remove WebXR API and WebGPU XR integration`

Removed:

- `Source/WebCore/Modules/webxr`, bindings, IDL and testing hooks.
- Platform XR service (`PlatformXRSystem`, `PlatformXRCoordinator`).
- UI/API permission requests (`WebKitXRPermissionRequest`).
- WebXR preferences, feature flags, event/task-source entries and testing
  interfaces.
- The WebGPU XR bridge in WebCore and WebKit, plus the WebGPU C-API XR
  objects/functions.

Verification:

```text
equivalence --feature webxr: 673 entries, 0 errors
leaks --feature webxr:       0 residual occurrences
CMake configure:             success
```

Historical `NEWS` and Xcode project metadata may still mention WebXR; those are
excluded from the leak scan and are not part of the GTK build.

### SystemD / journald

Commit: `a42a5067b Remove SystemD journald integration`

Removed:

- `Source/cmake/FindJournald.cmake`.
- `ENABLE_JOURNALD_LOG` options and journald logging branches in WTF.
- WPE DRM journald integration.
- Related documentation references.

Verification:

```text
equivalence --feature systemd: 19 entries, 0 errors
leaks --feature systemd:       0 residual occurrences
```

### Touch Bar

Commit: `195673c75 Remove remaining Touch Bar support`

Removed the remaining `HAVE(TOUCH_BAR)` IPC/UI surface, including
`WebPageProxy` messages, members and Cocoa Touch Bar hooks.

Verification:

```text
equivalence --feature touch-bar: 26 entries, 0 errors, 1 stale warning
```

The warning is a manifest entry for `HTMLMenuItemElement.cpp`, which upstream
already removed. It is informational.

### libnotify

libnotify is already absent upstream. The only pending entry was a WebRTC
symbol (`createRTCProvider`) misattributed to libnotify by the net-diff
bootstrap; it was moved to the `webrtc` feature in the manifest.

### FTP

Commit: `aad3b2584 Remove FTP Support`

Removed:

- The `FTP` URL scheme from `URLParser` (including `defaultPortForProtocol`).
- `URL::protocolIsInFTPFamily()`, the FTP/FTPS entries in `hasSpecialScheme()`
  and the port 21/22 exception in `portAllowed()`.
- `ProtectionSpace::ServerType::FTP/FTPS/ProxyFTP` and all platform mappings
  (soup, curl, cocoa, serialization, C API cast).
- The 2.54-only `ftpEnabled` plumbing: creation parameters, `NetworkProcess`
  state, `NetworkDataTask`/`NetworkLoad`/`BackgroundFetchLoad`/`PingLoad`/
  `NetworkCORSPreflightChecker` blocking paths, `ftpDisabledError()` and the
  `wasBlockedByDisabledFTP()` client hook.
- `FTPEnabled`, the deprecated `WKPreferences*FTP*` stubs and the FTP
  protection-space C API constants.
- Dead FTP directory-list leftovers (`unknownFileSizeText()`, logging channel).

Verification:

```text
equivalence --feature ftp: 82 entries, 0 errors, 0 warnings
leaks --feature ftp:       0 residual occurrences
make build DISTRO=debian:  success (WebKit + MiniBrowser)
make smoke DISTRO=debian:  success
```

Notes:

- The manifest used stale 2.34.1 symbol names
  (`ProtectionSpaceServerFTP`); it now names the 2.54.2 enum
  (`ProtectionSpaceBaseServerType::FTP`), so `equivalence` actually checks it.
- Removing the C API constants renumbers `kWKProtectionSpaceProxyType*` and
  the other `kWKProtectionSpace*` values; this is an intentional fork-level API
  break.
- `ftp://` and `ftps://` are now non-special schemes and fail as unsupported
  loads.

### Gamepad

Commit: `eea8c5a8e Remove Gamepad API`

Removed:

- WebCore `Modules/gamepad` (Gamepad, GamepadButton, GamepadEvent,
  GamepadHapticActuator, effect/haptic types, NavigatorGamepad,
  GamepadManager) and `platform/gamepad` providers (Manette, libwpe,
  Cocoa GameController/HID, mocks).
- Event names/categories, DOM window handlers, builtin names, the Permissions
  Policy `gamepad` feature, the `TaskSource` entry and the preferences
  (`GamepadsEnabled`, trigger rumble, vibration actuator).
- WebKit UI/Web-process gamepad IPC (`GamepadData`, `UIGamepad*`,
  `WebGamepad*`), process-pool tracking, page activity hysteresis,
  page-client/delegate hooks, C API test hooks and `WKPreferences`
  getters/setters.
- The `ENABLE_GAMEPAD` build option, `libmanette` detection, the container
  dependency and the WPEPlatform `WPEGamepad*` API.
- GameController framework soft-linking is retained, but only for
  pointer-lock mouse input (`GameControllerMouseSoftLink`).

Verification:

```text
equivalence --feature gamepad: 336 entries, 0 errors, 0 warnings
leaks --feature gamepad:       0 residual occurrences
make build DISTRO=debian:      success (WebKit + MiniBrowser)
make smoke DISTRO=debian:      success
```

Notes:

- `equivalence` cannot index declarations inside `#if ENABLE(GAMEPAD)` blocks
  or nested class enums, so those manifest rows are documentation only. The
  leak tokens (`Gamepad`, `gamepad`, `Manette`, …) are the regression guard.
- The `gamepad` candidate contained bogus `CMakeLists.txt` rows from the
  net-diff bootstrap; they were removed from the manifest.

## 5. Next steps

### Next: Device Motion and Orientation

Gamepad is done. The next fingerprinting cleanup is the Device Motion and
Orientation API pair, which is smaller than Geolocation and self-contained.

- Remove `Source/WebCore/dom/DeviceMotion*`, `DeviceOrientation*` and the
  related navigator/window IDL, clients and mock providers.
- Curate the `device-sensors` candidate first: it currently contains
  `PermissionName.idl` enum values (`camera`, `bluetooth`, `geolocation`, …)
  that belong to other features and must not be removed here.
- Remove `DEVICE_ORIENTATION`/`ORIENTATION_EVENTS` build flags and preferences
  only after confirming nothing else uses them.

### Then: Geolocation

After device sensors, remove Geolocation and the Geoclue integration. This is
larger: it covers the WebCore API, the UI-process Geoclue provider, the GLib
geolocation manager, IPC messages, settings and permission plumbing.

### Then: remaining features

Use the target-relative report to choose the next feature, and always verify
with `equivalence --fork HEAD --feature <id>` and `leaks --feature <id>`.

Suggested order:

1. Device Motion/Orientation, then Geolocation.
2. Remaining platform and dependency removals (libsecret, libnotify, geoclue).
3. Media stack (capture, streams, recorder, EME, PiP/fullscreen).
4. Automation, WebDriver, quirks and user-agent quirks.
5. Remaining build/behaviour fixes.

### Curation

Resolve `needs-review.tsv`. The 19 entries are mostly legacy `:bug:` commits
that touch source files without an obvious feature owner. The most important
ones are:

- IndexedDB origin leak fix.
- MEMMEM and X11 `None` fixes.
- GTK/GLib API fixes.
- Context menu and media-control fixes.
- DerivedSources and missing HTML tag fixes.

## 6. Build and test state

The 2.54.2 tree configures successfully with:

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

Both build containers now build the full tree and run MiniBrowser:

```text
make build DISTRO=debian     -> success (WebKit + MiniBrowser)
make smoke DISTRO=debian     -> success (MiniBrowser --help under Xvfb)
make build DISTRO=archlinux  -> success (WebKit + MiniBrowser)
make smoke DISTRO=archlinux  -> success (MiniBrowser --help under Xvfb)
```

The host itself does not need Ruby because the build runs inside the
containers. Both images are also validated by a full end-to-end compile.

Build caveats:

- `ENABLE_DOCUMENTATION=OFF` avoids needing the full gtkdoc toolchain.
- `ENABLE_SPEECH_SYNTHESIS=OFF` avoids Flite.
- `USE_LIBBACKTRACE=OFF` avoids libbacktrace.
- `USE_SYSTEM_UNIFDEF=OFF` uses the bundled unifdef.
- A real build still needs Ruby for JavaScriptCore generators.

## 7. Reproducible Podman builds

The repository provides rootless Podman build environments:

- [containers/debian.Containerfile](./containers/debian.Containerfile)
- [containers/archlinux.Containerfile](./containers/archlinux.Containerfile)
- [Makefile](./Makefile)

The containers are based on:

- Debian 13 (`docker.io/library/debian:13`)
- Arch Linux (`docker.io/library/archlinux:latest`, minimal rolling base)

They install the GTK4, libsoup3, GStreamer, graphics, Ruby, Python, Perl,
toolchain and smoke-test dependencies, then build from the mounted repository.

Typical usage:

```bash
# Build the Debian container image.
make image DISTRO=debian

# Configure and build WebKit in build/debian.
make build DISTRO=debian

# Build the Arch Linux equivalent.
make build DISTRO=archlinux

# Open an interactive shell in the build container.
make shell DISTRO=debian

# Run the MiniBrowser smoke test (needs Xvfb).
make smoke DISTRO=debian

# Inspect or clear the persistent compiler cache.
make ccache-stats DISTRO=debian
make ccache-clean DISTRO=debian

# Remove local build directories (keeps ccache).
make clean

# Remove builds, ccache and container images.
make distclean
```

The build directory is `build/<distro>` and is intentionally ignored by Git.
The container runs as a user matching the host UID/GID via Podman's
`--userns=keep-id`, so build artifacts are owned by the invoking user.

Compiler caching is persistent and per-distribution:

- The host cache lives at `build/ccache/<distro>`.
- It is mounted into the container at `/ccache`.
- `CCACHE_BASEDIR=/work` and `CCACHE_MAXSIZE=20G` are set by the Makefile.
- `make clean` removes the build output but keeps the ccache.
- `make distclean` removes both build output and cache.

Useful cache commands:

```bash
make ccache-stats DISTRO=debian
make ccache-clean DISTRO=debian
```

A repeated MiniBrowser compilation was verified to hit the shared cache
across separate `podman run` invocations on both Debian and Arch Linux.

If Podman is not installed, install it through the host package manager and
verify rootless operation:

```bash
podman info --format '{{.Host.Security.Rootless}}'
```

## 8. Known limitations and risks

- Feature attribution is generated from history and net diffs; bundled commits
  can misattribute symbols. `needs-review.tsv` and manual manifest edits are
  part of the workflow.
- `plan` is target-relative; use `equivalence` for actual fork state.
- `migrate` generates candidates, not guaranteed patches. Some hunks need
  `patch -p1` rather than `git apply` because upstream context has moved.
- New upstream files are detected by feature directories/tokens and reported,
  but they still require review before deletion.
- Behavioural changes (quirks, security fixes, build fixes) are manual entries.
- The WebGPU XR layer was removed together with WebXR. WebGPU itself remains a
  pending feature in `SECURITY.md`/`TODO.md`.
- Historical `NEWS`, Xcode project metadata and third-party code are excluded
  from leak scans.

## 9. Key references

- [README.md](./README.md) — project overview and host build instructions.
- [HOW-TO-MIGRATE.md](./HOW-TO-MIGRATE.md) — end-to-end migration runbook and lessons learned.
- [SECURITY.md](./SECURITY.md) — removed/pending API and dependency policy.
- [TODO.md](./TODO.md) — remaining removal candidates and legacy concerns.
- [NOTES.md](./NOTES.md) — architecture and generated-binding notes.
- [Helpers/upstreamcheck/README.md](./Helpers/upstreamcheck/README.md) — tool usage.
- [Helpers/upstreamcheck/MIGRATION.md](./Helpers/upstreamcheck/MIGRATION.md) — migration plan and progress.
- [Helpers/upstreamcheck/manifest/divergence.tsv](./Helpers/upstreamcheck/manifest/divergence.tsv) — authoritative divergence manifest.
- [Helpers/upstreamcheck/manifest/needs-review.tsv](./Helpers/upstreamcheck/manifest/needs-review.tsv) — ambiguous legacy commits.
- [containers/debian.Containerfile](./containers/debian.Containerfile) — Debian 13 build container.
- [containers/archlinux.Containerfile](./containers/archlinux.Containerfile) — Arch Linux build container.
- [Makefile](./Makefile) — Podman build/test entry points.

## 10. Suggested first actions for the next maintainer

1. `git switch migrate/2.54`
2. `cd Helpers/upstreamcheck && go build -o upstreamcheck ./cmd/upstreamcheck`
3. `make build DISTRO=debian` (or `archlinux`) to establish a full baseline
   build of the migrated tree.
4. `./upstreamcheck plan --feature device-sensors --target vendor/2.54.2`
5. Curate the `device-sensors` candidate, then use `./upstreamcheck migrate
   --feature device-sensors ...` and verify with `equivalence` and `leaks`.
6. Curate `needs-review.tsv` while build iterations run.
