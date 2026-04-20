# python/data — 2026 年算法测试数据集

本目录存放从 SR / LS / TS 三个数据源拉取的 2026 年体育赛事数据，供匹配算法离线测试使用。

## 数据文件说明

| 文件 | 数据源 | 内容 | 记录数 |
|------|--------|------|--------|
| `sr_leagues_2026.json` | SportRadar | 2026 年热门 + 常规联赛列表（足球 + 篮球） | 47 联赛 |
| `sr_events_2026.json` | SportRadar | 2026 年比赛数据（含主客队名称） | ~22,618 场 |
| `ls_leagues_2026.json` | LSports | 2026 年 Top 联赛列表（足球 Top30 + 篮球 Top15） | 45 联赛 |
| `ls_events_2026.json` | LSports | 2026 年比赛数据（含主客队名称） | ~15,039 场 |
| `ts_leagues_2026.json` | TheSports | 2026 年 Top 联赛列表（足球 Top30 + 篮球 Top15） | 45 联赛 |
| `ts_events_2026.json` | TheSports | 2026 年比赛数据（含主客队名称） | ~34,242 场 |
| `sr_ts_match_mapping_raw.json` | TS 映射表 | 原始 SR↔TS 比赛映射记录（三表合并去重） | 2,858 条 |
| `sr_ts_ground_truth.json` | 合并 | **SR↔TS 完整匹配记录**（含双方比赛详情） | 2,858 条 |
| `sr_ts_ground_truth_by_league.json` | 合并 | 按联赛分组的匹配记录 | 14 联赛 |
| `ground_truth_summary.json` | 统计 | Ground Truth 统计摘要 | — |
| `fetch_summary.json` | 统计 | 数据拉取统计摘要 | — |

## Ground Truth 数据集说明

`sr_ts_ground_truth.json` 是核心算法测试数据集，每条记录包含：

### SR 侧字段
| 字段 | 说明 |
|------|------|
| `sr_event_id` | SR 比赛 ID（格式：`sr:match:xxx`） |
| `sr_tournament_id` | SR 联赛 ID（格式：`sr:tournament:xxx`） |
| `sr_tournament_name` | SR 联赛名称 |
| `sr_scheduled` | SR 比赛时间（ISO8601，UTC） |
| `sr_home_id` / `sr_home_name` | SR 主队 ID / 名称 |
| `sr_away_id` / `sr_away_name` | SR 客队 ID / 名称 |
| `sr_status_code` | SR 比赛状态码 |

### TS 侧字段
| 字段 | 说明 |
|------|------|
| `ts_match_id` | TS 比赛 ID |
| `ts_competition_id` | TS 联赛 ID |
| `ts_competition_name` | TS 联赛名称 |
| `ts_match_time` | TS 比赛时间（Unix 时间戳，秒） |
| `ts_match_time_str` | TS 比赛时间（ISO8601，UTC） |
| `ts_home_id` / `ts_home_name` | TS 主队 ID / 名称 |
| `ts_away_id` / `ts_away_name` | TS 客队 ID / 名称 |
| `ts_status_id` | TS 比赛状态 ID |

### 元数据字段
| 字段 | 说明 |
|------|------|
| `mapping_score` | 映射置信分（0.0~1.0，来自 `ts_sr_match_mapping_3`） |
| `sport_id` | 运动类型 ID（`sr:sport:1`=足球，`sr:sport:2`=篮球） |
| `sport` | 运动名称（`football` / `basketball`） |
| `source_table` | 数据来源映射表 |
| `sr_found` / `ts_found` | SR/TS 比赛详情是否找到 |

## Ground Truth 统计

| 指标 | 值 |
|------|-----|
| 总匹配记录 | 2,858 条 |
| 完整记录（SR+TS均有详情） | 2,858 条（100%） |
| 足球匹配 | 2,030 场 |
| 篮球匹配 | 828 场 |
| 覆盖联赛 | 14 个 |
| 平均置信分 | 0.9757 |
| 置信分=1.0 | 1,934 条（67.7%） |
| 置信分≥0.9 | 2,753 条（96.3%） |

## 覆盖联赛列表

| SR 联赛 ID | 联赛名称 | 运动 | 匹配场数 | 平均置信分 |
|-----------|---------|------|---------|-----------|
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

## 数据生成脚本

| 脚本 | 功能 |
|------|------|
| `python/fetch_2026_data.py` | 拉取 SR/LS/TS 联赛和比赛数据 |
| `python/build_sr_ts_ground_truth.py` | 生成 SR↔TS Ground Truth 数据集 |
| `python/explore_leagues.py` | 探查数据库联赛分布（辅助工具） |

## 重新生成数据

```bash
# 确保 SSH 隧道已建立（端口 3308 → RDS，3309 → RDS）
ssh -i keys/id_ed25519 -N \
    -L 3308:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
    -L 3309:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
    ubuntu@54.69.237.139 &

# 拉取 SR/LS/TS 数据
python3 python/fetch_2026_data.py

# 生成 SR↔TS Ground Truth
python3 python/build_sr_ts_ground_truth.py
```
