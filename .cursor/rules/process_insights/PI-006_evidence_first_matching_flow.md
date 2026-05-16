---
id: "PI-006"
version: "v1.16"
last_updated: "2026-05-16"
author: "Manus AI, Claude Cowork"
related_modules: ["internal/matcher", "internal/db", "cmd", "docs"]
status: "active"
---

# PI-006: Evidence-First 比赛级匹配流程

## 流程概述

Evidence-First P3 将 P2 输出的 **多 competition TS 比赛候选池** 转化为高置信、一对一的比赛匹配结果。该流程不再假设输入来自某一个 TS 联赛的全部比赛，而是把每条候选比赛作为一条可解释的证据边进行打分，并在自动确认前执行全局冲突消解，确保一个 `ts_match_id` 最多被一个源侧事件占用。

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

## 关键耦合点

| 耦合点 | 说明 |
|--------|------|
| `MatchEvents` / L1-L4b | Evidence-First P3 复用现有 `levelConfigs`、`gaussianTimeFactor`、`RuleEventL1`~`RuleEventL4b`，避免另起孤立规则体系。 |
| `TeamAliasIndex` | 候选边使用 `NameSimWithAlias`，未来可注入持久化别名后继续复用别名命中加分和 reason code。 |
| `FSModel` | 候选边将主队相似度、客队相似度、时间差、联赛层级、运动类型转换为 FS 比较向量，作为综合分的一部分。 |
| `EventDTW` | 对候选池整体估计时间偏移，修正后再进入比赛边打分。 |
| `DeriveTeamMappings` | 两轮逻辑沿用现有比赛推导球队映射能力，第二轮启用 L4b 队伍 ID 兜底。 |
| P4 聚合 | `ResolvedEventMatch` 必须保留 `ts_competition_id`、`ts_match_id`、`reason_codes` 和冲突淘汰解释，供联赛级反向确认和人工复核使用。 |

## 版本变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|----------|------|
| v1.0 | 2026-04-30 | 初始记录 Evidence-First P3 比赛级候选边打分、一对一冲突消解、主客反转降权、DTW 偏移修正和两轮 L4b 兜底流程。 | Manus AI |
| v1.1 | 2026-05-16 | 落地 P1 候选池生成器 (`internal/matcher/candidate_pool.go`)：`CandidatePoolBuilder` 把 Top-N 联赛候选 + 联赛先验分 + 强约束转化为 `[]EvidenceEventCandidate`。**P1 不做配对/打分/筛选**（P3 职责）。`TSEventLoader` 最小接口 + 并行 goroutine fetch fallback；新增 P0BackwardCompat 单测。 | Claude Cowork |
| v1.2 | 2026-05-16 | 落地 P2 球队级先验填充 (`team_prior_enricher.go` / `EnrichTeamPriors`)：对每个 EvidenceEventCandidate 扫描全部 SR 球队名，取最佳相似度填入 HomeTeamCandidateScore / AwayTeamCandidateScore；别名感知；纯函数原地修改。P3 隐性 contract：`resolveConflicts` 每个 SR 都输出 ResolvedEventMatch（含 NoMatch stub）。 | Claude Cowork |
| v1.3 | 2026-05-16 | 落地联赛 Top-N 入口 (`league_topn.go` / `MatchLeagueTopN` + `ToTSCompetitionCandidates`)。KnownLeagueMap 命中保 Top-1，其余按 leagueNameScore 降序，每条携带 StrongConstraintOK。不破坏 P0（既有 MatchLeague 保留）。 | Claude Cowork |
| v1.4 | 2026-05-16 | Wire Evidence-First 流水线进 `UniversalEngine.RunLeague`：`TopNAdapter` 可选接口 + `SRSourceAdapter.MatchLeagueTopN` + `runLeagueEvidenceFirst` 主体 + `UseEvidenceFirst` / `EvidenceFirstTopN` 两个 opt-in 字段。RunLeague 仅加 4 行 type-assertion 分支，默认 false 走原 P0。 | Claude Cowork |
| v1.5 | 2026-05-16 | CLI 暴露 `--use-evidence-first` 和 `--evidence-first-topn N` 到 4 个生产命令（match2/batch2/ls-match/ls-batch）。 | Claude Cowork |
| v1.6 | 2026-05-16 | LS 链路镜像 (`league_topn_ls.go` / `LSMatchLeagueTopN`)：KnownLSLeagueMap 命中占 Top-1，lsLeagueNameScore 排序，LS 特有 `lsLocationVeto` / `lsLocationVetoByName` 显式记录在 strong constraint reason。`LSSourceAdapter.MatchLeagueTopN` 转发并 propagate NoKnownMap。SR 与 LS 完全对称。 | Claude Cowork |
| v1.7 | 2026-05-16 | P3 scoreEdge time-window 预筛：4 次 NameSimWithAlias 之前用 `correctedDiff > l5MaxTimeDiff (30 天)` + 无 teamIDAnchor 提前 false。严格子集裁剪不改结果集。3 个单测（SkipsFar / KeepsClose / KeepsFarWithAnchor）。 | Claude Cowork |
| v1.8 | 2026-05-16 | P3 NameSim 缓存 + P1 并行 goroutine fetch：`buildNameSimCache` 预计算 unique (srTeamID, tsTeamID) 对，scoreEdge 4 次 NameSim 改为 O(1) 查表 — EPL P3 stage >20s → 1s；P1 串行 4 个 competition 30s → 并行 21s。EPL 实测 v1.5 >40s → v1.8 30.6s。 | Claude Cowork |
| v1.9 | 2026-05-16 | DB batch SQL：`*db.TSAdapter.GetEventsBatch` + `GetTeamNamesBatch` (WHERE competition_id IN (...))；`BatchTSEventLoader` 可选接口。Loader 实现则 batch，否则回退到 v1.8 并行 goroutine。EPL P1 fetch 21s → 12s。 | Claude Cowork |
| v1.10 | 2026-05-16 | **EF 真数据有效性首次正面验证**（沙箱真 DB 跑 3 个高歧义非 KnownMap SR 联赛）。**关键 case：sr:tournament:929 Jordan League** — P0 联赛到 'Jordan League Division 1' (conf=0.908) 但 0/78 events matched；EF TopN=5 同 Top-1 league，但跨 5 个候选 competition 拉了 1845 events，最终 76/78 matched (97.4%)。Premier Soccer League Zimbabwe / Serie B Women Italy 都是 P0 高 conf league 但 0/N events，EF `edges=0` 显式暴露错误。报告：`docs/evidence_first_real_data_validation.md`。 | Claude Cowork |
| v1.11 | 2026-05-16 | `--max-runtime` 软超时：`UniversalEngine.MaxRuntime` + 4 个 CLI 命令 `--max-runtime` flag + `runLeagueEvidenceFirst` 各阶段入口 `budgetExceeded` 检查 → 超时输出当前最佳结果 `Stats.Truncated=true` + `TruncatedStage=topn\|p1\|p2\|p3`。**结构性局限**：budget check 只在 Go 栈空闲点工作，长跑 SQL 卡在 DB 调用里检查不到。3 个新单测。 | Claude Cowork |
| v1.12 | 2026-05-16 | **edges=0 → LEAGUE_SUSPECT 自动降级**：若 `len(efResult.Edges)==0 && league.Matched && != RuleLeagueKnown`，confidence × 0.5、Rule → `RuleLeagueSuspect`。自动捕获 v1.10 Case A/C 类型的 P0 silent 误匹配。沙箱真数据验证 Zimbabwe: NAME_HI 0.890 → SUSPECT 0.445。7 个单测。 | Claude Cowork |
| v1.13 | 2026-05-16 | SQL 时间窗下推（opt-in）：`GetEventsBatchInRange` (WHERE match_time BETWEEN tMin AND tMax) + `TimeRangeBatchTSEventLoader` + `BuildWithTimeRange` + `UniversalEngine.EvidenceFirstTimePadding` + CLI `--ef-time-padding`。Jordan TopN=5: 1845→252 events, 24s→13s (45% 加速), 76/78 不变。EPL: ±30d 217/225 (丢 6 L4b 跨季)，±180d 同样。**默认 0 不下推保 v1.12 行为**，用户按需 opt-in。 | Claude Cowork |
| v1.14 | 2026-05-16 | Context cancellation wire 进 SQL：让 `--max-runtime` 真正能打断长跑 SQL（堵 v1.11 局限）。三层 ctx-aware：(1) `*db.TSAdapter` 加 `GetEventsBatchCtx` / `GetEventsBatchInRangeCtx` / `GetTeamNamesBatchCtx` 用 `db.QueryContext`；(2) `ContextBatchTSEventLoader` + `ContextTimeRangeBatchTSEventLoader` 可选接口；(3) `BuildCtx` / `BuildWithTimeRangeCtx` + `runLeagueEvidenceFirst` 头部 `context.WithDeadline(t0+MaxRuntime)`。stub 不实现 ctx-aware 自动回退。3 个单测。 | Claude Cowork |
| v1.15 | 2026-05-16 | Direction D + E 双优化：(D) `UniversalEngine.tsCompCache` 缓存 `GetCompetitionsByFootball/Basketball` 跨 RunLeague 复用 + `InvalidateCompetitionCache()`。batch2 跨 29 联赛累计省 ~29s。(E) `BuildCtx` 与 `BuildWithTimeRangeCtx` 把 events + teamNames fetch 改为 sync.WaitGroup 并行 2 goroutine，单 RunLeague P1 fetch 串行 ~7s → 并行 ~4s。不改语义、不影响结果集。 | Claude Cowork |
| **v1.16** | **2026-05-16** | **球队优先兜底路径（Team-First Fallback）— 沙箱端到端验证！** Zimbabwe Premier Soccer League: P0=0/75（BPL conf=0.890 silent 误匹配）→ EF v1.15=0/75（edges=0 SUSPECT）→ **EF+team-first=66/75 (88%) Rule=LEAGUE_TEAM_FIRST**（找到真 Zimbabwe 联赛 `4zp5rzgh7loq82w`，TS 球队名带 `(ZWE)` 标）。沙箱完整 30.5s 跑完。实现栈：(1) `db.TSAdapter` 加 `GetAllTeamsBriefCtx` / `GetEventsByTeamIDsInRangeCtx` / `SearchTeamsByTokensCtx`（SQL token REGEXP 预筛绕过 79K 全表 cold start）；(2) `matcher.TeamFirstPoolBuilder` + `TokenSearchTeamLoader` 可选接口；(3) `UniversalEngine.EnableTeamFirstFallback` + `tfBuilder` lazy；(4) `runLeagueEvidenceFirst` second-pass：edges=0 且非 KnownMap → team-first → 多数表决 → 重跑 EF P3 一次；成功则 league.Rule=`LEAGUE_TEAM_FIRST` conf=0.70；(5) 与 v1.12 SUSPECT 互斥（tfApplied=true 跳过）。4 个单测 + CLI `--use-team-first-fallback`。默认 false 保 v1.15 行为。 | Claude Cowork |
