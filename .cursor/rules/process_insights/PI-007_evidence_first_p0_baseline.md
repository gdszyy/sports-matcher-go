---
id: "PI-007"
version: "v1.3"
last_updated: "2026-05-16"
author: "Manus AI, Claude Cowork"
related_modules: ["python", "python/data", "docs", "internal/matcher"]
status: "active"
---

# PI-007: Evidence-First P0 基线冻结与评估集流程

## 流程概述

Evidence-First P0 的目标是冻结旧流程基线，而不是优化核心匹配算法。推荐入口是 `python/evidence_first_baseline_eval.py`，该脚本复用 `python/test_sr_2026.py` 的 SR↔TS 离线匹配逻辑，并读取 `python/data/` 中的 SR、LS、TS 2026 年离线数据，输出统一的联赛匹配、比赛匹配、球队匹配、耗时、失败原因、规则分布与 RCR 指标。

## 核心防坑指南

### 坑 1: 在 P0 为了覆盖高歧义样本而伪造 GT

**现象**：U19、Women、二级联赛、商业冠名和缩写样本在离线数据中存在，但许多样本没有人工确认的 TS competition 或 SR↔TS GT。若 P0 为了让指标好看而手写正确答案，会污染后续 P1–P5 对比。

**根因**：P0 是基线冻结阶段，职责是记录当前旧流程的真实可运行边界；没有 KnownMap 或 GT 的样本应该以 `no_known_map`、`no_ts_events` 或 `no_ground_truth` 明确暴露，而不是被纳入 Precision/Recall 分母。

**正确做法**：保留补充高歧义样本，但仅将有 GT 的 SR 联赛纳入加权 Precision/Recall/F1。无 GT 样本输出 match_rate、team_match_rate、RCR 和 failure_reason，供 P1–P2 候选池生成与人工复核使用。

**关键位置**：`python/evidence_first_baseline_eval.py` → `SR_SUPPLEMENTAL_LEAGUES`、`LS_SUPPLEMENTAL_LEAGUES`、`failure_reason`。

### 坑 2: 把 LS 离线指标误读为完整在线 LS 基线

**现象**：`python/data/ls_events_2026.json` 的离线覆盖与 Go `ls-batch` 在线数据库覆盖并不完全一致，许多 `ls2026Leagues` 样本在离线 JSON 中没有 source events。

**根因**：P0 离线脚本为了可复现性只读取 `python/data/`，不直连数据库。在线 `ls-batch` 仍是 LS 完整回归入口，但依赖 SSH 隧道和数据库可用性。

**正确做法**：报告中同时记录 Go CLI 入口和离线验证路径。离线 LS 指标主要用于高歧义样本存在性、KnownMap 配置、RCR 公式和 failure_reason 冻结；在线 LS 批量结果应在数据库可用时另行运行并与 `docs/ls_ts_match_2026.md` 对比。

**关键位置**：`cmd/server/main.go` → `ls-batch`；`docs/evidence_first_baseline_report.md` → “当前可运行测试入口梳理”。

### 坑 3: 用耗时判断两次运行是否稳定

**现象**：相同配置两次运行时，耗时可能随沙箱负载波动，但联赛数、事件数、匹配数、匹配率、球队匹配率、RCR 和 SR GT 加权 P/R/F1 应保持一致。

**根因**：P0 脚本使用确定性离线数据和确定性匹配逻辑，核心指标不应受随机因素影响；耗时是性能观察值，不适合作为 1 个百分点稳定性标准。

**正确做法**：稳定性验收比较核心比例和计数字段，耗时只在报告中记录趋势。

**关键位置**：`docs/tests/evidence_first_baseline_metrics.json`、`docs/tests/evidence_first_baseline_metrics_run2.json`、`docs/tests/evidence_first_baseline_metrics_run3.json`、`docs/tests/evidence_first_baseline_metrics_run4.json`。

**已验证（v1.1，2026-05-16）**：第三次复跑在两周后、新沙箱环境下，除 `generated_at` 与各级 `elapsed_ms` 外，所有 summary 字段（`league_count` / `event_matched` / `event_match_rate` / `team_match_rate` / `avg_rcr` / `weighted_precision` / `weighted_recall` / `weighted_f1` / `failure_reasons` / `rule_distribution` / `ambiguity_coverage`）与 run1 指纹完全一致。SR 总耗时从 29 053 ms 降至 17 668 ms，再次证明耗时只是沙箱性能观察值。

**已验证（v1.2，2026-05-16）**：在 Evidence-First Go 侧 P1 / P2 / Top-N / Wire / CLI 五条 commit 全部落地后，第四次复跑（run4）与 run1 指纹仍然完全一致。这正面证明：**默认 `UseEvidenceFirst=false` 的设计保证 Python P0 评估脚本完全不受 Go 侧 EF 改动影响**。稳定性核心结论持续成立。

## 关键耦合点

| 耦合点 | 说明 |
|--------|------|
| `python/test_sr_2026.py` | P0 SR 离线事件匹配复用此脚本中的 `match_events_for_league`、`evaluate`、`KNOWN_LEAGUE_MAP` 和 `SR_2026_LEAGUES`。 |
| `python/data/` | P0 离线基线唯一数据源，禁止在评估脚本中直连数据库。 |
| `cmd/server/main.go` | Go 在线入口和 SR/LS 标准批量样本来源；P0 文档需记录但不改动。 |
| `docs/ls_ts_algo_vs_known_2026.md` | LS 纯算法误匹配案例与高歧义类型的重要来源。 |
| `docs/evidence_first_failure_cases.md` | P1–P5 失败案例回归清单，需与指标 JSON 中的 failure_reason 和 ambiguity_tags 对齐。 |

## 版本变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|----------|------|
| v1.0 | 2026-05-01 | 初始记录 Evidence-First P0 离线基线脚本、评估集边界、高歧义样本与稳定性验收防坑指南。 | Manus AI |
| v1.1 | 2026-05-16 | 新增 run3 复跑证据（`evidence_first_baseline_metrics_run3.json` 等），所有核心字段指纹与 run1/run2 一致；坑 3 章节补充三次复跑稳定性结论。 | Claude Cowork |
| v1.2 | 2026-05-16 | 在 Evidence-First Go 侧 P1/P2/Top-N/Wire/CLI 五条 commit 全部落地后，run4 复跑指纹与 run1 完全一致。正面证明 `UseEvidenceFirst=false` 默认零回退设计有效：Python P0 评估脚本完全不受 Go 侧 EF 改动影响。 | Claude Cowork |

## 测评 SOP（v1.18 新增）

**核心原则**：本算法明确**杜绝**把推断结果回写 KnownLeagueMap 或任何 mapping 表 — 任何 mapping 都会让算法效果失真（v1.18 决议）。`internal/matcher/known_league_map.go` 与 `known_ls_league_map.go` 仅保留作为生产侧"运营 override 层"，**不参与任何算法测评**。

### 必备 CLI flag

Go 侧 `match2` / `batch2` / `ls-match` / `ls-batch` 4 个测评命令一律带 `--strict-no-mapping`：

```bash
# SR 单跑
sports-matcher match2 sr:tournament:23479 \
    --use-evidence-first --use-team-first-fallback \
    --strict-no-mapping   # ← 必带

# SR 批量
sports-matcher batch2 --config eval_config.json \
    --use-evidence-first --use-team-first-fallback \
    --strict-no-mapping   # 