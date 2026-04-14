# XP-BET 数据库查询参考手册

本文档记录了通过 SSH 隧道访问 `test-db` 集群时，针对 LSports 和 TheSports 数据库的常用查询模式，以及本次 2026 年匹配任务中发现的关键数据特征。

---

## 1. 连接信息

| 参数 | 值 |
|------|-----|
| RDS Host | `test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com` |
| RDS Port | `3306` |
| User | `root` |
| Password | `r74pqyYtgdjlYB41jmWA` |
| SSH Host | `54.69.237.139` |
| SSH User | `ubuntu` |
| SSH Key | `~/skills/xp-bet-db-connector/templates/id_ed25519` |
| 本地隧道端口 | `3308`（主）/ `3309`（备） |

---

## 2. LSports 数据库（`test-xp-lsports`）

### 2.1 关键表结构

| 表名 | 说明 | 关键字段 |
|------|------|---------|
| `ls_sport_event` | 赛事主表 | `event_id`, `tournament_id`, `sport_id`, `scheduled`, `home_competitor_id`, `away_competitor_id`, `status` |
| `ls_tournament_en` | 联赛英文名 | `tournament_id`, `category_id`, `sport_id`, `name` |
| `ls_category_en` | 地区/国家 | `category_id`, `name`, `sport_id` |
| `ls_competitor_en` | 球队/球员英文名 | `competitor_id`(bigint), `name`, `sport_id` |

### 2.2 重要数据特征（避坑）

1. **`scheduled` 字段格式不统一**：同一表中存在 `2026-04-18T14:00:00` 和 `2026-04-18T14:00:00Z` 两种格式，需统一处理。
2. **`competitor_id` 类型不匹配**：`ls_competitor_en.competitor_id` 是 `bigint`（Python 返回 `int`），但 `ls_sport_event.home_competitor_id` 是 `varchar`（Python 返回 `str`）。构建字典时必须统一转 `str`：
   ```python
   competitors = {str(r[0]): r[1] for r in cur.fetchall()}
   ```
3. **`ls_category_en` JOIN 放大**：同一 `category_id` 对应多个 `sport_id`，直接 JOIN 会导致 `COUNT(*)` 被放大（约 10 倍）。应使用 `COUNT(DISTINCT event_id)` 或在 JOIN 条件中加 `AND cat.sport_id = e.sport_id`。
4. **2026 年数据范围**：LS 数据库中足球 2026 年约有 **10,474 场**比赛，篮球约 **1,234 场**，主要集中在 4-5 月。

### 2.3 常用查询

```sql
-- 查询 2026 年足球联赛列表（含比赛数）
SELECT e.tournament_id,
       COALESCE(t.name, '') AS tournament_name,
       COALESCE(cat.name, '') AS location,
       COUNT(DISTINCT e.event_id) AS event_count
FROM ls_sport_event e
LEFT JOIN ls_tournament_en t ON e.tournament_id = t.tournament_id
LEFT JOIN ls_category_en cat
       ON t.category_id = cat.category_id AND cat.sport_id = e.sport_id
WHERE e.sport_id = '6046'
  AND e.scheduled LIKE '2026%'
GROUP BY e.tournament_id, t.name, cat.name
ORDER BY event_count DESC;

-- 查询某联赛 2026 年比赛（含球队名称）
SELECT e.event_id, e.scheduled,
       e.home_competitor_id, hc.name AS home_name,
       e.away_competitor_id, ac.name AS away_name,
       e.status
FROM ls_sport_event e
LEFT JOIN ls_competitor_en hc ON e.home_competitor_id = hc.competitor_id
LEFT JOIN ls_competitor_en ac ON e.away_competitor_id = ac.competitor_id
WHERE e.tournament_id = '67'   -- Premier League
  AND e.scheduled LIKE '2026%'
ORDER BY e.scheduled;
```

### 2.4 Sport ID 对照

| 运动 | sport_id |
|------|---------|
| 足球 | `6046` |
| 篮球 | `48242` |

---

## 3. TheSports 数据库（`test-thesports-db`）

### 3.1 关键表结构

| 表名 | 说明 | 关键字段 |
|------|------|---------|
| `ts_fb_match` | 足球比赛 | `match_id`, `competition_id`, `home_team_id`, `away_team_id`, `match_time`(Unix 时间戳), `status_id` |
| `ts_fb_competition` | 足球联赛 | `competition_id`, `name`, `host_country` |
| `ts_fb_team` | 足球球队 | `team_id`, `name` |
| `ts_bb_match` | 篮球比赛 | `match_id`, `competition_id`, `home_team_id`, `away_team_id`, `match_time`(Unix 时间戳) |
| `ts_bb_competition` | 篮球联赛 | `competition_id`, `name`, `host_country` |
| `ts_bb_team` | 篮球球队 | `team_id`, `name` |

### 3.2 重要数据特征

1. **`match_time` 是 Unix 时间戳**（整数），不是字符串。
2. **2026 年时间戳范围**：`1767225600`（2026-01-01）到 `1798761600`（2027-01-01）。
3. **2026 年足球比赛量**：约 **91,118 场**（全年），月度分布均匀。
4. **2026 年篮球比赛量**：约 **数万场**，NBA 有 **765 场**（截至 2026-04-26）。
5. **`team_id` 类型**：`ts_fb_team.team_id` 返回 `str`，构建字典时直接用即可：
   ```python
   teams = {str(r[0]): r[1] for r in cur.fetchall()}
   ```

### 3.3 常用查询

```sql
-- 查询 2026 年足球联赛列表
SELECT competition_id, name, host_country
FROM ts_fb_competition
ORDER BY name;

-- 查询某联赛 2026 年比赛（含球队名称）
SELECT m.match_id,
       FROM_UNIXTIME(m.match_time) AS match_datetime,
       ht.name AS home_name,
       at.name AS away_name,
       m.status_id
FROM ts_fb_match m
LEFT JOIN ts_fb_team ht ON m.home_team_id = ht.team_id
LEFT JOIN ts_fb_team at ON m.away_team_id = at.team_id
WHERE m.competition_id = 'jednm9whz0ryox8'   -- English Premier League
  AND m.match_time >= 1767225600
  AND m.match_time < 1798761600
ORDER BY m.match_time;
```

---

## 4. 已知联赛 ID 映射（LS → TS）

以下映射经过人工验证，可直接用于 `KNOWN_LS_TS_MAP`：

| 运动 | LS 联赛名 | LS ID | TS 联赛名 | TS ID |
|------|----------|-------|----------|-------|
| 足球 | Premier League (England) | `67` | English Premier League | `jednm9whz0ryox8` |
| 足球 | LaLiga (Spain) | `8363` | Spanish La Liga | `vl7oqdehlyr510j` |
| 足球 | Ligue 1 (France) | `61` | French Ligue 1 | `yl5ergphnzr8k0o` |
| 足球 | Bundesliga (Germany) | `65` | Bundesliga | `gy0or5jhg6qwzv3` |
| 足球 | 2.Bundesliga (Germany) | `66` | German Bundesliga 2 | `kn54qllhjzqvy9d` |
| 足球 | UEFA Champions League | `32644` | UEFA Champions League | `z8yomo4h7wq0j6l` |
| 足球 | UEFA Europa League | `30444` | UEFA Europa League | `56ypq3nh0xmd7oj` |
| 篮球 | NBA | `64` | National Basketball Association | `49vjxm8xt4q6odg` |

---

## 5. 性能优化经验

### 5.1 批量预加载策略（推荐）

**不推荐**：每个联赛单独查询 TS 比赛（每次 JOIN 约 1.4 秒，76 个联赛 = 106 秒）。

**推荐**：一次性预加载所有 2026 年比赛，在内存中按 `competition_id` 分组：

```python
# 一次性加载所有 TS 足球比赛（约 4 秒）
cur.execute("""
    SELECT match_id, competition_id, home_team_id, away_team_id, match_time, status_id
    FROM ts_fb_match
    WHERE match_time >= 1767225600 AND match_time < 1798761600
    ORDER BY match_time
""")
rows = cur.fetchall()
# 按 competition_id 分组
by_comp = defaultdict(list)
for row in rows:
    by_comp[row[1]].append(row)
```

### 5.2 时间窗口索引

对于比赛匹配，使用 `bisect` 二分查找缩小候选集：

```python
ts_times = sorted([e['match_time'] for e in ts_events])
# 查找 ±24 小时内的候选比赛
lo = bisect.bisect_left(ts_times, ls_unix - 86400)
hi = bisect.bisect_right(ts_times, ls_unix + 86400)
candidates = ts_events[lo:hi]
```

---

## 6. 虚拟体育识别规则（待完善）

以下命名特征高度关联虚拟/电竞体育赛事，匹配时应特别标注：

- 联赛名以 `E-` 或 `E |` 开头（如 `E-Football | Battle (E)`）
- 联赛名包含 `Ebasketball`、`Efootball`（合写）
- 联赛名包含 `(E)` 后缀
- 联赛名包含 `eSports`、`Cyber`、`2K25`、`2K26`、`H2H GG`、`Blitz`
- 联赛名包含时间标注如 `8 Minutes`、`2x5 Minutes`、`4X5 Mins`

> 详见 `match_2026.py` 中的 `is_virtual_sport()` 函数（待实现）。
