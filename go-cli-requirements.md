# Go CLI 需求说明

本文整理 Harness Go CLI 的需求。目标是把门禁从 Agent 口头判断转为可执行、可审计、可在 CI 中阻塞的 CLI 协议。

## 背景

文章提出门禁应是机读的，而不是口头的。当前需要补齐工程落地方式：

- Agent 不直接决定是否通过。
- CLI 负责执行或验证门禁。
- CI 通过调用 CLI 获得确定的退出码。
- 历史 Markdown 只作为旧审计快照，不作为机器判定事实源。

## 核心目标

构建一个 Go 语言 CLI，暂称 `janus`，用于管理 Harness 门禁结果。

CLI 必须支持：

- 校验门禁 JSON。
- 验证门禁是否允许阶段推进。
- 在 CI / MR 中返回稳定退出码。

## 事实源模型

不同领域有不同事实源。不能让一个 JSON 管所有事实。

| 领域 | 事实源 | 说明 |
| --- | --- | --- |
| 需求语义 | `requirement.md` | 人修改，用于澄清需求 |
| 影响面 | `impact-analysis.md` | 人或 Agent 补充，用于描述服务、契约和风险 |
| 设计方案 | `design.md` | 人或 Agent 修改，用于记录工程方案 |
| 任务拆分 | `tasks.json` | 机器可读任务列表 |
| 服务矩阵 | `.service-matrix/dependencies.yaml` | 服务和仓库拓扑 |
| IDL 契约 | `.proto` 或其他 IDL 文件 | 契约自身真相源 |
| 门禁判定 | `*.gate.json` | 某次门禁对某组输入快照的判定结果 |
| 历史门禁快照 | `*.md` | 旧流程留下的审计快照，不再生成、刷新或校验 |

门禁 JSON 只表示：

```text
基于某一组输入文件的某一版内容，这个门禁当时是否通过。
```

## 非目标

- 不让 CLI 替代需求、设计或代码的事实源。
- 不把 Markdown 解析作为主判定路径。
- 不让 CI 临时调用 LLM 重新做语义评审。
- 不允许 Agent 绕过 CLI 直接宣布门禁通过。
- 不在第一版实现完整 Agent Runtime。

## 文件命名

门禁结果推荐使用：

```text
requirements/{requirement-id}/gates/{gate-id}.gate.json
```

示例：

```text
requirements/T12345/gates/design-review.gate.json
```

## JSON 数据结构

`*.gate.json` 是门禁判定事实源。

最小结构：

```json
{
  "schema_version": "1.0",
  "requirement_id": "T12345",
  "gate_id": "design-review",
  "gate_name": "设计门禁",
  "stage": "3.3",
  "checked_by": "detail-design-quality-reviewer",
  "checked_at": "2026-05-31T10:00:00+08:00",
  "result": "BLOCKED",
  "blocks_next_stage": true,
  "inputs": [
    {
      "path": "requirements/T12345/requirement.md",
      "sha256": "..."
    },
    {
      "path": "requirements/T12345/design.md",
      "sha256": "..."
    }
  ],
  "checklist": [
    {
      "item": "明确回滚方案",
      "result": "BLOCKED",
      "evidence": "design.md 缺少回滚章节"
    }
  ],
  "blocking_issues": [
    {
      "issue": "缺少回滚方案",
      "required_action": "补充灰度关闭和代码回滚策略",
      "owner": "backend"
    }
  ],
  "warnings": [],
  "waiver": {
    "required": false
  },
  "decision": "不允许进入 4.1 任务拆分。"
}
```

涉及仓库或外部证据时，可以扩展：

```json
{
  "repos": [
    {
      "name": "business-repo",
      "branch": "feature/T12345",
      "commit": "abc123"
    },
    {
      "name": "idl-repo",
      "branch": "feature/T12345",
      "commit": "def456"
    }
  ],
  "evidence": [
    {
      "type": "buf-breaking",
      "path": "reports/T12345/buf-breaking.txt",
      "sha256": "..."
    }
  ]
}
```

## Markdown 渲染规则

Markdown 由 CLI 从 `*.gate.json` 渲染。

Markdown 顶部必须写入生成声明：

```markdown
<!-- Generated from design-review.gate.json. Do not edit blocking fields here. -->
```

Markdown 可用于：

- 人类审阅。
- Code Review 展示。
- 复盘审计。

Markdown 不用于：

- CI 阻塞判定。
- 阶段推进判定。
- 作为 `Result` 的权威来源。

## 快照绑定和过期判定

门禁 JSON 必须记录输入文件快照。

当 `inputs[].sha256` 与当前文件内容不一致时，CLI 必须判定门禁结果已过期。

示例：

```text
requirement.md 被修改后，旧的 requirement-review.gate.json 不能继续放行。
design.md 被修改后，旧的 design-review.gate.json 不能继续放行。
tasks.json 被修改后，旧的 dev-entry.gate.json 不能继续放行。
.service-matrix/dependencies.yaml 被修改后，旧的 service-repo-check.gate.json 不能继续放行。
```

## CLI 命令

### `janus gate validate`

校验门禁 JSON 格式和状态一致性。

```sh
janus gate validate requirements/T12345/gates/design-review.gate.json
```

必须检查：

- JSON 可解析。
- 必填字段存在。
- `result` 只能是 `PASS`、`BLOCKED`、`WARN`、`WAIVED`。
- `blocks_next_stage` 与 `result` 一致。
- `BLOCKED` 必须包含 `blocking_issues`。
- `WAIVED` 必须包含完整豁免信息。

### `janus gate verify`

验证某个门禁是否可用于阶段推进或 CI 放行。

```sh
janus gate verify \
  --input requirements/T12345/gates/design-review.gate.json \
  --ticket-id T12345
```

必须检查：

- `validate` 通过。
- 输入文件 hash 未过期。
- 当 gate JSON 包含 `repos` 时，必须传入 `--ticket-id`。
- 当 gate JSON 包含 `repos` 时，所有 `repos[].branch` 必须完全一致，并且分支名必须包含 `--ticket-id`。
- `result` 不是 `BLOCKED`。
- `WAIVED` 未过期。
- 相关证据文件存在且 hash 匹配。

### `janus delivery verify`

验证多仓需求在当前 PR / push 上是否满足交付阶段规则。

```sh
janus delivery verify \
  --requirement T12345 \
  --repo business-repo \
  --workspace /path/to/spark-workspace \
  --base epic/foo \
  --head feature/T12345
```

必须检查：

- 读取 `harness-repo/requirements/<id>/requirement.md` front matter。
- `target_branch == release_branch` 时判定为 release-bound，并使用
  `release-readiness` / `formal-only`。
- `target_branch != release_branch` 时判定为 integration-bound，并使用
  `integration-readiness` / `rc-or-formal`。
- 当前 PR base 与 `target_branch` 一致。
- 当前 PR head 与 `related_branch` 一致。
- integration-bound peer repo 满足当前阶段：同名 `related_branch` 存在、
  `related_branch` 已合入 `target_branch`、或 `target_branch` 已合入
  `release_branch`。
- release-bound peer repo 满足当前阶段：`related_branch` 已合入
  `release_branch`，或 `target_branch` 已合入 `release_branch`。
- 当前仓是 `business-repo` 时，对当前 PR 变更到的 contract dependency 文件执行
  `rc-or-formal` 或 `formal-only` 扫描。
- release-bound 且变更文件消费 formal contract version 时，验证 IDL formal tag
  存在、tag commit 可从 `release_branch` 追溯、Java Maven artifact 或 Go module
  tag 存在，且 artifact version 与 dependency version 匹配。
- `--output-gate` 存在时写入 gate JSON，失败时也写入 `BLOCKED` 报告。

CLI 不自动执行 Formal 发布。Formal 发布由人完成后，delivery / release readiness
验证 business dependency、tag、commit 和 artifact。未变更 contract dependency 文件
时，不强扫历史依赖债务。

### `janus requirement verify`

验证某个需求在指定目标下是否满足门禁要求。

```sh
janus requirement verify \
  --requirement T12345 \
  --target merge
```

`--target merge` 至少应检查：

- 标准阶段门禁报告存在：`requirement-review`、`design-review`、`dev-entry`、`service-repo-check`。
- 最终合并门禁报告存在：`merge-readiness`。
- 门禁 JSON 未过期。
- 门禁结果允许继续。
- 包含 `repos` 的门禁必须通过分支一致性和 ticket id 追溯校验；默认使用 `--requirement` 作为 ticket id，显式传入 `--ticket-id` 时使用显式值。
- 早期阶段门禁只声明 IDL 影响和设计/任务/仓库准备状态，不要求实现证据。
- 涉及 IDL 时，`merge-readiness` 必须有 Buf 或契约检查证据。
- 不涉及 IDL 时有 `N/A` 理由。

## 退出码

CLI 必须返回稳定退出码。

| 退出码 | 含义 |
| --- | --- |
| 0 | 通过 |
| 1 | 门禁阻塞 |
| 2 | JSON schema 或字段格式错误 |
| 3 | 必要文件缺失 |
| 4 | 豁免不合法或已过期 |
| 5 | 输入快照过期 |
| 6 | 证据缺失或 hash 不匹配 |
| 7 | 分支或 peer repo 状态不满足 delivery policy |

CI 只根据退出码决定是否失败。

## CI 行为

CI 不重新判断业务语义，只调用 `janus`。

推荐流程：

```text
MR / Push 触发 CI
  -> 定位 requirement id
  -> 调用 janus requirement verify --target merge
  -> 根据退出码通过或失败
```

GitHub Actions 示例：

```yaml
name: harness-gates

on:
  pull_request:
    branches: [main]

jobs:
  verify-gates:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - name: Build janus
        run: go build -o ./bin/janus ./janus/cmd/janus

      - name: Detect requirement id
        id: req
        run: |
          REQ_ID=$(git log -1 --pretty=%B | sed -n 's/.*Requirement: \\(T[0-9]\\+\\).*/\\1/p')
          if [ -z "$REQ_ID" ]; then
            echo "Missing Requirement ID"
            exit 3
          fi
          echo "requirement_id=$REQ_ID" >> "$GITHUB_OUTPUT"

      - name: Verify Harness gates
        run: |
          ./bin/janus requirement verify \
            --requirement "${{ steps.req.outputs.requirement_id }}" \
            --target merge
```

## EARS-style 需求

### 通用规则

- The CLI shall treat `*.gate.json` as the canonical source for gate decisions.
- The CLI shall not generate, refresh, or validate gate Markdown.
- The CLI shall ignore historical gate Markdown for CI, stage progression, and merge verification.
- The CLI shall fail verification when a gate JSON input hash does not match the current file content.
- The CLI shall return stable non-zero exit codes for blocked, invalid, missing, stale, waived, and evidence-failed states.
- The Agent shall invoke the CLI for final gate status instead of declaring PASS or BLOCKED directly.
- The CI shall invoke the CLI and shall not re-run semantic LLM review during merge verification.

### 触发行为

- When `janus gate validate` receives invalid JSON, the CLI shall exit with code `2`.
- When `janus gate render` is requested, the CLI shall reject it as an unknown gate subcommand.
- When `janus gate verify` finds `result = BLOCKED`, the CLI shall exit with code `1`.
- When `janus gate verify` finds stale input hashes, the CLI shall exit with code `5`.
- When `janus requirement verify --target merge` cannot find the required gate report, the CLI shall exit with code `3`.

### 条件行为

- If `result = PASS`, then `blocks_next_stage` must be `false`.
- If `result = WARN`, then `blocks_next_stage` must be `false` and warnings must include follow-up actions.
- If `result = BLOCKED`, then `blocks_next_stage` must be `true` and blocking issues must be non-empty.
- If `result = WAIVED`, then waiver reason, approver, approval time, expiry time, and follow-up issue must be present.
- If IDL impact is `yes` on `merge-readiness`, then evidence for contract checks must be present.
- If IDL impact is `yes` on an earlier stage gate, then the CLI shall not require implementation evidence on that gate.
- If IDL impact is `no`, then an `N/A` reason must be present.

## 验收标准

第一版 CLI 完成时，应满足：

- 能读取并校验合法的 `*.gate.json`。
- 能拒绝非法状态组合。
- 能根据 JSON 渲染 Markdown。
- 能检查 Markdown 是否漂移。
- 能根据输入文件 hash 判断门禁是否过期。
- 能对 `PASS`、`BLOCKED`、`WARN`、`WAIVED` 返回正确退出码。
- 能在 CI 中以 `janus requirement verify --target merge` 作为合并阻塞入口。

## 后续可扩展点

- 支持 JSON Schema 文件导出。
- 支持多门禁批量校验。
- 支持从 MR 描述、分支名或提交信息自动解析 requirement id。
- 支持输出 SARIF 或 GitHub Check annotations。
- 支持插件化语义检查器，但最终结果仍写入 `*.gate.json`。
