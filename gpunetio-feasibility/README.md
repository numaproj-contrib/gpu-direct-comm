# GPUNetIO Feasibility PoC

DOCA GPUNetIO `gpunetio_rdma_client_server_write` サンプルを K8s Pod 間で動作させ、
GPU→GPU 直接通信（GPUDirect RDMA）の実現可能性を検証する PoC。

本ディレクトリには**成果物（コード）のみ**を置く。

| ファイル | 概要 |
|---|---|
| [Dockerfile](./Dockerfile) | **G3-M1-3 で使用予定**: DOCA 公式サンプルをビルドするコンテナイメージ定義。`doca_perftest` は DOCA devel イメージに同梱されているため、G3-M1-2 の 4 段検証では使用しない |
| [k8s/perftest-dra.yaml](./k8s/perftest-dra.yaml) | **G3-M1-2**: `doca_perftest` で Pod 内 4 段（通常 RDMA / GPUDirect Host / GPUDirect GPU / GDAKI）を検証するマニフェスト。NumaNetwork（VF + IP）+ GPU ResourceClaim + server/client Pod |
| [k8s/gpunetio-poc.yaml](./k8s/gpunetio-poc.yaml) | **G3-M1-3 で使用予定**: DOCA 公式サンプル（`gpunetio_rdma_client_server_write`）を Pod 間で動かすマニフェスト。VF を素の `ResourceClaim` で取るため IP は付かない |
| [verify-baremetal-env.sh](./verify-baremetal-env.sh) | ベアメタルノードのホスト側前提確認（揮発性設定の再適用漏れ検出）。各ノード上で実行する |
