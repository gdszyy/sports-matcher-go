---
id: "PI-006"
version: "v1.0"
last_updated: "2026-04-30"
author: "Manus AI"
related_modules: ["internal/matcher", "docs"]
status: "active"
---

# PI-006: Evidence-First 比赛级匹配流程

## 流程概述

Evidence-First P3 将 P2 输出的 **多 competition TS 比赛候选池** 转化为高置信、一对一的比赛匹配结果。该流程不再假设输入来自某一个 TS 联赛的全部比赛，而是把每条候选比赛作为一条可解释的证据边进行打分，并在自动确认前执行全局冲突消解，确保一个 `ts_match_id` 最多被一个源侧事件占用。Evidence-First P4 在此基础上按 `ts_competition_id` 聚合比赛证据，反推出最可能的 TS 联赛，并通过地区、性别、年龄、分区、赛制与层级数字强约束复核阻断同名误吸附。

## 核心防坑指南

### 坑 1: 把 P2 候选池重新退化为单联赛全量比赛

**现象**：P2 已经基于联赛候选、球队候选和强约束生成跨 competition 的比赛候选池，但 P3 如果继续调用只接受 `[]db.TSEvent` 的联赛内 `MatchEvents`，会丢失 `competition_id`、P2 先验分和强约束解释，导致联赛名称歧义样本无法提升召回。

**根因**：旧流程在进入比赛匹配时已经确认唯一 TS competition，因此 `db.TSEvent` 不携带 `competition_id`。Evidence-First 流程中，比赛证据必须保留候选来源 competition，否则 P4 无法按联赛聚合、回灌反向确认率。

**正确做法**：使用 `EvidenceEventCandidate` 包装 TS 比赛，并显式传递 `competition_id`、`competition_name`、P2 候选先验分、主客队候选分和强约束结果。输出 `ResolvedEventMatch` 时继续保留 `ts_match_id`、`ts_competition_id`、主客队、时间、置信度、规则和 reason code。

**关键位置**：`internal/matcher/evidence_event_matcher.go` → `EvidenceEventCandidate`、`EventEvidenceEdge`、`ResolvedEventMatch`。

### 坑 2: 先逐源选择最佳候选会造成隐性一对多

**现象**：两个源侧比赛都把同一条 TS 比赛作为最佳候选，若逐条 source 独立确认，最终会出现多个源侧事件匹配同一个 `ts_match_id`。

**根因**：旧版 `MatchEvents` 通过 `usedTSIDs` 在联赛内顺序占用 TS 事件，但 Evidence-First 候选池来自多个 competition，候选边需要先统一排序再消解，否则输入顺序会影响自动确认结果。

**正确做法**：先生成所有 `EventEvidenceEdge`，再按分数降序做贪心一对一消解。短期实现中每条源侧事件和每个 `ts_match_id` 都只能被占用一次；被淘汰候选必须记录 `lost_to`、`winner_score`、`loser_score`、`score_gap` 和淘汰原因。长期可替换为 Hungarian 或 min-cost max-flow，但输出解释字段应保持兼容。

**关键位置**：`internal/matcher/evidence_event_matcher.go` → `resolveConflicts`、`ConflictElimination`。

### 坑 3: 主客反转不能静默等价于正向匹配

**现象**：某些数据源存在主客标注反转，完全禁止反转会漏召回；但把反转候选当作正向候选会提高主客错配风险。

**根因**：主客方向是比赛实体匹配的强语义证据。反转候选可以作为证据保留，但必须进入可解释降权路径。

**正确做法**：同时计算正向和反向主客名称相似度。当反向更强时保留候选边，扣除反转惩罚，并在 `reason_codes` 中加入 `SIDE_REVERSED`。后续 P4 或人工复核可直接识别该风险。

**关键位置**：`internal/matcher/evidence_event_matcher.go` → `scoreEdge`。

### 坑 4: ±24h/±72h 时间偏移不能仅靠硬窗口处理

**现象**：时区或赛程同步错误会造成整批比赛统一偏移，单场高斯时间衰减会把候选压到阈值以下。

**根因**：单场时间差不能区分真实统一偏移和随机错误。旧流程中 EventDTW 是兜底模块，Evidence-First P3 仍应复用该时间序列锚点能力。

**正确做法**：在候选池上构造 DTW 事件序列，调用 `EventDTWMatcher.TryCorrect` 估计源侧整体时间偏移；若偏移可信，则使用修正后的时间差参与 L1-L4/L4b 评分，并在 reason code 中加入 `DTW_OFFSET`。

**关键位置**：`internal/matcher/evidence_event_matcher.go` → `estimateDTWOffset`。

### 坑 5: L4b 队伍 ID 兜底必须依赖两轮流程

**现象**：第一轮还没有稳定的 `teamIDMap`，过早使用队伍 ID 兜底会把噪声 ID 映射放大为比赛误匹配。

**根因**：L4b 的安全性来自第一轮高置信比赛推导出的球队映射，而不是候选池自身。

**正确做法**：`MatchTwoRound` 第一轮只用名称、时间、别名和候选先验打分；随后调用现有 `DeriveTeamMappings` 推导 `teamIDMap`；第二轮把 `teamIDMap` 注入 `Match`，允许 `TEAM_ID_FALLBACK` 兜底极端时间偏移或低名称相似样本。

**关键位置**：`internal/matcher/evidence_event_matcher.go` → `MatchTwoRound`、`hasTeamIDAnchor`。

### 坑 6: P4 不能把联赛名称重新变成主入口

**现象**：比赛证据已经指向多个 TS competition，但如果 P4 又优先按联赛名称相似度选候选，`Serie A`、`Ligue 1/Ligue 2`、商业冠名联赛、跨国同名联赛会重新发生误吸附。

**根因**：名称是弱证据，无法解释比赛覆盖、球队覆盖和反向确认率；同名联赛在国家、层级、性别、年龄或赛制上往往有强语义差异。

**正确做法**：使用 `LeagueEvidenceAggregator`，先按 `ts_competition_id` 聚合 P3 `ResolvedEventMatch`，再用默认权重 `event coverage 0.35`、`high confidence events 0.20`、`team coverage 0.20`、`two-team anchor 0.10`、`temporal overlap 0.05`、`location 0.05`、`league name keyword 0.05` 打分。联赛名称只能贡献 `league_name_keyword_score`，作为弱特征和 tie-break。

**关键位置**：`internal/matcher/league_evidence.go` → `LeagueEvidenceAggregator`、`LeagueEvidenceCandidate`、`LeagueEvidenceDecision`。

### 坑 7: P4 聚合后仍必须重新执行 hard veto

**现象**：P3 比赛匹配可能因球队与时间证据很强而把跨国同名 competition 聚合成高分候选，例如意大利 `Serie A` 被巴西 `Serie A` 的大量比赛数量偏置挤到 Top1。

**根因**：比赛证据是实体证据，不等于联赛语义完全一致；联赛层面的国家、性别、年龄、分区、赛制、层级冲突必须在最终决策前复核。

**正确做法**：P4 对每个候选执行 CountryCode/地区文本、Women/Men、U19/U21/Reserve、North/South 分区、Cup/League/Friendly/Futsal/5x5、层级数字 hard veto。存在明确 hard veto 的候选聚合分置零并输出 `veto_reason` / `veto_detail`，不得自动确认。

**关键位置**：`internal/matcher/league_evidence.go` → `evaluateLeagueEvidenceLocation`、`CheckLeagueVeto` 调用链。

## 关键耦合点

| 耦合点 | 说明 |
|--------|------|
| `MatchEvents` / L1-L4b | Evidence-First P3 复用现有 `levelConfigs`、`gaussianTimeFactor`、`RuleEventL1`~`RuleEventL4b`，避免另起孤立规则体系。 |
| `TeamAliasIndex` | 候选边使用 `NameSimWithAlias`，未来可注入持久化别名后继续复用别名命中加分和 reason code。 |
| `FSModel` | 候选边将主队相似度、客队相似度、时间差、联赛层级、运动类型转换为 FS 比较向量，作为综合分的一部分。 |
| `EventDTW` | 对候选池整体估计时间偏移，修正后再进入比赛边打分。 |
| `DeriveTeamMappings` | 两轮逻辑沿用现有比赛推导球队映射能力，第二轮启用 L4b 队伍 ID 兜底。 |
| `LeagueEvidenceAggregator` | P4 使用 `ResolvedEventMatch.ts_competition_id` 聚合比赛覆盖、球队覆盖、高置信比赛、两队锚点、时间重叠、地区和弱名称关键词，并输出候选排序、`candidate_gap`、hard veto 原因和最终状态。 |
| `KnownLeagueMapValidator` / RCR | KnownMap 的反向确认率若 `< 0.30`，P4 输出 `KNOWN_SUSPECT` 并进入审核，不再因已知映射或名称高分自动放行。 |

## 版本变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|----------|------|
| v1.1 | 2026-04-30 | 补充 Evidence-First P4 联赛证据聚合、默认权重、三段式决策、KnownMap 低 RCR 审核和 hard veto 复核规则。 | Manus AI |
| v1.0 | 2026-04-30 | 初始记录 Evidence-First P3 比赛级候选边打分、一对一冲突消解、主客反转降权、DTW 偏移修正和两轮 L4b 兜底流程。 | Manus AI |
