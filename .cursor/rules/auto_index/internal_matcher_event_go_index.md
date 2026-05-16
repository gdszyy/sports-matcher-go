# internal/matcher/event.go 函数索引

> 自动生成于 2026-05-16 | 总行数: 733 | 函数数: 2 | 语言: go
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

**巨型函数警告**: 本文件包含 1 个超过 200 行的函数，建议优先通过 `@section` 标记进行内部导航。

## 函数列表

> 定位方式：在源文件中 `grep -n "函数名"` 即可跳转，行号不在此列出（行号随代码变化而失效）。

| 函数名 | 类型 | 签名 | 备注 |
|--------|------|------|------|
| teamNameSimilarity | function | `teamNameSimilarity()` |  |
| len | function | `len()` | ⚠️ 巨型函数，见 @section 导航 |

## 巨型函数内部节点 (@section 标记)

### len

> 定位：`grep -n '@section:{}'` 跳转到对应节点

| 节点标记 | 说明 |
|----------|------|
| `@section:init_state` | 初始化匹配状态（usedTSIDs、results、aliasIdx） |
| `@section:multi_level_match` | 逐条 SR 比赛执行策略 1/2/3/4 + L5 + L4b 匹配 |
| `@section:strategy_1_to_4` | 策略 1/2/3/4 逐级尝试（策略 4 依赖 aliasIdx） |
| `@section:l5_unique_match` | L5 无时间约束唯一性匹配（策略 1~4 均未命中时激活） |
| `@section:l4b_team_id_fallback` | L4b 球队ID 精确对兜底 |
| `@section:l6_placeholder_time_anchor` | L6 占位符时间锚定匹配 |
