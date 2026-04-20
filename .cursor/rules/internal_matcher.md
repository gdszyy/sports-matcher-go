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
