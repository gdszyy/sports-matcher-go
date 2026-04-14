---
description: "Python 数据库连接模块规范：SSH 隧道封装、连接配置、常用 SQL 查询参考"
globs: ["python/db/**/*.py", "python/db/**/*.md"]
---

# Python 数据库模块规范 (python_db)

## 1. 模块职责

`python/db/` 目录封装了通过 SSH 隧道访问 XP-BET 测试数据库集群的所有底层逻辑，是所有 Python 脚本的唯一数据库访问入口。

## 2. 核心文件

| 文件 | 职责 |
|------|------|
| `connector.py` | SSH 隧道建立 + pymysql 连接封装，提供 `setup_tunnel()` / `get_conn()` 接口 |
| `db_queries.md` | 常用 SQL 查询参考 + 数据特征说明 + 已知联赛 ID 映射表 |

## 3. 标准使用方式

```python
from python.db.connector import setup_tunnel, get_conn, LS_DB, TS_DB

proc = setup_tunnel()
try:
    conn_ls = get_conn(LS_DB)
    conn_ts = get_conn(TS_DB)
    # ... 执行查询 ...
    conn_ls.close()
    conn_ts.close()
finally:
    proc.terminate()
```

## 4. 连接配置（禁止在其他文件重复硬编码）

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
| `LS_DB` | `test-xp-lsports` |
| `TS_DB` | `test-thesports-db` |

## 5. 关键数据特征（避坑）

- `ls_competitor_en.competitor_id` 是 **bigint**（Python int），`ls_sport_event.home_competitor_id` 是 **varchar**（Python str）→ 必须 `{str(r[0]): r[1]}`
- `ls_category_en` JOIN 必须加 `AND cat.sport_id = e.sport_id`，否则 COUNT 放大 ~10 倍
- TS `match_time` 是 Unix 时间戳，2026 年范围 = `[1767225600, 1798761600)`

## 6. 详细设计文档索引

- 完整 SQL 参考：[`python/db/db_queries.md`](../../python/db/db_queries.md)
- 连接封装实现：[`python/db/connector.py`](../../python/db/connector.py)
