# Reproducible WebKitGTK build environment for RetroKit.
#
# Build:
#   podman build -t retrokit-builder-debian -f containers/debian.Containerfile containers
#
# Run from the repository root through the Makefile. The source tree is mounted
# at /work and the container runs as UID/GID 1000 via Podman keep-id.

FROM docker.io/library/debian:13

ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    clang \
    lld \
    cmake \
    ninja-build \
    pkg-config \
    ccache \
    ruby \
    python3 \
    perl \
    git \
    patch \
    gperf \
    bison \
    flex \
    gawk \
    gettext \
    which \
    passwd \
    ca-certificates \
    curl \
    xz-utils \
    libglib2.0-dev \
    libglib2.0-bin \
    gobject-introspection \
    libgirepository1.0-dev \
    libgtk-4-dev \
    libcairo2-dev \
    libpango1.0-dev \
    libgdk-pixbuf-2.0-dev \
    libsoup-3.0-dev \
    libxml2-dev \
    libxslt1-dev \
    libsqlite3-dev \
    libjpeg-dev \
    libpng-dev \
    libwebp-dev \
    libavif-dev \
    libjxl-dev \
    libopenjp2-7-dev \
    liblcms2-dev \
    libwoff-dev \
    libharfbuzz-dev \
    libharfbuzz-icu0 \
    libicu-dev \
    libfreetype-dev \
    libfontconfig1-dev \
    libepoxy-dev \
    libegl1-mesa-dev \
    libgles2-mesa-dev \
    mesa-common-dev \
    libgl1-mesa-dri \
    libdrm-dev \
    libgbm-dev \
    libwayland-dev \
    wayland-protocols \
    libx11-dev \
    libxcb1-dev \
    libxcb-glx0-dev \
    libxkbcommon-dev \
    libxkbcommon-x11-dev \
    libxext-dev \
    libxrender-dev \
    libxi-dev \
    libxtst-dev \
    libxfixes-dev \
    libxcomposite-dev \
    libxdamage-dev \
    libxrandr-dev \
    libxinerama-dev \
    libxcursor-dev \
    libatk1.0-dev \
    libatspi2.0-dev \
    libgudev-1.0-dev \
    libenchant-2-dev \
    libhyphen-dev \
    libmanette-0.2-dev \
    libgcrypt20-dev \
    libtasn1-6-dev \
    libseccomp-dev \
    libsecret-1-dev \
    libgstreamer1.0-dev \
    libgstreamer-plugins-base1.0-dev \
    libgstreamer-plugins-bad1.0-dev \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    libevent-dev \
    libnghttp2-dev \
    libproxy-dev \
    libpsl-dev \
    libgnutls28-dev \
    libbrotli-dev \
    libzstd-dev \
    libsysprof-capture-4-dev \
    bubblewrap \
    xdg-dbus-proxy \
    dbus \
    dbus-x11 \
    xvfb \
    xauth \
  && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --uid 1000 --shell /bin/bash builder \
  && git config --system --add safe.directory /work

USER builder
ENV HOME=/home/builder
WORKDIR /work

CMD ["/bin/bash"]
