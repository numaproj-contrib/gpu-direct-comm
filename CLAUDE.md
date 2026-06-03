# CLAUDE.md (gpu-direct-comm)

## 位置付け
- アクセラレータ間直接通信 STEP2 の主開発リポジトリ

## Workspace Layout

```
~/project/
├── gpu-direct-comm/        # ここ（STEP2 main dev）
│   ├── k3d-config.yaml     # Local k3d cluster config
│   └── numaflow_dev_env_tools/  # Dev tool installers
├── numaflow/               # Upstream Numaflow fork (sesame0224, reference only)
└── compsysg-numaflow/      # Custom fork — STEP1（設計参考用）

~/project/kubernetes/       # Kubernetes source (reference only)
~/go/bin/                   # GOPATH bin: controller-gen, client-gen, etc.
```

## Upstream 参照ポリシー
- `../numaflow/` (sesame0224 fork): upstream 参照専用、編集禁止
- `../compsysg-numaflow/`: STEP1 カスタム fork、設計参考用
- 本リポは独立リポとして STEP2 を実装する

## Build / Test / Lint
- 未確定（go.mod 未作成。kubebuilder init 後に追記）

## ECC (Everything Claude Code)
- プロジェクトローカル導入済み: `.claude/` 配下
- 追加導入: `node ~/everything-claude-code/scripts/install-apply.js --target claude-project --modules <id>`
- 状態: `.claude/ecc/install-state.json`

## External Documents
### アクセラレータ間直接通信
- [AICP-Numaflow-PoCマイルストーン](https://compsysg.atlassian.net/wiki/spaces/DCC/pages/982679603/AICP-Numaflow-PoC)
- [アクセラレータ間直接通信 全体計画](https://compsysg.atlassian.net/wiki/spaces/DCC/pages/1052409966)
- [アクセラレータ間直接通信 STEP2設計](https://compsysg.atlassian.net/wiki/spaces/DCC/pages/1711996930/STEP?atlOrigin=eyJpIjoiOWJiNzc0OGIwZTEwNDUxMmFiMmRiYjQ2ODAyNTZkZWIiLCJwIjoiYyJ9)
- [アクセラレータ間直接通信 STEP2作業項目](https://compsysg.atlassian.net/wiki/spaces/DCC/pages/1715634194/STEP2)

### Tools
- [Everything-Claude-Codeの使用方法](https://compsysg.atlassian.net/wiki/spaces/SCG/pages/2087911559/Everything-Claude-Code?atlOrigin=eyJpIjoiOGM1Y2E3Nzg0MDFkNGUxZGFlZjJlN2FjMzhlNzFjNmEiLCJwIjoiYyJ9)
