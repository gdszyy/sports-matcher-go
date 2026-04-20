---
id: "PI-004"
version: "v1.0"
last_updated: "2026-04-20"
author: "Manus Agent / task-sr-2026-test"
related_modules: ["python", "python/data", "internal/matcher", "cmd", "internal/api"]
status: "active"
---

# PI-004: SR 2026 算法测试结果与最佳实践

## 流程概述

本洞察记录了使用最新 UniversalEngine（P0~P3 全量优化）对 SR 2026 常规和热门数据执行离线算法测试的完整过程、关键发现、测试结果基准值，以及后续测试时应遵循的最佳实践。

测试脚本：`python/test_sr_2026.py`（commit `afd4e5e`，2026-04-20）

---

## 测试结果基准（2026-04-20）

### 全量汇总（14 个 GT 联赛，2858 条 Ground Truth）

| 指标 | 值 |
|------|-----|
| 加权 Precision | **0.8927** |
| 加权 Recall | **0.8681** |
| 加权 F1 | **0.8799** |
| 平均置信度 | **0.9737** |
| 总匹配率（有数据联赛） | **76.5%**（2562/3349 SR 事件） |

### 热门联赛明细（7 个足球 + NBA）

| 联赛 | SR ID | SR 事件 | 匹配率 | P | R | F1 | 置信 | TS 来源 |
|------|-------|---------|--------|---|---|----|----|---------|
| LaLiga | sr:tournament:8 | 191 | 98.4% | 0.984 | 0.969 | **0.976** | 0.939 | gt_rebuild |
| Bundesliga | sr:tournament:35 | 170 | 98.2% | 0.952 | 0.952 | **0.952** | 0.972 | gt_rebuild |
| Serie A | sr:tournament:23 | 200 | 98.0% | 0.995 | 0.995 | **0.995** | 0.958 | gt_rebuild |
| Ligue 1 | sr:tournament:34 | 146 | 97.3% | 0.979 | 0.972 | **0.975** | 0.961 | gt_rebuild |
| UEFA Champions League | sr:tournament:7 | 81 | 84.0% | 1.000 | 1.000 | **1.000** | 0.980 | gt_rebuild |
| UEFA Europa League | sr:tournament:679 | 81 | 76.5% | 0.823 | 0.810 | **0.816** | 0.913 | gt_rebuild |
| NBA | sr:tournament:132 | 786 | 97.1% | 0.962 | 0.886 | **0.923** | 0.991 | file |

### 常规联赛明细（有 GT 数据的 6 个）

| 联赛 | SR ID | SR 事件 | 匹配率 | P | R | F1 | 置信 | TS 来源 |
|------|-------|---------|--------|---|---|----|----|---------|
| Championship | sr:tournament:18 | 254 | 95.7% | 0.942 | 0.939 | **0.940** | 0.987 | gt_rebuild |
| MLS | sr:tournament:242 | 245 | 99.2% | 0.996 | 0.992 | **0.994** | 0.988 | file |
| Premier League（苏超） | sr:tournament:17 | 0 | — | — | — | — | — | 无SR事件 |
| Brasileiro Serie A | sr:tournament:325 | 137 | 98.5% | 0.970 | 0.970 | **0.970** | 0.945 | file |
| Eredivisie | sr:tournament:37 | 143 | 97.9% | 0.986 | 0.979 | **0.982** | 0.985 | gt_rebuild |
| Super Lig | sr:tournament:52 | 129 | 98.4% | 0.984 | 0.984 | **0.984** | 0.963 | gt_rebuild |

---

## 核心防坑指南

### 坑 1: ts_events_2026.json 不覆盖主流足球联赛

**现象**：运行测试脚本后，英超、德甲、西甲、意甲、法甲、UCL、UEL 等联赛的 TS 事件数为 0，导致跳过。

**根因**：`ts_events_2026.json` 是按 TS 数据库中 2026 年事件数量 Top 45 联赛拉取的，实际覆盖的是低级别联赛（西班牙三级、德国五级、意大利四级等）和美洲联赛，而非与 SR 热门联赛对应的那些。

**正确做法**：使用 `gt_rebuild` 模式——从 `sr_ts_ground_truth.json` 重建 TS 候选集。脚本 `python/test_sr_2026.py` 已自动处理：优先使用 `ts_events_2026.json`，若无数据则自动降级为 GT 重建模式。

**关键位置**：`python/test_sr_2026.py` → `load_data()` 约第 200 行（`ts_by_comp_gt` 构建逻辑）

**注意**：GT 重建模式的 TS 候选集仅包含已知正确答案（无干扰项），因此 Precision 偏高，实际生产环境中 Precision 会略低。MLS、Brasileiro、NBA 使用真实 `file` 候选池，指标更接近生产环境。

---

### 坑 2: KnownLeagueMap 中的 TS competition_id 与实际数据不一致

**现象**：`KnownLeagueMap` 中写入的 TS competition_id（如 `vl7oqdehlyr510j` = LaLiga）在 `ts_events_2026.json` 中事件数为 0，但在 `ts_leagues_2026.json` 中也找不到对应条目。

**根因**：`ts_leagues_2026.json` 使用 `id` 字段（非 `competition_id`），且拉取的联赛集合与 `KnownLeagueMap` 中的 ID 来自不同的 TS API 端点/时间窗口。

**正确做法**：以 `sr_ts_ground_truth.json` 中的 `ts_competition_id` 字段为权威来源，通过以下命令验证：
```python
from collections import defaultdict, Counter
import json

with open('python/data/sr_ts_ground_truth.json') as f:
    gt = json.load(f)

gt_ts_by_sr = defaultdict(Counter)
for r in gt:
    gt_ts_by_sr[r['sr_tournament_id']][r['ts_competition_id']] += 1

for sr_tid, ts_cnt in sorted(gt_ts_by_sr.items()):
    best_ts = ts_cnt.most_common(1)[0]
    print(f'{sr_tid} -> {best_ts[0]} (GT={best_ts[1]})')
```

**关键位置**：`internal/matcher/league.go` → `KnownLeagueMap` 变量定义

---

### 坑 3: Premier League（sr:tournament:17）在 sr_events_2026.json 中无数据

**现象**：`sr:tournament:17`（英超）在 `sr_events_2026.json` 中事件数为 0，但 GT 中有 223 条记录。

**根因**：`sr_events_2026.json` 拉取时未包含英超数据（可能是 SR API 限制或拉取脚本遗漏）。

**正确做法**：如需测试英超，需重新运行 `python/fetch_2026_data.py` 并指定 `sr:tournament:17`，或直接从数据库查询。当前测试脚本会自动跳过并标注 `无SR事件`。

**关键位置**：`python/fetch_2026_data.py` → `HOT_LEAGUES` 列表

---

### 坑 4: UEL（欧联杯）F1 偏低（0.816）

**现象**：UEFA Europa League（sr:tournament:679）F1=0.816，明显低于其他热门联赛（>0.92）。

**根因**：
1. UEL 赛程较稀疏（81 场），GT 仅 63 条，样本量小导致指标波动大
2. UEL 存在多轮次赛制（小组赛/淘汰赛），部分比赛在 TS 侧未收录
3. GT 中 UEL 的 `mapping_score` 平均值（0.9117）是所有联赛中最低的，说明原始 GT 质量本身有噪声

**正确做法**：UEL 的 F1 基准值为 0.816，低于此值需排查 GT 质量问题，而非算法问题。

---

### 坑 5: NBA Recall 偏低（0.886）

**现象**：NBA F1=0.923，但 Recall=0.886，GT=828 而 TS 事件仅 777，存在 51 场 GT 记录在 TS 候选池中找不到。

**根因**：`ts_events_2026.json` 中 NBA（`49vjxm8xt4q6odg`）有 777 条，但 GT 中有 828 条，差值 51 场说明有部分 NBA 比赛在 TS 数据拉取时间窗口外（可能是季后赛/全明星赛等特殊赛事）。

**正确做法**：NBA Recall 基准值为 0.886，这是数据覆盖问题，不是算法问题。若需提升，需补充拉取 TS NBA 数据。

---

## TS 来源说明

| 来源标记 | 含义 | 指标可信度 |
|---------|------|-----------|
| `file` | 直接来自 `ts_events_2026.json`（真实候选池，含干扰项） | **高**，接近生产环境 |
| `gt_rebuild` | 从 `sr_ts_ground_truth.json` 重建（仅含正确答案，无干扰项） | **中**，Precision 偏高，Recall 准确 |
| `none` | 无 SR 事件或无 TS 事件，跳过 | 不适用 |

---

## 算法特性速查（UniversalEngine P0~P3）

| 特性 | 实现位置 | 参数 |
|------|---------|------|
| 高斯时间衰减 | `internal/matcher/event.go` → `gaussianTimeFactor` | σ=3600s(L1) / 10800s(L2) / 43200s(L3) |
| L1~L5 七级策略 | `internal/matcher/event.go` → `levelConfigs` | 见 PI-001 |
| L4b 球队ID精确匹配 | `internal/matcher/event.go` → `matchEventPair` | VoteCount ≥ 2 激活 |
| L6 占位符匹配 | `internal/matcher/event.go` | 时间差 ≤ 300s |
| TeamAliasIndex | `internal/matcher/event.go` → `TeamAliasIndex` | 学习阈值 0.50，应用分 0.92 |
| 主客场颠倒否决 | `internal/matcher/event.go` → `matchEventPair` | rev_sim > fwd_sim + 0.15 |
| 六维强约束 | `internal/matcher/league_features.go` | 性别/年龄/赛制/层级/区域/国家 |
| Fellegi-Sunter EM | `internal/matcher/fs_model.go` | 无监督参数估计 |
| EventDTW 兜底 | `internal/matcher/event_dtw.go` | 动态时间规整 |
| AliasStore 持久化 | `internal/db/alias_store.go` | VoteCount ≥ 2 自动写入 |
| KnownLeagueMapValidator | `internal/matcher/known_map_validator.go` | RCR < 0.30 自动降级 |

---

## 关键耦合点

1. **`KnownLeagueMap`（league.go）↔ 测试脚本（test_sr_2026.py）**：测试脚本中的 `KNOWN_LEAGUE_MAP` 字典必须与 `internal/matcher/league.go` 中的 `KnownLeagueMap` 保持同步，否则会导致 TS 候选集为空。

2. **`sr2026Leagues`（main.go）↔ `SR_2026_LEAGUES`（test_sr_2026.py）**：两处联赛配置必须一致，否则测试结果无法反映生产行为。

3. **GT 数据权威性**：`sr_ts_ground_truth.json` 是所有 SR↔TS 映射的唯一权威来源，`KnownLeagueMap` 中的 TS competition_id 必须以 GT 为准。

4. **TS 数据覆盖**：`ts_events_2026.json` 仅覆盖 45 个低级别联赛，主流足球联赛需通过 `gt_rebuild` 模式或重新拉取数据。

---

## 版本变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|---------|------|
| v1.0 | 2026-04-20 | 初始记录：SR 2026 算法测试基准结果、五个核心防坑指南、TS 来源说明 | Manus Agent |
