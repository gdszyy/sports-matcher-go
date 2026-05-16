# internal/matcher/ls_engine.go 函数索引

> 自动生成于 2026-05-16 | 总行数: 1006 | 函数数: 3 | 语言: go
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

**巨型函数警告**: 本文件包含 1 个超过 200 行的函数，建议优先通过 `@section` 标记进行内部导航。

## 函数列表

> 定位方式：在源文件中 `grep -n "函数名"` 即可跳转，行号不在此列出（行号随代码变化而失效）。

| 函数名 | 类型 | 签名 | 备注 |
|--------|------|------|------|
| jaccardSimilarity | function | `jaccardSimilarity()` |  |
| lsLocationVeto | function | `lsLocationVeto()` |  |
| lsLocationVetoByName | function | `lsLocationVetoByName()` | ⚠️ 巨型函数，见 @section 导航 |

## 巨型函数内部节点 (@section 标记)

### lsLocationVetoByName

> **缺少 @section 标记**：此巨型函数内部没有节点标记，建议添加以提升导航精度。

## 其他 @section 标记

| 节点标记 | 说明 |
|----------|------|
| `@section:load_ls_league` | 加载 LSports 联赛数据 |
| `@section:league_match` | 联赛匹配（已知映射 + 名称相似度） |
| `@section:load_events` | 加载 LS/TS 比赛与球队名称数据 |
| `@section:event_match_round1` | 比赛匹配第一轮（L1/L2/L3/L4）+ 球队映射推导 |
| `@section:event_match_round2` | 比赛匹配第二轮（L4b 球队ID兜底） |
| `@section:known_map_validation` | 已知映射反向确认率验证（P2） |
| `@section:player_match_bottom_up` | 球员匹配 + 自底向上反向验证（P1） |
| `@section:league_abbrev_country_map` | 联赛缩写/简称→国家知识图谱（修复缩写联赛跨国误配） |
| `@section:location_veto_by_name` | 通过联赛名称缩写推断国家的地理否决函数（新增） |
