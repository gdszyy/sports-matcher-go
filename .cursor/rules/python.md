---
description: "python 模块的设计规范，包含批量匹配脚本、数据拉取工具和算法测试数据集"
globs: ["python/**/*", "direct_match_export.py"]
---

# python 模块规范

## 1. 模块职责

Python 模块提供数据分析和批量匹配脚本，作为 Go 主服务的补充工具，用于离线批量处理、数据导出和**算法测试**。

| 文件 | 职责 |
|------|------|
| `python/match_2026.py` | 2026 年批量匹配脚本（1054 行，24 个函数） |
| `python/db/connector.py` | Python 数据库连接工具（170 行） |
| `direct_match_export.py` | 直接匹配导出脚本（706 行，13 个函数） |
| `python/fetch_2026_data.py` | SR/LS/TS 2026 年数据拉取脚本（输出到 `python/data/`） |
| `python/build_sr_ts_ground_truth.py` | SR↔TS 比赛匹配 Ground Truth 生成脚本 |
| `python/explore_leagues.py` | 数据库联赛分布探查工具（辅助用途） |
| `python/data/` | **2026 年算法测试数据集**（SR/LS/TS 原始数据 + Ground Truth） |

## 2. 核心数据模型 / API

### 数据库连接（connector.py）

通过 SSH 隧道连接数据库，配置方式与 Go 服务一致（环境变量）。详见 [python/db/db_queries.md](../../python/db/db_queries.md)。

### 批量匹配脚本（match_2026.py）

- 支持足球和篮球的批量联赛匹配
- 输出结果到 Excel 文件（`output/` 目录）
- 函数级索引见 [auto_index](auto_index/python_match_2026_py_index.md)

### 算法测试数据集（python/data/）

`python/data/` 目录存放从 SR / LS / TS 拉取的 2026 年赛事数据及 SR↔TS 匹配标注，是**离线算法评估的唯一标准数据源**：

- `sr_ts_ground_truth.json` — 核心：2,858 条 SR↔TS 完整匹配记录（平均置信分 0.9757）
- `sr_events_2026.json` / `ts_events_2026.json` / `ls_events_2026.json` — 三数据源原始比赛数据

完整字段说明见 [python_data.md](python_data.md)。

## 3. 状态流转 / 业务规则

- Python 脚本仅用于离线批量处理，不参与在线服务
- 批量匹配输出文件统一写入 `output/` 目录（该目录为归档区，不纳入主索引）
- 数据库连接必须通过 `python/db/connector.py` 建立，禁止直接硬编码连接串
- **算法测试脚本必须从 `python/data/` 读取数据**，禁止在测试时直连数据库（保证可复现性）
- `fetch_2026_data.py` 和 `build_sr_ts_ground_truth.py` 使用直连模式（pymysql），不依赖 `connector.py`，仅在需要更新数据集时运行

## 4. 详细设计文档索引

- [Python DB 查询文档](../../python/db/db_queries.md)
- [Python README](../../python/README.md)
- [**算法测试数据集规范**](python_data.md)（`python/data/` 目录完整说明）
- [数据集 README](../../python/data/README.md)
