---
id: "PI-003"
version: "v1.0"
last_updated: "2026-04-20"
author: "Manus Agent / feat(data) commit 240a16b"
related_modules: ["python", "python/data", "internal/matcher"]
status: "active"
---

# PI-003: SR/LS/TS 2026 年算法测试数据集使用指南

## 流程概述

本洞察记录 `python/data/` 目录中 2026 年算法测试数据集的来源、结构特征、使用方式及已知陷阱，供所有涉及算法评估的 Agent 在读取数据前参考。

---

## 数据集来源与生成链路

```
数据库（SSH 隧道）
  ├── xp-bet-test（SR）         → python/fetch_2026_data.py → sr_events_2026.json
  ├── test-xp-lsports（LS）     → python/fetch_2026_data.py → ls_events_2026.json
  ├── test-thesports-db（TS）   → python/fetch_2026_data.py → ts_events_2026.json
  └── test-thesports-db（TS）   → python/build_sr_ts_ground_truth.py
        ├── ts_sr_match_mapping_3   （主映射表，2789 条，sr:tournament:xxx 格式）
        └── ts_sr_match_mapping     （辅助映射表，2553 条，数字 tournament_id 格式）
              ↓ 合并去重 + 关联 SR/TS 详情
              → sr_ts_ground_truth.json（2858 条，100% 完整）
```

---

## 核心防坑指南

### 坑 1：`ts_sr_match_mapping` 与 `ts_sr_match_mapping_3` 的 ID 格式不一致

**现象**：直接使用 `ts_sr_match_mapping` 时，`type`（即 `sr_tournament_id`）是纯数字（如 `"132"`），而 `ts_sr_match_mapping_3` 中是完整格式（如 `"sr:tournament:132"`）。

**根因**：两张表是不同版本的映射结果，`_3` 是最新版本，已统一为 SR 标准格式。

**正确做法**：使用 `sr_ts_ground_truth.json` 时无需关心此差异（已在生成脚本中统一处理）。若直接查询数据库，优先使用 `ts_sr_match_mapping_3`。

**关键位置**：`python/build_sr_ts_ground_truth.py` → `load_raw_mappings()` 约第 55 行

---

### 坑 2：TS 比赛时间是 Unix 时间戳，SR 是 ISO8601 字符串

**现象**：直接比较 `sr_scheduled` 和 `ts_match_time` 会报类型错误或时间差计算错误。

**根因**：SR 存储格式为 `"2026-01-15T20:00:00+00:00"`，TS 存储格式为整数秒（如 `1736971200`）。

**正确做法**：
```python
from datetime import datetime, timezone

# SR 时间解析
sr_dt = datetime.fromisoformat(record['sr_scheduled'].replace('Z', '+00:00'))

# TS 时间解析（已在 ground_truth 中提供 ts_match_time_str 字段）
ts_dt = datetime.fromisoformat(record['ts_match_time_str'].replace('Z', '+00:00'))

# 计算时间差（分钟）
diff_minutes = abs((sr_dt - ts_dt).total_seconds()) / 60
```

**关键位置**：`python/data/sr_ts_ground_truth.json` → 字段 `ts_match_time_str`（已预处理为 ISO8601）

---

### 坑 3：LS 竞争对手 ID 类型不匹配导致 JOIN 失败

**现象**：LS 数据中 `ls_competitor_en.competitor_id` 是 bigint，而 `ls_sport_event.home_competitor_id` 是 varchar，直接 JOIN 时可能因隐式类型转换导致索引失效或结果错误。

**根因**：LS 数据库表设计不一致（历史遗留问题）。

**正确做法**：在 Python 中使用 `CAST` 或字符串比较：
```sql
LEFT JOIN ls_competitor_en hc
    ON CAST(e.home_competitor_id AS CHAR) = CAST(hc.competitor_id AS CHAR)
```

**关键位置**：`python/fetch_2026_data.py` → `fetch_ls()` → LS 比赛查询 SQL

---

### 坑 4：`ls_category_en` JOIN 必须加 sport_id 条件

**现象**：不加 `AND cat.sport_id = e.sport_id` 时，联赛数量统计会放大约 10 倍（因为同一 category 在多个运动中重复）。

**根因**：`ls_category_en` 表按 `(category_id, sport_id)` 复合主键设计，单独按 `category_id` JOIN 会产生笛卡尔积。

**正确做法**：
```sql
LEFT JOIN ls_category_en cat
    ON t.category_id = cat.category_id AND cat.sport_id = e.sport_id
```

**关键位置**：`python/fetch_2026_data.py` → `fetch_ls()` → 联赛统计查询

---

### 坑 5：Ground Truth 的 `mapping_score` 不等于算法匹配正确率

**现象**：`mapping_score` 字段来自 `ts_sr_match_mapping_3`，是系统历史运行时产生的置信分，不是人工标注的正确性评分。

**根因**：该分数由历史匹配算法计算，高分（≥0.9）通常可信，但低分（<0.75）的记录可能存在误匹配。

**正确做法**：
- 算法评估时建议**仅使用 `mapping_score >= 0.9` 的记录**（共 2,753 条）作为可信 Ground Truth
- 低置信分记录（9 条 `<0.75`）可用于测试算法的鲁棒性，但不应计入精确率/召回率计算
- 完整数据集（2,858 条）可用于覆盖率测试

---

## 关键耦合点

1. **数据集与匹配引擎的联赛 ID 格式**：`sr_ts_ground_truth.json` 中的 `sr_tournament_id` 使用 `sr:tournament:xxx` 格式，与 `internal/matcher/league.go` 中的联赛 ID 格式一致，可直接用于引擎测试。

2. **TS 比赛 ID 格式**：TS 的 `match_id` 是字母数字混合字符串（如 `"4jwq25tzw3klq0v"`），与 SR 的 `sr:match:xxx` 格式完全不同，在构建评估索引时需注意 key 类型。

3. **联赛覆盖范围**：Ground Truth 仅覆盖 14 个联赛（足球 13 + 篮球 1），其他联赛（如德甲、法甲等）在 `sr_events_2026.json` 中有原始数据但无 Ground Truth 标注，算法测试时需区分"有标注"和"无标注"联赛。

4. **数据时效性**：数据集于 2026-04-20 生成，`sr_events_2026.json` 包含 2026 全年数据（含未来比赛），`sr_ts_ground_truth.json` 仅包含已发生并完成映射的比赛。

---

## 快速参考：数据集规模

| 数据集 | 联赛数 | 比赛数 | 可信 GT |
|--------|--------|--------|---------|
| SR 原始数据 | 47 | ~22,618 | — |
| LS 原始数据 | 45 | ~15,039 | — |
| TS 原始数据 | 45 | ~34,242 | — |
| SR↔TS Ground Truth | 14 | 2,858 | 2,753（score≥0.9） |

---

## 版本变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|---------|------|
| v1.0 | 2026-04-20 | 初始记录，数据集首次生成 | Manus Agent |
