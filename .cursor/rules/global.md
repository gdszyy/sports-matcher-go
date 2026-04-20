---
description: "sports-matcher-go 系统的全局架构概述、核心工作流串联及全局禁止行为清单"
globs: ["README.md", "docs/**/*.md"]
---

# 全局架构规范 (Global Architecture)

## 1. 架构概述

**sports-matcher-go** 是 XP-BET 平台的跨库体育赛事数据匹配服务，负责将 **SportRadar（SR）** 的联赛/比赛/球队/球员数据单向匹配到 **TheSports（TS）**，为下游赔率系统提供统一的赛事标识。

| 维度 | 说明 |
|------|------|
| 语言 | Go（主服务）+ Python（数据分析/批量匹配脚本） |
| 数据源 | SportRadar（`xp-bet-test`）、TheSports（`test-thesports-db`） |
| 数据库访问 | 通过 SSH 隧道连接 AWS RDS（`test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com`） |
| 当前覆盖运动 | 足球（sport_id=6046）、篮球（sport_id=48242） |
| HTTP 框架 | Gin |

### 目录结构

```
cmd/server/main.go          CLI 入口（serve / match 命令）
internal/
  config/config.go          配置读取（环境变量 / 默认值）
  db/
    tunnel.go               SSH 隧道 + MySQL 连接池管理
    models.go               数据模型（SR/TS 联赛、比赛、球队、球员）
    sr_adapter.go           SR 数据库查询适配器
    ts_adapter.go           TS 数据库查询适配器
    ls_adapter.go           LSports 数据库查询适配器
    alias_store.go          球队别名存储
    league_alias_store.go   联赛别名存储
    data_router.go          标准化数据路由
  matcher/
    result.go               匹配结果数据结构和规则常量
    name.go                 名称归一化（变音符/先后名/中间名/Unicode）
    league.go               联赛匹配（已知映射表 + 名称相似度 + 全局占用机制）
    league_alias.go         联赛别名匹配逻辑
    league_features.go      联赛特征提取
    event.go                比赛匹配（五级降级规则 L1/L2/L3/L4/L4b + TeamAliasIndex）
    event_dtw.go            DTW 时间序列比赛匹配
    team_player.go          球队映射推导 + 球员匹配（多格式名称）
    engine.go               主流程编排（两轮迭代 + 自底向上校验）
    universal_engine.go     通用匹配引擎（LSports ↔ TheSports）
    ls_engine.go            LSports 专用匹配引擎
    dense_blocking.go       密集候选块生成
    fs_model.go             特征评分模型
  api/server.go             HTTP API 服务（Gin）
python/
  match_2026.py             2026 年批量匹配脚本
  db/connector.py           Python 数据库连接工具
```

## 2. 核心工作流串联

### 五级匹配规则（v3）

| 级别 | 触发条件 | 时间窗口 | 名称阈值 | 最低置信度 | 特殊约束 |
|:----:|:--------|:-------:|:-------:|:---------:|:--------|
| **L1** | 精确时间 | ≤ 5 min | 0.40 | 0.50 | — |
| **L2** | 宽松时间 | ≤ 6 h | 0.65 | 0.60 | — |
| **L3** | 同一 UTC 日期 | ≤ 24 h（同日） | 0.75 | 0.70 | — |
| **L4** | 超宽时间 + 别名强匹配 | ≤ 72 h | 0.85 | 0.80 | `require_alias=true` |
| **L4b** | 球队 ID 精确对兜底 | 无限制 | — | 0.75 | 需第一轮推导的 `teamIDMap` |

### 两轮迭代流程

```
第一轮：MatchEvents(teamIDMap=nil)
        → L1 / L2 / L3 / L4（TeamAliasIndex 内部驱动）
        → DeriveTeamMappings → teamIDMap

第二轮：MatchEvents(teamIDMap=<第一轮推导>)
        → L4b 球队 ID 精确对兜底
        → DeriveTeamMappings（最终）
```

### HTTP API 入口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/api/v1/match/league` | GET | 单联赛匹配 |
| `/api/v1/match/batch` | POST | 批量联赛匹配 |

## 3. 全局禁止行为清单

为保障项目架构的纯净性和系统稳定性，特制定以下禁止行为清单：

1.  **禁止破坏"代码-文档同步"契约**：在修改任何架构设计、API 或核心逻辑时，必须同步更新 `.cursor/rules/` 下对应的规则文档。
2.  **禁止硬编码凭证信息**：在所有文档示例和代码中，严禁出现真实的 API Key、Token、密码或数据库连接串，必须使用占位符代替。
3.  **禁止跳过流程洞察沉淀**：在任务中发现隐蔽逻辑或耦合陷阱时，必须在 `.cursor/rules/process_insights/` 中记录对应洞察。
4.  **禁止手动编辑自动索引**：`.cursor/rules/auto_index/` 目录下的文件由 `code-indexer` 脚本自动生成，严禁手动编辑。
5.  **巨型函数必须标记内部节点**：当函数超过 200 行时，必须在内部按业务逻辑块添加 `// @section:{snake_case_name} - {一句话说明}` 标记，以便索引器提取内部节点。
6.  **禁止修改五级匹配阈值而不更新文档**：L1–L4b 的时间窗口和置信度阈值是经过验证的核心参数，任何调整必须同步更新 `README.md` 和 `global.md`。
7.  **禁止绕过 SSH 隧道直连数据库**：所有数据库连接必须通过 `internal/db/tunnel.go` 建立的 SSH 隧道，严禁直接配置外网数据库连接。
