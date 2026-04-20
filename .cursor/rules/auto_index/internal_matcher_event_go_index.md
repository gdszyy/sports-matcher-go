# internal/matcher/event.go 函数索引

> 自动生成于 2026-04-20 | 总行数: 733 | 函数数: 2 | 语言: go
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

**巨型函数警告**: 本文件包含 1 个超过 200 行的函数，建议优先通过 `@section` 标记进行内部导航。

## 函数列表

| 函数名 | 类型 | 起始行 | 结束行 | 行数 | 签名 |
|--------|------|--------|--------|------|------|
| teamNameSimilarity | function | L144 | L159 | 16 | `teamNameSimilarity()` |
| len | function | L160 | L734 | **575** | `len()` |

## 巨型函数内部节点 (@section 标记)

### len (L160-L734, 575行)

| 节点标记 | 行号 | 说明 |
|----------|------|------|
| `@section:init_state` | L310 | 初始化匹配状态（usedTSIDs、results、aliasIdx） |
| `@section:multi_level_match` | L315 | 逐条 SR 比赛执行策略 1/2/3/4 + L5 + L4b 匹配 |
| `@section:strategy_1_to_4` | L329 | 策略 1/2/3/4 逐级尝试（策略 4 依赖 aliasIdx） |
| `@section:l5_unique_match` | L352 | L5 无时间约束唯一性匹配（策略 1~4 均未命中时激活） |
| `@section:l4b_team_id_fallback` | L364 | L4b 球队ID 精确对兜底 |
| `@section:l6_placeholder_time_anchor` | L374 | L6 占位符时间锚定匹配 |
