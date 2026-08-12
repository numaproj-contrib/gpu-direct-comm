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
| `make test-e2e-full-local` | k3d で統合 E2E テストを実行（M1〜M6 全検証） |
| `make test-e2e-full-baremetal` | ベアメタルで統合 E2E テストを実行（SR-IOV VF） |
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

E2E テストは以下の 2 つのゴールをエンドツーエンドで検証します:

- **Goal 1（IP 割当）**: 各 k3d ノードに `dummy0` インターフェースを作成し、DRANET がそれを割当可能なデバイスとして公開し、`connectionType: direct` エッジを持つ Numaflow Pipeline がデプロイされた際に `webhook-whereabouts-numanetwork` が `whereabouts` 経由で `ipRange` から IP を割り当てます。Mutating Webhook はエッジの両方の vertex に ResourceClaimTemplate を注入するため、両方の vertex Pod が Secondary NIC を受け取ります。実 SR-IOV VF ハードウェアは不要です — `dummy0` が実 Secondary NIC の代わりを務めます（dranet 自身の upstream E2E テストも同じ手法を使用しています）。
- **Goal 2（DNS 名前解決）**: vertexDomainMutator が Pod に FQDN annotation を注入し、vertexDomainController が CoreDNS etcd に DNS レコードを登録し、パイプライン中の各 Pod から宛先 Pod の IP 一覧が DNS で取得できることを検証します。

**前提条件**: まず[ローカルクラスタ](docs/setup-guide.ja.md#1-ローカルクラスタ)の環境セットアップを完了してください — すべてのコンポーネント（`whereabouts`、DRANET、`dranet` DeviceClass、controller manager、`webhook-whereabouts-numanetwork`、CoreDNS etcd バックエンド）がデプロイされ `READY` である必要があります。E2E テストの実行前にユニットテスト（`make test`）が通ることを確認してください。

すべてのステップを一括実行するには:

```bash
# Goal 1 + Goal 2 の全フロー
make test-e2e-full-local
# または: ./hack/e2e-full-flow.sh --env local

# Goal 1（IP 割当）のみ検証したい場合
# ./hack/e2e-webhook-whereabouts.sh
```

以下の個別ステップは、スクリプトが行っている内容を説明しています。環境のセットアップ（dummy インターフェース作成、DeviceClass、BYODP webhook 連携設定など）は [setup-guide.ja.md](docs/setup-guide.ja.md#1-ローカルクラスタ) の確認セクションがすべて通っていることを前提とします。

#### 1. Pipeline のデプロイ（NumaNetwork + ISBSvc + Pipeline）

`e2e_full_flow_local.yaml` は NumaNetwork と `connectionType: direct` エッジを持つ Pipeline をバンドルしています。NumaNetworkReconciler が RCT（`<numaNetwork名>-rct`）を作成し、Pipeline Mutating Webhook が direct binding に参加する vertex に RCT を注入します:

```bash
kubectl apply -f config/testdata/e2e_full_flow_local.yaml
kubectl get resourceclaimtemplate e2e-full-flow-nn-rct   # controller によって作成される
kubectl wait --for=condition=Ready pod -l numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline,app.kubernetes.io/component=vertex --timeout=120s
```

#### 2. ipRange から IP が割り当てられたことを検証

DRA ResourceClaim の `status.devices[].networkData` に、DRANET ドライバがデバイス割当後に書き込むネットワーク情報が格納されています。この方法はコンテナイメージにネットワークツール（`ip`, `ls` 等）が含まれていなくても動作します:

```bash
# vertex-domain label でフィルタリング（controller と同じ入口）
for pod in $(kubectl get pods -l gpu-direct-comm.numaproj.io/vertex-domain=true,numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline -o name); do
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
# 期待値: direct binding に参加する vertex Pod で dummy0 に 192.168.140.0/24 内の IP が割り当てられている
```

#### 3. etcd に Pod の FQDN が登録されていることを検証

vertexDomainController は、`vertex-domain=true` label が付与された Pod の Secondary NIC IP を CoreDNS etcd に登録します。FQDN は `<vertex>.<pipeline>.<namespace>.vertexdomain.local` の形式です。

```bash
# etcd に登録された DNS レコードのキーを一覧
kubectl -n kube-system exec etcd-coredns-0 -- \
  etcdctl get --prefix /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/ --keys-only
# 期待値: in vertex と out vertex それぞれの Pod 分のキーが存在する
#   /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/in/<pod-id>
#   /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/out/<pod-id>

# 各レコードの値（Pod の Secondary NIC IP）を確認
kubectl -n kube-system exec etcd-coredns-0 -- \
  etcdctl get --prefix /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/ --print-value-only
# 期待値: 各レコードが {"host":"192.168.141.x"} 形式の JSON
```

#### 4. パイプライン中の Pod から宛先 Pod の IP 一覧が取得できることを検証

送信側（From: `in`）から宛先（To: `out`）の FQDN を DNS で引き、宛先 Pod の IP が返ることを確認します（ADR-004: 単方向通信）。`out` vertex は `scale.min: 2` のため、同一 FQDN から複数 IP が返ります（ラウンドロビン）。

```bash
# DNS 検証用の一時 Pod を起動
kubectl run e2e-dns-test --image=busybox:1.36 --restart=Never -- sleep 3600
kubectl wait --for=condition=Ready pod/e2e-dns-test --timeout=30s

# out vertex（宛先）の FQDN を解決（2 Pod → 2 IP、ラウンドロビン）
# 直接通信では From 側が To 側の FQDN を引いて宛先 IP を取得する
kubectl exec e2e-dns-test -- nslookup out.e2e-full-flow-pipeline.default.vertexdomain.local
# 期待値: Address 行に 192.168.141.x が 1 件以上返る

# テスト Pod はステップ５で使用するため，まだ削除しない
```

#### 5. Pipeline を削除しリソースが解放されることを検証

Pipeline と NumaNetwork を削除し、DNS レコードと IP アドレスがすべてクリーンアップされることを確認します:

```bash
# Pipeline と NumaNetwork を削除
kubectl delete -f config/testdata/e2e_full_flow_local.yaml --wait=true --timeout=60s

# DNS レコードが etcd から削除されたことを確認
kubectl -n kube-system exec etcd-coredns-0 -- \
  etcdctl get --prefix /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/ --keys-only
# 期待値: 出力なし（レコードがすべて削除されている）

# nslookup で NXDOMAIN が返ることを確認
kubectl exec e2e-dns-test -- nslookup out.e2e-full-flow-pipeline.default.vertexdomain.local
# 期待値: NXDOMAIN

# whereabouts IP pool の allocations が解放されたことを確認
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
# 期待値: 空（{}）

# テスト Pod を削除
kubectl delete pod e2e-dns-test
```

> 上記のステップ 1〜5 は現時点では手動のウォークスルーであり、自動化されたテストターゲットではありません。`make test-e2e`（`test/e2e/`）は kubebuilder がスキャフォールドした汎用テストスイートで、独自の Kind クラスタを起動しますが、DRANET、whereabouts、`NumaNetwork` は一切テストしません。このセクションに記載されたフローをカバーすることを期待して実行しないでください。

### ベアメタルクラスタ

ベアメタルでの E2E 検証は、上記の[ローカルクラスタ](#ローカルクラスタ)と同じ Goal 1（IP 割当）+ Goal 2（DNS 名前解決）を検証します。ローカルクラスタとの違いは以下の通りです:

- `dummy0` インターフェースは不要 — 実 SR-IOV VF が Secondary NIC として機能します。
- IP の検証には `docker exec` の代わりに DRA ResourceClaim の `networkData` を使用します。

**前提条件**: まず[ベアメタルクラスタ](docs/setup-guide.ja.md#2-ベアメタルクラスタ)の環境セットアップを完了し、確認セクションがすべて通っていることを確認してください。

すべてのステップを一括実行するには:

```bash
# Goal 1 + Goal 2 の全フロー
make test-e2e-full-baremetal
# または: ./hack/e2e-full-flow.sh --env baremetal
```

以下の個別ステップは、スクリプトが行っている内容を説明しています。

#### 1. Pipeline のデプロイ（NumaNetwork + ISBSvc + Pipeline）

`config/testdata/e2e_full_flow_baremetal.yaml` は `ipRange: "192.168.140.0/24"` を使用しており、ハードウェア上の実ネットワークがその範囲を既に使用していないことを前提としています。環境と競合する場合は、マニフェストのコピーで `NumaNetwork.spec.refResourceClaimDranet.ipRange` を調整してください:

```bash
kubectl apply -f config/testdata/e2e_full_flow_baremetal.yaml
kubectl get resourceclaimtemplate e2e-full-flow-nn-rct   # controller によって作成される
kubectl wait --for=condition=Ready pod -l numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline,app.kubernetes.io/component=vertex --timeout=120s
```

Pod が `Pending` のままの場合、ResourceClaim 割当失敗のイベントを確認してください — よくある原因は DeviceClass がどの VF デバイスにもマッチしていないことです:

```bash
kubectl describe pod -l numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline | grep -A5 Events
```

#### 2. ipRange から IP が割り当てられたことを検証

DRA ResourceClaim の `status.devices[].networkData` に、DRANET ドライバがデバイス割当後に書き込むネットワーク情報が格納されています。この方法はベアメタルノードへの SSH アクセスや `sudo` 権限を必要としません:

```bash
for pod in $(kubectl get pods -l gpu-direct-comm.numaproj.io/vertex-domain=true,numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline -o name); do
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

#### 3. etcd に Pod の FQDN が登録されていることを検証

[ローカルクラスタのステップ 3](#3-etcd-に-pod-の-fqdn-が登録されていることを検証) と同じです。

#### 4. パイプライン中の Pod から宛先 Pod の IP 一覧が取得できることを検証

[ローカルクラスタのステップ 4](#4-パイプライン中の-pod-から宛先-pod-の-ip-一覧が取得できることを検証) と同じです。

#### 5. Pipeline を削除しリソースが解放されることを検証

[ローカルクラスタのステップ 5](#5-pipeline-を削除しリソースが解放されることを検証) と同じ手順です。testdata ファイルのみ変更してください:

```bash
kubectl delete -f config/testdata/e2e_full_flow_baremetal.yaml --wait=true --timeout=60s
```

DNS レコード削除、NXDOMAIN 確認、IP pool 解放の検証はローカルクラスタと同じです。

#### クリーンアップ

```bash
kubectl delete pod e2e-dns-test --ignore-not-found
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
