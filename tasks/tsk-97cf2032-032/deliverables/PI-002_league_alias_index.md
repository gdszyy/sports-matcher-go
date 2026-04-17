---
id: "PI-002"
version: "v1.0"
last_updated: "2026-04-17"
author: "Manus AI"
related_modules: ["internal/matcher/league_alias.go", "internal/db/league_alias_store.go", "internal/matcher/league.go", "internal/matcher/ls_engine.go"]
status: "active"
---

# PI-002: 联赛别名索引 — 官方名称与常用名称差异处理

## 流程概述

本洞察记录了针对 **English Football League One** 等联赛官方名称与常用名称差异较大问题的完整解决方案。当 SR 数据源使用官方名称（如 `EFL League One`）而 TS 数据源使用常用名称（如 `League One`）时，纯字符串相似度计算会产生较低的匹配分数，导致联赛匹配失败。

---

## 核心问题

### 问题现象

| SR 官方名称 | TS 常用名称 | 原始相似度 | 别名索引后相似度 |
|:-----------|:-----------|:---------:|:--------------:|
| `EFL League One` | `League One` | ~0.57 | **0.98** |
| `EFL Championship` | `Championship` | ~0.60 | **0.98** |
| `Carabao Cup` | `EFL Cup` | ~0.40 | **0.98** |
| `FA Premier League` | `Premier League` | ~0.72 | **0.98** |
| `UEFA Champions League` | `Champions League` | ~0.75 | **0.98** |

### 根因分析

1. **赞助商/机构前缀差异**：`EFL`（English Football League）前缀在 SR 中常出现，TS 中省略
2. **官方全称 vs 简称**：`English Football League One` vs `League One`
3. **赞助商冠名变更**：`Carabao Cup`、`Capital One Cup`、`Carling Cup` 均指同一联赛（EFL Cup）
4. **缩写差异**：`UCL` vs `UEFA Champions League`；`EPL` vs `Premier League`

---

## 解决方案

### 架构设计

```
┌─────────────────────────────────────────────────────────────────┐
│                    联赛别名索引系统                                │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  LeagueAliasIndex（内存索引）                             │   │
│  │  - 静态别名词典（内置 145+ 条，覆盖主流联赛）              │   │
│  │  - 运行时动态扩展（RegisterAlias/RegisterGroup）          │   │
│  │  - 双向索引：别名 → 规范名                                │   │
│  └─────────────────────────────────────────────────────────┘   │
│                            ↑ 启动时加载                          │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  LeagueAliasStore（持久化存储）                           │   │
│  │  - 数据库表 league_alias_knowledge                       │   │
│  │  - 支持 manual（人工录入）/ sr / ls（自动学习）三种来源   │   │
│  │  - manual 来源优先级最高，不被自动学习覆盖                │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  leagueNameSimilarityWithAlias（别名感知相似度函数）       │   │
│  │  1. 计算原始名称相似度（基线）                            │   │
│  │  2. 查找两侧名称的规范名称                                │   │
│  │  3. 若映射到同一规范名 → 直接返回 0.98                   │   │
│  │  4. 展开别名后计算相似度                                  │   │
│  │  5. 遍历所有别名计算交叉相似度                            │   │
│  │  6. 取所有相似度的最大值                                  │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### 文件清单

| 文件 | 说明 |
|:-----|:-----|
| `internal/matcher/league_alias.go` | 核心实现：静态别名词典、`LeagueAliasIndex`、`leagueNameSimilarityWithAlias` |
| `internal/db/league_alias_store.go` | 持久化存储：`LeagueAliasStore`，自动建表，支持 manual/sr/ls 三种来源 |
| `internal/matcher/league_alias_test.go` | 单元测试：静态查找、同组规范名、动态注册、别名感知相似度 |
| `internal/matcher/league.go` | 修改：`leagueNameScore` 改用 `leagueNameSimilarityWithAlias`；补充英格兰联赛已知映射 |
| `internal/matcher/ls_engine.go` | 修改：`lsLeagueNameScore` 改用 `leagueNameSimilarityWithAlias` |

---

## 静态别名词典覆盖范围

### 英格兰足球联赛体系（重点覆盖）

| 规范名称 | 覆盖的别名 |
|:--------|:---------|
| `EFL League One` | `League One`、`English Football League One`、`Football League One`、`League 1` |
| `EFL Championship` | `Championship`、`The Championship`、`English Championship` |
| `EFL League Two` | `League Two`、`Football League Two`、`League 2` |
| `EFL Cup` | `League Cup`、`Carabao Cup`、`Capital One Cup`、`Carling Cup` |
| `FA Cup` | `FA Challenge Cup`、`Football Association Challenge Cup` |

### 其他主要联赛

| 国家/赛事 | 覆盖的别名 |
|:---------|:---------|
| 德国 | `1. Bundesliga`、`German Bundesliga`；`2. Bundesliga`、`Bundesliga 2` |
| 西班牙 | `La Liga`、`Primera Division`、`LaLiga Santander` |
| 意大利 | `Serie A TIM`、`Lega Serie A` |
| 法国 | `Ligue 1 Uber Eats`、`Division 1 France` |
| UEFA | `Champions League`、`UCL`；`Europa League`、`UEL`；`Conference League`、`UECL` |
| 美国 | `Major League Soccer`、`American MLS` |

---

## 集成方式

### SR 链路（league.go）

```go
// 修改前
base := nameSimilarityMax(sr.Name, ts.Name)

// 修改后（PI-002）
base := leagueNameSimilarityWithAlias(sr.Name, ts.Name)
```

### LS 链路（ls_engine.go）

```go
// 修改前
base := nameSimilarityMax(ls.Name, ts.Name)

// 修改后（PI-002）
base := leagueNameSimilarityWithAlias(ls.Name, ts.Name)
```

### 持久化存储集成（可选，用于运行时学习）

```go
// 初始化
store, err := db.NewLeagueAliasStore(sqlDB)

// 人工录入高优先级别名
store.UpsertManual("EFL League One", "League One England", "football")

// 自动学习（联赛匹配成功后）
store.UpsertAlias("sr", "EFL League One", srLeagueName, "football", confidence)

// 注入内存索引
store.LoadIntoIndex(matcher.GetLeagueAliasIndex())
```

---

## 防坑指南

### 坑 1: 别名展开与强约束一票否决的顺序

**现象**：别名展开后，两侧名称可能产生新的特征冲突（如展开后一侧变成 cup 类型，另一侧是 league 类型）。

**正确做法**：`leagueNameSimilarityWithAlias` 只负责计算相似度分数，强约束一票否决在外层（`leagueNameScore`/`lsLeagueNameScore`）中执行。两者顺序不可颠倒。

**关键位置**：`league.go` → `leagueNameScore`；`ls_engine.go` → `lsLeagueNameScore`

### 坑 2: 别名词典与层级数字强约束冲突

**现象**：`2. Bundesliga` 和 `Bundesliga` 在别名词典中属于不同组（层级不同），但若错误地将两者放入同一组，会导致层级数字强约束失效。

**正确做法**：别名词典中每个层级的联赛必须单独成组。`Bundesliga`（一级）和 `2. Bundesliga`（二级）是不同的规范名称组。

**关键位置**：`league_alias.go` → `staticLeagueAliasGroups`

### 坑 3: 赞助商冠名变更导致别名过时

**现象**：`Carabao Cup` 的赞助商可能变更，导致别名词典中的名称过时（如历史上的 `Capital One Cup`、`Carling Cup`）。

**正确做法**：静态别名词典保留历史赞助商名称（用于匹配历史数据），新赞助商名称通过 `LeagueAliasStore.UpsertManual` 动态添加，无需修改代码。

**关键位置**：`league_alias.go` → `staticLeagueAliasGroups`（EFL Cup 组）

### 坑 4: 全局单例索引的线程安全

**现象**：`GetLeagueAliasIndex()` 返回全局单例，若在多个 goroutine 中同时调用 `RegisterAlias`，可能产生竞态。

**正确做法**：`LeagueAliasIndex` 内部使用 `sync.RWMutex` 保护所有读写操作，已线程安全。但注意 `globalLeagueAliasOnce` 仅保证初始化一次，测试中若需要独立索引，应直接创建 `&LeagueAliasIndex{...}` 而非使用全局单例。

---

## 预期效果

| 场景 | 优化前 | 优化后 |
|:-----|:------|:------|
| `EFL League One` vs `League One` | 相似度 ~0.57，匹配失败 | 相似度 0.98，直接命中 |
| `Carabao Cup` vs `EFL Cup` | 相似度 ~0.40，匹配失败 | 相似度 0.98，直接命中 |
| `Champions League` vs `UEFA Champions League` | 相似度 ~0.75，低置信度 | 相似度 0.98，高置信度 |
| 英格兰联赛体系整体匹配率 | 估计 ~60-70% | 预期 ≥ 90% |

---

## 版本变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|---------|------|
| v1.0 | 2026-04-17 | 初始实现：静态别名词典（145条）、`LeagueAliasIndex`、`LeagueAliasStore`、`leagueNameSimilarityWithAlias`；集成到 SR/LS 两条链路；补充英格兰联赛已知映射 | Manus AI |
