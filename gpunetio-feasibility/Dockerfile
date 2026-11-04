# GPUNetIO Feasibility PoC — DOCA sample build environment
#
# Builds the official gpunetio_rdma_client_server_write sample from DOCA SDK.
# The devel image includes source code, meson build system, and CUDA toolkit.
#
# Usage:
#   docker build -t gpunetio-poc poc/gpunetio-feasibility/
#   # On a node with GPU + ConnectX NIC:
#   docker run --rm -it --gpus all --cap-add IPC_LOCK \
#     --device /dev/infiniband gpunetio-poc --help

# DOCA 3.4.0 changed tag format: devel-cuda{VER}-{DOCA_VER}-devel-host
ARG DOCA_IMAGE=nvcr.io/nvidia/doca/doca:devel-cuda13.0.0-3.4.0-devel-host

FROM ${DOCA_IMAGE} AS builder

# Without pipefail, `cmd | tail` returns tail's exit status, so a failing
# meson or ninja would pass silently. SHELL applies to this stage only.
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ARG SAMPLE_DIR=/opt/mellanox/doca/samples/doca_gpunetio/gpunetio_rdma_client_server_write

# libdoca_gpunetio_device.a is built with CUDA 13.2, but the image ships
# CUDA 13.0. Upgrade nvcc/nvlink to 13.2 so nvlink accepts the device lib.
RUN apt-get update -qq && \
    apt-get install -y --no-install-recommends cuda-nvcc-13-2 && \
    rm -rf /var/lib/apt/lists/*

ENV PATH="/usr/local/cuda-13.2/bin:${PATH}"

# Fixes in the official sample source, applied before the build.
# 0001: create_mmap() sets the memory range twice and always fails
#       (https://github.com/NVIDIA-DOCA/doca-samples/issues/7).
COPY patches/ /tmp/patches/
RUN patch -d /opt/mellanox/doca/samples/doca_gpunetio -p1 --forward \
        < /tmp/patches/0001-rdma_common-remove-duplicate-set-memrange.patch

# DOCA 3.4.0: each sample has its own meson.build with project() declaration,
# so we can build directly from the sample directory (no amalgamation needed).
RUN cd ${SAMPLE_DIR} && \
    meson setup /tmp/build . --buildtype=release 2>&1 | tail -40 && \
    ninja -C /tmp/build 2>&1 | tail -40

RUN BINARY=$(find /tmp/build -name "doca_gpunetio_rdma_client_server_write" -type f -executable 2>/dev/null | head -1) && \
    test -n "$BINARY" && \
    echo "Built binary: $BINARY" && \
    cp "$BINARY" /tmp/gpunetio_rdma_write

# GDRCopy — GPUNetIO "Installation and Setup", GDRCopy Installation.
# Only the lib target: the driver (gdrdrv) is built and loaded on the host,
# and the container shares the host kernel. Pinned to the commit the hosts
# built, so libgdrapi matches the loaded gdrdrv 2.6.
ARG GDRCOPY_COMMIT=fcec3ce0bb40a97a6cc45dd4afeec4bccb509712
RUN git clone https://github.com/NVIDIA/gdrcopy.git /opt/mellanox/gdrcopy && \
    git -C /opt/mellanox/gdrcopy checkout ${GDRCOPY_COMMIT} && \
    make -C /opt/mellanox/gdrcopy lib

# --- Runtime stage ---
# Use the same devel image for runtime — it includes all DOCA/CUDA shared
# libraries needed by the sample. PoC size is not a concern here.
FROM ${DOCA_IMAGE}

COPY --from=builder /tmp/gpunetio_rdma_write /usr/local/bin/gpunetio_rdma_write
# The search path is set by LD_LIBRARY_PATH in the Pod manifest.
COPY --from=builder /opt/mellanox/gdrcopy/src/libgdrapi.so* /opt/mellanox/gdrcopy/src/

ENTRYPOINT ["gpunetio_rdma_write"]
