# sports-matcher-go 全局协作规范 (AGENTS.md)

本文档是 `sports-matcher-go` 仓库中所有 AI Agent 必须遵守的协作规范和工作指南。

> **Agent 进入本仓库时，必须首先阅读本文档，再按需加载对应模块规范。**

---

## 1. 项目概览

**sports-matcher-go** 是 XP-BET 平台的体育赛事数据匹配服务，负责将 **LSports（左）** 的联赛/比赛数据与 **TheSports（右）** 进行双向 ID 映射，为下游赔率系统提供统一的赛事标识。

| 维度 | 说明 |
|------|------|
| 语言 | Go（主服务）+ Python（数据分析/批量匹配脚本） |
| 数据源 | LSports（`test-xp-lsports`）、TheSports（`test-thesports-db`） |
| 数据库访问 | 通过 SSH 隧道连接 AWS RDS（`test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com`） |
| 当前覆盖运动 | 足球（sport_id=6046）、篮球（sport_id=48242） |

---

## 2. 快速导航

| 需求 | 阅读文档 |
|------|---------|
| 数据库连接方法 | [`python/db/connector.py`](python/db/connector.py) + [`python/db/db_queries.md`](python/db/db_queries.md) |
| 2026年批量匹配脚本 | [`python/match_2026.py`](python/match_2026.py) |
| Go 匹配核心逻辑 | [`internal/matcher/ls_engine.go`](internal/matcher/ls_engine.go) |
| Go 数据库适配器 | [`internal/db/ls_adapter.go`](internal/db/ls_adapter.go)、[`internal/db/ts_adapter.go`](internal/db/ts_adapter.go) |
| 全局架构与防坑 | [`.cursor/rules/global.md`](.cursor/rules/global.md) |
| Python 数据库规范 | [`.cursor/rules/python_db.md`](.cursor/rules/python_db.md) |
| Python 匹配脚本规范 | [`.cursor/rules/python.md`](.cursor/rules/python.md) |
| 匹配评估文档 | [`docs/ls_ts_matching_assessment.md`](docs/ls_ts_matching_assessment.md) |
| 联赛评价规则 | [`docs/league_match_evaluation_rule.md`](docs/league_match_evaluation_rule.md) |
| 强校验关键词词典 | [`docs/league_guard_keywords.json`](docs/league_guard_keywords.json) |
| 通用算法设计规划 | [`docs/universal_matching_algorithm_design.md`](docs/universal_matching_algorithm_design.md) |
| 优化 TODO 与防坑指南 | [`.cursor/rules/process_insights/PI-001_universal_matching_algorithm_design.md`](.cursor/rules/process_insights/PI-001_universal_matching_algorithm_design.md) |

---

## 3. 全局 AI 编辑策略

### 智能编辑决策树

1. **微型修改（< 20 行）**：使用搜索替换或行内编辑
2. **中型修改（20–200 行）**：局部重写或追加
3. **大型修改（> 200 行）**：全文件重写（仅适用于较小文件）

### 强制要求

- **活文档契约**：修改核心逻辑时，**必须同步更新** `.cursor/rules/` 下的对应规范文档
- **已知映射表维护**：每次验证新的联赛 ID 映射后，必须同步更新 `python/match_2026.py` 中的 `KNOWN_LS_TS_MAP` 和 `python/db/db_queries.md` 中的映射表

---

## 4. 上下文防污染策略

> **⚠️ 归档区：默认不读取**

| 目录 | 说明 |
|------|------|
| `output/` | 历史匹配结果 Excel，仅供追溯 |
| `lsports_ts_match_result_football.xlsx` | 旧版匹配结果，已被 `python/match_2026.py` 取代 |

---

## 5. 子模块规范文档索引

| 模块 | 规范文档 | 职责 |
|------|---------|------|
| 全局架构 | [`.cursor/rules/global.md`](.cursor/rules/global.md) | 系统全景、核心工作流、禁止行为 |
| Go 入口 | [`.cursor/rules/cmd.md`](.cursor/rules/cmd.md) | HTTP 服务入口、启动配置 |
| Go 数据库层 | [`.cursor/rules/internal_db.md`](.cursor/rules/internal_db.md) | SSH 隧道、LS/TS/SR 适配器 |
| Go 匹配引擎 | [`.cursor/rules/internal_matcher.md`](.cursor/rules/internal_matcher.md) | 联赛/比赛/球队名称匹配算法 |
| Go API 层 | [`.cursor/rules/internal_api.md`](.cursor/rules/internal_api.md) | HTTP 接口定义 |
| Python 数据库 | [`.cursor/rules/python_db.md`](.cursor/rules/python_db.md) | SSH 隧道封装、连接配置、SQL 参考 |
| Python 匹配脚本 | [`.cursor/rules/python.md`](.cursor/rules/python.md) | 批量匹配脚本、Excel 导出 |
| 流程洞察索引 | [`.cursor/rules/process_insights/index.md`](.cursor/rules/process_insights/index.md) | 所有流程洞察文档的注册表 |

---

## 6. 全局禁止行为

1. **禁止提交私钥**：SSH 私钥（`id_ed25519`）存放在 `~/skills/xp-bet-db-connector/templates/`，**绝对不能提交到 git**
2. **禁止硬编码密码**：数据库密码已在 `python/db/connector.py` 和 `python/db/db_queries.md` 中集中管理，其他文件引用时应 import 常量，不得重复硬编码
3. **禁止跳过 SSH 隧道**：数据库不对公网直接开放，必须通过 SSH 隧道（跳板机 `54.69.237.139`）访问
4. **禁止直接 JOIN `ls_category_en`**：该表存在 `category_id` 对应多个 `sport_id` 的情况，直接 JOIN 会导致 COUNT 被放大，必须加 `AND cat.sport_id = e.sport_id` 条件或使用 `COUNT(DISTINCT event_id)`
5. **禁止混用 competitor_id 类型**：`ls_competitor_en.competitor_id` 是 `bigint`，`ls_sport_event.home_competitor_id` 是 `varchar`，构建字典时必须统一转 `str`
