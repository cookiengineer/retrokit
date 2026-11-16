# Reproducible WebKitGTK build environment for RetroKit.
#
# Arch Linux is a rolling distribution. This image is intentionally based on
# the official minimal Arch image, which is equivalent to a rolling "latest"
# snapshot. For byte-for-byte reproducible images, pin the base image digest or
# use an archived Arch Linux image tag.
#
# Build:
#   podman build -t retrokit-builder-archlinux -f containers/archlinux.Containerfile containers
#
# Run from the repository root through the Makefile. The source tree is mounted
# at /work and the container runs as UID/GID 1000 via Podman keep-id.

FROM docker.io/library/archlinux:latest

RUN pacman -Syu --noconfirm --needed \
    base-devel \
    clang \
    lld \
    cmake \
    ninja \
    ruby \
    python \
    perl \
    git \
    pkgconf \
    gperf \
    bison \
    flex \
    gawk \
    gettext \
    patch \
    which \
    shadow \
    ca-certificates \
    curl \
    xz \
    unzip \
    tar \
    gtk4 \
    libsoup3 \
    glib2 \
    cairo \
    pango \
    gdk-pixbuf2 \
    harfbuzz \
    harfbuzz-icu \
    icu \
    libjpeg-turbo \
    libpng \
    libwebp \
    libavif \
    libjxl \
    openjpeg2 \
    lcms2 \
    woff2 \
    libxml2 \
    libxslt \
    sqlite \
    libepoxy \
    mesa \
    libdrm \
    wayland \
    wayland-protocols \
    libx11 \
    libxcb \
    libxkbcommon \
    libxext \
    libxrender \
    libxi \
    libxtst \
    libxfixes \
    libxcomposite \
    libxdamage \
    libxrandr \
    libxinerama \
    libxcursor \
    atk \
    at-spi2-core \
    libgudev \
    enchant \
    hyphen \
    libmanette \
    libgcrypt \
    libtasn1 \
    libseccomp \
    libsecret \
    gobject-introspection \
    gstreamer \
    gst-plugins-base \
    gst-plugins-good \
    gst-plugins-bad \
    gst-plugins-ugly \
    libevent \
    libnghttp2 \
    libproxy \
    libpsl \
    gnutls \
    brotli \
    zstd \
    libsysprof-capture \
    bubblewrap \
    xdg-dbus-proxy \
    dbus \
    xorg-server-xvfb \
    xorg-xauth \
  && pacman -Scc --noconfirm

RUN useradd --create-home --uid 1000 --shell /bin/bash builder \
  && git config --system --add safe.directory /work

USER builder
ENV HOME=/home/builder
WORKDIR /work

CMD ["/bin/bash"]
