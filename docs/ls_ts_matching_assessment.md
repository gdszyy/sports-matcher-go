# LSports → TheSports 匹配方案完整性评估报告

> **仓库**：`gdszyy/sports-matcher-go`  
> **评估日期**：2026-04-14  
> **评估范围**：`internal/matcher/ls_engine.go`、`internal/db/ls_adapter.go`、`internal/matcher/result.go`、`internal/api/server.go` 及相关公共模块

---

## 一、方案架构概览

LSports → TheSports（以下简称 LS→TS）匹配方案以 **SR→TS 匹配框架为基础**，通过适配层将 LSports 数据转换为通用格式后复用核心匹配引擎，整体分为四个层次：

| 层次 | 模块 | 实现状态 |
|:----:|:-----|:-------:|
| 数据接入层 | `db/ls_adapter.go` + `db/tunnel.go` | 已完成 |
| 联赛匹配层 | `matcher/ls_engine.go → matchLSLeague` | 已完成（含 LS 特化） |
| 比赛匹配层 | `matcher/ls_engine.go → matchLSEvents` | 已完成（复用 SR 引擎） |
| 球队映射层 | `matcher/ls_engine.go → deriveLSTeamMappings` | 已完成 |
| **球员匹配层** | — | **缺失** |
| **自底向上校验层** | — | **缺失** |

---

## 二、已实现规则详解

### 2.1 联赛匹配规则

联赛匹配采用**三级策略**，并在名称兜底阶段引入了 LS 专有的地区强约束逻辑。

#### 2.1.1 已知映射（`LEAGUE_KNOWN`，置信度 = 1.0）

`KnownLSLeagueMap` 维护 `sport:ls_tournament_id → ts_competition_id` 的硬编码映射表，当前覆盖 7 条记录：

| 运动 | LS tournament_id | 对应联赛 | TS competition_id |
|:----:|:----------------:|:--------|:-----------------:|
| football | 67 | Premier League (England) | `jednm9whz0ryox8` |
| football | 8363 | LaLiga (Spain) | `vl7oqdehlyr510j` |
| football | 61 | Ligue 1 (France) | `yl5ergphnzr8k0o` |
| football | 66 | 2.Bundesliga (Germany) → TS Bundesliga | `gy0or5jhg6qwzv3` |
| football | 32644 | UEFA Champions League | `z8yomo4h7wq0j6l` |
| football | 30444 | UEFA Europa League | `56ypq3nh0xmd7oj` |
| basketball | 132 | NBA | `49vjxm8xt4q6odg` |

> **注意**：`football:66`（LS 的 2.Bundesliga）被映射到 TS 的 Bundesliga，存在**跨级别联赛的有意映射**，需关注是否为业务需求或配置错误。此外，Serie A（意甲）、Bundesliga（德甲一级）等热门联赛**尚未进入已知映射表**。

#### 2.1.2 名称相似度兜底（`LEAGUE_NAME_HI/MED/LOW`）

当已知映射未命中时，通过 `lsLeagueNameScore` 函数计算 LS 联赛与 TS 联赛的名称相似度，阈值分三档：

| 规则 | 置信度阈值 | 说明 |
|:----:|:---------:|:----|
| `LEAGUE_NAME_HI` | ≥ 0.85 | 高相似度，可信 |
| `LEAGUE_NAME_MED` | ≥ 0.70 | 中相似度，需关注 |
| `LEAGUE_NAME_LOW` | ≥ 0.55 | 低相似度，风险较高 |

**LS 特化增强**：`lsLeagueNameScore` 在 SR 版本基础上做了两项改进：
1. **地区强否决（`lsLocationVeto`）**：若 LS 的 `CategoryName` 与 TS 的 `CountryName` 的 Jaccard 相似度 < 0.4，且非洲际/国际赛事，则直接返回 0 分，防止跨国误配（如 Libya vs Laos）。
2. **同国加权提升**：若地区相似度 ≥ 0.6，将基础名称分按 `base×0.75 + 0.25×locSim` 加权，提升同国联赛的置信度。SR 版本的加权系数为 `base×0.8 + 0.2×locSim`，LS 版本地区权重更高（0.25 vs 0.20）。

### 2.2 比赛匹配规则（六级降级）

LS→TS 完整复用 SR→TS 的六级比赛匹配框架，通过将 `LSEvent` 转换为通用 `SREvent` 格式后调用 `MatchEvents` 函数实现。

| 级别 | 规则 | 时间窗口 | 名称阈值 | 最低置信度 | 特殊约束 |
|:----:|:-----|:-------:|:-------:|:---------:|:--------|
| **L1** | 精确时间 | ≤ 5 min | 0.40 | 0.50 | — |
| **L2** | 宽松时间 | ≤ 6 h | 0.65 | 0.60 | — |
| **L3** | 同一 UTC 日期 | ≤ 24 h（同日） | 0.75 | 0.70 | — |
| **L4** | 超宽时间 + 别名强匹配 | ≤ 72 h | 0.85 | 0.80 | `require_alias=true` |
| **L5** | 无时间约束唯一性匹配 | ≤ 30 天 | 0.90 | — | 候选唯一且主客场顺序一致 |
| **L4b** | 球队 ID 精确对兜底 | 无限制 | — | 0.75 | 需第一轮推导的 `teamIDMap` |

**两轮迭代流程**与 SR 版本完全一致：
- **第一轮**：执行 L1/L2/L3/L4/L5，由内部 `TeamAliasIndex` 驱动别名学习；
- **推导阶段**：从第一轮匹配结果中推导 `LS competitor_id → TS team_id` 映射；
- **第二轮**：以推导映射激活 L4b 球队 ID 精确对兜底。

#### TeamAliasIndex 别名学习机制

在同一联赛的比赛匹配过程中，每当 L1/L2/L3/L4 成功匹配一场比赛（名称相似度 ≥ 0.50），自动将 `(ls_competitor_id → ts_team_id)` 写入别名索引。后续比赛若两队均在索引中命中，直接返回高置信度分数（0.92），解决 `Chelsea FC` vs `Chelsea` 等名称细微差异问题。

### 2.3 球队映射推导规则

`deriveLSTeamMappings` 采用**投票机制**：从所有已匹配比赛中统计每个 `ls_competitor_id` 对应的 `ts_team_id` 投票数，选取票数最多（相同则取置信度最高）的 TS 队伍作为最终映射，置信度为该 TS 队伍的平均比赛置信度。

与 SR 版本的 `DeriveTeamMappings` 相比，LS 版本**缺少**以下增强：
- SR 版本将投票置信度与 `teamNameSimilarity` 按 `0.6/0.4` 融合作为最终球队置信度；LS 版本仅使用平均比赛置信度，**未融合名称相似度**。

---

## 三、方案完整性评估

### 3.1 覆盖层次对比（LS→TS vs SR→TS）

| 功能层 | SR→TS | LS→TS | 差距说明 |
|:------|:-----:|:-----:|:--------|
| 联赛匹配（已知映射） | 覆盖 20+ 联赛 | 仅 7 条 | **LS 已知映射严重不足**，缺少意甲、德甲、西甲（完整版）等 |
| 联赛匹配（名称兜底） | 有 | 有（含地区约束） | LS 版本更严格，地区约束防误配 |
| 比赛匹配（L1~L5） | 有 | 有（完整复用） | 功能等价 |
| 比赛匹配（L4b 兜底） | 有 | 有 | 功能等价 |
| TeamAliasIndex 别名学习 | 有 | 有（复用） | 功能等价 |
| 球队映射推导 | 有（含名称融合） | 有（仅投票） | **LS 版本缺少名称相似度融合** |
| 球员匹配 | 有（3 级规则） | **无** | **完全缺失** |
| 自底向上置信度校验 | 有 | **无** | **完全缺失** |
| API 暴露 | 单联赛 + 批量 | 单联赛 + 批量 | 功能等价，但 LS API 无 `run_players` 参数 |

### 3.2 数据层覆盖范围

| 数据实体 | SR 数据模型 | LS 数据模型 | 差距 |
|:--------|:----------:|:----------:|:----|
| 联赛（Tournament） | `SRTournament` | `LSTournament` | 字段等价 |
| 比赛（Event） | `SREvent`（含场馆） | `LSEvent`（无场馆） | LS 缺少 `VenueID/VenueName` |
| 球队（Team） | `SRTeam` | **无独立结构** | LS 仅通过比赛关联 competitor |
| 球员（Player） | `SRPlayer`（含生日/国籍） | **无** | **完全缺失** |

### 3.3 支持运动类型覆盖

| 运动类型 | LS sport_id | SR→TS 支持 | LS→TS 支持 |
|:--------|:-----------:|:---------:|:---------:|
| 足球（football） | 6046 | 是 | 是 |
| 篮球（basketball） | 48242 | 是 | 是 |
| 网球（tennis） | 54094 | 否 | **已识别 sport_id，但无 TS 对应查询** |
| 冰球（ice_hockey） | 131506 | 否 | **已识别 sport_id，但无 TS 对应查询** |
| 棒球（baseball） | 154914 | 否 | **已识别 sport_id，但无 TS 对应查询** |

> LS 适配器已识别 5 种运动类型的 sport_id，但 `LSEngine.RunLeague` 中仅支持 `football` 和 `basketball` 两种，其余运动类型调用会返回错误。

### 3.4 时间解析风险

LS 的 `scheduled` 字段经 `parseLSScheduled` 解析，支持三种格式（裸 ISO、尾随 Z、+00:00），但**统一按本地时间解析**（`time.Parse` 不含时区信息时默认 UTC）。若 LS 数据库中存储的是本地时区时间而非 UTC，将导致时间偏差，进而影响 L1/L2/L3 的时间窗口判断，可能降低精确时间匹配率，迫使更多比赛降级至 L4/L4b 才能命中。

---

## 四、缺口汇总与优先级建议

### 4.1 高优先级缺口（影响核心匹配质量）

**缺口 1：已知联赛映射表严重不足**

当前 `KnownLSLeagueMap` 仅有 7 条记录，缺少大量主流联赛（意甲 Serie A、德甲 Bundesliga 一级、英冠 Championship、西甲 Segunda Division、欧联杯资格赛等）。对于未在映射表中的联赛，系统将回退到名称相似度匹配，置信度和准确率均会下降。

**建议**：参照 SR 版本的 `KnownLeagueMap`（覆盖 20+ 联赛），系统性补全 LS 热门联赛的 tournament_id → TS competition_id 映射。

---

**缺口 2：`football:66` 映射存在跨级别风险**

`football:66` 在 LS 数据库中为 **2.Bundesliga（德乙）**，但映射到 TS 的 `gy0or5jhg6qwzv3`（Bundesliga，德甲）。若业务上确实需要将德乙数据对接到德甲 TS 联赛，应在注释中明确说明；若为配置错误，需立即修正，否则会导致德乙比赛被错误匹配到德甲数据。

---

**缺口 3：球员匹配层完全缺失**

SR→TS 方案实现了完整的三级球员匹配规则（`PLAYER_NAME_HI/MED/DOB`），并通过 `ApplyBottomUp` 将球员匹配结果反向校验球队和比赛置信度。LS→TS 方案在数据层（无 `LSPlayer` 模型）、适配层（无 `GetPlayersByTeam`）、匹配层（无 `MatchPlayersForTeam`）和 API 层（无 `run_players` 参数）均未实现。

**建议**：若业务需要 LS→TS 球员级别的数据关联，需完整补充上述四层实现。

---

### 4.2 中优先级缺口（影响置信度准确性）

**缺口 4：球队映射推导缺少名称相似度融合**

SR 版本的 `DeriveTeamMappings` 将投票置信度与名称相似度按 0.6/0.4 融合，使球队置信度更能反映名称匹配质量。LS 版本 `deriveLSTeamMappings` 仅使用平均比赛置信度，在名称差异较大但比赛置信度较高的情况下，可能高估球队映射质量。

**建议**：在 `deriveLSTeamMappings` 中引入 `teamNameSimilarity` 融合，与 SR 版本保持一致。

---

**缺口 5：自底向上置信度校验缺失**

SR→TS 方案通过 `ApplyBottomUp` 将球员匹配重叠率反向加成到球队和比赛置信度，实现多层交叉验证。LS→TS 方案缺少此机制，导致置信度仅依赖比赛层的名称/时间相似度，无法通过球员层进行交叉验证。

---

### 4.3 低优先级缺口（扩展性与健壮性）

**缺口 6：网球/冰球/棒球运动类型未完整支持**

LS 适配器已识别 5 种运动的 sport_id，但引擎层仅处理 football 和 basketball。若未来需要扩展到网球等运动，需在 `LSEngine.RunLeague` 中补充对应的 TS 查询分支（`GetCompetitionsByTennis` 等）。

---

**缺口 7：LS 比赛数据缺少场馆信息**

`LSEvent` 模型不包含 `VenueID/VenueName` 字段，而 SR 版本的 `SREvent` 包含场馆信息。若未来需要基于场馆进行辅助匹配（如同一场馆的主场比赛），LS 方案将无法支持。

---

**缺口 8：时间解析未显式指定 UTC**

`parseLSScheduled` 在去掉 `Z` 和 `+00:00` 后，使用无时区的格式字符串解析，实际上等同于按 UTC 处理。若 LS 数据库中的 `scheduled` 字段存储的是带时区偏移的本地时间（如 `+08:00`），当前解析逻辑会丢失时区信息，导致时间偏差。建议增加对 `+HH:MM` 格式的显式处理。

---

## 五、总体评分

| 评估维度 | 得分 | 说明 |
|:--------|:----:|:----|
| 比赛匹配规则完整性 | 9/10 | 完整复用 SR 六级规则，功能等价 |
| 联赛匹配规则完整性 | 6/10 | 已知映射严重不足（7 条 vs 20+），名称兜底有地区约束增强 |
| 球队映射完整性 | 7/10 | 投票机制完整，但缺少名称融合 |
| 球员匹配完整性 | 0/10 | 完全缺失 |
| 置信度校验完整性 | 5/10 | 缺少自底向上校验 |
| 运动类型覆盖 | 4/10 | 仅支持足球和篮球，3 种运动类型未完整支持 |
| **综合评分** | **52/100** | 核心比赛匹配能力完备，但联赛映射、球员层和校验层存在明显缺口 |

---

## 六、改进路线图建议

```
阶段一（短期，1~2 周）：
  ├── 补全 KnownLSLeagueMap（目标覆盖 20+ 热门联赛）
  ├── 核查 football:66 映射是否为有意设计
  └── 修复 parseLSScheduled 时区处理

阶段二（中期，2~4 周）：
  ├── deriveLSTeamMappings 引入名称相似度融合
  └── 扩展 LSEngine 支持网球/冰球/棒球运动类型

阶段三（长期，1~2 个月）：
  ├── 实现 LSPlayer 数据模型与 GetPlayersByTeam 适配器
  ├── 实现 LS→TS 球员三级匹配规则
  ├── 实现 ApplyBottomUp 自底向上校验
  └── 在 API 层暴露 run_players 参数
```
