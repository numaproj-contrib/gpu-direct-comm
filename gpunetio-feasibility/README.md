# GPUNetIO Feasibility PoC

DOCA GPUNetIO `gpunetio_rdma_client_server_write` サンプルを K8s Pod 間で動作させ、
GPU→GPU 直接通信（GPUDirect RDMA）の実現可能性を検証する PoC。

本ディレクトリには**成果物（コード）のみ**を置く。

| ファイル | 概要 |
|---|---|
| [Dockerfile](./Dockerfile) | DOCA 公式サンプルをビルドするコンテナイメージ定義 |
| [k8s/](./k8s/) | DRA ResourceClaim + Pod マニフェスト |
| [verify-baremetal-env.sh](./verify-baremetal-env.sh) | ベアメタルノードのホスト側前提確認（揮発性設定の再適用漏れ検出）。各ノード上で実行する |
