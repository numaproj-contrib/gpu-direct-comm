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

ARG SAMPLE_DIR=/opt/mellanox/doca/samples/doca_gpunetio/gpunetio_rdma_client_server_write

# libdoca_gpunetio_device.a is built with CUDA 13.2, but the image ships
# CUDA 13.0. Upgrade nvcc/nvlink to 13.2 so nvlink accepts the device lib.
RUN apt-get update -qq && \
    apt-get install -y --no-install-recommends cuda-nvcc-13-2 && \
    rm -rf /var/lib/apt/lists/*

ENV PATH="/usr/local/cuda-13.2/bin:${PATH}"

# DOCA 3.4.0: each sample has its own meson.build with project() declaration,
# so we can build directly from the sample directory (no amalgamation needed).
RUN cd ${SAMPLE_DIR} && \
    meson setup /tmp/build . --buildtype=release 2>&1 | tail -40 && \
    ninja -C /tmp/build 2>&1 | tail -40

RUN BINARY=$(find /tmp/build -name "doca_gpunetio_rdma_client_server_write" -type f -executable 2>/dev/null | head -1) && \
    test -n "$BINARY" && \
    echo "Built binary: $BINARY" && \
    cp "$BINARY" /tmp/gpunetio_rdma_write

# --- Runtime stage ---
# Use the same devel image for runtime — it includes all DOCA/CUDA shared
# libraries needed by the sample. PoC size is not a concern here.
FROM ${DOCA_IMAGE}

COPY --from=builder /tmp/gpunetio_rdma_write /usr/local/bin/gpunetio_rdma_write

ENTRYPOINT ["gpunetio_rdma_write"]
