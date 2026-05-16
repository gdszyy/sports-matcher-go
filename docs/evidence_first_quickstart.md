# Evidence-First Quickstart

> 一份给团队成员 / 新接手 Agent 的 Evidence-First 流水线入门指南。
> 适用版本：commit `73d89a6` 及之后（SR + LS 链路均已对称）。
> 配套流程洞察：[PI-006 v1.6 Evidence-First 比赛级匹配流程](../.cursor/rules/process_insights/PI-006_evidence_first_matching_flow.md)。

---

## 1. 这是什么 / 为什么

旧匹配路径（"P0"）的工作方式是：

```
源侧联赛  →  league.MatchLeague(单胜者)  →  一个 TS competition  →  MatchEvents(单 competition)
```

问题：联赛级一旦选错 / 不准（同名跨国、缩写歧义、商业冠名、二级联赛…），后面比赛级再准也救不回来。

Evidence-First（"EF"）把这一刀切的决策换成 **候选池 + 证据边一对一消解**：

```
源侧联赛
   │
   ▼  league_topn.go / league_topn_ls.go
MatchLeagueTopN(...)                                 ←─ Top-N 联赛候选
   │  KnownMap 命中占 Top-1（score=1.0）
   │  其余按 leagueNameScore 降序，>=0.55 入选
   ▼
[]LeagueMatchCandidate（含 StrongConstraintOK/Reason）
   │
   ▼  ToTSCompetitionCandidates(_, sport)
[]TSCompetitionCandidate
   │
   ▼  candidate_pool.go : CandidatePoolBuilder.Build    (P1)
[]EvidenceEventCandidate  +  unioned TSTeamNames
   │
   ▼  team_prior_enricher.go : EnrichTeamPriors         (P2)
   │  填充 Home/Away TeamCandidateScore（别名感知）
   ▼
[]EvidenceEventCandidate（priors 已填）
   │
   ▼  evidence_event_matcher.go : MatchTwoRound         (P3)
   │  scoreEdge + 一对一 resolveConflicts + 第二轮 L4b
   ▼
ConflictResolutionResult{Matches, Eliminated, Edges, TeamIDMap}
```

四个关键不同：

1. **多候选**：联赛级不再只挑一个，而是带着 Top-N 候选下到比赛级。
2. **可解释边**：每条 SR↔TS 候选边都有 `reason_codes`（`TIME_WINDOW`/`ALIAS_HIT`/`TEAM_ID_FALLBACK`/`SIDE_REVERSED`/`FS_MODEL`/`DTW_OFFSET`/`P2_CANDIDATE_PRIOR`/`STRONG_CONSTRAINT`/`CONFLICT_TS_USED`/`CONFLICT_SOURCE_USED`/`BELOW_AUTO_THRESHOLD`/`GUARD_VETO`）。
3. **一对一冲突消解**：一个 `ts_match_id` 最多被一个源侧事件占用，被淘汰边记录 `lost_to` 与原因。
4. **两轮 L4b**：第一轮基于名称推导 `teamIDMap`，第二轮注入后启用 `TEAM_ID_FALLBACK`。

---

## 2. 各阶段一句话

| 阶段 | 文件 | 职责 | 关键产物 |
|------|------|------|---------|
| **Top-N 入口（SR）** | `internal/matcher/league_topn.go` | KnownMap → Top-1，其余按 leagueNameScore 降序 | `[]LeagueMatchCandidate` |
| **Top-N 入口（LS）** | `internal/matcher/league_topn_ls.go` | SR 镜像 + lsLocationVeto 显式记录 + NoKnownMap 模式 | `[]LeagueMatchCandidate` |
| **P1 候选池生成** | `internal/matcher/candidate_pool.go` | 拉每个 TS comp 的事件 + 球队名 union，包成候选单元 | `[]EvidenceEventCandidate` + `TSTeamNames` |
| **P2 球队先验填充** | `internal/matcher/team_prior_enricher.go` | TS team 名 × 全部 SR team 名取最佳相似度 | mutated `HomeTeamCandidateScore` / `AwayTeamCandidateScore` |
| **P3 比赛级匹配** | `internal/matcher/evidence_event_matcher.go` | scoreEdge 打分 + resolveConflicts 一对一消解 + 两轮 L4b | `ConflictResolutionResult{Matches, Eliminated, Edges, TeamIDMap}` |
| **Wire 进 RunLeague** | `internal/matcher/universal_engine_evidence_first.go` | TopNAdapter 接口 + runLeagueEvidenceFirst 串联 | `*UniversalMatchResult` |

---

## 3. CLI 用法

SR 与 LS 4 个生产命令全部支持 EF 开关：

```bash
# SR 单联赛 EF
./sports-matcher match2 "sr:tournament:17" \
  --use-evidence-first \
  --evidence-first-topn 5 \
  --json

# SR 批量 EF
./sports-matcher batch2 --use-evidence-first --evidence-first-topn 5

# LS 单联赛 EF
./sports-matcher ls-match 67 --use-evidence-first

# LS 单联赛 EF + 纯算法（跳过 KnownLSLeagueMap）
./sports-matcher ls-match 67 --use-evidence-first --no-known-map

# LS 批量 EF
./sports-matcher ls-batch --use-evidence-first
```

Flag 速查：

| Flag | 默认 | 含义 |
|------|------|------|
| `--use-evidence-first` | false | 启用 EF 流水线；默认 false 走原 P0 路径 |
| `--evidence-first-topn N` | 0 → 5 | Top-N 联赛候选数 |
| `--no-known-map`（仅 LS） | false | 跳过 KnownLSLeagueMap；与 `--use-evidence-first` 组合 = 纯算法 + 完整 EF |
| `--no-players` | false | 跳过球员匹配（提速） |
| `--json` | false | 输出完整 JSON 结果（便于工具消费） |
| `--ts-id` | "" | 强制指定 TS competition_id（跳过联赛匹配） |

---

## 4. 跑通最小示例

假设 KnownMap 命中（SR Premier League id=17）：

```bash
# 配好 SSH 隧道（参考 internal/db/tunnel.go）后
go build -o sports-matcher ./cmd/server

./sports-matcher match2 "sr:tournament:17" \
  --sport football --tier hot \
  --use-evidence-first --evidence-first-topn 5
```

日志会出现 `[sr][EF]` 前缀的四个阶段：

```
[sr][EF] [1/4] 联赛 Top-N: 1 候选 (n=5)
[sr][EF]   Top-1: jednm9whz0ryox8 rule=RuleLeagueKnown score=1.000 (共 1 候选)
[sr][EF] [2/4] P1 候选池: 380 候选事件 (跨 1 competition, 60 球队)
[sr][EF] [3/4] 源侧: 380 events, 20 teams; P2 priors filled
[sr][EF] [4/4] EF Match: 360/380 [L1=355 L2=2 L3=1 L4=2 L5=0 L4b=0 L6=0] edges=420 eliminated=60
[sr][EF]   teamMappings: 20
```

单候选输入（KnownMap 命中）时，EF 的行为与 P0 在数据层面等价 —— 已有 `TestCandidatePool_P0BackwardCompat_SingleCompMatches` 单测锁定。

---

## 5. EF vs P0 对照

要直接对比同一联赛两条路径的产出，跑两次再用 Python 工具 diff：

```bash
# 同一联赛分别跑 P0 / EF，输出 JSON
./sports-matcher match2 "sr:tournament:17" --json > /tmp/p0.json
./sports-matcher match2 "sr:tournament:17" --use-evidence-first --json > /tmp/ef.json

# 对照（脚本会输出关键指标差异表）
python3 python/compare_p0_ef.py /tmp/p0.json /tmp/ef.json
```

`compare_p0_ef.py` 会汇总：

- 联赛匹配是否变化（rule / confidence / TS competition）
- 比赛匹配总数 / 各规则（L1~L4b）的计数差
- 新增匹配 / 失去匹配的 SR event_id 清单（最多前 20 条）
- 球队映射数量与覆盖率

---

## 6. P0 基线零回退保证

默认 `UseEvidenceFirst=false`，整个 EF 模块的 Go 代码完全不参与 P0 路径。Python P0 评估脚本（`python/evidence_first_baseline_eval.py`）从 `python/data/` 离线数据集出发，不调用任何 Go 代码，因此 P0 结果与 EF 落地完全解耦。

四次复跑指纹（来自 `docs/tests/evidence_first_baseline_metrics_run{1..4}.json`）：

| run | event_match% | team_match% | SR P/R/F1 |
|-----|------------:|------------:|-----------|
| run1（基线） | 48.67% | 49.29% | 0.8927 / 0.8681 / 0.8799 |
| run2 | 48.67% | 49.29% | 0.8927 / 0.8681 / 0.8799 |
| run3 | 48.67% | 49.29% | 0.8927 / 0.8681 / 0.8799 |
| run4（EF 全部落地后） | 48.67% | 49.29% | 0.8927 / 0.8681 / 0.8799 |

详见 [PI-007](../.cursor/rules/process_insights/PI-007_evidence_first_p0_baseline.md) v1.2。

---

## 7. 常见排错

### 启用 EF 后日志里没有 `[EF]` 前缀

- 确认 `--use-evidence-first` 拼写正确；
- 确认 `tsComps` 非空（联赛匹配前的 `GetCompetitionsByFootball/Basketball` 没失败）；
- 旧链路适配器（非 SR / LS）未实现 `TopNAdapter` → 自动降级回 P0。日志会缺 `[EF]` 标识。

### EF 模式下 Top-N 始终只有 1 条

- KnownMap 命中：score=1.0 占 Top-1，**其余名称相似度候选会被同 ID 的 KnownMap 占位"挤掉"**（去重逻辑）。
- LS 用了 `--no-known-map`：等价于 nightlybuild 纯算法模式。
- 名称相似度阈值是 0.55，低于的都被丢弃。

### `len(result.Matches) > 0` 但全是未匹配

**P3 contract**：`resolveConflicts` 每个 SR event 都会输出一条 `ResolvedEventMatch`，未匹配的是 `Matched=false / Rule=RuleEventNoMatch` 的 stub。判断"真的匹配上了"看 `Matched=true`，不要看 `len(Matches)==0`。

### 部分 SR 事件预期能匹配上但 EF 没匹配

按 `reason_codes` 排查：

- `BELOW_AUTO_THRESHOLD`：综合分低于 `defaultEvidenceAutoConfirmThreshold=0.75`，调阈值或加 `--no-known-map` 排除 KnownMap 干扰。
- `CONFLICT_TS_USED` / `CONFLICT_SOURCE_USED`：被一对一消解淘汰，看 `lost_to` 字段确认赢家是否合理。
- `SIDE_REVERSED`：主客反转命中，惩罚 0.12 后分数还是低；检查源侧主客标注。
- `GUARD_VETO`：六维强约束触发；检查性别/年龄/赛制/层级/区域/国家特征是否一致。

---

## 8. 我想新增一条数据链路（不是 SR / LS）

实现两件事：

1. 把新链路的 adapter 接进 `SourceAdapter` 主接口（已有的，不会变）；
2. 在新 adapter 上实现 `TopNAdapter` 接口（写一个 `MatchLeagueTopN` 方法）。

之后 `UniversalEngine.RunLeague` 的 EF 分支会自动识别（type assertion）。无需修改 `universal_engine.go` 或 EF 任何核心文件。

---

## 9. 相关测试

| 测试 | 覆盖 |
|------|------|
| `TestCandidatePool_P0BackwardCompat_SingleCompMatches` | 单候选 EF 等价于 P0 |
| `TestP1_P3_EndToEnd` | P1 → P3 接力 + 一对一消解 |
| `TestP1_P2_P3_EndToEnd` | P1 → P2 → P3 端到端 |
| `TestLeagueTopN_To_P1_To_P2_To_P3_FullPipeline` | 完整 4-stage 流水线（SR） |
| `TestSRSourceAdapter_MatchLeagueTopN_ForwardsToPackageFunc` | SR adapter wire |
| `TestLSSourceAdapter_MatchLeagueTopN_ForwardsToPackageFunc` | LS adapter wire |
| `TestLSSourceAdapter_NoKnownMap_PropagatesToTopN` | LS `--no-known-map` 透传 |
| `TestUniversalEngine_NonTopNAdapter_FallsBackInterfaceCheck` | 非 TopN adapter 自动降级 |

```bash
go test ./internal/matcher/ -count=2 -v
```

---

## 10. 引用

- 完整设计与历次变更：[PI-006 v1.6](../.cursor/rules/process_insights/PI-006_evidence_first_matching_flow.md)
- 基线冻结与稳定性证据：[PI-007 v1.2](../.cursor/rules/process_insights/PI-007_evidence_first_p0_baseline.md)
- 联赛别名词典：[PI-002](../.cursor/rules/process_insights/PI-002_league_alias_index.md)
- 通用算法五级降级：[PI-001](../.cursor/rules/process_insights/PI-001_universal_matching_algorithm_design.md)
- 模块规范：[internal_matcher](../.cursor/rules/internal_matcher.md)、[cmd](../.cursor/rules/cmd.md)
