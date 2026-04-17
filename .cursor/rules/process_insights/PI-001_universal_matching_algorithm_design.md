---
id: "PI-001"
version: "v1.3"
last_updated: "2026-04-17"
author: "Manus AI"
related_modules: ["internal/matcher", "internal/db", "docs"]
status: "active"
---

# PI-001: 通用匹配算法设计规划与优化 TODO

## 流程概述

本洞察记录了将 `sports-matcher-go` 中 SR↔TS 和 LS↔TS 两条独立匹配链路统一为一套**通用匹配算法引擎**的完整设计规划。当前联赛匹配准确率仅为 44.44%（`docs/league_match_evaluation_rule.md`），核心问题是联赛名称泛化误吸附、比赛时间容差僵化、球队/球员名称差异处理不足。本规划基于"层次化集体实体解析"理念，通过球员→球队→比赛→联赛的四层自下而上置信度传导，结合强约束一票否决和模糊时间窗口，系统性地解决上述问题。

---

## 核心防坑指南

### 坑 1: 联赛名称泛化导致跨国/跨赛事体系误吸附

**现象**：`The Championship (England)` 被匹配到 `ASEAN Championship`；`FA Cup (China)` 被匹配到 `FANS Cup`；`BudnesLiga LFL 5x5 (International)` 被匹配到 `Bundesliga`。

**根因**：`lsLeagueNameScore` 函数仅做整体字符串的 Jaccard 相似度 + 国家加权，缺乏对赛制类型（5x5 vs 标准）、层级（Liga 3 vs 4 Liga）、性别（Women vs Men）、年龄段（U19 vs 成年队）等维度的强约束拦截。高频泛化词（League、Cup、Championship）在 Jaccard 计算中贡献了虚假的高相似度。

**正确做法**：必须在联赛匹配阶段引入**结构化特征解析 + 六维强约束一票否决**。将联赛名称拆解为 `{国家} + {赛事体系} + {层级} + {性别} + {年龄段} + {区域分区} + {赛制类型}` 的结构化特征向量，任何强约束维度的冲突直接否决候选，无论名称相似度多高。强约束关键词维护在 `docs/league_guard_keywords.json`。

**关键位置**：`internal/matcher/ls_engine.go` → `lsLeagueNameScore` / `matchLSLeague`；`internal/matcher/league.go` → `leagueNameScore`

### 坑 2: 硬性时间分级导致赛程延期/时区错误的比赛漏匹配

**现象**：SR/TS 时间戳来源不同（UTC vs 本地时区、赛程延期），导致同一场比赛的时差超过 24h，在 L3 阶段被遗漏。虽然 L4（72h）可以兜底，但 L4 要求 `requireAlias=true`，新联赛首次匹配时别名索引为空，无法激活。

**根因**：`levelConfigs` 使用固定阈值（L1=300s, L2=21600s, L3=同日, L4=259200s），每个级别内部使用线性衰减。这种硬性分级在级别边界处产生不连续的断崖效应，且无法适应极端时间偏移场景。

**正确做法**：将硬性时间分级替换为**基于高斯衰减的连续模糊时间窗口**：$S_{time}(\Delta t) = \exp(-\Delta t^2 / 2\sigma^2)$。标准匹配 σ=3600s，宽松匹配 σ=10800s，超宽容差 σ=43200s。对于极端偏移（>72h），可选择性下钻至事件流数据，应用 EventDTW 对齐锚点事件。

**关键位置**：`internal/matcher/event.go` → `levelConfigs` / `tryMatchLevel` 约第 150-360 行

### 坑 3: LS 侧缺失球员层导致自底向上传导链条断裂

**现象**：SR↔TS 链路有完整的球员匹配和 `ApplyBottomUp` 自底向上校验，但 LS↔TS 链路完全缺失球员层。这意味着 LS 侧的球队匹配只能依赖投票法和名称相似度，无法获得球员重叠率的置信度加成。

**根因**：LSports 的 TRADE 数据流中球员数据嵌套在 Fixture 消息的 `Participants[].Players` 中，当前系统未解析和存储这些数据。`models.go` 中没有定义 `LSPlayer` 结构体。

**正确做法**：通过 lsport-connector 的 Snapshot API 获取球员数据，新增 `LSPlayer` 模型和 `ls_player_adapter.go` 适配器，然后在 `ls_engine.go` 中激活 `ApplyBottomUp` 逻辑。

**关键位置**：`internal/db/models.go`（缺失 LSPlayer）；`internal/matcher/ls_engine.go`（缺失 ApplyBottomUp 调用）；`internal/matcher/team_player.go` → `ApplyBottomUp`

### 坑 4: TeamAliasIndex 仅为单次运行的内存级索引

**现象**：每次运行匹配任务时，`TeamAliasIndex` 从零开始学习，之前任务中已确认的球队别名映射全部丢失。这导致同一对球队（如 `Chelsea FC` vs `Chelsea`）在每次任务中都需要重新通过 L1/L2/L3 匹配来"再次学习"。

**根因**：`TeamAliasIndex` 定义为 `event.go` 中的内存结构体，生命周期仅限于单次 `MatchEvents` 调用。

**正确做法**：将 `TeamAliasIndex` 升级为持久化的全局知识图谱，存储到数据库或文件系统中。每次成功匹配的球队对自动写入知识图谱，供后续任务直接查询。新增 `internal/db/alias_store.go` 实现持久化存储。

**关键位置**：`internal/matcher/event.go` → `TeamAliasIndex` 约第 43-130 行

### 坑 5: KnownLSLeagueMap 存在跨级别映射风险

**现象**：`KnownLSLeagueMap` 中 `football:66` 被映射到德甲（Bundesliga），但实际上 LS 的 `tournament_id=66` 可能对应的是德乙（2. Bundesliga）。

**根因**：已知映射表是手动维护的，缺乏自动化验证机制。当联赛 ID 的含义发生变化或初始录入错误时，没有检测和纠正手段。

**正确做法**：为 `KnownLSLeagueMap` 引入自动化验证流程。每次使用已知映射时，用比赛反向确认率（成功匹配的比赛数 / 总比赛数）进行交叉验证。如果反向确认率低于阈值（如 0.30），标记该映射为"待复核"。

**关键位置**：`internal/matcher/ls_engine.go` → `KnownLSLeagueMap`；`python/match_2026.py` → `KNOWN_LS_TS_MAP`

---

## 优化 TODO 清单

### P0 阶段：强约束拦截与联赛匹配优化（预计 2 周）

- [x] **TODO-001**: 新增 `internal/matcher/league_features.go`，实现联赛名称的结构化特征提取（地区、性别、年龄段、区域分区、赛制类型、层级数字）—— **已完成 2026-04-17**
- [x] **TODO-002**: 修改 `ls_engine.go` 和 `league.go` 中的联赛匹配函数，引入六维强约束一票否决机制 —— **已完成 2026-04-17**
- [x] **TODO-003**: 在 `name.go` 中新增 Jaro-Winkler 相似度函数，与 Jaccard 取最大值作为名称相似度 —— **已完成 2026-04-17**
- [x] **TODO-004**: 实现联赛名称中的层级数字提取与精确校验（如 `Liga 3` 中的 `3`、`4 Liga` 中的 `4`）—— **已完成 2026-04-17**（在 `league_features.go` 中实现）
- [x] **TODO-005**: 更新 `docs/league_guard_keywords.json`，补充完整的强约束关键词词典 —— **已完成 2026-04-17**

### P1 阶段：LS 球员层接入与自底向上传导（预计 3 周）

- [x] **TODO-006**: 在 `internal/db/models.go` 中新增 `LSPlayer` 和 `LSTeam` 结构体 —— **已完成 2026-04-17**
- [x] **TODO-007**: 新增 `internal/db/ls_player_adapter.go`，实现数据库优先 + Snapshot API 兆底的双路球员获取，支持批量查询 —— **已完成 2026-04-17**
- [x] **TODO-008**: 修改 `team_player.go`，新增 `LSPlayerMatch` 结构、`MatchPlayersForLSTeam`（高阈值名称匹配）、`DeriveTeamMappingsFromLS`、`ApplyBottomUpLS` —— **已完成 2026-04-17**
- [x] **TODO-009**: 在 `ls_engine.go` 中激活球员匹配阶段和 `ApplyBottomUpLS` 自底向上校验；新增 `NewLSEngineWithPlayers`；扩展 `LSMatchStats`/`LSMatchResult` 球员字段 —— **已完成 2026-04-17**
- [x] **TODO-010**: 新增 `internal/matcher/reverse_confirm.go`，实现 `ComputeReverseConfirmRate`、`ClassifyRCR`、`ApplyRCRToLeague` —— **已完成 2026-04-17**

### P2 阶段：时间容差优化与全局别名（预计 2 周）

- [x] **TODO-011**: 修改 `event.go` 中的 `levelConfigs`，将硬性时间分级替换为高斯衰减连续模糊时间窗口 —— **已完成 2026-04-17**（`gaussianTimeFactor` 函数，四种策略 σ 分别为 3600s/10800s/43200s/43200s）
- [x] **TODO-012**: 新增 `internal/db/alias_store.go`，将 `TeamAliasIndex` 升级为数据库持久化的全局知识图谱 —— **已完成 2026-04-17**（`AliasStore` 支持 Upsert/Lookup/UpsertBatch/PruneStale，`TeamAliasIndex` 新增 `InjectAlias` 实现 `db.AliasIndexLoader` 接口）
- [x] **TODO-013**: 重构 `engine.go` 和 `ls_engine.go`，将 SR↔TS 和 LS↔TS 两条链路统一为一套通用引擎 —— **已完成 2026-04-17**（新增 `universal_engine.go`：`UniversalEngine`/`SourceAdapter`/`SRSourceAdapter`/`LSSourceAdapter`）
- [ ] **TODO-014**: 为 `KnownLSLeagueMap` / `KNOWN_LS_TS_MAP` 引入比赛反向确认率自动验证

### P3 阶段：高级特性与持续优化（预计 4 周）

- [ ] **TODO-015**: 新增 `internal/matcher/dense_blocking.go`，引入 Entity2Vec + HNSW 向量检索的稠密分块
- [ ] **TODO-016**: 新增 `internal/matcher/fs_model.go`，实现 Fellegi-Sunter 模型的无监督 EM 参数估计
- [ ] **TODO-017**: 新增 `internal/matcher/event_dtw.go`，实现基于事件流的动态时间规整（EventDTW）
- [ ] **TODO-018**: 开发人在回路可视化前端，供体育数据专家人工研判低置信度匹配

---

## 关键耦合点

本优化涉及多个模块的深度耦合，以下是需要特别注意的依赖关系：

1. **联赛特征提取 ↔ 强约束关键词词典**：`league_features.go` 的特征提取逻辑必须与 `docs/league_guard_keywords.json` 中的关键词保持同步。新增关键词时两处必须同时更新。

2. **LS 球员层 ↔ lsport-connector 技能**：`ls_player_adapter.go` 的实现依赖 lsport-connector 技能中定义的 Snapshot API 接口和数据格式。API 变更时适配器必须同步更新。

3. **全局别名知识图谱 ↔ 数据库 Schema**：`alias_store.go` 的持久化方案需要在 TheSports 或独立数据库中新建表。Schema 变更需要与 DBA 协调。

4. **通用引擎统一 ↔ 现有 API 层**：将两条链路统一为通用引擎后，`internal/api/` 层的接口定义和调用方式可能需要适配。

5. **比赛反向确认 ↔ 联赛匹配**：`reverse_confirm.go` 的输出（反向确认率）需要回灌到联赛匹配的置信度计算中，形成闭环。这要求联赛匹配和比赛匹配的执行顺序从"先联赛后比赛"变为"联赛候选→比赛验证→联赛确认"的三阶段流程。

---

## 预期效果

| 指标 | 当前值 | P0 目标 | P1 目标 | P2 目标 |
|------|-------|---------|---------|---------|
| 联赛匹配准确率（event_count ≥ 5） | 44.44% | ≥ 70% | ≥ 80% | ≥ 90% |
| 比赛匹配率（热门联赛） | ~85% | ~88% | ~93% | ~95% |
| 球队映射覆盖率 | ~80% | ~82% | ~90% | ~95% |
| 误匹配率（False Positive） | ~30% | ≤ 15% | ≤ 8% | ≤ 3% |

---

## 版本变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|---------|------|
| v1.0 | 2026-04-16 | 初始记录：完整算法设计规划与 18 项优化 TODO | Manus AI |
| v1.1 | 2026-04-17 | P0 阶段完成：TODO-001~005 全部完成。新增 `league_features.go`（六维特征提取 + 强约束否决 + 层级数字提取）；`name.go` 新增 Jaro-Winkler；`ls_engine.go` 和 `league.go` 引入 `CheckLeagueVeto`；`league_guard_keywords.json` 扩充 70+ 国家别名组和赛制关键词 | Manus AI |
| v1.2 | 2026-04-17 | P1 阶段完成：TODO-006~010 全部完成。`models.go` 新增 `LSPlayer`/`LSTeam`；新增 `ls_player_adapter.go`（数据库优先+Snapshot API 兆底，支持批量查询）；`team_player.go` 新增 `LSPlayerMatch`/`MatchPlayersForLSTeam`/`DeriveTeamMappingsFromLS`/`ApplyBottomUpLS`；`ls_engine.go` 激活球员匹配阶段和自底向上校验；新增 `reverse_confirm.go`（RCR 计算/分级/回灰联赛置信度）；`result.go` 扩展 LS 结构体球员字段 | Manus AI |
| v1.3 | 2026-04-17 | P2 阶段完成：TODO-011~013 全部完成。`event.go` 将硬性时间分级替换为高斯衰减连续模糊时间窗口（`gaussianTimeFactor`），新增 `InjectAlias` 实现 `db.AliasIndexLoader` 接口；新增 `internal/db/alias_store.go`（`AliasStore` 持久化球队别名知识图谱，支持 Upsert/UpsertBatch/Lookup/PruneStale/LoadIntoIndex）；新增 `internal/matcher/universal_engine.go`（`UniversalEngine` 通用引擎 + `SourceAdapter` 适配器接口 + `SRSourceAdapter`/`LSSourceAdapter` 实现）；全项编译通过 | Manus AI |
