# Python 工具目录

本目录包含 sports-matcher-go 项目的 Python 工具脚本，主要用于数据库查询、匹配任务执行和结果导出。

## 目录结构

```
python/
├── db/
│   ├── connector.py     # SSH 隧道 + 数据库连接封装
│   ├── db_queries.md    # 常用 SQL 查询参考 + 数据特征说明
│   └── __init__.py
├── match_2026.py        # 2026 年 LSports→TheSports 匹配主脚本
└── README.md
```

## 快速开始

### 环境依赖

```bash
sudo pip3 install pymysql openpyxl
```

### 运行 2026 年匹配任务

```bash
cd /path/to/sports-matcher-go

# 确保 SSH 隧道已建立（脚本会自动建立）
python3 python/match_2026.py
```

输出文件：`lsports_ts_match_2026.xlsx`

### 仅测试数据库连接

```bash
python3 python/db/connector.py
```

## 核心模块说明

### `db/connector.py`

封装了 SSH 隧道建立和数据库连接，主要接口：

| 函数/常量 | 说明 |
|----------|------|
| `setup_tunnel(local_port=3308)` | 建立 SSH 隧道，返回子进程 |
| `get_conn(database, local_port=3308)` | 获取 pymysql 连接 |
| `LS_DB` | LSports 数据库名 `test-xp-lsports` |
| `TS_DB` | TheSports 数据库名 `test-thesports-db` |

### `match_2026.py`

2026 年比赛匹配主脚本，功能：
- 加载 LSports 和 TheSports 的 2026 年足球/篮球联赛和比赛
- 执行联赛名称相似度匹配（含已知映射 KNOWN_LS_TS_MAP）
- 执行比赛时间窗口 + 球队名称相似度匹配
- 导出 Excel 报表（第一个 sheet 为联赛统计，后续每联赛一个 sheet）

## 数据库连接信息

详见 `db/db_queries.md`，包含：
- RDS 连接参数
- SSH 跳板机信息
- 关键表结构
- 数据特征和避坑指南
- 已知联赛 ID 映射表
- 性能优化经验
