# Evidence-First Matching Plan

## 1. 背景与阶段边界

Evidence-First 流程把联赛匹配从“先确认单个 TS competition，再在该 competition 内匹配全部比赛”调整为“先生成多 competition 证据候选池，再用比赛级证据反向确认联赛”。其中 P2 负责输出 TS 比赛候选池，P3 负责把候选池转化为高置信、一对一的比赛匹配结果，P4 再基于比赛结果聚合联赛置信度与人工复核线索。

P3 的实现入口是 `internal/matcher/evidence_event_matcher.go`。该适配层复用现有 `MatchEvents` 体系中的 L1-L4b 规则、高斯时间衰减、`TeamAliasIndex`、`FSModel`、`EventDTW` 和 `DeriveTeamMappings`，避免产生与主匹配器孤立的第二套比赛算法。

## 2. P3 输入与输出

| 类型 | 结构 | 说明 |
|------|------|------|
| 输入 | `[]db.SREvent` | 源侧比赛列表，当前以 SR 事件结构作为首个适配形态。 |
| 输入 | `[]EvidenceEventCandidate` | P2 输出的 TS 候选比赛池，显式保留 `competition_id`、候选先验分、主客队候选分和强约束结果。 |
| 输入 | `srTeamNames` / `tsTeamNames` | 与旧 `MatchEvents` 一致的球队名称索引，可为空；为空时回退到事件自身名称。 |
| 输入 | `teamIDMap` | 第二轮可选输入，用于 L4b 队伍 ID 精确对兜底。 |
| 输出 | `ConflictResolutionResult` | 包含最终 `ResolvedEventMatch`、所有候选边、冲突淘汰记录、推导后的 `teamIDMap` 和 DTW 偏移信息。 |

## 3. 比赛候选边打分特征

每条源侧比赛到 TS 候选比赛会生成一条 `EventEvidenceEdge`。候选边评分不是单一字符串相似度，而是融合多类证据：

| 特征 | 字段 | 作用 |
|------|------|------|
| 时间差 | `TimeDiffSec` / `CorrectedTimeDiffSec` | 使用现有 `gaussianTimeFactor` 和 L1-L4 时间窗口；若 DTW 生效则使用修正后的时间差参与评分。 |
| 主队候选分 | `HomeScore` / `HomeTeamCandidateScore` | 主队名称相似度结合别名索引，外加 P2 主队候选先验。 |
| 客队候选分 | `AwayScore` / `AwayTeamCandidateScore` | 客队名称相似度结合别名索引，外加 P2 客队候选先验。 |
| 主客方向 | `SideReversed` | 反向主客更强时保留候选，但扣除反转惩罚，并输出 `SIDE_REVERSED`。 |
| Alias 命中 | `AliasHomeHit` / `AliasAwayHit` | `TeamAliasIndex.NameSimWithAlias` 命中后提高名称分，并输出 `ALIAS_HIT`。 |
| 队伍 ID 锚点 | `TeamIDAnchor` | 第二轮 `teamIDMap` 中两队均命中时触发 `TEAM_ID_FALLBACK`，对应 L4b 兜底。 |
| 强约束 | `StrongConstraintOK` / `StrongConstraintReason` | P2 强约束通过时输出 `STRONG_CONSTRAINT`；明确 veto 的候选不进入候选边。 |
| FS 模型 | `FSScore` | 使用 `CompareEventPair` 与 `FSModel.ScoreNormalized`，把主客名称、时间差、联赛层级、运动类型转化为概率型证据。 |
| DTW 时间偏移 | `DTWOffsetSec` | 用 `EventDTWMatcher.TryCorrect` 估计整体偏移，输出 `DTW_OFFSET`。 |

## 4. 一对一冲突消解策略

当前短期实现采用 **按分数降序贪心 + 冲突解释记录**。流程先生成全量候选边，再统一排序；每条源侧事件最多占用一条 TS 比赛，每个 `ts_match_id` 最多被一个源侧事件占用。低于自动确认阈值的候选会被淘汰并标记 `BELOW_AUTO_THRESHOLD`。

| 冲突类型 | 淘汰原因 | 记录字段 |
|----------|----------|----------|
| 同一源侧事件已有更高分候选 | `CONFLICT_SOURCE_USED` | `lost_to_sr_event_id`、`lost_to_ts_match_id`、`winner_score`、`loser_score`、`score_gap` |
| 同一 TS match 已被更高分源侧事件占用 | `CONFLICT_TS_USED` | `lost_to_sr_event_id`、`lost_to_ts_match_id`、`winner_score`、`loser_score`、`score_gap` |
| 分数低于自动确认阈值 | `BELOW_AUTO_THRESHOLD` | `loser_score` 和负向 `score_gap`，表示未被自动确认 |

该策略可在 P4 或后续阶段替换为 Hungarian / min-cost max-flow，但应保持 `ConflictElimination` 字段稳定，避免破坏人工复核和联赛聚合解释链。

## 5. 两轮 teamIDMap 逻辑

P3 提供 `MatchTwoRound` 兼容旧流程的两轮逻辑。第一轮不注入 `teamIDMap`，主要依赖名称、时间、别名、P2 先验、FSModel 和 DTW 证据；随后把第一轮 `ResolvedEventMatch` 转换为现有 `EventMatch`，复用 `DeriveTeamMappings` 推导 `teamIDMap`；第二轮重新执行候选边评分，并允许 `TEAM_ID_FALLBACK` / L4b 兜底极端偏移或低名称相似度样本。

## 6. P4 聚合所需输出字段

P4 聚合联赛级结果时至少需要读取以下字段：

| 字段 | 来源 | 用途 |
|------|------|------|
| `sr_event_id` | `ResolvedEventMatch.EventMatch` | 计算源侧比赛覆盖与召回。 |
| `ts_match_id` | `ResolvedEventMatch.EventMatch` | 去重、反向确认率和一对一约束校验。 |
| `ts_competition_id` | `ResolvedEventMatch` | 按 TS competition 聚合比赛证据。 |
| `ts_home_id` / `ts_away_id` | `ResolvedEventMatch.EventMatch` | 推导球队映射、检测主客错配。 |
| `ts_match_time` / `time_diff_sec` | `ResolvedEventMatch.EventMatch` | 时间偏移统计与异常检测。 |
| `confidence` / `score` | `ResolvedEventMatch` | 联赛聚合加权与自动确认阈值判断。 |
| `match_rule` / `rule` | `ResolvedEventMatch` | 统计 L1-L4b、L5、DTW 和兜底来源。 |
| `reason_codes` | `ResolvedEventMatch` | 人工复核解释、风险过滤和质量回归分析。 |
| `conflict_info` | `ResolvedEventMatch` | 展示被当前 winner 淘汰的候选和分差。 |
| `edges` | `ConflictResolutionResult` | 保留完整候选边，供 P4 回溯被淘汰候选和计算 competition 级证据分布。 |

## 7. P4 联赛证据聚合与强约束决策

P4 新增 `internal/matcher/league_evidence.go`，由 `LeagueEvidenceAggregator` 接收源侧联赛特征、P3 `ResolvedEventMatch` 和 TS competition 元数据，并按 `ts_competition_id` 聚合候选证据。聚合分数以比赛证据为主，联赛名称只保留为弱特征和 tie-break，不再作为主入口。

| 特征 | 默认权重 | 说明 |
|------|------:|------|
| `event_coverage_score` | 0.35 | 候选 competition 命中的比赛数 / 源侧比赛总数。 |
| `high_conf_event_score` | 0.20 | 高置信比赛数相对自动确认最小数量的饱和比例，默认高置信阈值 0.85、最小数量 3。 |
| `team_coverage_score` | 0.20 | 候选覆盖的源侧球队数 / 源侧球队总数。 |
| `two_team_anchor_score` | 0.10 | 两队 ID/名称均形成稳定锚点的比赛比例。 |
| `temporal_overlap_score` | 0.05 | 基于修正后时间差的高斯时间重叠均值。 |
| `location_score` | 0.05 | CountryCode、地区文本或国际赛事语义的地理一致性得分。 |
| `league_name_keyword_score` | 0.05 | 别名感知名称相似度与性别/年龄/分区/赛制/层级关键词一致性的弱特征。 |

最终分数为上述特征的加权和；若 CountryCode、地区文本、Women/Men、U19/U21/Reserve、North/South 分区、Cup/League/Friendly/Futsal/5x5 或层级数字出现明确冲突，则候选 hard veto、分数置零，并输出 `veto_reason` 与 `veto_detail`。输出审核证据行为 `LeagueEvidenceCandidate`，字段包含 `competition_id`、`competition_name`、`score`、`coverage`、`matched_events`、`high_conf_events`、`team_coverage`、`location_result`、`keyword_result`、`veto_reason`、`candidate_gap` 和 `top_event_examples`。

| 状态 | 规则 |
|------|------|
| `AUTO_CONFIRMED` | `score ≥ 0.85`、高置信比赛数 `≥ 3`、无 hard veto、Top1/Top2 分差 `≥ 0.10`。 |
| `REVIEW_REQUIRED` | `0.60 ≤ score < 0.85`，或高置信比赛不足，或候选分差 `< 0.10`。 |
| `REJECTED` | `score < 0.60`，或存在明确 hard veto。 |
| `KNOWN_SUSPECT` | KnownMap 反向确认率 `RCR < 0.30`，进入审核。 |

## 8. 已知限制

当前 P3 仍是轻量适配层，冲突消解采用贪心算法，不保证全局最优。`EvidenceEventCandidate` 以 SR 事件为首个源侧形态，后续若 LS 或其他数据源进入 Evidence-First 流程，应抽象为 canonical source event。FSModel 使用现有经验先验，若未来积累标注样本，应按 competition 或 sport 维度做参数校准。主客反转候选目前可被自动确认，但始终带 `SIDE_REVERSED` reason code，建议 P4 对该类结果设置更严格的聚合阈值或人工复核策略。P4 当前已提供独立聚合器和审核证据结构，但生产化仍需在 P2/P3/P5 编排层接入 TS competition 候选元数据、赛事总量分母、KnownMap RCR 与审核导出接口。
