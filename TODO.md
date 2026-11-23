# TODO — WebKitGTK 2.54 migration backlog

This is the remaining work on `migrate/2.54`. Completed features and their
verification are tracked in [handover.md](./handover.md); the authoritative
removal policy is [SECURITY.md](./SECURITY.md).

Completed on `migrate/2.54`:

```text
WebXR · SystemD/journald · Touch Bar · FTP · Gamepad
Device Motion/Orientation · Geolocation · libsecret · Media Usage
Web API Statistics · Legacy Custom Protocol · PlayStation platform
Web Speech
```

## In progress — media capture / WebRTC / media recorder

The first media commit (`72ba05388`) removed the sources but left the tree
broken and unverified. The follow-up work is split as follows.

### Media capture — committed as WIP (`12ea622f2`), build not verified

`equivalence --feature media-capture` is clean (0 errors, 11 stale-path
warnings). `leaks --feature media-capture` still reports 107 occurrences.

- [ ] Finish a clean `make build DISTRO=debian`, then `make smoke`.
      The last attempt was stopped during WebCore. The commit already restores
      the SampleBuffer files, removes the orphaned `JSBlobEvent` and RTC
      binding includes, and disables `USE_LIBRICE`.
- [ ] Build and smoke `DISTRO=archlinux`.
- [ ] Remove the remaining capture leaks. Most are Cocoa/iOS API that the GTK
      build does not compile: WKWebView/UIDelegate capture states, AVFoundation
      capture soft links, MediaCapabilities display capture, `PageClient`
      capture callbacks.
- [ ] Add manifest rows for the capture deletions and 2.54-only files.

### WebRTC

`equivalence` 14 errors, `leaks` 222 occurrences.

- [ ] Mock RTC handlers (`RTCDataChannelHandlerMock`, `RTCNotifiersMock`,
      `TimerEventBasedMock`).
- [ ] `RTCNetwork`, `RTCPacketOptions` and the `WebProcess` /
      `NetworkProcessConnection` users.
- [ ] Worker `RTCRtpScriptTransform` / `RTCRtpScriptTransformer`.
- [ ] `NetworkConnectionToWebProcess::createRTCProvider`,
      `MessageSource::WebRTC`.
- [ ] Inspector RTC models, `RTCLogsCallback`, `SaferCPPExpectations`,
      `NativeFunctionParameters.js`.
- [ ] Remove the remaining `ENABLE(WEB_RTC)` / `USE_LIBRICE` / `WebRTC`
      flags and preferences.

### Media recorder

`equivalence` 0 errors, 5 leak occurrences.

- [ ] `ENABLE(MEDIA_RECORDER)` guards in `Quirks.cpp` and
      `AudioSampleBufferConverter`.
- [ ] `Internals.setCustomPrivateRecorderCreator` hook.
- [ ] The WebM preference description/condition that mentions MediaRecorder.
- [ ] Remove `ENABLE_MEDIA_RECORDER` / `ENABLE_MEDIA_RECORDER_WEBM` from WTF
      and `GStreamerChecks.cmake`.

### Manifest and docs

- [ ] Add per-file rows for the 734 first-party deletions, the 2.54-only
      media/WebRTC files and `Source/ThirdParty/libwebrtc` (19,510 files).
- [ ] Attribute shared `mediastream` files once (media-capture) rather than
      overlapping with WebRTC/recorder.
- [ ] Move the three features to completed only after build + smoke pass.

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
- [ ] Remove Wireless Playback Target (`wireless-playback`)
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

## Tooling

- [x] Add the `retained` disposition (positive invariant) to `upstreamcheck`
- [x] Curate the media/speech rows and add the `web-speech` feature
- [x] Drop the Xcode-only `*.xcfilelist` files and check scripts
- [ ] Add a helper that flags orphaned generated-source build entries
      (`JS<Name>.cpp` without an IDL) so clean builds stop exposing them

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
