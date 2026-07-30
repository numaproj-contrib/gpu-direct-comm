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

### 前提環境（クラスタ + Numaflow）

#### 1. k3d クラスタの作成

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

#### 2. Numaflow のインストール

Numaflow は公開リリースマニフェストからインストールします。Numaflow 自体を開発する場合を除き、自分でビルドする必要はありません（ビルドする場合は下記のオプション手順を参照）。

```bash
kubectl create namespace numaflow-system
kubectl apply -n numaflow-system -f https://raw.githubusercontent.com/numaproj/numaflow/stable/config/install.yaml
```

前のステップで `~/.kube/config` の `current-context` を `k3d-numaflow-cluster` に設定済みのため、この `kubectl apply` は k3d クラスタの API server に対して実行されます — つまり Numaflow はホスト側ではなく k3d クラスタ内にデプロイされます。特定バージョンに合わせたい場合は `stable` の代わりに具体的なリリースタグを指定してください（例: `.../numaflow/v1.7.1/config/install.yaml`）。

インストール後、Numaflow の各コンポーネントが起動していることを確認してください。

```bash
kubectl get pods -n numaflow-system
# 期待値: numaflow-controller, numaflow-server, numaflow-dex-server — すべて Running
```

> `config/install.yaml` には `numaflow-webhook`（Pipeline / InterStepBufferService のimmutableフィールドを検証する Numaflow 自身の validating webhook）は含まれません — 別配布です。これはオプションで、gpu-direct-comm 自身の webhook はこれに依存しません。併せてインストールする場合は以下を実行してください。
>
> ```bash
> kubectl apply -n numaflow-system -f https://raw.githubusercontent.com/numaproj/numaflow/stable/config/validating-webhook-install.yaml
> kubectl get pods -n numaflow-system -l app.kubernetes.io/name=numaflow-webhook
> # 期待値: numaflow-webhook — Running
> ```

> **オプション: Numaflow をソースからビルドする。** Numaflow 自体を開発する場合（例: 本リポジトリの webhook に対して未リリースの変更を検証する場合）のみ必要です。[numaflow リポジトリ](https://github.com/numaproj/numaflow) をクローンして以下を実行してください。
>
> ```bash
> cd /path/to/numaflow
> IMAGE_NAMESPACE=<your-registry> VERSION=latest make start
> ```
>
> このコマンドは Numaflow のコンテナイメージをビルドし、クラスタにインストールします（この場合も `current-context` により対象は `k3d-numaflow-cluster` になります）— 上記の `kubectl apply` の代わりに使用します。Numaflow のビルド環境全体（Go、Rust、protoc 等）のセットアップについては、[Numaflow Development](https://numaflow.numaproj.io/development/development/) ドキュメントを参照してください。

### gpu-direct-comm 固有のセットアップ

#### 3. DRANET のインストール

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

#### 4. DeviceClass の作成

`DeviceClass` は、DRANET が公開したどのデバイスが割当対象かを Kubernetes に伝えます。`NumaNetwork.spec.refDeviceClass.name` はこのオブジェクトを参照する必要があります。ローカルクラスタでは `dummy` タイプのインターフェースのみにフィルタする DeviceClass を使用します:

```bash
kubectl apply -f config/testdata/e2e_deviceclass_dranet_local.yaml
```

DeviceClass が作成されたことを確認します:

```bash
kubectl get deviceclass dranet-e2e-local
# 期待値: AGE 付きで DeviceClass が表示される
```

#### 5. whereabouts のインストール

whereabouts は、`webhook-whereabouts-numanetwork` が `NumaNetwork.spec.refResourceClaimDranet.ipRange` から IP を割り当てるために exec する CNI IPAM プラグインです。その DaemonSet はノードごとに flat な設定ファイル（`/etc/cni/net.d/whereabouts.d/whereabouts.conf`。IPPool CRD と通信するための kubeconfig を含む）も生成し、`webhook-whereabouts-numanetwork` はこれに依存します — webhook をデプロイする前にインストールしてください。

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/dranet/main/tests/manifests/whereabouts_upstream.yaml
kubectl -n kube-system rollout status ds/whereabouts --timeout=120s
```

whereabouts が各ノードに設定ファイルを書き込んだことを確認します。この DaemonSet は自身のコンテナ内でホストの `/etc/cni/net.d` を `/host/etc/cni/net.d` としてマウントしているため、`kubectl exec` でこの Pod に入ればホスト側のファイルを直接読めます — この方法はローカルクラスタ・ベアメタルクラスタで全く同じコマンドが使え、ノードへの SSH 鍵や sudo 権限にも依存しません。

```bash
kubectl -n kube-system exec ds/whereabouts -- cat /host/etc/cni/net.d/whereabouts.d/whereabouts.conf
# 期待値: "kubeconfig" フィールドを含む JSON
```

#### 6. cert-manager のインストール

gpu-direct-comm の controller manager の webhook（`internal/webhook/v1alpha1`）は cert-manager が管理する TLS 証明書を必要とします（`config/default/kustomization.yaml` は `../certmanager` を含む）。

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
kubectl -n cert-manager wait --for=condition=Available --timeout=120s deployment --all
```

cert-manager の全 Pod が起動していることを確認します。

```bash
kubectl get pods -n cert-manager
# 期待値: cert-manager, cert-manager-cainjector, cert-manager-webhook — すべて Running
```

#### 7. gpu-direct-comm のインストール

以下の 3 つのサブステップは、本リポジトリのソースコードからビルドしたコンポーネントをインストールします。

##### 7-1. gpu-direct-comm CRD のインストール

```bash
make install
```

これは `kustomize build config/crd | kubectl apply -f -` を実行し、`NumaNetwork` CRD を登録します。この CRD は gpu-direct-comm が独自に定義したもので、Numaflow 上流の `config/install.yaml` には含まれません。

CRD が登録されたことを確認します。

```bash
kubectl get crd numanetworks.numaflow.numaproj.io
# 期待値: CREATED AT タイムスタンプ付きで CRD が表示される
```

##### 7-2. gpu-direct-comm controller manager のデプロイ

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

##### 7-3. webhook-whereabouts-numanetwork のビルドとデプロイ

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
# 期待値: numaflow-controller, numaflow-server, numaflow-dex-server — すべて Running（オプションの validating webhook を入れた場合は numaflow-webhook も）

# DRANET デバイス
kubectl get resourceslice
# 期待値: 1 つ以上の ResourceSlice オブジェクト

# DeviceClass
kubectl get deviceclass dranet-e2e-local
# 期待値: AGE 付きで DeviceClass が表示される

# whereabouts のノード設定ファイル
kubectl -n kube-system exec ds/whereabouts -- cat /host/etc/cni/net.d/whereabouts.d/whereabouts.conf
# 期待値: "kubeconfig" フィールドを含む JSON

# cert-manager
kubectl get pods -n cert-manager
# 期待値: cert-manager, cert-manager-cainjector, cert-manager-webhook — すべて Running

# gpu-direct-comm CRD
kubectl get crd numanetworks.numaflow.numaproj.io
# 期待値: CREATED AT タイムスタンプ付きで CRD が表示される

# gpu-direct-comm controller manager
kubectl get pods -n gpu-direct-comm-system
# 期待値: gpu-direct-comm-controller-manager-... — Running, READY 1/1

# DaemonSet（DRANET + whereabouts + webhook-whereabouts-numanetwork）
kubectl -n kube-system get ds whereabouts dranet webhook-whereabouts-numanetwork
# 期待値: 全 DaemonSet が全ノードで READY
```

---

## 2. ベアメタルクラスタ

実 NVIDIA GPU および SR-IOV VF ハードウェアを使用するマルチノードベアメタルクラスタで controller を動かす場合。

### ハードウェア前提条件

各ワーカーノードには以下のハードウェアが搭載されている必要があります。

#### GPU

- NVIDIA GPU（DRA driver 対応のもの — `numaflow-dra-ansible` の `dra-driver-nvidia-gpu` ロールでセットアップ）

#### d-plane NIC

データプレーン（d-plane）には、以下の要件を満たす NIC が各ワーカーノードに接続されている必要があります。

| 要件 | 説明 |
|------|------|
| RDMA 対応 | NIC がハードウェアレベルで RDMA verbs をサポートしていること。GPU Direct RDMA ではデバイス間の直接通信を行うため、RDMA 非対応の NIC では機能しない |
| SR-IOV 対応 | SR-IOV の Virtual Function（VF）を作成できること。DRANET は VF を `ResourceSlice` として公開し、DRA 経由で Pod に割り当てる |
| GPUDirect RDMA 対応（GPU Direct RDMA を使用する場合） | NIC の peer memory 経由で GPU メモリに直接アクセスできること。GPU と NIC が同一 PCIe スイッチ配下（PIX トポロジー）に配置されていることが性能上望ましい |

**検証実績のある NIC：**

| ベンダー | NIC | ドライバ | RDMA プロトコル | 備考 |
|---------|-----|---------|---------------|------|
| NVIDIA/Mellanox | ConnectX-6 以降 | `mlx5_core`（OFED） | RoCE v2（Ethernet 接続時）/ ネイティブ IB RDMA（InfiniBand 接続時） | GPUDirect RDMA の検証実績あり。VPI カードはポートモードの切り替えが必要な場合あり（[ステップ 2: ポートモードの確認と切り替え](#ステップ-2-ポートモードの確認と切り替え) 参照） |
| Intel | E810 | `ice` + `irdma` | RoCE v2 | GPUDirect RDMA の対応は限定的 |

> ポートモードはスイッチの種類と一致させる必要があります（Ethernet スイッチ → Ethernet モード / RoCE v2、InfiniBand スイッチ → InfiniBand モード / ネイティブ IB RDMA）。詳細は [ステップ 2: ポートモードの確認と切り替え](#ステップ-2-ポートモードの確認と切り替え) を参照してください。

### 必要なツール

- Ansible control node（`ansible-core` >= 2.16）— [numaflow-dra-ansible](https://github.com/compsysg/numaflow-dra-ansible) の実行に使用
- 各管理対象ノードへの SSH アクセス
- kubectl（クラスタの Kubernetes バージョンに一致するもの — numaflow-dra-ansible の `vars-stg.yml` を参照）
- Docker（controller / webhook イメージのビルド用）
- 全クラスタノードから到達可能なコンテナレジストリ（ベアメタルでは `k3d image import` が使えないため）
- [DRANET](https://github.com/kubernetes-sigs/dranet)、[cert-manager](https://cert-manager.io/)、[whereabouts](https://github.com/k8snetworkingplumbingwg/whereabouts) — 以下の gpu-direct-comm 固有のセットアップ手順でインストールする（ansible playbookではインストールされない）

### 前提環境（クラスタ・GPU・DRA・Numaflow）

クラスタ構築、NVIDIA GPU ドライバ／ツールキット、GPU 用 DRA driver の有効化、Numaflow のインストールは [numaflow-dra-ansible](https://github.com/compsysg/numaflow-dra-ansible)（本ワークスペースでは `~/project/numaflow-dra-ansible`）に一任します。まず同リポジトリの README に従って inventory（`inventory/stg.yml`。`inventory/inventory.yml.template` からコピー）を設定し、その上でルート playbook を実行してください。

```bash
cd ~/project/numaflow-dra-ansible
ansible-playbook -i inventory/stg.yml -e @vars-stg.yml site-stg-dci-poc.yml
```

この playbook は以下を順にインストールします（`site-stg-dci-poc.yml` 参照）。

1. Ubuntu の前提設定
2. kubeadm + Calico CNI による Kubernetes クラスタ構築（`playbooks/kubernetes-cluster.yml`）
3. NVIDIA GPU ドライバ + コンテナツールキット（`playbooks/nvidia-gpu-support.yml`）
4. DRA feature gate + NVIDIA GPU 用 DRA driver（`playbooks/dra-driver-nvidia-gpu.yml`）
5. Numaflow（`playbooks/numaflow.yml`）
6. Numaflow 向け Prometheus 監視（`playbooks/monitor.yml`）

> この playbook は DRANET・whereabouts・gpu-direct-comm のコンポーネントを**インストールしません**。これらは次のセクションで、ローカルクラスタと同じ手順でセットアップします。

次に進む前に、各レイヤーを確認してください。

```bash
# 現在の context がベアメタルの API server を指しているか確認（ローカル k3d クラスタを誤って見ていないか）
kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}'
# 期待値: 実ノードの IP（例: https://<control-plane ノードの IP>:6443）— https://127.0.0.1:6443 ではないこと
# （127.0.0.1:6443 の場合、kubectl はローカル k3d クラスタを見ています。「1. ローカルクラスタ」を参照してください）

# Kubernetes クラスタ
kubectl get nodes
# 期待値: 全ノードが Ready

# NVIDIA GPU 用 DRA driver
kubectl get pods -n nvidia-dra-driver-gpu
# 期待値: nvidia-dra-driver-gpu-controller, nvidia-dra-driver-gpu-kubeletplugin — すべて Running

# Numaflow コンポーネント
kubectl get pods -n numaflow-system
# 期待値: numaflow-controller, numaflow-server, numaflow-dex-server — すべて Running

# Prometheus 監視
kubectl get pods -n monitoring
# 期待値: prometheus-k8s, prometheus-operator 等 — すべて Running
```

> ローカルクラスタのデフォルトのYAMLインストールと同様、ここでも `numaflow-webhook` は**期待値に含まれません**。`numaflow-dra-ansible`の`numaflow_install`ロールが適用するのは、ローカルクラスタがデフォルトで使うのと同じ`config/install.yaml`ベースマニフェストであり（[ローカルクラスタ > Numaflow のインストール](#numaflow-のインストール)の注記を参照）、これには含まれません。これはオプション機能で、gpu-direct-comm自身のwebhookはこれに依存しませんが、ベアメタルでも確認したい場合は、ansible playbookがインストールしたのと同じNumaflowバージョン（`vars-stg.yml`の`numaflow_install.numaflow_version`を参照）に合わせて手動インストールしてください。
>
> ```bash
> kubectl apply -n numaflow-system -f https://raw.githubusercontent.com/numaproj/numaflow/<numaflow_version>/config/validating-webhook-install.yaml
> kubectl get pods -n numaflow-system -l app.kubernetes.io/name=numaflow-webhook
> # 期待値: numaflow-webhook — Running
> ```

### gpu-direct-comm 固有のセットアップ

DRANET、`dranet` DeviceClass、whereabouts、cert-manager、および gpu-direct-comm 自身のコンポーネント（CRD、controller manager、`webhook-whereabouts-numanetwork`）は `numaflow-dra-ansible` に**含まれません**。以下の置き換えを除き、[ローカルクラスタ](#1-ローカルクラスタ)と同じ手順でインストールします。

インストール順序はコンポーネント間の依存関係に基づいています。各コンポーネントは前段のコンポーネントに依存します。

| ステップ | コンポーネント | 役割 | 依存先 |
|---------|-------------|------|--------|
| 1 | SR-IOV VF | 各ノードに仮想 NIC インターフェースを作成し、DRANET が検出できるようにする | ホストの NIC ハードウェア + OFED ドライバ |
| 2 | DRANET | ノードのインターフェースをスキャンし、DRA 経由で `ResourceSlice` オブジェクトとして公開する | ノード上に VF が存在すること |
| 3 | DeviceClass | どの `ResourceSlice` デバイスが割当対象かを Kubernetes に伝える CEL セレクタ | DRANET が VF を公開済みであること |
| 4 | whereabouts | `ipRange` から IP を割り当てる CNI IPAM プラグイン。webhook が読むノードごとの設定ファイルも生成する | — |
| 5 | cert-manager | controller manager の admission webhook 用の TLS 証明書を発行する | — |
| 6 | gpu-direct-comm | controller + mutating webhook + BYODP webhook が VF 割当と IP 割当を結びつける | 上記すべて |

> ステップ 4 と 5 はステップ 1〜3 に対する依存がなく、相互に任意の順序でインストールできますが、ステップ 6 の前に完了している必要があります。

#### 1. SR-IOV VF の準備

DRANET のインストール**前に**、各ワーカーノードの RDMA 対応 NIC 上に SR-IOV Virtual Function（VF）が存在している必要があります。DRANET の DaemonSet は起動時にノードのインターフェースをスキャンするため、VF がまだ存在しなければ `ResourceSlice` エントリとして表示されません。

> VF の作成はノードごとに一度だけ行えば十分です（systemd で永続化すれば再起動後も維持されます — 下記参照）。以前のセットアップで既に VF が存在する場合は、[VF が認識されていることの確認](#vf-が認識されていることの確認) までスキップしてください。

##### ステップ 1: RDMA 対応 NIC が認識されていることを確認する

ワーカーノードに SSH でログインし、Mellanox/NVIDIA NIC が PCI バス上で認識されていることを確認します：

```bash
lspci | grep -i mellanox
# 出力例:
#   6a:00.0 Infiniband controller: Mellanox Technologies MT28908 Family [ConnectX-6]
#   6a:00.1 Infiniband controller: Mellanox Technologies MT28908 Family [ConnectX-6]
```

出力がない場合、NIC が物理的に装着されていないか、ドライバがロードされていない可能性があります。

##### ステップ 2: ポートモードの確認

ConnectX VPI カードのポートはデフォルトで **InfiniBand モード** になっています。必要なモードは NIC が接続されているケーブルやスイッチの種類によって決まります：

| 接続先スイッチ | 必要なポートモード | RDMA プロトコル |
|--------------|------------------|---------------|
| InfiniBand スイッチ | InfiniBand（デフォルト） | ネイティブ IB RDMA |
| Ethernet スイッチ | Ethernet | RoCE v2 |


```bash
# mstflint が未インストールの場合（mstconfig コマンドを提供する）
sudo apt install -y mstflint

# 各ポートの物理リンクのモードを確認
PCI_ADDR=$(lspci | grep -i mellanox | head -1 | awk '{print $1}')
sudo mstconfig -d 0000:${PCI_ADDR} query | grep LINK_TYPE
# 出力例:
#   LINK_TYPE_P1                        IB(1)
#   LINK_TYPE_P2                        IB(1)
```

得られたポートモードが想定のもの通りであるかを確認してください．そうでない場合は後ほど変更します．

##### ステップ 3: リンク状態の確認

**どの物理ポートにケーブルが接続されているかを確認します。** デュアルポートカードは 2 つの独立した PF を持ちます — リンクのあるポートのみを VF 作成に使用します。

前ステップで確認したポートモードごとに確認方法が異なります．

| ポートモード | 代表的なインターフェース名 | 確認場所 |
|------------|------------------------|---------|
| Ethernet | `enp<bus>s<slot>f<func>np<port>` | `/sys/class/net/` |
| InfiniBand | `ib0`, `ib1`（IPoIB） | `/sys/class/net/` |
| InfiniBand（RDMA デバイス） | `mlx5_0`, `mlx5_1` | `/sys/class/infiniband/` |

**Ethernet モード** の場合（`lspci` で既に "Ethernet controller" と表示されている場合）、sysfs でリンク状態を確認します：

```bash
for PCI_ADDR in $(lspci | grep -i mellanox | awk '{print $1}'); do
  IFACE=$(ls -l /sys/class/net/ | grep "${PCI_ADDR}" | awk '{print $9}')
  echo "${IFACE}: $(cat /sys/class/net/${IFACE}/operstate)"
done
# 出力例:
#   enp101s0f0np0: up
#   enp101s0f1np1: down
```

**InfiniBand モード** の場合、RDMA デバイス経由で確認します：

```bash
for PCI_ADDR in $(ls /sys/class/infiniband/); do
  STATE=$(cat /sys/class/infiniband/${PCI_ADDR}/ports/1/state)
  echo "${PCI_ADDR}: ${STATE}"
done
# 出力例:
#   mlx5_0: 4: ACTIVE     ← ケーブル接続済み
#   mlx5_1: 1: DOWN       ← 未接続
```

リンクがあった方のポートのインタフェース名を覚えてください — 後の VF 作成状況の確認と作成で使用します。

##### ステップ 4: VF の作成状況の確認

```bash
IFACE="<your-pf-name>"   # 例: enp4s0f0np0（Ethernet）または ib0（InfiniBand）

# ファームウェアで SR-IOV が有効になっていることを確認
cat /sys/class/net/${IFACE}/device/sriov_totalvfs
# 期待値: 8（ステップ 5 で設定した NUM_OF_VFS の値）
```

ここでsriov_totalvfsがない場合は，ファームウェアレベルでSR-IOVが有効になっていないので，次ステップで有効化してください．

##### ステップ 5: ポートモードの変更 と VFの有効化

**ファームウェア設定の適用。** ポートモードの切り替えと SR-IOV の有効化はどちらもファームウェアレベルの設定でリブートが必要なため、一括で実行します

1 回の `mstconfig` コマンドでまとめて適用します。リンクアップされているポートのアドレスに対して，いずれかを実行してください：

```bash
# head -1 or -2
PCI_ADDR=$(lspci | grep -i mellanox | head -1 | awk '{print $1}')

# ポートモードの変更
# lspciの出力結果に応じて，Ethernetに変更する場合はETHを，InfiniBandに変更する場合はIBを代入して実行
sudo mstconfig -d 0000:${PCI_ADDR} set  LINK_TYPE_P1=ETH LINK_TYPE_P2=ETH

# SR-IOVの有効化
# 前ステップ4でSR-IOVが有効化されていなかった場合は，こちらを実行
# SR-IOV を有効化し、VF の最大数を設定（8 あれば E2E テストには十分）
sudo mstconfig -d 0000:${PCI_ADDR} set SRIOV_EN=1 NUM_OF_VFS=8 LINK_TYPE_P1=ETH LINK_TYPE_P2=ETH

# ファームウェア設定の反映にはリブートが必要
sudo reboot
```

リブート後の確認：
- `lspci | grep -i mellanox` で **"Ethernet controller"** と表示されること（モードを切り替えた場合）

##### ステップ 6: VFの作成

```bash
# Ethernetの場合
## head -1 or -2
PCI_ADDR=$(lspci | grep -i mellanox | head -1 | awk '{print $1}')
IFACE=$(ls -l /sys/class/net/ | grep "$PCI_ADDR" | awk '{print $9}')

# InfiniBandの場合（head -1 or -2 でACTIVEなデバイスを選択）
RDMA_DEV=$(ls /sys/class/infiniband/ | head -1)
IFACE=$(ls /sys/class/infiniband/${RDMA_DEV}/device/net/ | head -1)

# 作成可能なVFの数を確認する
cat /sys/class/net/${IFACE}/device/sriov_totalvfs

# VF を作成（2 つあれば E2E テストに十分 — vertex Pod ごとに 1 つ）
sudo sh -c "echo 2 > /sys/class/net/${IFACE}/device/sriov_numvfs"


# VF が作成されたことを確認
# VF のネットワークインターフェースが存在することを確認
ip link show | grep -E 'vf '

# VF の PCI デバイスが見えることを確認
lspci | grep -i 'virtual function'
# 期待値: VF ごとに 1 行（例: "04:00.2 ... Virtual Function"）
```

gpu-direct ワークロードを実行するすべてのワーカーノードで同じ操作を繰り返してください。

##### ステップ 7: VF 作成の再起動後の永続化

永続化しないと、再起動時に VF は消えます。各ワーカーノードで systemd の oneshot サービスを作成してください：

```bash
cat <<EOF | sudo tee /etc/systemd/system/sriov-vf@.service
[Unit]
Description=Create SR-IOV VFs on %i
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/bin/bash -c 'echo 2 > /sys/class/net/%i/device/sriov_numvfs'
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

# PF 名を特定する（VF 作成後は VF も同じ PCI アドレスでマッチするため、vN サフィックスを除外する）
# head -1 or -2
PCI_ADDR=$(lspci | grep -i mellanox | head -1 | awk '{print $1}')
PF_NAME=$(ls -l /sys/class/net/ | grep "$PCI_ADDR" | grep -v 'v[0-9]' | awk '{print $9}')

sudo systemctl enable --now sriov-vf@${PF_NAME}.service
```

#### 2. DRANET のインストール

DRANET は各ノードのネットワークインターフェース（上で作成した VF を含む）をスキャンし、Kubernetes DRA 経由で `ResourceSlice` オブジェクトとして公開します。DRANET がなければ、クラスタは VF を Pod に割り当てる手段がありません。

[ローカルクラスタ > 3. DRANET のインストール](#3-dranet-のインストール) と同一です。`DynamicResourceAllocation` feature gate は前段の ansible playbook（`feature_gates_dra_master` ロール）が有効化するため、DRANET インストール後に `kubectl get resourceslice` が空の場合は `k3d-config.yaml` の編集ではなく、そのロールの実行結果を確認してください（`k3d-config.yaml` はローカルクラスタにのみ適用されます）。インストール後、SR-IOV VF が DRANET に認識されていることを確認してください。

```bash
kubectl get resourceslice -o yaml | grep -A5 'ifName'
# 期待値: VF のインターフェース名に対応するエントリが dra.net/type: sriov（または類似の値）とともに表示される
```

#### 3. DeviceClass の作成

`DeviceClass` は、DRANET が公開した `ResourceSlice` デバイスのうちどれが割当対象かをフィルタする CEL セレクタを定義します。DRANET の**後に**作成する必要があります。セレクタが参照する属性（例: `dra.net/sriov`）は、DRANET がデバイスを公開して初めて存在するためです。

ベアメタルの DeviceClass は SR-IOV VF のみにフィルタします。DRANET は VF に対してのみ `dra.net/isSriovVf: true` を設定します。PF、非 SR-IOV 物理 NIC、ソフトウェアインターフェースにはこの属性自体が存在しません:

```bash
kubectl apply -f config/testdata/e2e_deviceclass_dranet_baremetal.yaml
```

DeviceClass が作成されたことを確認します:

```bash
kubectl get deviceclass dranet-e2e-baremetal
# 期待値: AGE 付きで DeviceClass が表示される
```

#### 4. whereabouts のインストール

[ローカルクラスタ > 5. whereabouts のインストール](#5-whereabouts-のインストール) と同一です。

#### 5. cert-manager のインストール

[ローカルクラスタ > 6. cert-manager のインストール](#6-cert-manager-のインストール) と同一です。

#### 6. gpu-direct-comm のインストール

controller は `NumaNetwork` を `ResourceClaimTemplate`（DeviceClass を使用）に reconcile し、mutating webhook が Pipeline Pod にクレームを注入し、`webhook-whereabouts-numanetwork` が whereabouts を呼び出して IP を割り当てます。そのため、上流のコンポーネント（ステップ 1〜5）がすべて準備済みである必要があります。

ベアメタルノードは `k3d image import` が使えないため、イメージは各ノードが pull できるレジストリへ push する必要があります。以下のサブステップが [ローカルクラスタ > 7. gpu-direct-comm のインストール](#7-gpu-direct-comm-のインストール) の置き換えです。

##### 6-1. gpu-direct-comm CRD のインストール

[ローカルクラスタ > 7-1. gpu-direct-comm CRD のインストール](#7-1-gpu-direct-comm-crd-のインストール) と同一です。

```bash
make install
kubectl get crd numanetworks.numaflow.numaproj.io
# 期待値: CREATED AT タイムスタンプ付きで CRD が表示される
```

##### 6-2. イメージ用レジストリの準備

ビルドを行うホストと、クラスタの全ノードの両方から到達可能なコンテナレジストリを用意（または既存のものを利用）してください（例: 社内Harbor等）。以下では、そのアドレスと project/repository パスを `<registry>/<project>` というプレースホルダーで表記します。実際の値に置き換えてください。

##### 6-3. gpu-direct-comm controller manager のデプロイ

```bash
make docker-build IMG=<registry>/<project>/controller:<tag>
make docker-push IMG=<registry>/<project>/controller:<tag>
make deploy IMG=<registry>/<project>/controller:<tag>
kubectl -n gpu-direct-comm-system rollout status deployment/gpu-direct-comm-controller-manager --timeout=120s
```

`make deploy` は内部で `kustomize edit set image controller=<IMG>` を実行し、`config/manager/kustomization.yaml` をその場で書き換えます。これは想定通りの動作で、コミットする必要はありません。

確認:

```bash
kubectl get pods -n gpu-direct-comm-system
# 期待値: gpu-direct-comm-controller-manager-... — Running, READY 1/1
```

##### 6-4. webhook-whereabouts-numanetwork のビルドとデプロイ

controller manager と異なり、このイメージには `docker-push`/`deploy` に相当する Make ターゲットがなく、また `config/webhook-whereabouts-numanetwork/kustomization.yaml` にはまだ `images:` トランスフォーマーが設定されていません — `kustomize edit set image` を初めて実行した際にこれが追加されます。

```bash
make docker-build-webhook-nn WEBHOOK_NN_IMG=<registry>/<project>/webhook-whereabouts-numanetwork:<tag>
docker push <registry>/<project>/webhook-whereabouts-numanetwork:<tag>
make kustomize
(cd config/webhook-whereabouts-numanetwork && ../../bin/kustomize edit set image webhook-whereabouts-numanetwork=<registry>/<project>/webhook-whereabouts-numanetwork:<tag>)
kubectl apply -k config/webhook-whereabouts-numanetwork/
kubectl -n kube-system rollout status ds/webhook-whereabouts-numanetwork --timeout=60s
```

`config/manager/kustomization.yaml` の場合と同様、これは `config/webhook-whereabouts-numanetwork/kustomization.yaml` をその場で書き換えます。想定通りの動作で、コミット不要です。

確認:

```bash
kubectl -n kube-system get pods -l app.kubernetes.io/name=webhook-whereabouts-numanetwork
# 期待値: DaemonSet がスケジュール可能な各ノードに1つずつ Pod — すべて Running, READY 1/1
```

### 確認

[ローカルクラスタ > 確認](#確認) と同じチェックリストを実行しますが、1点だけ置き換えます。`kubectl config current-context` の確認は不要です（ベアメタルクラスタは k3d で作成されないため）。whereabouts の設定ファイル確認（`kubectl -n kube-system exec ds/whereabouts -- cat ...`）はそのままで構いません — ノードへの SSH アクセスは不要です。

---

## 補足

- k3d クラスタの設定はリポジトリルートの `k3d-config.yaml` にあります（ローカルクラスタのみ）。
- k3d は `~/.kube/config` を自動更新し、新しいクラスタに current context を切り替えます（設定内の `switchCurrentContext: true`）。
- [numaflow-dra-ansible](https://github.com/compsysg/numaflow-dra-ansible) は、ベアメタルクラスタにおけるクラスタ／GPU／DRA／Numaflow のプロビジョニングを自動化します。DRANET・whereabouts・gpu-direct-comm のコンポーネントはカバーしないため、ローカル・ベアメタルの両方で常に本ガイドの手順に沿って手動セットアップします。
