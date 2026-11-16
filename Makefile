SHELL := /bin/bash

# Rootless Podman build entry points for RetroKit.
#
# Examples:
#   make image DISTRO=debian
#   make build DISTRO=debian
#   make build DISTRO=archlinux
#   make shell DISTRO=debian
#   make smoke DISTRO=debian
#   make images
#   make build-all

DISTRO ?= debian
DISTROS := debian archlinux

PODMAN ?= podman
IMAGE ?= retrokit-builder-$(DISTRO)
BUILD_DIR ?= build/$(DISTRO)
CCACHE_DIR ?= $(CURDIR)/build/ccache/$(DISTRO)
CCACHE_MAXSIZE ?= 20G
BUILD_TYPE ?= Release
JOBS ?= $(shell nproc)
CONTAINER_UID ?= 1000
CONTAINER_GID ?= 1000
VOLUME_SUFFIX ?= :Z

REPO := $(CURDIR)
CONTAINERFILE := containers/$(DISTRO).Containerfile

# Keep the build compatible with the migrated 2.54.2 tree and the minimal
# Source + Tools closure. Override CMAKE_FLAGS on the command line if needed.
CMAKE_FLAGS ?= \
	-DPORT=GTK \
	-DCMAKE_BUILD_TYPE=$(BUILD_TYPE) \
	-DENABLE_MINIBROWSER=ON \
	-DENABLE_DOCUMENTATION=OFF \
	-DENABLE_API_TESTS=OFF \
	-DENABLE_LAYOUT_TESTS=OFF \
	-DENABLE_SPEECH_SYNTHESIS=OFF \
	-DUSE_LIBBACKTRACE=OFF \
	-DUSE_SYSTEM_UNIFDEF=OFF \
	-DBWRAP_EXECUTABLE=/usr/bin/bwrap \
	-DDBUS_PROXY_EXECUTABLE=/usr/bin/xdg-dbus-proxy

PODMAN_RUN_BASE = $(PODMAN) run --rm \
	--userns=keep-id:uid=$(CONTAINER_UID),gid=$(CONTAINER_GID) \
	--user builder \
	--shm-size=2g \
	--ulimit nofile=65536:65536 \
	-e CCACHE_DIR=/ccache \
	-e CCACHE_BASEDIR=/work \
	-e CCACHE_MAXSIZE=$(CCACHE_MAXSIZE) \
	-v "$(REPO):/work$(VOLUME_SUFFIX)" \
	-v "$(CCACHE_DIR):/ccache$(VOLUME_SUFFIX)" \
	-w /work

PODMAN_RUN = $(PODMAN_RUN_BASE) $(IMAGE)

.PHONY: help check-distro image images build build-all configure compile shell smoke test ccache-stats ccache-clean clean distclean

help:
	@echo "RetroKit rootless Podman build targets"
	@echo
	@echo "  make image DISTRO=debian|archlinux   Build the build container image"
	@echo "  make images                          Build both container images"
	@echo "  make build DISTRO=debian|archlinux   Configure and compile WebKit"
	@echo "  make build-all                       Build both distributions"
	@echo "  make configure DISTRO=...            Configure only"
	@echo "  make compile DISTRO=...              Compile an existing configuration"
	@echo "  make shell DISTRO=...                Interactive shell in the container"
	@echo "  make smoke DISTRO=...                Run MiniBrowser under Xvfb"
	@echo "  make ccache-stats DISTRO=...         Show compiler cache statistics"
	@echo "  make ccache-clean DISTRO=...         Delete this distro's compiler cache"
	@echo "  make clean DISTRO=...                Remove build/$(DISTRO) (keeps ccache)"
	@echo "  make distclean                       Remove build outputs, ccache and images"
	@echo
	@echo "Variables: BUILD_TYPE=$(BUILD_TYPE), JOBS=$(JOBS), IMAGE=$(IMAGE)"
	@echo "           CCACHE_DIR=$(CCACHE_DIR)"
	@echo "           CCACHE_MAXSIZE=$(CCACHE_MAXSIZE)"
	@echo "Override CMAKE_FLAGS to change configure options."

check-distro:
	@case "$(DISTRO)" in \
		debian|archlinux) ;; \
		*) echo "Unknown DISTRO=$(DISTRO); expected debian or archlinux" >&2; exit 1 ;; \
	esac

$(CCACHE_DIR):
	mkdir -p "$@"

image: check-distro
	$(PODMAN) build --pull=missing -t $(IMAGE) -f $(CONTAINERFILE) containers

images:
	$(MAKE) image DISTRO=debian
	$(MAKE) image DISTRO=archlinux

configure: image $(CCACHE_DIR)
	$(PODMAN_RUN) bash -lc 'cmake -S . -B $(BUILD_DIR) -GNinja $(CMAKE_FLAGS)'

compile: image $(CCACHE_DIR)
	$(PODMAN_RUN) bash -lc 'cmake --build $(BUILD_DIR) --parallel $(JOBS)'

build: image $(CCACHE_DIR)
	$(PODMAN_RUN) bash -lc 'cmake -S . -B $(BUILD_DIR) -GNinja $(CMAKE_FLAGS) && cmake --build $(BUILD_DIR) --parallel $(JOBS)'

build-all:
	$(MAKE) build DISTRO=debian
	$(MAKE) build DISTRO=archlinux

shell: image $(CCACHE_DIR)
	$(PODMAN_RUN_BASE) -it $(IMAGE) bash

smoke: image $(CCACHE_DIR)
	$(PODMAN_RUN) bash -lc 'timeout 30s xvfb-run -a dbus-run-session -- $(BUILD_DIR)/bin/MiniBrowser --help'

test: smoke

ccache-stats: image $(CCACHE_DIR)
	$(PODMAN_RUN) ccache -s

ccache-clean: $(CCACHE_DIR)
	rm -rf "$(CCACHE_DIR)"

clean:
	rm -rf "$(BUILD_DIR)"

distclean:
	rm -rf build
	-$(PODMAN) rmi -f retrokit-builder-debian retrokit-builder-archlinux
