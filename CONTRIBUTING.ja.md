# gpu-direct-comm への貢献

> このドキュメントは [CONTRIBUTING.md](./CONTRIBUTING.md)（英語版）の日本語訳です。内容に差異がある場合は英語版を正としてください。

gpu-direct-comm への貢献に興味をお持ちいただきありがとうございます。このドキュメントでは、始め方と貢献プロセスで期待されることを説明します。

## 目次

- [Issue の報告](#issue-の報告)
- [開発環境のセットアップ](#開発環境のセットアップ)
- [コーディングスタイル](#コーディングスタイル)
- [Make ターゲット](#make-ターゲット)
- [ユニットテスト](#ユニットテスト)
- [E2E テスト](#e2e-テスト)
- [コミットメッセージ](#コミットメッセージ)
- [プルリクエストガイドライン](#プルリクエストガイドライン)
- [Claude Code の使用方法](#claude-code-の使用方法)
- [行動規範](#行動規範)

## Issue の報告

新しい Issue を作成する前に、重複を避けるために既存の Issue を検索してください。

バグを報告する際は、以下を含めてください:

- 明確で説明的なタイトル
- 問題を再現する手順
- 期待される動作と実際の動作
- Kubernetes バージョン（`kubectl version`）
- Go バージョン（`go version`）
- 関連するログやエラーメッセージ

## 開発環境のセットアップ

ローカル k3d クラスタまたはベアメタルクラスタの構築手順については、[環境セットアップ](docs/setup-guide.ja.md)ガイドを参照してください。

## コーディングスタイル

- コミット前に `gofmt` または `goimports` でコードをフォーマットしてください。
- PR を提出する前に `make lint` を実行し、すべての警告を修正してください。
- Go の慣用的なパターンに従ってください。参考: [Effective Go](https://go.dev/doc/effective_go)。
- 関数は短く、焦点を絞ったものにしてください（可能な限り50行以内）。
- 変数、関数、型には意味のある名前を付けてください。
- インターフェースを受け取り、構造体を返す。
- エラーは常に `fmt.Errorf("...: %w", err)` でコンテキスト付きでラップしてください。
- まだ必要でない機能や抽象化を追加しないでください（YAGNI）。

パッケージを変更・追加する際は、対応する `doc.go` ファイルを更新し、パッケージレベルのドキュメントを正確に保ってください。

## Make ターゲット

| ターゲット | 説明 |
|--------|-------------|
| `make build` | manager バイナリをビルド |
| `make test` | envtest でユニットテストを実行 |
| `make test-e2e` | k3d で E2E テストを実行 |
| `make lint` | golangci-lint を実行 |
| `make lint-fix` | golangci-lint を実行し、自動修正を適用 |
| `make manifests` | CRD、RBAC、webhook の YAML を再生成 |
| `make generate` | deepcopy メソッドを再生成 |
| `make fmt` | 全パッケージで `go fmt` を実行 |
| `make vet` | 全パッケージで `go vet` を実行 |

## ユニットテスト

ユニットテストは [envtest](https://book.kubebuilder.io/reference/envtest) が提供するローカルコントロールプレーンに対して実行されます。実行中のクラスタや Docker は不要です — `internal/ipam` と `internal/controller` のテストは実クラスタの代わりに fake Kubernetes クライアント（`sigs.k8s.io/controller-runtime/pkg/client/fake`）とスタブバイナリを使用します。`make test` は初回実行時に `envtest` バイナリ（`setup-envtest`）を自動的にダウンロードします — インターネットアクセスが一度だけ必要ですが、実行中のクラスタは不要です。

```bash
go mod download
make test
```

### テストガイドライン

- すべての新機能について、実装を書く前にテストを書いてください（TDD）。
- controller と webhook のテストには [envtest](https://book.kubebuilder.io/reference/envtest) を使用してください。
- 変更するパッケージ全体で 80% 以上のテストカバレッジを目指してください。
- テストの可読性のために AAA（Arrange-Act-Assert）構造を使用してください。
- テスト対象の動作を説明する、説明的なテスト関数名を使用してください。

```go
func TestReconcile_CreatesRCT_WhenNumaNetworkIsCreated(t *testing.T) {
    // Arrange
    nn := buildNumaNetwork("default", "my-network", "vf.nvidia.dra.net", "192.168.10.0/24")

    // Act
    result, err := reconciler.Reconcile(ctx, requestFor(nn))

    // Assert
    require.NoError(t, err)
    assert.Equal(t, ctrl.Result{}, result)
}
```

提出前にテストスイート全体を実行してください:

```bash
make test
make lint
```

## E2E テスト

### ローカルクラスタ

E2E テストは `NumaNetwork.spec.refResourceClaimDranet.ipRange` をエンドツーエンドで検証します: 各 k3d ノードに `dummy0` インターフェースを作成し、DRANET がそれを割当可能なデバイスとして公開し、`connectionType: direct` エッジを持つ Numaflow Pipeline がデプロイされた際に `webhook-whereabouts-numanetwork` が `whereabouts` 経由で `ipRange` から IP を割り当てます。Mutating Webhook はエッジの両方の vertex に ResourceClaimTemplate を注入するため、両方の vertex Pod が Secondary NIC を受け取ります。実 SR-IOV VF ハードウェアは不要です — `dummy0` が実 Secondary NIC の代わりを務めます（dranet 自身の upstream E2E テストも同じ手法を使用しています）。

**前提条件**: まず[ローカルクラスタ](docs/setup-guide.ja.md#1-ローカルクラスタ)の環境セットアップを完了してください — すべてのコンポーネント（`whereabouts`、DRANET、`dranet` DeviceClass、controller manager、`webhook-whereabouts-numanetwork`）がデプロイされ `READY` である必要があります。E2E テストの実行前にユニットテスト（`make test`）が通ることを確認してください。

すべてのステップを一括実行するには、ヘルパースクリプトを使用してください:

```bash
./hack/e2e-webhook-whereabouts.sh
```

以下の個別ステップは、スクリプトが行っている内容を説明しています。

#### 1. 各ノードに dummy インターフェースを作成

```bash
for node in k3d-numaflow-cluster-server-0 k3d-numaflow-cluster-agent-0 k3d-numaflow-cluster-agent-1; do
  docker exec "$node" sh -c "ip link show dummy0 >/dev/null 2>&1 || (ip link add dummy0 type dummy && ip link set up dev dummy0)"
done
```

DRANET が検出したことを確認します（出力に `dra.net/type: dummy` があることを確認）:

```bash
kubectl get resourceslice -o yaml | grep -A2 'ifName: dummy0'
```

#### 2. DeviceClass がデプロイされていることを確認

セットアップガイドでデプロイした dummy 専用の DeviceClass が存在することを確認します:

```bash
kubectl get deviceclass dranet-e2e-local
# 期待値: AGE 付きで DeviceClass が表示される
```

#### 3. DRANET の BYODP webhook 連携設定

このステップでは DRANET が IPAM を `webhook-whereabouts-numanetwork`（本プロジェクトで構築した webhook）に委譲するよう設定します。パッチは 3 種類の変更を行います:

- **Webhook 引数（常に必要）**: `--profile-provider=webhook` と `--webhook-url` は、NIC 割り当て時に DRANET が webhook を呼び出して IP を割り当てるための設定です。gpu-direct-comm を使用するすべての環境で恒久的に必要です。
- **dnsPolicy（常に必要）**: DRANET は `hostNetwork: true` で動作するため、デフォルトの `dnsPolicy: Default` ではホストの DNS リゾルバが使われ、クラスタ内 Service の `.svc` 名を解決できません。`ClusterFirstWithHostNet` でクラスタ DNS を使用させます。
- **イメージ差し替え（一時的）**: この記述時点で、公式の `registry.k8s.io/networking/dranet:stable` タグは `v1.3.0`（2026-05-28 リリース）からビルドされており、BYODP webhook 機能（[dranet PR #223](https://github.com/kubernetes-sigs/dranet/pull/223) で 2026-06-10 にマージ）より前のものです。公式リリースに含まれるまでは CI ビルドイメージを使用します:

```bash
docker pull gcr.io/k8s-staging-networking/dranet:v1.3.0-29-g1b7c7e5
k3d image import gcr.io/k8s-staging-networking/dranet:v1.3.0-29-g1b7c7e5 -c numaflow-cluster
```

> 将来この固定タグを再利用する前に、公式リリースに BYODP が含まれたか確認してください: `crane ls registry.k8s.io/networking/dranet` を実行し、[dranet リリースページ](https://github.com/kubernetes-sigs/dranet/releases)で PR #223 以降のバージョンを確認してください。存在する場合は、上記の `docker pull`/`k3d image import` をスキップし、公式の `stable` タグを使用してください。

パッチを適用します:

```bash
kubectl -n kube-system patch ds dranet --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"gcr.io/k8s-staging-networking/dranet:v1.3.0-29-g1b7c7e5"},
  {"op":"replace","path":"/spec/template/spec/dnsPolicy","value":"ClusterFirstWithHostNet"},
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--profile-provider=webhook"},
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--webhook-url=http://webhook-whereabouts-numanetwork.kube-system.svc:8443"}
]'
kubectl -n kube-system rollout status ds/dranet --timeout=90s
```

> dranet は `--webhook-url` の `/health` エンドポイントに起動時に到達できない場合、即座にクラッシュします（`Fatal`）。このため、このステップの*前に* `webhook-whereabouts-numanetwork` がデプロイ済みで `READY` でなければなりません — dranet を先に webhook モードに切り替え、その後に webhook をデプロイすると、crash-loop になります。

#### 4. Pipeline のデプロイ（NumaNetwork + ISBSvc + Pipeline）

`e2e_ip_assign_local.yaml` は NumaNetwork、InterStepBufferService、および `connectionType: direct` エッジを持つ Pipeline をバンドルしています。Mutating Webhook は `in`（source）と `out`（sink）の両方の vertex に `e2e-numanetwork-rct` を注入します:

```bash
kubectl apply -f config/testdata/e2e_ip_assign_local.yaml
kubectl get resourceclaimtemplate e2e-numanetwork-rct   # controller によって作成される
kubectl wait --for=condition=Ready pod -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline --timeout=120s
```

#### 5. ipRange から IP が割り当てられたことを検証

DRA ResourceClaim の `status.devices[].networkData` に、DRANET ドライバがデバイス割当後に書き込むネットワーク情報が格納されています。この方法はコンテナイメージにネットワークツール（`ip`, `ls` 等）が含まれていなくても動作します:

```bash
for pod in $(kubectl get pods -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline -o name | grep -E 'in-|out-'); do
  pod_name=$(echo "$pod" | sed 's|pod/||')
  node=$(kubectl get "$pod" -o jsonpath='{.spec.nodeName}')
  echo "=== $pod_name (node: $node) ==="
  # resourceClaimStatuses[] — この Pod にバインドされた ResourceClaim の一覧
  for claim in $(kubectl get "$pod" -o jsonpath='{.status.resourceClaimStatuses[*].resourceClaimName}'); do
    echo "  Claim: $claim"
    # devices[]          — Claim 内の各割当済みデバイス
    # networkData.ips[]  — IPAM プロバイダ（whereabouts）が割り当てた IP アドレス
    # networkData.interfaceName      — Pod 内の NIC 名（例: dummy0, enp4s0f0v0）
    # networkData.hardwareAddress    — NIC の MAC アドレス
    kubectl get resourceclaim "$claim" -o jsonpath='{range .status.devices[*]}    Interface: {.networkData.interfaceName}  MAC: {.networkData.hardwareAddress}  IPs: {.networkData.ips[*]}{"\n"}{end}'
  done
done
# 期待値: in と out の両方の vertex Pod で dummy0 に 192.168.140.0/24 内の IP が割り当てられている
```

#### 6. Pipeline 削除時に IP が解放されることを検証

```bash
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
kubectl delete pipeline e2e-gpu-direct-pipeline
sleep 5
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
# 期待値: 削除後に allocations マップが空（{}）になる
```

#### クリーンアップ

```bash
kubectl delete -f config/testdata/e2e_ip_assign_local.yaml
```

DRANET をテスト前の状態に完全に戻す（webhook 引数の除去、dnsPolicy の復元、公式イメージへの復元）には、以下を実行してください。本番環境では webhook 引数と dnsPolicy はそのまま維持します — 一時的なのはイメージ差し替えのみです:

```bash
kubectl -n kube-system patch ds dranet --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"registry.k8s.io/networking/dranet:stable"},
  {"op":"replace","path":"/spec/template/spec/dnsPolicy","value":"Default"},
  {"op":"remove","path":"/spec/template/spec/containers/0/args/4"},
  {"op":"remove","path":"/spec/template/spec/containers/0/args/3"}
]'
```

> 上記のステップ 1〜6 は現時点では手動のウォークスルーであり、自動化されたテストターゲットではありません。`make test-e2e`（`test/e2e/`）は kubebuilder がスキャフォールドした汎用テストスイートで、独自の Kind クラスタを起動しますが、DRANET、whereabouts、`NumaNetwork` は一切テストしません。このセクションに記載されたフローをカバーすることを期待して実行しないでください。

### ベアメタルクラスタ

ベアメタルでの E2E 検証は、上記の[ローカルクラスタ](#ローカルクラスタ)と同じフローに従います — NumaNetwork アノテーション付きの Pipeline がデプロイされ、両方の vertex Pod が Secondary NIC（SR-IOV VF）上で `NumaNetwork.spec.refResourceClaimDranet.ipRange` から IP を受け取る必要があります。ローカルクラスタとの違いは以下の通りです:

- `dummy0` インターフェースは不要 — 実 SR-IOV VF が Secondary NIC として機能します。
- ハードウェアが DRANET を通じて NIC 以外のデバイスも公開している場合を除き、E2E 用の絞り込み `DeviceClass`（`config/testdata/e2e_deviceclass_dranet_local.yaml`）は不要です。
- DRANET の固定イメージはレジストリから pull する必要があります（`k3d image import` は使用不可）。
- IP の検証には `docker exec` の代わりに SSH + `nsenter` を使用します。

**前提条件**: まず[ベアメタルクラスタ](docs/setup-guide.ja.md#2-ベアメタルクラスタ)の環境セットアップを完了してください — SR-IOV VF の準備を含め、`numaflow-dra-ansible` によるクラスタ/GPU/DRA/Numaflow レイヤー、DRANET、`dranet` DeviceClass、whereabouts、cert-manager、および gpu-direct-comm コンポーネント（CRD、controller manager、`webhook-whereabouts-numanetwork`）がすべてデプロイされ `READY` である必要があります。

#### 0. SR-IOV VF が DRANET に認識されていることを確認

E2E フローを開始する前に、DRANET が各ワーカーノードの SR-IOV VF を検出していることを確認します。

DRANET はノード上のすべての NIC を `ResourceSlice` オブジェクト（ノードごと・ドライバごとに1つ）内のデバイスとして公開します。各デバイスには `dra.net/*` 属性のセットが付与されます。DRANET は VF に対してのみ `dra.net/isSriovVf: true` を設定します。PF、非 SR-IOV 物理 NIC、ソフトウェアインターフェースにはこの属性自体が存在しません。

以下のコマンドで全ノードの VF デバイスのみを一覧できます:

```bash
kubectl get resourceslices -o json | jq -r '
  .items[]
  | select(.spec.driver == "dra.net")
  | .spec.nodeName as $node
  | .spec.devices[]
  | select(.attributes["dra.net/isSriovVf"].bool == true)
  | "\($node): \(.attributes["dra.net/ifName"].string) (PCI: \(.attributes["dra.net/pciAddress"].string))"
'
```

各ワーカーノードにつき VF が1行ずつ表示されるはずです。VF エントリが表示されない場合は、セットアップガイドの [SR-IOV VF の準備](docs/setup-guide.ja.md#1-sr-iov-vf-の準備) を見直してください。

また、セットアップガイドでデプロイした VF 専用の DeviceClass が存在することを確認します:

```bash
kubectl get deviceclass dranet-e2e-baremetal
# 期待値: AGE 付きで DeviceClass が表示される
```

DeviceClass が存在しない場合は、セットアップガイドの [DeviceClass の作成](docs/setup-guide.ja.md#3-deviceclass-の作成) ステップを見直してください。

#### 1. DRANET の BYODP webhook 連携設定

[ローカルクラスタのステップ 3](#3-dranet-の-byodp-webhook-連携設定) と同じですが、1点異なります: ベアメタルノードは `k3d image import` ではなくレジストリからイメージを pull します。固定された DRANET イメージがノードからアクセス可能なレジストリに存在することを確認してください:

```bash
# ノードが gcr.io から直接 pull できる場合:
kubectl -n kube-system patch ds dranet --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"gcr.io/k8s-staging-networking/dranet:v1.3.0-29-g1b7c7e5"},
  {"op":"replace","path":"/spec/template/spec/dnsPolicy","value":"ClusterFirstWithHostNet"},
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--profile-provider=webhook"},
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--webhook-url=http://webhook-whereabouts-numanetwork.kube-system.svc:8443"}
]'
kubectl -n kube-system rollout status ds/dranet --timeout=90s
```

ノードが `gcr.io` に到達できない場合は、まずイメージをプライベートレジストリにミラーリングし（`docker pull` + `docker tag` + `docker push`）、上記のパッチでミラーリングしたイメージリファレンスを使用してください。

> ローカルクラスタと同様に、この固定タグを再利用する前に、公式 DRANET リリースに BYODP が含まれたか確認してください — 根拠と確認方法は[ローカルクラスタのステップ 3](#3-dranet-の-byodp-webhook-連携設定) を参照してください。

#### 2. Pipeline のデプロイ（NumaNetwork + ISBSvc + Pipeline）

`config/testdata/e2e_ip_assign_baremetal.yaml` は `ipRange: "192.168.140.0/24"` を使用しており、ハードウェア上の実ネットワークがその範囲を既に使用していないことを前提としています。環境と競合する場合は、マニフェストのコピーで `NumaNetwork.spec.refResourceClaimDranet.ipRange` を調整してください:

```bash
kubectl apply -f config/testdata/e2e_ip_assign_baremetal.yaml
kubectl get resourceclaimtemplate e2e-numanetwork-rct   # controller によって作成される
kubectl wait --for=condition=Ready pod -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline --timeout=120s
```

Pod が `Pending` のままの場合、ResourceClaim 割当失敗のイベントを確認してください — よくある原因は DeviceClass がどの VF デバイスにもマッチしていないことです:

```bash
kubectl describe pod -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline | grep -A5 Events
```

#### 3. ipRange から IP が割り当てられたことを検証

DRA ResourceClaim の `status.devices[].networkData` に、DRANET ドライバがデバイス割当後に書き込むネットワーク情報が格納されています。この方法はベアメタルノードへの SSH アクセスや `sudo` 権限を必要としません:

```bash
for pod in $(kubectl get pods -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline -o name | grep -E 'in-|out-'); do
  pod_name=$(echo "$pod" | sed 's|pod/||')
  node=$(kubectl get "$pod" -o jsonpath='{.spec.nodeName}')
  echo "=== $pod_name (node: $node) ==="
  # resourceClaimStatuses[] — この Pod にバインドされた ResourceClaim の一覧
  for claim in $(kubectl get "$pod" -o jsonpath='{.status.resourceClaimStatuses[*].resourceClaimName}'); do
    echo "  Claim: $claim"
    # devices[]          — Claim 内の各割当済みデバイス
    # networkData.ips[]  — IPAM プロバイダ（whereabouts）が割り当てた IP アドレス
    # networkData.interfaceName      — Pod 内の NIC 名（例: enp4s0f0v0）
    # networkData.hardwareAddress    — NIC の MAC アドレス
    kubectl get resourceclaim "$claim" -o jsonpath='{range .status.devices[*]}    Interface: {.networkData.interfaceName}  MAC: {.networkData.hardwareAddress}  IPs: {.networkData.ips[*]}{"\n"}{end}'
  done
done
# 期待値: in と out の両方の vertex Pod で Secondary NIC に NumaNetwork.spec.refResourceClaimDranet.ipRange 内の IP が割り当てられている
```

Secondary NIC のインターフェース名はハードウェアに依存します（例: `enp4s0f0v0`）。上記出力の `Interface` フィールドに表示されます。

#### 4. Pipeline 削除時に IP が解放されることを検証

[ローカルクラスタのステップ 6](#6-pipeline-削除時に-ip-が解放されることを検証) と同じです:

```bash
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
kubectl delete pipeline e2e-gpu-direct-pipeline
sleep 5
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
# 期待値: 削除後に allocations マップが空（{}）になる
```

#### クリーンアップ

```bash
kubectl delete -f config/testdata/e2e_ip_assign_baremetal.yaml
```

DRANET をテスト前の状態に完全に戻す（webhook 引数の除去、dnsPolicy の復元、公式イメージへの復元）には、以下を実行してください。本番環境では webhook 引数と dnsPolicy はそのまま維持します — 一時的なのはイメージ差し替えのみです:

```bash
kubectl -n kube-system patch ds dranet --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"registry.k8s.io/networking/dranet:stable"},
  {"op":"replace","path":"/spec/template/spec/dnsPolicy","value":"Default"},
  {"op":"remove","path":"/spec/template/spec/containers/0/args/4"},
  {"op":"remove","path":"/spec/template/spec/containers/0/args/3"}
]'
```

> ローカルクラスタと同様に、これは現時点では手動のウォークスルーであり、自動化された CI ターゲットではありません。

## コミットメッセージ

[Conventional Commits](https://www.conventionalcommits.org/) 形式を使用してください:

```
<type>(<scope>): <description>

<optional body>

Signed-off-by: Your Name <your.email@example.com>
```

### タイプ

| タイプ | 使用する場面 |
|------|-------------|
| `feat` | 新機能 |
| `fix` | バグ修正 |
| `refactor` | 機能追加でもバグ修正でもないコード変更 |
| `test` | テストの追加または更新 |
| `docs` | ドキュメントの変更 |
| `chore` | ビルドプロセス、ツール、依存関係の更新 |
| `perf` | パフォーマンスの改善 |
| `ci` | CI/CD 設定の変更 |

### DCO 署名

すべてのコミットには DCO（Developer Certificate of Origin）署名を含める必要があります。コミット時に `-s` フラグを使用してください:

```bash
git commit -s -m "feat(controller): add health check endpoint"
```

これによりコミットメッセージに `Signed-off-by` 行が追加されます。あなたがコードを書いたか、プロジェクトのライセンスの下で提出する権利があることを証明するものです。全文は [developercertificate.org](https://developercertificate.org/) を参照してください。

署名のないコミットはマージされません。

## プルリクエストガイドライン

- すべてのプルリクエストは `main` ではなく **`develop`** ブランチに提出してください。
- PR は単一の変更に焦点を当ててください。関連のない変更を一つの PR に混ぜないでください。
- 明確なタイトルを書いてください（70文字以内）。
- レビューをリクエストする前に `make test` と `make lint` が通ることを確認してください。
- レビューをリクエストする前にマージコンフリクトを解消してください。

### PR 説明のテンプレート

```markdown
## Summary
- What does this PR do?
- Why is this change needed?

## Test Plan
- [ ] Unit tests added or updated
- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] (if applicable) E2E tests pass
```

PR の説明はシンプルな英語で書いてください。明確で簡潔な文が、複雑な表現よりも優れています。英語のネイティブスピーカーでない方の貢献も歓迎し、大切にしています。

## Claude Code の使用方法

このプロジェクトには `CLAUDE.md` が含まれており、Claude Code にコードベース、コマンド、アーキテクチャの全コンテキストを提供します。

```bash
claude    # プロジェクトルートで Claude Code を起動
```

Claude Code は `CLAUDE.md` を自動的に読み取り、機能の実装、テストの作成、コードベースのナビゲーションを支援できます。

## 行動規範

このプロジェクトは [Contributor Covenant 行動規範](https://www.contributor-covenant.org/version/2/1/code_of_conduct/) に従います。参加することにより、この基準を遵守することに同意します。容認できない行為はプロジェクトのメンテナに報告してください。
