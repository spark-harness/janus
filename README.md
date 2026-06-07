# Janus

`janus/` 用于整理 Harness Go CLI 的需求、设计和后续实现计划。

Janus 是 Harness 门禁 CLI。它把 `*.gate.json` 作为门禁判定事实源，把 Markdown 作为审计视图，并通过稳定退出码让 CI 可以阻塞不合规的阶段推进。

## 构建

```sh
go build -o ./bin/janus ./cmd/janus
```

构建所有目标平台：

```sh
./scripts/build-all.sh
```

Windows PowerShell：

```powershell
./scripts/build-all.ps1
```

## 命令

### 查看版本

```sh
janus version
```

所有 Harness 运行环境必须保证 `janus` 在 PATH 中，并能执行 `janus version`。

### 校验门禁 JSON

```sh
janus gate validate requirements/T12345/gates/design-review.gate.json
```

校验内容包括：

- JSON 可解析，且不包含未声明字段。
- 必填字段存在。
- `result` 只能是 `PASS`、`BLOCKED`、`WARN`、`WAIVED`。
- `blocks_next_stage` 与 `result` 一致。
- `BLOCKED` 必须有 `blocking_issues`。
- `WARN` 必须有带后续动作的 `warnings`。
- `WAIVED` 必须有完整豁免信息。

### 渲染 Markdown

```sh
janus gate render \
  --input requirements/T12345/gates/design-review.gate.json \
  --output requirements/T12345/gates/design-review.md
```

检查 Markdown 是否漂移：

```sh
janus gate render --check \
  --input requirements/T12345/gates/design-review.gate.json \
  --output requirements/T12345/gates/design-review.md
```

### 验证单个门禁

```sh
janus gate verify \
  --input requirements/T12345/gates/design-review.gate.json \
  --ticket-id T12345
```

`gate verify` 会先执行 JSON 校验，再检查：

- `inputs[].sha256` 是否匹配当前文件内容。
- 如果 gate JSON 包含 `repos`，必须传入 `--ticket-id`，并且所有 `repos[].branch` 必须一致且包含该 ticket id。
- `result` 是否为 `BLOCKED`。
- `WAIVED` 是否已过期。
- `evidence[].sha256` 是否匹配当前证据文件。

路径按当前工作目录解析。CI 应从仓库根目录运行 `janus`。

### 验证需求合并门禁

```sh
janus requirement verify \
  --requirement T12345 \
  --target merge
```

### 需求生命周期命令

创建需求目录：

```sh
janus requirement new T12345 \
  --title "Mobile Code Register/Login" \
  --owner "Harness Team"
```

`new` 会创建 `requirements/<id>/`，复制需求、影响面、设计和任务模板，并初始化 `gates/`、`reviews/`、`evidence/` 目录。`README.md` 会记录 `current_stage: "1"`。

查看当前状态：

```sh
janus requirement status T12345
```

`status` 会检查生命周期产物、四道门禁、输入 hash、证据 hash 和 Markdown 渲染漂移，并给出下一步动作。

生成指定门禁报告：

```sh
janus requirement gate-check \
  --requirement T12345 \
  --gate requirement-review
```

支持的 gate id：

- `requirement-review`
- `design-review`
- `dev-entry`
- `service-repo-check`

`gate-check` 会生成 `requirements/<id>/gates/<gate-id>.gate.json`，并同步渲染 Markdown 审计视图。当前实现只做确定性机器检查；即使机器检查通过，只要没有人工批准记录，顶层结果仍为 `BLOCKED`。

人工审批状态保留在对应 Markdown 的 front matter 中：

- `requirement-review` 读取 `requirements/<id>/requirement.md`。
- `design-review` 读取 `requirements/<id>/design.md`。
- `dev-entry` 继续读取结构化的 `tasks.json`，当前不从 Markdown 自动放行。
- `service-repo-check` 当前不从 Markdown 自动放行。

`requirement-review` 的批准字段示例：

```yaml
---
requirement_review_status: "approved"
approved_by: "forest"
approved_at: "2026-06-07T20:30:00+08:00"
decision: "需求定义通过，可以进入设计阶段。"
---
```

`design-review` 使用同样字段，但状态字段为 `design_review_status: "approved"`。Janus 会把 Markdown 审批源和输入文件 hash 固化到 gate JSON 快照中；后续输入文件变化会让 gate 变为 stale。

推进阶段：

```sh
janus requirement next --requirement T12345
```

`next` 根据 `current_stage` 找到必需门禁，运行门禁验证，通过后更新 `README.md` 的 `current_stage`。缺失门禁、过期 hash、`BLOCKED` 门禁都会阻塞推进。

合并前总体验证：

```sh
janus requirement verify \
  --requirement T12345 \
  --target merge
```

`merge` 目标使用这个规则：

- 查找 `requirements/<requirement-id>/gates/*.gate.json`。
- 至少必须存在一个 gate JSON。
- 每个 gate JSON 都必须通过 `gate verify`。
- 如果 gate JSON 包含 `repos`，默认使用 `--requirement` 作为 ticket id 校验分支；需要覆盖时可传 `--ticket-id`。
- 如果 gate 声明 `idl_impact.impact = "yes"`，必须提供 `evidence`。
- 如果 gate 声明 `idl_impact.impact = "no"`，必须提供 `idl_impact.na_reason`。

后续如果新增必需 gate 清单或阶段配置，`requirement verify` 应改为读取配置，而不是只验证已发现的 gate 文件。

### Codex Hook 输出

```sh
janus hook gate-drift-check
```

该命令扫描当前仓库 `requirements/**/gates/*.gate.json`，检查对应 Markdown 是否由当前 JSON 渲染。输出 Codex Hook JSON，可在 macOS、Linux 和 Windows 上使用。

指定仓库根目录：

```sh
janus hook gate-drift-check --root /path/to/harness-repo
```

## 退出码

| 退出码 | 含义 |
| --- | --- |
| 0 | 通过 |
| 1 | 门禁阻塞 |
| 2 | JSON schema 或字段格式错误 |
| 3 | 必要文件缺失 |
| 4 | 豁免不合法或已过期 |
| 5 | 输入快照过期 |
| 6 | 证据缺失或 hash 不匹配 |

## 文档

- [Go CLI 需求说明](go-cli-requirements.md)
