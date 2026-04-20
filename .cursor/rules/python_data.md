---
description: "python/data 目录规范：SR/LS/TS 2026 年算法测试数据集结构、字段说明与使用方式"
globs: ["python/data/**/*", "python/fetch_2026_data.py", "python/build_sr_ts_ground_truth.py", "python/explore_leagues.py"]
---

# python/data 数据集规范

## 1. 目录职责

`python/data/` 是算法离线测试的**唯一标准数据目录**，存放从 SR / LS / TS 三个数据源拉取的 2026 年体育赛事数据及 Ground Truth 匹配标注。

> **核心原则**：所有算法测试脚本必须从此目录读取数据，禁止在测试脚本中直连数据库实时拉取。

---

## 2. 数据文件清单

### 2.1 原始数据（三数据源）

| 文件 | 数据源 | 内容 | 规模 |
|------|--------|------|------|
| `sr_leagues_2026.json` | SportRadar | 2026 年热门 + 常规联赛列表 | 47 联赛 |
| `sr_events_2026.json` | SportRadar | 2026 年比赛数据（含主客队名称） | ~22,618 场 |
| `ls_leagues_2026.json` | LSports | 2026 年 Top 联赛列表 | 45 联赛 |
| `ls_events_2026.json` | LSports | 2026 年比赛数据（含主客队名称） | ~15,039 场 |
| `ts_leagues_2026.json` | TheSports | 2026 年 Top 联赛列表 | 45 联赛 |
| `ts_events_2026.json` | TheSports | 2026 年比赛数据（含主客队名称） | ~34,242 场 |

### 2.2 Ground Truth 数据集（SR↔TS 匹配标注）

| 文件 | 内容 | 规模 |
|------|------|------|
| `sr_ts_ground_truth.json` | **核心**：SR↔TS 完整匹配记录（含双方详情） | 2,858 条 |
| `sr_ts_ground_truth_by_league.json` | 按联赛分组的匹配记录 | 14 联赛 |
| `sr_ts_match_mapping_raw.json` | 原始映射表数据（三表合并去重） | 2,858 条 |

### 2.3 统计摘要

| 文件 | 内容 |
|------|------|
| `ground_truth_summary.json` | Ground Truth 统计摘要（联赛分布、置信分分布） |
| `fetch_summary.json` | 数据拉取统计摘要（拉取时间、各源记录数） |

---

## 3. Ground Truth 字段说明

`sr_ts_ground_truth.json` 每条记录的完整字段：

### SR 侧字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `sr_event_id` | string | SR 比赛 ID，格式 `sr:match:xxx` |
| `sr_tournament_id` | string | SR 联赛 ID，格式 `sr:tournament:xxx` |
| `sr_tournament_name` | string | SR 联赛名称（英文） |
| `sr_scheduled` | string | SR 比赛时间（ISO8601，UTC） |
| `sr_home_id` | string | SR 主队 ID，格式 `sr:competitor:xxx` |
| `sr_home_name` | string | SR 主队名称（英文） |
| `sr_away_id` | string | SR 客队 ID |
| `sr_away_name` | string | SR 客队名称（英文） |
| `sr_status_code` | int | SR 比赛状态码 |

### TS 侧字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `ts_match_id` | string | TS 比赛 ID（字母数字混合） |
| `ts_competition_id` | string | TS 联赛 ID |
| `ts_competition_name` | string | TS 联赛名称 |
| `ts_match_time` | int | TS 比赛时间（Unix 时间戳，秒） |
| `ts_match_time_str` | string | TS 比赛时间（ISO8601，UTC） |
| `ts_home_id` | string | TS 主队 ID |
| `ts_home_name` | string | TS 主队名称 |
| `ts_away_id` | string | TS 客队 ID |
| `ts_away_name` | string | TS 客队名称 |
| `ts_status_id` | int | TS 比赛状态 ID |

### 元数据字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `mapping_score` | float | 映射置信分（0.0~1.0，来自 `ts_sr_match_mapping_3`） |
| `sport_id` | string | 运动类型 ID（`sr:sport:1`=足球，`sr:sport:2`=篮球） |
| `sport` | string | 运动名称（`football` / `basketball`） |
| `source_table` | string | 数据来源映射表名 |
| `sr_found` | bool | SR 比赛详情是否找到（当前全为 `true`） |
| `ts_found` | bool | TS 比赛详情是否找到（当前全为 `true`） |

---

## 4. Ground Truth 质量统计

| 指标 | 值 |
|------|-----|
| 总匹配记录 | 2,858 条 |
| 完整记录（SR+TS 均有详情） | 2,858 条（100%） |
| 足球匹配 | 2,030 场 |
| 篮球匹配 | 828 场 |
| 覆盖联赛 | 14 个 |
| 平均置信分 | 0.9757 |
| 置信分 = 1.0 | 1,934 条（67.7%） |
| 置信分 ≥ 0.9 | 2,753 条（96.3%） |

### 覆盖联赛

| SR 联赛 ID | 联赛名称 | 运动 | 场数 | 平均置信分 |
|-----------|---------|------|------|-----------|
| `sr:tournament:132` | NBA | 篮球 | 828 | 0.9316 |
| `sr:tournament:18` | Championship | 足球 | 244 | 0.9988 |
| `sr:tournament:242` | MLS | 足球 | 244 | 0.9924 |
| `sr:tournament:17` | Premier League | 足球 | 223 | 0.9973 |
| `sr:tournament:23` | Serie A | 足球 | 196 | 0.9974 |
| `sr:tournament:8` | LaLiga | 足球 | 191 | 0.9976 |
| `sr:tournament:35` | Bundesliga | 足球 | 167 | 0.9993 |
| `sr:tournament:34` | Ligue 1 | 足球 | 143 | 0.9955 |
| `sr:tournament:37` | Eredivisie | 足球 | 141 | 0.9951 |
| `sr:tournament:325` | Brasileiro Serie A | 足球 | 135 | 0.9966 |
| `sr:tournament:52` | Super Lig | 足球 | 127 | 0.9934 |
| `sr:tournament:203` | Premier League（苏超） | 足球 | 88 | 0.9919 |
| `sr:tournament:7` | UEFA Champions League | 足球 | 68 | 0.9973 |
| `sr:tournament:679` | UEFA Europa League | 足球 | 63 | 0.9117 |

---

## 5. 算法测试使用方式

### 5.1 推荐测试模式

```python
import json

# 加载 Ground Truth（算法测试标注集）
with open('python/data/sr_ts_ground_truth.json') as f:
    ground_truth = json.load(f)

# 筛选高置信度样本（推荐阈值 >= 0.9，共 2,753 条）
high_conf = [r for r in ground_truth if r['mapping_score'] >= 0.9]

# 按联赛筛选（示例：仅测试英超）
epl = [r for r in ground_truth if r['sr_tournament_id'] == 'sr:tournament:17']

# 加载待匹配数据（SR 侧输入）
with open('python/data/sr_events_2026.json') as f:
    sr_events = json.load(f)

# 加载待匹配数据（TS 侧输入）
with open('python/data/ts_events_2026.json') as f:
    ts_events = json.load(f)
```

### 5.2 评估指标计算

```python
# 以 sr_event_id + ts_match_id 为 key 构建 Ground Truth 索引
gt_index = {
    (r['sr_event_id'], r['ts_match_id']): r['mapping_score']
    for r in ground_truth
}

# 算法输出格式：[(sr_event_id, ts_match_id), ...]
def evaluate(predictions, ground_truth_index):
    tp = sum(1 for p in predictions if p in ground_truth_index)
    precision = tp / len(predictions) if predictions else 0
    recall = tp / len(ground_truth_index)
    f1 = 2 * precision * recall / (precision + recall) if (precision + recall) > 0 else 0
    return {'precision': precision, 'recall': recall, 'f1': f1}
```

### 5.3 按联赛分组测试

```python
# 使用按联赛分组的数据（更细粒度的评估）
with open('python/data/sr_ts_ground_truth_by_league.json') as f:
    by_league = json.load(f)

# 遍历每个联赛单独评估
for tournament_id, league_data in by_league.items():
    matches = league_data['matches']
    name = league_data['sr_tournament_name']
    print(f"{name}: {len(matches)} 场可用于测试")
```

---

## 6. 数据生成脚本

| 脚本 | 职责 | 触发条件 |
|------|------|---------|
| `python/fetch_2026_data.py` | 从 SR/LS/TS 数据库拉取联赛和比赛数据 | 需要更新原始数据时 |
| `python/build_sr_ts_ground_truth.py` | 从 `ts_sr_match_mapping_3` 生成 Ground Truth | 需要更新标注数据时 |
| `python/explore_leagues.py` | 探查数据库联赛分布（辅助工具） | 调研新联赛时 |

### 重新生成数据

```bash
# 1. 建立 SSH 隧道（参考 python_db.md）
ssh -i keys/id_ed25519 -N \
    -L 3308:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
    -L 3309:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
    ubuntu@54.69.237.139 &

# 2. 拉取原始数据（约 2 分钟）
python3 python/fetch_2026_data.py

# 3. 生成 Ground Truth（约 30 秒）
python3 python/build_sr_ts_ground_truth.py
```

---

## 7. 禁止行为

1. **禁止在算法测试脚本中直连数据库**：必须使用 `python/data/` 中的 JSON 文件，保证测试可复现。
2. **禁止手动编辑 JSON 数据文件**：数据文件由脚本自动生成，手动修改会破坏数据一致性。
3. **禁止将 `python/data/` 中的文件用于生产环境**：这些是测试数据集，不保证实时性。
4. **禁止删除 `ground_truth_summary.json`**：它是数据集质量的元信息，供 Agent 快速了解数据规模。
