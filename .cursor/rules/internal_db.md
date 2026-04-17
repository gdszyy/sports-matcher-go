---
description: "internal_db 模块的设计规范与核心逻辑说明"
globs: ["internal/db/**/*"]
---

# internal_db 模块规范

## 1. 模块职责

`internal/db` 包承担数据访问与标准化的双重职责，是算法层与数据库之间的完整隔离层。

| 文件 | 职责 |
|------|------|
| `models.go` | 源侧原始数据模型（SRTournament/SREvent/LSEvent 等）与 TS 目标侧模型（TSCompetition/TSEvent 等） |
| `canonical_models.go` | **新增**：规范化实体模型（CanonicalTournament/CanonicalEvent/CanonicalPlayer），消除数据源前缀，供算法层统一消费 |
| `normalizer.go` | **新增**：DataNormalizer 接口及 SR/LS/TS 三个实现，集中管理数据清洗逻辑（时间格式解析、运动类型映射、字段重命名等） |
| `data_router.go` | **新增**：DataRouter 路由层，根据 source 标识动态路由到对应 Fetcher+Normalizer，对外提供统一的 GetCanonicalEvents/GetCanonicalTournament 接口 |
| `sr_adapter.go` | SportRadar 数据库适配器（GetTournament/GetEvents/GetTeamNames/GetPlayersByTeam） |
| `ls_adapter.go` | LSports 数据库适配器（GetTournament/GetEvents/GetTeamNames），内含 parseLSScheduled 多格式时间解析 |
| `ts_adapter.go` | TheSports 数据库适配器，按 sport 动态切换 ts_fb_*/ts_bb_* 表 |
| `ls_player_adapter.go` | LS 球员数据适配器（数据库优先 + Snapshot API 兜底，支持批量查询） |
| `alias_store.go` | 持久化球队别名知识图谱（AliasStore，支持 Upsert/Lookup/PruneStale/LoadIntoIndex） |
| `tunnel.go` | SSH 隧道封装，连接 AWS RDS（跳板机 54.69.237.139） |

## 2. 核心数据模型 / API 接口

### 2.1 规范化实体层（Canonical Models）

算法层应优先消费 Canonical 实体，而非直接使用 SREvent/LSEvent 等带源前缀的模型。

```go
// CanonicalEvent — 统一比赛实体（消除 SR/LS 差异）
type CanonicalEvent struct {
    ID           string
    TournamentID string
    StartUnix    int64  // 统一 Unix 秒时间戳
    HomeID       string
    HomeName     string
    AwayID       string
    AwayName     string
    StatusCode   int
    Source       string // "sr" / "ls"
}
```

### 2.2 DataRouter 路由接口

```go
router := db.NewDataRouter()
router.RegisterSource("sr", db.NewSRFetcher(srAdapter), db.NewSRNormalizer())
router.RegisterSource("ls", db.NewLSFetcher(lsAdapter), db.NewLSNormalizer())

// 统一获取比赛数据，不关心来源
events, err := router.GetCanonicalEvents("ls", "8363")
```

### 2.3 数据库连接信息

| 参数 | 值 |
|------|-----|
| RDS Host | `test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com` |
| SSH Host | `54.69.237.139` |
| 本地隧道端口 | `3308`（主）/ `3309`（备） |

## 3. 状态流转 / 业务规则

### 3.1 数据流转路径

```
DB (AWS RDS via SSH Tunnel)
    ↓
Adapter (原始数据获取，含基础清洗)
    ↓
Normalizer (标准化，消除源差异)
    ↓
Canonical 实体 (统一形状)
    ↓
UniversalEngine / SourceAdapter (匹配算法)
```

### 3.2 清洗规则汇总

| 清洗项 | SR 处理 | LS 处理 | TS 处理 |
|--------|---------|---------|--------|
| 时间格式 | `parseISO8601Unix`（多种 ISO8601） | `parseLSScheduled`（支持无 Z 后缀） | 直接 Unix 时间戳 |
| 运动类型 | `sportFromID`（sr:sport:1 → football） | `lsSportName`（6046 → football） | 按表前缀（ts_fb_/ts_bb_） |
| 球队 ID 类型 | varchar | bigint→str（AGENTS.md 禁止混用） | varchar |
| 球员生日 | YYYY-MM-DD 字符串 | 无（Snapshot API 不提供） | Unix 时间戳字符串 |
| 去重 | 无 | event_id 去重（seen map） | name\|birthday 去重 |

### 3.3 全局禁止行为

1. **禁止直接 JOIN `ls_category_en`**：必须加 `AND cat.sport_id = e.sport_id`
2. **禁止混用 competitor_id 类型**：构建字典时必须统一转 `str`
3. **禁止绕过 SSH 隧道**：数据库不对公网开放

## 4. 详细设计文档索引

| 文档 | 说明 |
|------|------|
| [`docs/standardized_data_router_design.md`](../../docs/standardized_data_router_design.md) | **新增**：标准化数据路由与清洗层完整设计文档（Router + Normalizer + Canonical Models） |
| [`python/db/db_queries.md`](../../python/db/db_queries.md) | Python 侧 SQL 参考与已知联赛映射表 |

## 5. 变更日志

| 版本 | 日期 | 变更内容 |
|------|------|----------|
| v1.0 | 2026-04-17 | 初始规范文档 |
| v2.0 | 2026-04-17 | 新增标准化数据层：`canonical_models.go`（Canonical 实体）、`normalizer.go`（DataNormalizer 接口及 SR/LS/TS 实现）、`data_router.go`（DataRouter 路由层）；补充完整模块职责、数据流转路径、清洗规则汇总 |
