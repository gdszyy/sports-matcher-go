---
description: "sports-matcher-go 系统的全局架构概述、核心工作流串联及全局禁止行为清单"
globs: ["README.md", "docs/**/*.md", "python/**/*.py", "internal/**/*.go"]
---

# 全局架构规范 (Global Architecture)

## 1. 架构概述

**sports-matcher-go** 是 XP-BET 平台的体育赛事数据匹配服务，负责将 **LSports（左）** 的联赛/比赛数据与 **TheSports（右）** 进行 ID 映射，为下游赔率系统提供统一赛事标识。

```
LSports DB (test-xp-lsports)      TheSports DB (test-thesports-db)
           └──────── SSH 隧道 (54.69.237.139) ────────┘
                        本地端口 3308
                              │
              ┌───────────────┴───────────────┐
              │        sports-matcher-go       │
              │  Go: internal/db + matcher     │  ← 主服务
              │  Python: python/db + match_*   │  ← 批量脚本
              └───────────────────────────────┘
                              │
                      Excel / API 响应
```

### 数据库连接信息

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

### Sport ID 对照

| 运动 | LS sport_id | TS 表前缀 |
|------|------------|----------|
| 足球 | `6046` | `ts_fb_*` |
| 篮球 | `48242` | `ts_bb_*` |

## 2. 核心工作流串联

### 联赛匹配流程
1. 优先查 `KNOWN_LS_TS_MAP`（硬编码已验证映射）
2. 回退：名称相似度（Jaccard + SequenceMatcher）+ 地理过滤
3. 阈值：`NAME_HI`（≥0.85）、`NAME_MED`（≥0.70）、`NAME_LOW`（≥0.55）

### 比赛匹配流程
1. 一次性预加载所有 TS 2026 年比赛（约 4 秒），内存中按 competition_id 分组
2. 对每场 LS 比赛，用 `bisect` 二分查找 ±24h 时间窗口内的候选比赛
3. 对候选比赛计算主客队名称相似度，取最高分且 ≥0.7 的作为匹配结果

### 已知联赛 ID 映射（经验证，勿随意修改）

| LS 联赛名 | LS ID | TS 联赛名 | TS ID |
|----------|-------|----------|-------|
| Premier League | `67` | English Premier League | `jednm9whz0ryox8` |
| LaLiga | `8363` | Spanish La Liga | `vl7oqdehlyr510j` |
| Ligue 1 | `61` | French Ligue 1 | `yl5ergphnzr8k0o` |
| Bundesliga (Germany) | `65` | Bundesliga | `gy0or5jhg6qwzv3` |
| 2.Bundesliga | `66` | German Bundesliga 2 | `kn54qllhjzqvy9d` |
| UEFA Champions League | `32644` | UEFA Champions League | `z8yomo4h7wq0j6l` |
| UEFA Europa League | `30444` | UEFA Europa League | `56ypq3nh0xmd7oj` |
| NBA | `64` | National Basketball Association | `49vjxm8xt4q6odg` |

> ⚠️ **陷阱**：LS Bundesliga ID=56 是奥地利联赛，ID=65 才是德国联赛！

### 关键数据特征（避坑）

1. **`scheduled` 格式不统一**：同表中存在 `2026-04-18T14:00:00` 和 `...Z` 两种格式
2. **`competitor_id` 类型不匹配**：`ls_competitor_en.competitor_id` 是 `bigint`（Python int），`ls_sport_event.home_competitor_id` 是 `varchar`（Python str）→ 构建字典必须 `{str(r[0]): r[1]}`
3. **`ls_category_en` JOIN 放大**：同一 `category_id` 对应多个 `sport_id`，JOIN 必须加 `AND cat.sport_id = e.sport_id`
4. **TS `match_time` 是 Unix 时间戳**：2026 年范围 = `[1767225600, 1798761600)`
5. **虚拟体育识别**：联赛名以 `E-`、`E |` 开头，或含 `(E)`、`eSports`、`Cyber`、`2K`、`Blitz`、`H2H GG` 等关键词

## 3. 全局禁止行为清单

1. **禁止提交 SSH 私钥**：私钥在 `~/skills/xp-bet-db-connector/templates/id_ed25519`，绝不提交 git
2. **禁止绕过 SSH 隧道**：数据库不对公网开放，必须通过跳板机 `54.69.237.139`
3. **禁止直接 JOIN `ls_category_en`**：必须加 `AND cat.sport_id = e.sport_id` 或用 `COUNT(DISTINCT event_id)`
4. **禁止混用 competitor_id 类型**：构建字典时必须统一转 `str`
5. **禁止修改 `KNOWN_LS_TS_MAP` 而不同步更新 `python/db/db_queries.md`**
6. **禁止破坏"代码-文档同步"契约**：修改核心逻辑时必须同步更新 `.cursor/rules/` 对应文档

