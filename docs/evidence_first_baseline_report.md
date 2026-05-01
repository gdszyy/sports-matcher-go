# Evidence-First P0 基线冻结与评估集报告

## 1. 阶段边界与基线原则

本报告记录 **Evidence-First P0：基线冻结与评估集准备** 的执行结果。P0 的目标不是改动核心匹配算法，而是在当前旧流程基础上建立可复现、可追踪、可重复运行的基线。后续 P1–P5 的每次改动均应使用同一评估脚本和同一最小评估集回归，比较联赛匹配、比赛匹配、球队匹配、耗时、失败原因、规则分布与 RCR 的变化。

本阶段已遵守项目入口规范，优先阅读 `AGENTS.md`、`.cursor/rules/global.md`、`.cursor/rules/internal_matcher.md`、`.cursor/rules/process_insights/index.md`、`.cursor/rules/process_insights/PI-006_evidence_first_matching_flow.md` 与 `docs/evidence_first_matching_plan.md`。实现时未修改 `internal/matcher`、`cmd/server/main.go` 或任何核心匹配阈值，仅新增离线评估脚本与基线文档。

| 项目 | 当前取值 |
|------|----------|
| 基线 commit | `4fa7c1c405da3c8898889ef37f632d9c6f0cc4b0` |
| 评估脚本 | `python/evidence_first_baseline_eval.py` |
| 指标输出 | `docs/tests/evidence_first_baseline_metrics.json`、`docs/tests/evidence_first_baseline_metrics.csv` |
| 第二次运行输出 | `docs/tests/evidence_first_baseline_metrics_run2.json`、`docs/tests/evidence_first_baseline_metrics_run2.csv` |
| 离线数据目录 | `python/data/` |
| 核心算法改动 | 无 |

## 2. 当前可运行测试入口梳理

当前仓库同时保留 Go CLI、Python 离线脚本与历史报告三类入口。P0 优先复用这些入口，不重新发明主流程。Go CLI 依赖 SSH 隧道和数据库连接，适合作为在线回归入口；Python 离线脚本只读取 `python/data/`，适合作为 P0 可复现基线。

| 链路 | 在线入口 | 离线入口 | 样本来源 | 主要输出 |
|------|----------|----------|----------|----------|
| SR↔TS 单联赛 | `go run ./cmd/server match2 <sr:tournament:id> --sport <football/basketball> --tier <hot/regular> --no-players` | `python/test_sr_2026.py --league <sr:tournament:id>` | `cmd/server/main.go` 的 `sr2026Leagues`、`internal/matcher/league.go` 的 `KnownLeagueMap`、`python/data/sr_ts_ground_truth.json` | 联赛映射、比赛匹配率、规则分布、球队匹配、耗时；有 GT 时输出 Precision/Recall/F1 |
| SR↔TS 批量 | `go run ./cmd/server batch2 --no-players` | `python/test_sr_2026.py` 与 `python/evidence_first_baseline_eval.py --source sr` | `python/data/sr_events_2026.json`、`python/data/ts_events_2026.json`、GT 重建候选 | 23 个标准 SR 联赛与补充高歧义 SR 样本的离线指标 |
| LS↔TS 单联赛 | `go run ./cmd/server ls-match <ls_id> --sport <football/basketball> --tier <hot/regular> --no-players` | `python/evidence_first_baseline_eval.py --source ls` | `cmd/server/main.go` 的 `ls2026Leagues`、`python/data/ls_events_2026.json`、`python/data/ts_events_2026.json` | LS KnownMap 基线的离线事件匹配、球队推导、RCR 与失败原因 |
| LS↔TS 批量 | `go run ./cmd/server ls-batch --no-players`，纯算法对比可加 `--no-known-map` | `python/evidence_first_baseline_eval.py --source ls` | `docs/ls_ts_match_2026.md` 与 `docs/ls_ts_algo_vs_known_2026.md` 记录历史基线与高歧义失败类型 | 联赛 KnownMap 与纯算法误吸附对比、失败案例来源 |

Ground Truth 的权威来源是 `python/data/sr_ts_ground_truth.json`，由 `python/build_sr_ts_ground_truth.py` 生成；其按联赛分组文件为 `python/data/sr_ts_ground_truth_by_league.json`。KnownMap 样本来自 Go CLI 内置批量配置及 `python/test_sr_2026.py` 中与 Go 侧同步的 `KNOWN_LEAGUE_MAP`。高歧义样本主要来自 `docs/ls_ts_algo_vs_known_2026.md`、`docs/league_match_evaluation_rule.md`、`docs/league_guard_keywords.json` 以及 `python/data/*_leagues_2026.json` 中实际存在的 U19、Women、二级联赛、杯赛与缩写联赛。

## 3. 最小评估集范围

本次 P0 评估集覆盖 **85 个联赛样本**，其中标准 SR 样本 23 个、补充 SR 高歧义样本 7 个、标准 LS 样本 49 个、补充 LS 高歧义样本 6 个。高歧义样本共 **33 个**，满足“不少于 30 个联赛、高歧义不少于 10 个”的验收条件。补充样本中部分没有经过人工确认的 TS competition，脚本会明确输出 `no_known_map` 或 `no_ts_events`，这些样本用于 P1–P2 候选池生成与强约束验证，不伪造 GT。

| 覆盖类型 | 代表样本 | 当前状态 |
|----------|----------|----------|
| U19/青年队 | SR `U19 DFB Nachwuchsliga`、SR `U19 Division de Honor Juvenil`、LS `Bundesliga U19` | 已纳入高歧义评估集；部分无 KnownMap |
| Women/女子 | SR `NCAA Women, Regular Season`、LS `IPBL. Pro Division - Women`、LS `NBL 1 - Women` | 已纳入；部分用于强约束回归 |
| 二级联赛 | LS `2.Bundesliga`、LS `Ligue 2`、LS `LaLiga2`、SR `National 2`、SR `2. Lig` | 已纳入；用于跨级别误匹配监控 |
| 杯赛 | SR `UEFA Champions League`、SR `UEFA Europa League`、LS `Bskt Cup`、LS `Copa Sudamericana` | 已纳入；用于 cup/league 类型约束 |
| 国际赛事 | SR `UEFA Champions League`、SR `AFC Asian Cup QF`、LS `CONMEBOL Copa Libertadores` | 已纳入；用于洲际/国家维度约束 |
| 商业冠名 | LS `BNXT League`、LS `Orlen Basket Liga`、LS `Meiji Yasuda J2/J3 100 Year Vision League` | 已纳入；用于名称噪声与 sponsor token 处理 |
| 跨国同名联赛 | LS `Serie A Brazil`、LS `Premier League Egypt`、LS `NBL` | 已纳入；用于国家/区域强约束 |
| 缩写歧义 | LS `FNL`、LS `HNL`、LS `NBL`、LS `BNXT League`、LS `NBL1` | 已纳入；用于缩写词典与候选解释 |

## 4. 运行命令与离线路径

本次 P0 的可复现基线路径如下。该路径不需要数据库、不需要 SSH 隧道，只依赖仓库内已提交的 `python/data/` 数据集。

```bash
python3 python/evidence_first_baseline_eval.py \
  --output-json docs/tests/evidence_first_baseline_metrics.json \
  --output-csv docs/tests/evidence_first_baseline_metrics.csv

python3 python/evidence_first_baseline_eval.py \
  --output-json docs/tests/evidence_first_baseline_metrics_run2.json \
  --output-csv docs/tests/evidence_first_baseline_metrics_run2.csv
```

在线 Go 入口仍建议在数据库与 Go 工具链可用时执行，但当前沙箱环境缺少 `go` 命令，因此本次未能运行 `go test ./...`。本次已运行 `git diff --check` 与 `python3 -m py_compile python/evidence_first_baseline_eval.py`，均通过；`go test ./...` 的失败原因为环境工具缺失：`bash: go: command not found`。

## 5. 基线指标结果

两次相同配置运行的核心指标完全一致，差异为 0 个百分点，满足“同一配置连续两次运行，核心指标差异不超过 1 个百分点”的验收标准。耗时指标会随机器负载波动，P0 仅将其作为性能基线记录，不纳入稳定性硬比较。

| 范围 | 联赛数 | 高歧义联赛数 | 源比赛数 | 已匹配比赛 | 比赛匹配率 | 球队数 | 已匹配球队 | 球队匹配率 | 平均 RCR | 加权 P | 加权 R | 加权 F1 |
|------|--------|--------------|----------|------------|------------|--------|------------|------------|----------|--------|--------|---------|
| 全量 | 85 | 33 | 9,017 | 4,389 | 48.6747% | 1,469 | 724 | 49.2852% | 0.207329 | 0.892655 | 0.868089 | 0.879918 |
| SR↔TS | 30 | 9 | 8,538 | 4,301 | 50.3748% | 1,110 | 674 | 60.7207% | 0.520767 | 0.892655 | 0.868089 | 0.879918 |
| LS↔TS | 55 | 24 | 479 | 88 | 18.3716% | 359 | 50 | 13.9276% | 0.036364 | — | — | — |

SR↔TS 的加权 Precision/Recall/F1 仅对有 GT 的 14 个 SR 联赛计算，与既有 `python/test_sr_2026.py` 历史基线保持一致。LS↔TS 当前离线 JSON 不包含人工 GT，因此 P0 不输出 LS Precision/Recall/F1，而以 KnownMap 联赛、事件匹配率、球队推导率、RCR 与失败原因作为冻结指标。

| 范围 | L1 | L2 | L3 | L4 | L5 | L4b | L6 |
|------|----|----|----|----|----|-----|----|
| 全量 | 3,866 | 96 | 96 | 331 | 0 | 0 | 0 |
| SR↔TS | 3,778 | 96 | 96 | 331 | 0 | 0 | 0 |
| LS↔TS | 88 | 0 | 0 | 0 | 0 | 0 | 0 |

| 失败原因 | 样本数 | 说明 |
|----------|--------|------|
| `no_source_events` | 55 | 离线源侧数据集中没有该联赛 2026 事件；主要来自 LS 标准配置与少量 SR KnownMap 样本 |
| `has_false_positive` | 12 | SR 有 GT 联赛中出现至少一个 FP；用于后续 P1–P5 回归 |
| `no_known_map` | 10 | 高歧义补充样本暂无人工确认 TS competition；保留为候选池/强约束输入 |
| `no_ground_truth` | 3 | 有源侧与 TS 候选并能匹配，但没有 SR↔TS GT 标注；用于无 GT 覆盖监控 |
| `ok` | 3 | 当前离线指标无错误原因 |
| `no_ts_events` | 2 | 已有 TS competition 配置但离线 TS 事件缺失 |

## 6. 主要失败与风险观察

当前基线暴露的关键事实是：**标准 SR GT 联赛的事件级指标稳定，但最小高歧义覆盖中仍存在大量无 GT、无 KnownMap 或离线源侧缺数的样本**。这并非脚本错误，而是 P0 需要冻结的现实约束。后续 P1–P2 应优先把这些样本转化为多 competition 候选池与人工复核样本，而不是在 P0 中伪造正确答案。

| 代表样本 | 现象 | P1–P5 对比关注点 |
|----------|------|------------------|
| SR `U19 DFB Nachwuchsliga`、LS `Bundesliga U19` | 当前无 KnownMap，无法进入确定 TS competition 的旧流程 | P1 需保留年龄组强约束；P2 候选池应找到 U19 TS 候选并解释年龄一致性 |
| SR `NCAA Women, Regular Season`、LS `NBL 1 - Women` | 女子/男子同名风险高，部分样本无 GT | P1 需将 gender 作为一票否决维度；P4 对 Women 标签不一致应降级人工复核 |
| LS `2.Bundesliga`、LS `J1 League`、SR `National 2` | 易跨一级/二级联赛误吸附 | P1 需强化 tier/level 特征；P4 RCR 低时应阻止联赛自动确认 |
| LS `Serie A Brazil`、LS `Premier League Egypt` | 跨国同名联赛风险 | P1 country/region 强约束应优先于名称相似度；P2 候选保留国家证据 |
| LS `FNL`、`HNL`、`NBL`、`BNXT League` | 缩写在不同国家和运动中含义不同 | P1 需建立缩写白名单与上下文约束；P2 输出 Top-N 候选解释 |

## 7. P1–P5 对比方式

后续每一阶段完成后，均应先运行本报告中的两条离线命令，再将新输出与 `docs/tests/evidence_first_baseline_metrics.json` 对比。P1–P5 的目标不是单纯提高全量 match_rate，而是在不牺牲高置信 GT 联赛 Precision 的前提下提升高歧义样本的可解释召回。

| 阶段 | 对比输入 | 主要对比指标 | 通过条件建议 |
|------|----------|--------------|--------------|
| P1 强约束与候选生成 | 高歧义联赛与 KnownMap 联赛 | `failure_reason`、候选覆盖率、强约束 veto 原因 | U19/Women/二级/跨国同名不再被高分误吸附 |
| P2 多 competition 候选池 | `no_known_map` 与 `no_ts_events` 样本 | 候选数量、候选命中率、解释字段完整性 | 高歧义样本至少产出可复核候选，不伪确认 |
| P3 比赛证据边 | 有候选池样本 | 规则分布、事件匹配率、冲突淘汰原因 | 保持一对一约束，输出 reason codes |
| P4 联赛聚合 | P3 结果 | RCR、联赛置信、人工复核队列 | 低 RCR 或强约束冲突不得自动确认 |
| P5 回归验收 | 全量 85 联赛 | SR 加权 P/R/F1、LS RCR、高歧义 failure_reason | SR GT 指标不下降超过 1 个百分点，高歧义解释性提升 |

## 8. 验收状态

| 验收标准 | 当前结果 | 状态 |
|----------|----------|------|
| 同一配置连续两次运行核心指标差异不超过 1 个百分点 | 两次运行核心指标完全一致 | 通过 |
| 样本覆盖不少于 30 个联赛，高歧义不少于 10 个 | 全量 85 个联赛，高歧义 33 个 | 通过 |
| 每次运行至少输出联赛匹配、比赛匹配率、球队匹配率、耗时和错误原因 | JSON/CSV 均包含这些字段 | 通过 |
| 失败案例可追踪，包含源联赛、错误 TS 联赛、触发规则和误判原因 | `docs/evidence_first_failure_cases.md` 已记录 | 通过 |
| `git diff --check` 通过；脚本能运行 | 已通过；Go 测试因当前环境缺少 `go` 未运行 | 部分通过 |
