# GPUNetIO Feasibility PoC

gpunetio-feasibility ディレクトリはブランチfeature/g3-m1-gpunetio-feasibilityでのみ使用する．これをdevelopにマージする際は不要情報を省くこと．
verify-gpunetio-baremetal-env.sh は，対応する手順書がリポジトリに無いため，マージの際に hack/ に上げる（setup-guide にホスト側の GPUDirect RDMA の節を足す）か削除するかを判断する．

DOCA GPUNetIO `gpunetio_rdma_client_server_write` サンプルを K8s Pod 間で動作させ、
GPU→GPU 直接通信（GPUDirect RDMA）の実現可能性を検証する PoC。

本ディレクトリには**成果物（コード）のみ**を置く。手順・結果は sesame-brain の `wiki/gpu-direct-comm/gpunetio-feasibility/` にある（DOCA サンプルは `GPUNETIO_RDMA_WRITE_SAMPLE_GUIDE.md`）。

| ファイル | 概要 |
|---|---|
| [Dockerfile](./Dockerfile) | **G3-M1-3**: DOCA 公式サンプルをビルドするコンテナイメージ定義。`patches/` を当ててからビルドし、GDRCopy のライブラリも入れる。`doca_perftest` は DOCA devel イメージに同梱されているため、G3-M1-2 の 4 段検証では使用しない |
| [patches/0001-rdma_common-remove-duplicate-set-memrange.patch](./patches/0001-rdma_common-remove-duplicate-set-memrange.patch) | **G3-M1-3**: 公式サンプルの `create_mmap()` がメモリ範囲を 2 回設定して必ず失敗する問題の修正（[NVIDIA-DOCA/doca-samples#7](https://github.com/NVIDIA-DOCA/doca-samples/issues/7)）。Dockerfile が適用する |
| [k8s/perftest-dra.yaml](./k8s/perftest-dra.yaml) | **G3-M1-2**: `doca_perftest` で Pod 内 4 段（通常 RDMA / GPUDirect Host / GPUDirect GPU / GDAKI）を検証するマニフェスト。NumaNetwork（VF + IP）+ GPU ResourceClaim + server/client Pod |
| [k8s/gpunetio-poc.yaml](./k8s/gpunetio-poc.yaml) | **G3-M1-3**: DOCA 公式サンプルを Pod 間で動かすマニフェスト。`perftest-dra.yaml` と同じ構成（NumaNetwork `gpunetio-sample` で VF + IP、Pod ごとの GPU ResourceClaim、`sleep infinity`、server=cobalt06 / client=cobalt04）。サンプルは `kubectl exec` で起動する |
| [verify-gpunetio-baremetal-env.sh](./verify-gpunetio-baremetal-env.sh) | ベアメタルノードのホスト側前提確認（揮発性設定の再適用漏れ検出）。各ノード上で実行する |
