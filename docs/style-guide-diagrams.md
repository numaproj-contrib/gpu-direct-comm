# Architecture Diagram Style Guide

gpu-direct-comm プロジェクトのアーキテクチャ図（draw.io）を作成・修正する際の設計原則。

## 1. Kubernetes の構造を正確に反映する

- **Control Plane / Worker Node の境界**を破線ボックスで必ず明示する
- **kube-apiserver** を中央ハブとして配置し、すべての通信が API Server を経由するフローにする
- **etcd** を明示し、保存されるリソースは etcd ボックス内に配置する
  - CRD / Custom Resource（NumaNetwork CR, Pipeline CR）
  - K8s Native Resource（RCT, ResourceClaim, Pod spec）
  - ルーティング設定（Service, ValidatingWebhookConfiguration）
  - Secret（TLS 証明書等）
- Pod 作成フローは必ず以下の順序で描く:
  ```
  Controller → API Server → etcd (Pod spec) → kube-scheduler → kubelet → Pod
  ```
  Controller が直接 Pod を作成する矢印は誤り

## 2. ユーザーの始点を明示する

- ユーザーがデプロイする YAML（CR 等）を明示的に始点として残す
- User アクターから API Server への矢印で `kubectl apply` を表現する
- ①② のような番号で操作順序を示す

## 3. デプロイ形態を明示する

各コンポーネントのデプロイ形態をラベルに含める:

| コンポーネント | デプロイ形態 | 備考 |
|---|---|---|
| NumaNetwork Controller + ValidatingWebhook | 単一 Deployment (`gpu-direct-comm`) | 同一 Pod 内で動作 |
| Pipeline Controller | Deployment (Numaflow) | 外部プロジェクト |
| cert-manager | Deployment x3 | cert-manager, webhook, cainjector |
| DRANET | DaemonSet | kubelet の DRA Plugin |
| whereabouts IPAM | DaemonSet | |

- 同一 Deployment 内のコンポーネントは破線ボックスでグループ化する
- cert-manager は operator とは独立した Deployment 群である

## 4. 矢印でリクエストとレスポンスを区別する

| 線種 | 意味 | 用途 |
|---|---|---|
| 実線 (→) | リクエスト / アクション | API 呼出、リソース作成、Pod 起動 |
| 破線 (⇢) | レスポンス / イベント通知 | 結果返却、watch イベント、証明書注入 |

- 双方向の通信（例: DRANET ↔ whereabouts IPAM）は、リクエストとレスポンスを別矢印にする
- 各矢印のラベルには「何を」伝えているかを簡潔に記載する

## 5. 処理チェーンを省略しない

処理フロー上に登場するコンポーネントは全て出す:

- kube-scheduler（Pod スケジューリング）
- kubelet（Pod 起動）
- kube-proxy（Service ルーティング、エッジラベルで表現可）
- 中間リソース（ResourceClaim, Certificate CR, Secret）

省略されたコンポーネントがあると、フローの因果関係が断絶して見える。

## 6. 可読性を確保する

- 線と線のラベルが重ならないよう配置する
- エッジの経由点 (waypoint) を明示的に指定し、線の経路を分散させる
- フローの方向を統一する（例: 左→右 = リクエスト方向、上→下 = 依存方向）
- draw.io の GUI で最終的な微調整を行う

## 7. 色分けの凡例

| 色 | 意味 |
|---|---|
| 紫 (`#E1D5E7`) | K8s Core Component（apiserver, scheduler） |
| 青 (`#dae8fc`) | CRD / Custom Resource（ユーザーが YAML でデプロイ） |
| 緑 (`#d5e8d4`) | Controller / Webhook（本プロジェクトのコード） |
| 黄 (`#fff2cc`) | External / Plugin Component（cert-manager, DRANET, whereabouts） |
| グレー (`#f5f5f5`) | K8s Native Resource（RCT, ResourceClaim, Service, Secret） |
| オレンジ (`#FFE6CC`) | Secondary NIC（RDMA 対応） |
| 赤線 (`#CC0000`) | RDMA データパス |
