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
| `candidate_pool.go` | **Evidence-First P1 候选池生成器** — 把"一组候选 TS competition + 联赛先验 + 强约束"转化为 []EvidenceEventCandidate；依赖最小 TSEventLoader 接口，单测可注入 stub（194 行） |
| `team_prior_enricher.go` | **Evidence-First P2 球队级先验填充** — 对每条 EvidenceEventCandidate 扫描全部 SR 球队名，取最佳相似度填入 HomeTeamCandidateScore / AwayTeamCandidateScore；别名感知（aliasIdx.NameSimWithAlias）；纯函数原地修改 |
| `evidence_event_matcher.go` | Evidence-First P3 比赛候选池适配层（多 competition 候选边打分 + 一对一冲突消解） |
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

### Evidence-First P1 候选池生成

`CandidatePoolBuilder` 是 Evidence-First 流水线的 P1 阶段：把"一组候选 TS competition + 联赛先验 + 联赛级强约束"直接转化为 `[]EvidenceEventCandidate`，供后续 P3 评分。它**不做任何 SR↔TS 配对、打分、筛选**，那是 P3 的职责。

| 字段 / 接口 | 说明 |
|------------|------|
| `TSCompetitionCandidate` | P1 输入单元：`{Competition, LeaguePriorScore∈[0,1], StrongConstraintOK, StrongConstraintReason}` |
| `TSEventLoader` | 最小依赖接口（`GetEvents` + `GetTeamNames`）；`*db.TSAdapter` 自然实现，单测可注入 stub |
| `CandidatePoolBuilder.Build(candidates, sport)` | 主入口，输出 `CandidatePoolResult{Candidates, TSTeamNames(union), Stats}` |
| P0 向后兼容 | 当 `candidates` 长度为 1 时，输出与 P0 单 competition 路径在事件数 / 球队名映射上等价（已通过单测 `TestCandidatePool_P0BackwardCompat_SingleCompMatches` 锁定） |
| Loader 错误处理 | 任一 competition 的 `GetEvents` / `GetTeamNames` 失败 → 返回 wrapped error，**不会部分写回**已拉的 candidates |
| 球队名 union | 不同 competition 的 `TSTeamID` 冲突时**先到不被覆盖**（与 `MergeTSTeamNames` 一致） |
| Veto 传递 | `StrongConstraintOK=false` 的 competition 仍写入候选池（标记 `CompetitionsVetoed`），由 P3 `bestEvidenceLevel` / `scoreEdge` 决定降级或淘汰，避免 P1 提前丢候选 |

**关键入口**（`candidate_pool.go`）：

| 入口 | 作用 |
|------|------|
| `NewCandidatePoolBuilder(loader TSEventLoader)` | 构造器 |
| `(b *CandidatePoolBuilder).Build(candidates, sport)` | 主入口 |
| `MergeTSTeamNames(maps...)` | 工具函数，先到不被覆盖语义的 map union |

### Evidence-First P2 球队级先验填充

`EnrichTeamPriors` 把 P1 候选池里每个 TS event 的 home/away 队，与全部 SR 球队跑相似度，取最佳值填入 `HomeTeamCandidateScore` / `AwayTeamCandidateScore`。这是 P1 与 P3 之间的桥接 —— P3 `scoreEdge` 通过 `normalizePrior` 把这两个先验 + 联赛先验做平均，按 `candidatePriorWeight=0.10` 注入最终边得分。

| 字段 / 行为 | 说明 |
|------------|------|
| 计算公式 | 对每个 TS team，`max over srTeams { aliasIdx.NameSimWithAlias(srID, srName, tsID, tsName) }` |
| 别名感知 | `NameSimWithAlias` 在 `*TeamAliasIndex` 命中时直接返回 `aliasApplyScore`，未命中走 `teamNameSimilarity` (Jaccard) |
| 原地修改 | 只动 