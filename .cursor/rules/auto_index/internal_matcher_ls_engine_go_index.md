# internal/matcher/ls_engine.go 函数索引

> 自动生成于 2026-04-20 | 总行数: 767 | 函数数: 2 | 语言: go
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

**巨型函数警告**: 本文件包含 1 个超过 200 行的函数，建议优先通过 `@section` 标记进行内部导航。

## 函数列表

| 函数名 | 类型 | 起始行 | 结束行 | 行数 | 签名 |
|--------|------|--------|--------|------|------|
| jaccardSimilarity | function | L442 | L485 | 44 | `jaccardSimilarity()` |
| lsLocationVeto | function | L486 | L768 | **283** | `lsLocationVeto()` |

## 巨型函数内部节点 (@section 标记)

### lsLocationVeto (L486-L768, 283行)

> **缺少 @section 标记**：此巨型函数内部没有节点标记，建议添加以提升导航精度。

## 其他 @section 标记

| 节点标记 | 行号 | 说明 |
|----------|------|------|
| `@section:load_ls_league` | L62 | 加载 LSports 联赛数据 |
| `@section:league_match` | L74 | 联赛匹配（已知映射 + 名称相似度） |
| `@section:load_events` | L112 | 加载 LS/TS 比赛与球队名称数据 |
| `@section:event_match_round1` | L137 | 比赛匹配第一轮（L1/L2/L3/L4）+ 球队映射推导 |
| `@section:event_match_round2` | L149 | 比赛匹配第二轮（L4b 球队ID兜底） |
| `@section:known_map_validation` | L170 | 已知映射反向确认率验证（P2） |
| `@section:player_match_bottom_up` | L191 | 球员匹配 + 自底向上反向验证（P1） |
