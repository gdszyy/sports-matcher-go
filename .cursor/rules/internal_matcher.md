---
description: "internal_matcher 模块的设计规范与核心逻辑说明"
globs: ["internal_matcher/**/*"]
---

# internal_matcher 模块规范

## 1. 模块职责

`internal/matcher` 包是 `sports-matcher-go` 的核心匹配引擎，负责将 LSports（LS）和 SportRadar（SR）的联赛、比赛、球队数据与 TheSports（TS）进行双向 ID 映射。

| 文件 | 职责 |
|------|------|
| `engine.go` | SR↔TS 匹配引擎入口（`MatchLeagues`、`MatchEvents`） |
| `ls_engine.go` | LS↔TS 匹配引擎入口（`LSEngine.RunLeague`） |
| `league.go` | SR 侧联赛匹配逻辑（`MatchLeague`、`leagueNameScore`）+ 已知映射表 `KnownLeagueMap` |
| `league_features.go` | **P0 新增**：联赛名称结构化特征提取（`ExtractLeagueFeatures`）+ 六维强约束一票否决（`CheckLeagueVeto`）+ 层级数字提取（`extractTierNumber`） |
| `event.go` | 多级比赛匹配（L1-L5 + L4b）、`TeamAliasIndex` 动态别名学习 |
| `team_player.go` | SR 球队/球员匹配、`ApplyBottomUp`；**P1 新增** `LSPlayerMatch`、`MatchPlayersForLSTeam`、`DeriveTeamMappingsFromLS`、`ApplyBottomUpLS` |
| `reverse_confirm.go` | **P1 新增**：比赛反向确认率（`ComputeReverseConfirmRate`）、分级（`ClassifyRCR`）、回灰联赛置信度（`ApplyRCRToLeague`） |
| `name.go` | 名称归一化（`normalizeName`）、Jaccard 相似度、**P0 新增** Jaro-Winkler 相似度（`jaroWinklerSimilarity`）、`nameSimilarityMax` |
| `team_name_normalizer.go` | 球队名称深度归一化（8 步流水线，去信乐部缩写/赞助商/变音符等） |
| `result.go` | 匹配结果数据结构定义；**P1 扩展** `LSEventMatch`/`LSTeamMapping`/`LSMatchStats`/`LSMatchResult` 球员字段 |

---

## 2. 核心数据模型 / API 接口

### 2.1 联赛特征向量（P0 新增）

```go
// LeagueFeatures 联赛名称结构化特征向量（六维强约束 + 辅助信息）
type LeagueFeatures struct {
    Gender          LeagueGender // 性别：Unknown / Men / Women
    AgeGroup        string       // 年龄段："" / "u23" / "u21" / "u19" / "youth" 等
    Region          string       // 区域分区："" / "north" / "south" / "east" / "west" / "central" 等
    CompetitionType string       // 赛制类型："" / "cup" / "league" / "short_format" / "playoff" / "friendly"
    TierNumber      int          // 层级数字：0 表示未检测到；1=顶级，2=次级…
    TierRoman       string       // 层级罗马数字原始值（调试用）
    NormalizedName  string       // 归一化后的核心名称
}
```

### 2.2 强约束否决结果

```go
type LeagueVetoResult struct {
    Vetoed bool
    Reason VetoReason // gender_conflict / age_conflict / region_conflict / competition_type_conflict / tier_number_conflict / short_format_conflict
    Detail string
}
```

### 2.3 匹配规则常量（`result.go`）

| 常量 | 含义 |
|------|------|
| `RuleLeagueKnown` | 已知映射表命中（最高置信度） |
| `RuleLeagueNameHi` | 名称相似度 ≥ 0.85 |
| `RuleLeagueNameMed` | 名称相似度 ≥ 0.70 |
| `RuleLeagueNameLow` | 名称相似度 ≥ 0.55 |
| `RuleLeagueNoMatch` | 未匹配 |
| `RuleEventL1` ~ `RuleEventL5` | 比赛多级匹配（L1=5min, L2=6h, L3=同日, L4=72h, L5=球队ID精确） |
| `RuleEventL4b` | 球队 ID 兜底匹配 |

---

## 3. 状态流转 / 业务规则

### 3.1 联赛匹配流程（P0 更新后）

```
输入：LS/SR 联赛 + TS 联赛候选列表
  │
  ├─ Step 1: 已知映射表查询（KnownLSLeagueMap / KnownLeagueMap）
  │     命中 → 直接返回，跳过强约束校验（人工审核白名单）
  │
  └─ Step 2: 名称相似度兜底（对每个 TS 候选）
        ├─ lsLocationVeto：地区明显不匹配 → 0.0（跨国否决）
        ├─ ExtractLeagueFeatures(ls.Name) + ExtractLeagueFeatures(ts.Name)
        ├─ nameSimilarityMax(Jaccard, JW) → base 分数
        ├─ 确定 confLevel（hi/med/low）
        ├─ CheckLeagueVeto(lsFeatures, tsFeatures, confLevel)
        │     六维否决：性别 / 年龄段 / 区域分区 / 赛制类型 / 短赛制 / 层级数字
        │     任一否决 → 0.0
        └─ 同国加权 → 最终分数
              ≥ 0.85 → RuleLeagueNameHi
              ≥ 0.70 → RuleLeagueNameMed
              ≥ 0.55 → RuleLeagueNameLow
              < 0.55 → 未匹配
```

### 3.2 六维强约束否决规则

| 维度 | 否决条件 | 典型案例 |
|------|---------|---------|
| **性别** | 一侧 Women，另一侧 Men 或 Unknown | `UEFA U19 Women` vs `UEFA U19` → 否决 |
| **年龄段** | 两侧年龄段不一致，或一侧有年龄标注另一侧无 | `Premier League U19` vs `Premier League` → 否决 |
| **区域分区** | 两侧区域不一致，或一侧有区域另一侧无 | `National League South` vs `National League` → 否决 |
| **赛制类型** | Cup vs League；short_format vs 任何非 short_format | `LFL 5x5` vs `Bundesliga` → 否决 |
| **层级数字** | 两侧层级数字不一致（仅在 med/low 置信度时否决） | `4 Liga` vs `Liga 3` → 否决（med） |
| **短赛制** | short_format 与任何非 short_format 类型冲突 | `Futsal Cup` vs `FA Cup` → 否决 |

### 3.3 Jaro-Winkler 相似度使用规范

- `nameSimilarityMax(a, b)` = max(Jaccard(a,b), JaroWinkler(a,b))
- 适用于联赛名称、球队名称等短文本场景
- 对公共前缀（最多 4 字符，p=0.1）给予额外加分，比 Jaccard 更适合处理缩写和短名称

### 3.4 层级数字提取规则

优先级从高到低：
1. 正则模式：`2.Bundesliga`、`Liga 3`、`3 Liga`、`Division 2`、`Serie B`
2. 文字层级词：`first division`=1、`second division`=2、`third division`=3
3. 独立罗马数字（仅名称末尾或括号内）：`II`=2、`III`=3

---

## 4. 详细设计文档索引

| 文档 | 说明 |
| --- | --- |
| [`docs/ls_ts_matching_assessment.md`](../../docs/ls_ts_matching_assessment.md) | LS 与 TS 联赛匹配的历史评估与背景说明 |
| [`docs/league_match_evaluation_rule.md`](../../docs/league_match_evaluation_rule.md) | 当前阶段正式使用的联赛评价口径、最小比赛数阈值与准确率结论 |
| [`docs/league_guard_keywords.json`](../../docs/league_guard_keywords.json) | 地区、性别、年龄、区域与赛制形态的强约束关键词词典（P0 已扩充） |
| [`docs/universal_matching_algorithm_design.md`](../../docs/universal_matching_algorithm_design.md) | 通用匹配算法完整设计文档（P0-P3 路线图） |
| [`.cursor/rules/process_insights/PI-001_universal_matching_algorithm_design.md`](process_insights/PI-001_universal_matching_algorithm_design.md) | P0-P3 优化 TODO 清单与防坑指南 |

---

## 5. 变更日志

| 版本 | 日期 | 变更内容 |
|------|------|---------|
| v1.0 | 2026-04-16 | 初始规范文档（占位） |
| v2.0 | 2026-04-17 | P0 阶段完成：新增 `league_features.go`（六维特征提取 + 强约束否决 + 层级数字提取）；`name.go` 新增 Jaro-Winkler；`ls_engine.go` 和 `league.go` 引入 `CheckLeagueVeto`；`docs/league_guard_keywords.json` 扩充完整的模块规范（职责、数据模型、业务规则、变更日志） |
| v2.1 | 2026-04-17 | P1 阶段完成：`models.go` 新增 `LSPlayer`/`LSTeam`；新增 `ls_player_adapter.go`（数据库优先+Snapshot API 兆底，支持批量）；`team_player.go` 新增 LS 球员匹配和自底向上校验；`ls_engine.go` 激活球员匹配阶段；新增 `reverse_confirm.go`；`result.go` 扩展 LS 结构体 |