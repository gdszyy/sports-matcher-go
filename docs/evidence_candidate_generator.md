# Evidence-First P2 候选生成器说明

`internal/matcher/evidence_candidate.go` 实现 **EvidenceCandidateGenerator**。它把源侧单联赛的小规模队伍集合与源侧比赛时间范围转化为 TS 队伍候选、TS 比赛候选和 P3 可直接消费的 `[]EvidenceEventCandidate`。P2 的职责边界是**召回、守门、降权和解释**；它不会确认最终 TS 联赛，也不会执行比赛级自动确认。

## 候选生成流程

| 阶段 | 输入 | 输出 | 说明 |
|------|------|------|------|
| 源侧队伍合并 | 显式 `[]EvidenceSourceTeam` 与 `[]db.SREvent` 中的主客队 | 去重后的源侧队伍集合 | 当调用方未显式传入队伍集合时，生成器会从源侧比赛抽取主客队。 |
| 队伍召回 | 队名变体、源联赛国家、别名 TS 队伍 ID、比赛时间范围 | `[]EvidenceQueriedTeam` | 通过 `EvidenceTSCandidateQuerier.QueryTeamCandidates` 对接 P1 TS 队伍候选查询接口。 |
| 队伍打分与守门 | 源侧队伍、TS 队伍候选、联赛名称与国家 | `map[source_team_id][]TeamCandidateEvidence` | 按 Alias、Name+Country、Name+RecentMatch、International fallback 优先级合并，并输出 reason code。 |
| 比赛召回 | 非 veto 的 TS 队伍候选 ID 与源侧时间范围 | `[]EvidenceQueriedEvent` | 通过 `EvidenceTSCandidateQuerier.QueryEventCandidates` 对接 P1 TS 比赛候选查询接口。 |
| 比赛候选解释 | 源侧比赛、TS 比赛候选、队伍候选先验 | `[]EventCandidateEvidence` 与 `[]EvidenceEventCandidate` | 比赛候选分由主队候选分、客队候选分和时间分组成，随后按硬上限截断。 |

## 数据结构

| 结构 | 关键字段 | 下游用途 |
|------|----------|----------|
| `EvidenceCandidateOptions` | `TeamTopK`、`InternationalTeamTopK`、`EventCandidateLimit`、`LeagueName`、`LeagueCountry`、`AliasStore`、`TeamAliasIndex` | 控制召回规模、强约束上下文、别名来源和 P1 查询参数。 |
| `TeamCandidateEvidence` | `TSTeamID`、`CompetitionID`、`Score`、`Priority`、`Vetoed`、`StrongConstraintOK`、`ReasonCodes` | P2 审计和 P3 前置队伍证据；强约束失败候选可保留解释，但不会进入普通自动确认路径。 |
| `EventCandidateEvidence` | `SourceEventID`、`CompetitionID`、`Event`、`CandidateScore`、`HomeTeamCandidateScore`、`AwayTeamCandidateScore`、`StrongConstraintOK`、`ReasonCodes` | P2 比赛候选审计结构，保留候选来源和截断解释。 |
| `EvidenceCandidateResult.P3EventCandidates` | `[]EvidenceEventCandidate` | P3 `EvidenceEventMatcher` 可直接消费的比赛候选池。 |

## reason code 列表

| reason code | 类型 | 含义 |
|-------------|------|------|
| `ALIAS_HIT` | 正向证据 | 已确认 `AliasStore` 或 `TeamAliasIndex` 命中，是最高优先级候选来源。 |
| `NAME_SIM_HIGH` | 正向证据 | 队名相似度达到高阈值，默认阈值为 `0.86`。 |
| `NAME_SIM_MEDIUM` | 正向证据 | 队名相似度达到中阈值，默认阈值为 `0.68`。 |
| `COUNTRY_MATCH` | 正向证据 | 源联赛国家/地区与 TS 候选国家/地区一致。 |
| `COUNTRY_MISSING` | 风险提示 | 源侧或 TS 侧国家/地区为空，进入降权而非硬确认路径。 |
| `RECENT_MATCH_WINDOW` | 正向证据 | 候选队伍或比赛落在源侧近期时间窗口内。 |
| `INTERNATIONAL_FALLBACK` | 降权召回 | 国际赛事、跨洲赛事或地区缺失时允许跨地区召回，但降权并受 TopK 限制。 |
| `GUARD_VETO_AGE` | 强约束 | U19/U21/其他年龄段冲突或单侧年龄标识缺失导致 veto。 |
| `GUARD_VETO_GENDER` | 强约束 | Women/Men 或 Women/未知冲突导致 veto。 |
| `GUARD_VETO_RESERVE` | 强约束 | Reserve/B team 标识冲突导致 veto。 |
| `GUARD_VETO_COUNTRY` | 强约束 | 非国际赛事下明确国家/地区冲突导致 veto。 |
| `GUARD_VETO_LEAGUE_KEYWORD` | 强约束 | 联赛结构化关键词（如赛制、层级、区域）冲突导致 veto。 |
| `EVENT_TEAM_PAIR` | 比赛召回 | TS 比赛由主客两队候选共同命中。 |
| `CANDIDATE_TRUNCATED` | 规模控制 | 队伍 TopK 或整体比赛候选硬上限触发截断。 |

## 规模控制与性能验证

默认情况下，每个源队伍最多保留 `TeamTopK=8` 个 TS 队伍候选；国际赛事最多保留 `InternationalTeamTopK=15` 个。比赛候选使用 `EventCandidateLimit=300` 作为整体硬上限，超出部分会设置 `TruncatedEventCandidates=true` 并记录 `DroppedEventCandidateCount`。理论查询规模为 `O(source_team_count × P1_team_query_limit + event_candidate_limit)`；单联赛通常只有几十支队伍，因此 P95 目标小于 3 秒主要依赖 P1 查询端的索引和 `LIMIT` 控制。

建议验证命令如下：

```bash
PATH=/usr/local/go/bin:$PATH go test ./internal/matcher -run EvidenceCandidateGenerator -v
PATH=/usr/local/go/bin:$PATH go test ./...
git diff --check
```
