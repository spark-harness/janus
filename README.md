# Janus

`janus/` 用于整理 Harness Go CLI 的需求、设计和后续实现计划。

Janus 是 Harness 门禁 CLI。它把 `*.gate.json` 作为门禁判定事实源，把 Markdown 作为审计视图，并通过稳定退出码让 CI 可以阻塞不合规的阶段推进。

## 构建

```sh
go build -o ./bin/janus ./cmd/janus
```

## 命令

### 校验门禁 JSON

```sh
janus gate validate requirements/T12345/gates/3.3-design-review.gate.json
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
  --input requirements/T12345/gates/3.3-design-review.gate.json \
  --output requirements/T12345/gates/3.3-design-review.md
```

检查 Markdown 是否漂移：

```sh
janus gate render --check \
  --input requirements/T12345/gates/3.3-design-review.gate.json \
  --output requirements/T12345/gates/3.3-design-review.md
```

### 验证单个门禁

```sh
janus gate verify \
  --input requirements/T12345/gates/3.3-design-review.gate.json
```

`gate verify` 会先执行 JSON 校验，再检查：

- `inputs[].sha256` 是否匹配当前文件内容。
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

第一版 `merge` 目标使用这个规则：

- 查找 `requirements/<requirement-id>/gates/*.gate.json`。
- 至少必须存在一个 gate JSON。
- 每个 gate JSON 都必须通过 `gate verify`。
- 如果 gate 声明 `idl_impact.impact = "yes"`，必须提供 `evidence`。
- 如果 gate 声明 `idl_impact.impact = "no"`，必须提供 `idl_impact.na_reason`。

后续如果新增必需 gate 清单或阶段配置，`requirement verify` 应改为读取配置，而不是只验证已发现的 gate 文件。

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
