---
description: "internal/matcher 模块的设计规范与核心逻辑说明，包含五级匹配规则、TeamAliasIndex、通用引擎等"
globs: ["internal/matcher/**/*"]
---

# internal/matcher 模块规范

## 1. 模块职责

`internal/matcher` 是系统的核心匹配引擎，负责将 SR 的联赛/比赛/球队/球员数据与 TS 数据进行相似度计算和 ID 映射。模块采用多级降级策略，在精度与召回率之间取得平衡。

| 文件 | 职责 |
|------|------|
| `engine.go` | 主流程编排（两轮迭代 + 自底向上校验） |
| `universal_engine.go` | 通用匹配引擎（LSports ↔ TheSports，844 行） |
| `ls_engine.go` | LSports 专用匹配引擎（760 行） |
| `event.go` | 比赛匹配（五级降级规则 L1–L4b + TeamAliasIndex，625 行） |
| `event_dtw.go` | DTW 时间序列比赛匹配（525 行） |
| `evidence_event_matcher.go` | Evidence-First P3 比赛候选池适配层（多 competition 候选边打分 + 一对一冲突消解） |
| `league_evidence.go` | Evidence-First P4 联赛证据聚合器（按 TS competition 聚合 P3 比赛证据 + 强约束复核 + 三段式决策） |
| `league.go` | 联赛匹配（已知映射表 + 名称相似度 + 全局占用机制） |
| `league_alias.go` | 联赛别名匹配（629 行） |
| `league_features.go` | 联赛特征提取（624 行） |
| `team_player.go` | 球队映射推导 + 球员匹配（487 行） |
| `name.go` | 名称归一化（变音符/先后名/中间名/Unicode，308 行） |
| `result.go` | 匹配结果数据结构和规则常量 |
| `dense_blocking.go` | 密集候选块生成（450 行） |
| `fs_model.go` | 特征评分模型（519 行） |
| `known_map_validator.go` | 已知映射验证器（433 行） |
| `reverse_confirm.go` | 反向确认逻辑 |
| `team_name_normalizer.go` | 球队名称归一化（332 行） |

## 2. 核心数据模型

### 匹配结果（result.go）

```go
// MatchResult 包含单场比赛的匹配结果
type MatchResult struct {
    SREventID   string
    TSEventID   string
    Confidence  float64
    Level       MatchLevel  // L1/L2/L3/L4/L4b
    TeamIDMap   map[string]string
}
```

### 五级匹配规则常量

| 常量 | 值 | 说明 |
|------|-----|------|
| `LevelL1` | 1 | 精确时间（≤5min），名称阈值 0.40，置信度 0.50 |
| `LevelL2` | 2 | 宽松时间（≤6h），名称阈值 0.65，置信度 0.60 |
| `LevelL3` | 3 | 同日期（≤24h），名称阈值 0.75，置信度 0.70 |
| `LevelL4` | 4 | 超宽时间（≤72h）+ 别名强匹配，置信度 0.80 |
| `LevelL4b` | 5 | 球队 ID 精确对兜底，置信度 0.75 |

## 3. 状态流转 / 业务规则

### TeamAliasIndex（联赛级队伍别名学习）

在同一联赛的比赛匹配过程中，每当 L1/L2/L3/L4 成功匹配一场比赛，自动将 `(sr_team_id → ts_team_id)` 写入别名索引。后续比赛若两队均在索引中命中，直接返回高置信度分数（0.92），不再依赖字面名称相似度。

**解决的问题**：`Chelsea FC`（SR）vs `Chelsea`（TS）等名称细微差异导致置信度偏低进而漏匹配的问题。

### 两轮迭代流程

```
第一轮：MatchEvents(teamIDMap=nil)
        → L1 / L2 / L3 / L4（TeamAliasIndex 内部驱动）
        → DeriveTeamMappings → teamIDMap

第二轮：MatchEvents(teamIDMap=<第一轮推导>)
        → L4b 球队 ID 精确对兜底
        → DeriveTeamMappings（最终）
```

### Evidence-First P3 比赛候选池适配

`EvidenceEventMatcher` 面向 P2 输出的多 competition TS 比赛候选池，输入为 `[]EvidenceEventCandidate` 而不是单一联赛内的 `[]db.TSEvent`。候选结构显式保留 `competition_id`、P2 候选先验分、主客队候选分和强约束结果；输出 `ResolvedEventMatch` 保留 `ts_match_id`、`ts_competition_id`、主客队、时间、置信度、规则、reason code 与冲突淘汰解释。

| 约束 | 说明 |
|------|------|
| 候选边打分 | 复用 `levelConfigs`、`gaussianTimeFactor`、`TeamAliasIndex.NameSimWithAlias`、`FSModel` 和 `EventDTWMatcher`，综合时间差、主客队相似度、P2 先验、强约束、别名命中和队伍 ID 锚点。 |
| 主客反转 | 反转候选允许保留，但必须扣除反转惩罚，并在 `reason_codes` 中输出 `SIDE_REVERSED`。 |
| 一对一确认 | 自动确认前按候选边分数降序做冲突消解，保证一个 `ts_match_id` 最多匹配一个源侧事件。 |
| 冲突解释 | 被淘汰候选记录 `lost_to`、`winner_score`、`loser_score`、`score_gap` 和原因（如 `CONFLICT_TS_USED` / `CONFLICT_SOURCE_USED`）。 |
| 两轮 L4b | `MatchTwoRound` 第一轮基于名称/候选分/别名推导 `teamIDMap`，第二轮注入 `teamIDMap` 激活 `TEAM_ID_FALLBACK` / L4b 兜底。 |

### Evidence-First P4 联赛证据聚合

`LeagueEvidenceAggregator` 接收源侧联赛特征、P3 `ResolvedEventMatch`、TS competition 元数据，并按 `ts_competition_id` 聚合 `event_coverage_score`、`high_conf_event_score`、`team_coverage_score`、`two_team_anchor_score`、`temporal_overlap_score`、`location_score`、`league_name_keyword_score` 与 `candidate_gap`。联赛名称只能作为弱特征和 tie-break，默认权重集中在 `DefaultLeagueEvidenceWeights`，禁止重新把名称相似度作为主入口。

| 决策状态 | 触发条件 |
|------|------|
| `AUTO_CONFIRMED` | `score ≥ 0.85`、高置信比赛数 `≥ 3`、无 hard veto、Top1/Top2 分差 `≥ 0.10`。 |
| `REVIEW_REQUIRED` | `0.60 ≤ score < 0.85`，或高置信比赛不足，或候选分差 `< 0.10`。 |
| `REJECTED` | `score < 0.60`，或 CountryCode/地区文本、Women/Men、U19/U21/成年队、North/South 分区、Cup/League/Friendly/Futsal/5x5、层级数字等强约束明确冲突。 |
| `KNOWN_SUSPECT` | KnownMap 反向确认率 `RCR < 0.30`，即使聚合分高也进入审核。 |

审核证据行由 `LeagueEvidenceCandidate` 输出，必须包含 `competition_id`、`competition_name`、`score`、`coverage`、`matched_events`、`high_conf_events`、`team_coverage`、`location_result`、`keyword_result`、`veto_reason`、`candidate_gap` 与 `top_event_examples`。

详见流程洞察：[PI-006 Evidence-First 比赛级匹配流程](process_insights/PI-006_evidence_first_matching_flow.md) 与计划文档：[`docs/evidence_first_matching_plan.md`](../../docs/evidence_first_matching_plan.md)。

### 名称归一化规则（name.go）

- 去除变音符（如 `Müller → Muller`）
- 处理先后名顺序（`John Smith` ↔ `Smith John`）
- 处理中间名缩写
- Unicode 标准化

## 4. 大文件函数索引

以下文件超过 500 行，已由 `code-indexer` 生成函数级索引，修改前必须查阅：

| 文件 | 行数 | 索引文件 |
|------|------|---------|
| `universal_engine.go` | 844 | [auto_index](auto_index/internal_matcher_universal_engine_go_index.md) |
| `ls_engine.go` | 760 | [auto_index](auto_index/internal_matcher_ls_engine_go_index.md) |
| `integration_test.go` | 642 | [auto_index](auto_index/internal_matcher_integration_test_go_index.md) |
| `league_alias.go` | 629 | [auto_index](auto_index/internal_matcher_league_alias_go_index.md) |
| `event.go` | 625 | [auto_index](auto_index/internal_matcher_event_go_index.md) |
| `league_features.go` | 624 | [auto_index](auto_index/internal_matcher_league_features_go_index.md) |
| `event_dtw.go` | 525 | [auto_index](auto_index/internal_matcher_event_dtw_go_index.md) |
| `fs_model.go` | 519 | [auto_index](auto_index/internal_matcher_fs_model_go_index.md) |

## 5. 详细设计文档索引

- [通用匹配算法设计](../../docs/universal_matching_algorithm_design.md)
- [联赛匹配评估规则](../../docs/league_match_evaluation_rule.md)
- [LS/TS 匹配评估报告](../../docs/ls_ts_matching_assessment.md)
- [优化测试报告](../../docs/optimization_test_report.md)
