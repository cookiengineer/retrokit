# TODO — WebKitGTK 2.54 migration backlog

This is the remaining work on `migrate/2.54`. Completed features and their
verification are tracked in [handover.md](./handover.md); the authoritative
removal policy is [SECURITY.md](./SECURITY.md).

Completed on `migrate/2.54`:

```text
WebXR · SystemD/journald · Touch Bar · FTP · Gamepad
Device Motion/Orientation · Geolocation · libsecret · Media Usage
Web API Statistics · Legacy Custom Protocol · PlayStation platform
```

## In progress — media capture / WebRTC / media recorder

Commit `72ba05388` removes the source for `media-capture`, `webrtc` and
`media-recorder`. Debian builds; the feature is **not verified yet**.

Prioritized first:

- [ ] Run `make smoke DISTRO=debian`.
- [ ] Run `make build DISTRO=archlinux && make smoke DISTRO=archlinux`.
- [ ] Remove the temporary compatibility shims until `equivalence` is clean:
      `WKPreferences` capture/WebRTC getters, GTK `WebKitSettings` media
      properties, `WebProcessProxy` capture state, `MediaProducer` capture
      state, `APIUIClient` user-media callbacks, `PermissionName` media names,
      `AudioContext` MediaStream factories, process-pool capture options,
      `HTMLInputElement` `capture`, and the no-op preferences listed in
      [handover.md](./handover.md).
- [ ] Remove the remaining `#if ENABLE(MEDIA_STREAM)` / `ENABLE(WEB_RTC)`
      blocks (121 / 151 / 9 leak hits) and clean `EventInterfaces.in`,
      `EventTargetFactory.in`, `WebCoreBuiltinNames.h` and inspector settings.
- [ ] Curate `divergence.tsv` for the 734 first-party deletions, the 19,510
      `Source/ThirdParty/libwebrtc` files and the new 2.54 files.
- [ ] Only then mark `media-capture`, `webrtc` and `media-recorder` complete.

## Next up

- [ ] Encrypted media (`encrypted-media`), payment (`payment`),
      fullscreen/PiP (`fullscreen-pip`) and wireless playback
      (`wireless-playback`).
- [ ] WebGL / WebGL2 — the largest remaining feature; plan this separately
      from the media stack.

## Fingerprinting and web APIs

- [ ] Remove WebGL and WebGL2 (`WEBGL`, `WEBGL2`, `GraphicsContextGL`)
- [ ] Remove Payment Request and Apple Pay (`payment`)
- [ ] Remove Encrypted Media and Thunder (`encrypted-media`)
- [ ] Remove Fullscreen and Picture-in-Picture (`fullscreen-pip`)
- [ ] Remove WebRTC (`webrtc`) — in progress, see above
- [ ] Remove Wireless Playback Target (`wireless-playback`)
- [ ] Remove Media Capture/Streams (`media-capture`) — in progress, see above
- [ ] Remove Media Recorder (`media-recorder`) — in progress, see above
- [ ] Remove Media Statistics (`media-statistics`, 3 work items)
- [ ] Remove remaining site-specific quirks (`quirks`, 2 work items)
- [ ] Remove Web Automation/WebDriver (`automation`)
- [ ] Remove remaining legacy plugin surface (`plugins`)

## Platform and dependency cleanup

- [ ] Remove watchOS platform support (`platform-watchos`)
- [ ] Remove ARKit/Model Element residue (`arkit-model`)
- [ ] Remove sleep disablers (`sleep-disablers`)
- [ ] Decide on translation catalogs (`translations`)
- [ ] Curate the `needs-review.tsv` legacy `:bug:` commits (19 entries)

## Graphics / WebGL internals

These are the parts that need a dedicated plan once the WebGL feature starts:

- [ ] `GSTREAMER_GL`
- [ ] `GraphicsContextGL`
- [ ] `Nicosia:GCGLLayer`
- [ ] `GraphicsContextGLTextureMapper`
- [ ] `TextureMapperGCGLPlatformLayer`

Undecided, to be reviewed before removing:

- [ ] `USE_ANGLE` compiler flag
- [ ] `USE_ANGLE_EGL` compiler flag
- [ ] `OpenGLShims`
- [ ] `TemporaryOpenGLSetting`
- [ ] `NicosiaGCGLANGLELayer`

## Legacy leftovers still present in 2.54.2

- [ ] `InstallMissingMediaPluginsPermissionRequest`
- [ ] `ScriptExecutionContext::allowsMediaDevices()`
- [ ] Incremental PDF loader (`PDFIncrementalLoader`, mac-only plugin path)
- [ ] PDF printing helpers (`headerHeightForPrinting`,
      `footerHeightForPrinting`, `drawHeaderForPrinting`,
      `drawFooterForPrinting`, `savePDFToFileInDownloadsFolder`)
- [ ] `omitPDFSupport`, `UseImageDocumentForSubframePDF`
- [ ] Decide whether the remaining PDF plugin surface is in scope for GTK

## WebInspectorUI

- [ ] Remove `three.js` from WebInspectorUI
- [ ] Remove Esprima from WebInspectorUI
- [ ] Re-check the inspector resource bundle after the media/inspector
      removals
