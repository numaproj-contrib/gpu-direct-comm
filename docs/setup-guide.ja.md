# 環境セットアップ

このガイドは gpu-direct-comm で使用する環境の構築方法を説明します。自分の用途に合ったセクションを選んでください。

> このドキュメントは [setup-guide.md](./setup-guide.md)（英語版）の日本語訳です。内容に差異がある場合は英語版を正としてください。

## 1. ローカルクラスタ

controller とその依存関係を k3d クラスタ上で動かす場合。これが標準的な開発ワークフローです。

### 必要なツール

- Go 1.25+
- Docker
- [k3d](https://k3d.io/)
- kubectl（k3d の Kubernetes バージョンに一致するもの）
- [Numaflow](https://numaflow.numaproj.io/)
- [DRANET](https://github.com/kubernetes-sigs/dranet)
- [cert-manager](https://cert-manager.io/)
- [whereabouts](https://github.com/k8snetworkingplumbingwg/whereabouts)

### セットアップ手順

#### k3d クラスタの作成

リポジトリルートの設定ファイルを使ってクラスタを作成します。

```bash
k3d cluster create --config k3d-config.yaml
```

このコマンドは以下を行います。

1. k3d クラスタ（Docker コンテナ内で動く k3s）を作成する
2. クラスタを `k3d-numaflow-cluster` として `~/.kube/config` に登録する
3. `current-context` を自動的に `k3d-numaflow-cluster` に切り替える（`k3d-config.yaml` の `switchCurrentContext: true` による）

これ以降、すべての `kubectl` コマンドは `127.0.0.1:6443`（Docker コンテナからホストへポートフォワードされた k3s API server）経由で k3d クラスタに対して実行されます。

context が正しく設定されているか確認できます。

```bash
kubectl config current-context
# 期待値: k3d-numaflow-cluster
```

#### Numaflow のインストール

Numaflow は [numaflow リポジトリ](https://github.com/numaproj/numaflow) からビルド・デプロイしてインストールします。リポジトリをクローンして以下を実行してください。

```bash
cd /path/to/numaflow
IMAGE_NAMESPACE=<your-registry> VERSION=latest make start
```

このコマンドは Numaflow のコンテナイメージをビルドし、クラスタにインストールします。前のステップで `~/.kube/config` の `current-context` を `k3d-numaflow-cluster` に設定済みのため、`make start` は内部的に k3d クラスタの API server に対して `kubectl apply` を実行します — つまり Numaflow はホスト側ではなく k3d クラスタ内にデプロイされます。

インストール後、Numaflow の各コンポーネントが起動していることを確認してください。

```bash
kubectl get pods -n numaflow-system
# 期待値: numaflow-controller, numaflow-server, numaflow-dex-server, numaflow-webhook
```

Numaflow のビルド環境全体（Go、Rust、protoc 等）のセットアップについては、[Numaflow Development](https://numaflow.numaproj.io/development/development/) ドキュメントを参照してください。

#### cert-manager のインストール

controller の webhook（`internal/webhook/v1alpha1`）は cert-manager が管理する TLS 証明書を必要とします（`config/default/kustomization.yaml` は `../certmanager` を含む）。

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
kubectl -n cert-manager wait --for=condition=Available --timeout=120s deployment --all
```

cert-manager の全 Pod が起動していることを確認します。

```bash
kubectl get pods -n cert-manager
# 期待値: cert-manager, cert-manager-cainjector, cert-manager-webhook — すべて Running
```

#### CRD のインストール

```bash
make install
```

これは `kustomize build config/crd | kubectl apply -f -` を実行し、`NumaNetwork` CRD（および依存する Numaflow の CRD 群 — 上記の Numaflow インストール手順を参照）を登録します。

CRD が登録されたことを確認します。

```bash
kubectl get crd numanetworks.numaflow.numaproj.io
# 期待値: CREATED AT タイムスタンプ付きで CRD が表示される
```

#### DRANET のインストール

DRANET は NIC を `ResourceSlice` オブジェクトとして公開し、Pod にアタッチする DRA（Dynamic Resource Allocation）driver です。

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/dranet/refs/heads/main/install.yaml
kubectl -n kube-system rollout status ds/dranet --timeout=120s
```

DRANET がデバイスを公開しているか確認します。

```bash
kubectl get resourceslice
```

何も返らない場合、k3s の DRA feature gate が有効になっていない可能性があります。k3s v1.34.x では `DynamicResourceAllocation` が既定で beta 有効ですが、それでも出力が無い場合は `k3d-config.yaml` に feature gate を明示的に追加してクラスタを再作成してください。

```yaml
options:
  k3s:
    extraArgs:
      - arg: "--feature-gates=DynamicResourceAllocation=true"
        nodeFilters: ["server:*", "agent:*"]
```

#### DeviceClass の作成

`DeviceClass` は、DRANET が公開したどのデバイスが割当対象かを Kubernetes に伝えます。`NumaNetwork.spec.refDeviceClass.name` はこのオブジェクトを参照する必要があります。

```bash
kubectl apply -f config/samples/deviceclass_dranet.yaml
```

DeviceClass が作成されたことを確認します。

```bash
kubectl get deviceclass dranet
# 期待値: AGE 付きで DeviceClass が表示される
```

> この基本フィルタは DRANET が公開する全デバイス（ノード自身のブリッジインターフェース `cni0` 等を含む）にマッチします。E2E テストで dummy interface を使う際は、より絞り込んだセレクタを使います — [CONTRIBUTING.md](../CONTRIBUTING.md#local-cluster) を参照してください。

#### whereabouts のインストール

whereabouts は、`webhook-whereabouts-numanetwork` が `NumaNetwork.spec.refResourceClaimDranet.ipRange` から IP を割り当てるために exec する CNI IPAM プラグインです。その DaemonSet はノードごとに flat な設定ファイル（`/etc/cni/net.d/whereabouts.d/whereabouts.conf`。IPPool CRD と通信するための kubeconfig を含む）も生成し、`webhook-whereabouts-numanetwork` はこれに依存します — webhook をデプロイする前にインストールしてください。

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/dranet/main/tests/manifests/whereabouts_upstream.yaml
kubectl -n kube-system rollout status ds/whereabouts --timeout=120s
```

whereabouts が各ノードに設定ファイルを書き込んだことを確認します。

```bash
docker exec k3d-numaflow-cluster-server-0 cat /etc/cni/net.d/whereabouts.d/whereabouts.conf
# 期待値: "kubeconfig" フィールドを含む JSON
```

#### controller manager のデプロイ

```bash
make docker-build IMG=controller:latest
k3d image import controller:latest -c numaflow-cluster
make deploy IMG=controller:latest
kubectl -n gpu-direct-comm-system rollout status deployment/gpu-direct-comm-controller-manager --timeout=120s
```

controller manager の Pod が Ready であることを確認します。

```bash
kubectl get pods -n gpu-direct-comm-system
# 期待値: gpu-direct-comm-controller-manager-... — Running, READY 1/1
```

#### webhook-whereabouts-numanetwork のビルドとデプロイ

`webhook-whereabouts-numanetwork` は本リポジトリに実装された dranet BYODP（Bring Your Own DRANET Provider）用のカスタム webhook です（`cmd/webhook-whereabouts-numanetwork`、`internal/ipam`）。`NumaNetwork` の `ipRange` を解決し、`whereabouts` を exec して IP を割り当てます。

```bash
make docker-build-webhook-nn WEBHOOK_NN_IMG=webhook-whereabouts-numanetwork:latest
k3d image import webhook-whereabouts-numanetwork:latest -c numaflow-cluster
kubectl apply -k config/webhook-whereabouts-numanetwork/
kubectl -n kube-system rollout status ds/webhook-whereabouts-numanetwork --timeout=60s
```

webhook の Pod が Ready であることを確認します。`READY 1/1` は readinessProbe が
既に `/health` の応答を確認済みであることを意味するため、別途 health チェックは不要です。

```bash
kubectl -n kube-system get pods -l app=webhook-whereabouts-numanetwork
# 期待値: ノードごとに 1 Pod — すべて Running, READY 1/1
```

### 確認

全チェックを一括実行して、環境が完全に動作していることを確認します。

```bash
# k3d クラスタ context
kubectl config current-context
# 期待値: k3d-numaflow-cluster

# Numaflow コンポーネント
kubectl get pods -n numaflow-system
# 期待値: numaflow-controller, numaflow-server, numaflow-dex-server, numaflow-webhook — すべて Running

# cert-manager
kubectl get pods -n cert-manager
# 期待値: cert-manager, cert-manager-cainjector, cert-manager-webhook — すべて Running

# NumaNetwork CRD
kubectl get crd numanetworks.numaflow.numaproj.io
# 期待値: CREATED AT タイムスタンプ付きで CRD が表示される

# DRANET デバイス
kubectl get resourceslice
# 期待値: 1 つ以上の ResourceSlice オブジェクト

# DeviceClass
kubectl get deviceclass dranet
# 期待値: AGE 付きで DeviceClass が表示される

# whereabouts のノード設定ファイル
docker exec k3d-numaflow-cluster-server-0 cat /etc/cni/net.d/whereabouts.d/whereabouts.conf
# 期待値: "kubeconfig" フィールドを含む JSON

# controller manager
kubectl get pods -n gpu-direct-comm-system
# 期待値: gpu-direct-comm-controller-manager-... — Running, READY 1/1

# DaemonSet
kubectl -n kube-system get ds whereabouts dranet webhook-whereabouts-numanetwork
# 期待値: 全 DaemonSet が全ノードで READY
```

---

## 2. ベアメタルクラスタ

実 SR-IOV VF ハードウェアを使用するマルチノードベアメタルクラスタで controller を動かす場合。

> **TBD** — ベアメタル E2E ワークフローの検証完了後に追記予定です。

---

## 補足

- k3d クラスタの設定はリポジトリルートの `k3d-config.yaml` にあります。
- k3d は `~/.kube/config` を自動更新し、新しいクラスタに current context を切り替えます（設定内の `switchCurrentContext: true`）。
- [numaflow-dra-ansible](https://github.com/compsysg/numaflow-dra-ansible) の playbook でこのセットアップの大部分を自動化できます。詳細は README を参照してください。
