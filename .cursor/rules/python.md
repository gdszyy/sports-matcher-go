---
description: "python 模块的设计规范，包含批量匹配脚本和数据库连接工具"
globs: ["python/**/*", "direct_match_export.py"]
---

# python 模块规范

## 1. 模块职责

Python 模块提供数据分析和批量匹配脚本，作为 Go 主服务的补充工具，用于离线批量处理和数据导出。

| 文件 | 职责 |
|------|------|
| `python/match_2026.py` | 2026 年批量匹配脚本（1054 行，24 个函数） |
| `python/db/connector.py` | Python 数据库连接工具（170 行） |
| `direct_match_export.py` | 直接匹配导出脚本（706 行，13 个函数） |

## 2. 核心数据模型 / API

### 数据库连接（connector.py）

通过 SSH 隧道连接数据库，配置方式与 Go 服务一致（环境变量）。详见 [python/db/db_queries.md](../../python/db/db_queries.md)。

### 批量匹配脚本（match_2026.py）

- 支持足球和篮球的批量联赛匹配
- 输出结果到 Excel 文件（`output/` 目录）
- 函数级索引见 [auto_index](auto_index/python_match_2026_py_index.md)

## 3. 状态流转 / 业务规则

- Python 脚本仅用于离线批量处理，不参与在线服务
- 输出文件统一写入 `output/` 目录（该目录为归档区，不纳入主索引）
- 数据库连接必须通过 `python/db/connector.py` 建立，禁止直接硬编码连接串

## 4. 详细设计文档索引

- [Python DB 查询文档](../../python/db/db_queries.md)
- [Python README](../../python/README.md)
