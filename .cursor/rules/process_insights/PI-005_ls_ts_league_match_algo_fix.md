---
id: "PI-005"
version: "v1.0"
last_updated: "2026-04-21"
author: "Manus Agent / fix-ls-ts-algo"
related_modules: ["internal/matcher", "docs"]
status: "active"
---

# PI-005: LS→TS 联赛匹配高频算法误匹配修复指南

## 流程概述

本洞察记录了 2026-04-21 针对 LS→TS 联赛匹配纯算法模式（`--no-known-map`）高频误匹配问题的四项根因分析与代码修复，供后续开发者在扩展赛事类型、别名词典或地理约束时参考，避免重复踩坑。

---

## 核心防坑指南

### 坑 1: 新赛事类型（draft/all_star）被泛化关键词误识别为 league

**现象**：`CBA Draft` 被匹配到 `CBA`（正赛），`German All Star` 被匹配到 `Bundesliga`。  
**根因**：`competitionTypeKeywords` 中没有 `draft` 和 `all_star` 类型，`competitionTypeOrder` 中 `league` 排在末尾，但 `detectCompetitionType` 对 `draft`/`all_star` 返回空字符串，导致 `CheckLeagueVeto` 无法触发 `competition_type_conflict` 否决。  
**正确做法**：
1. 在 `competitionTypeKeywords` 中新增类型条目。
2. 在 `competitionTypeOrder` 中将新类型插入 `league` **之前**（防止泛化关键词先行匹配）。
3. 在 `CheckLeagueVeto` 中为新类型添加完整的强约束否决逻辑（含"与未知类型互斥"的分支）。
4. 同步更新 `docs/league_guard_keywords.json`。

**关键位置**：`internal/matcher/league_features.go` → `competitionTypeOrder`（约第 122 行）、`CheckLeagueVeto` → `@section:competition_type_veto`（约第 491 行）

---

### 坑 2: 联赛缩写（FNL/HNL/NBL）在 staticLeagueAliasGroups 中缺失导致相似度失效

**现象**：`FNL`（俄罗斯第一联赛）被匹配到 `Finalissima`（国际赛事），`HNL`（克罗地亚）被匹配到 `Israel C League`。  
**根因**：`staticLeagueAliasGroups` 中没有 `FNL`/`HNL` 等缩写组，`leagueNameSimilarityWithAlias` 回退到纯字符串相似度，`FNL` 与 `Finalissima` 的 Jaccard 相似度异常偏高（共享 `f`/`n`/`l` 字符）。  
**正确做法**：
1. 在 `staticLeagueAliasGroups` 中为每个高频缩写联赛添加别名组，将缩写与官方全称绑定。
2. 对于多国通用缩写（如 `Superliga`），不要在别名组中绑定特定国家，而是依赖地理约束过滤。
3. 同时在 `leagueAbbrevCountryMap` 中为该缩写添加国家映射（见坑 4）。

**关键位置**：`internal/matcher/league_alias.go` → `staticLeagueAliasGroups`（约第 43 行起）

---

### 坑 3: J1/J2/K1/K2/Ligue 等格式的层级数字无法被 tierPatterns 提取

**现象**：`J2 League` 与 `J1 League` 的 `TierNumber` 均为 0，`tier_number_veto` 无法拦截跨层级误配。  
**根因**：`tierPatterns` 中只有 `Liga \d+` 等格式，缺少 `J\d+ League`、`K League \d+`、`Ligue \d+` 等格式的正则。  
**正确做法**：
1. 在 `tierPatterns` 末尾（`serie [a-e]` 之后）添加新格式正则。
2. 在 `wordTierMap` 中添加对应的文字层级词（如 `"j1 league": 1`）。
3. 注意正则的捕获组位置：`j(\d+)\s*league` 捕获 `j` 后的数字，`k\s*league\s*(\d+)` 捕获末尾数字。

**关键位置**：`internal/matcher/league_features.go` → `tierPatterns`（约第 150 行）、`wordTierMap`（约第 200 行）

---

### 坑 4: 缩写联赛因 CategoryName 为空绕过 lsLocationVeto 地理约束

**现象**：`NBL`（澳大利亚篮球）被匹配到 `ENBL`（欧洲），原因是 LS 数据中 `CategoryName` 字段为空，`lsLocationVeto` 直接返回 `false`（不否决）。  
**根因**：`lsLocationVeto` 仅依赖 `ls.CategoryName` 字段，当该字段为空时完全失效，无法阻止跨洲际误配。  
**正确做法**：
1. 在 `ls_engine.go` 中维护 `leagueAbbrevCountryMap`（联赛缩写→国家知识图谱）。
2. 实现 `lsLocationVetoByName`：从联赛名称本身推断国家，作为 `lsLocationVeto` 的补充约束。
3. 在 `lsLeagueNameScore` 中，在 `lsLocationVeto` 之后紧接调用 `lsLocationVetoByName`。
4. 对于多国通用联赛名称（如 `Premier League`、`Serie A`），在 `leagueAbbrevCountryMap` 中将 country 设为空字符串 `""`，表示不约束。

**关键位置**：`internal/matcher/ls_engine.go` → `@section:league_abbrev_country_map`（约第 554 行）、`@section:location_veto_by_name`（约第 681 行）、`lsLeagueNameScore`（约第 716 行）

---

### 坑 5: lsLocationVeto 使用纯 Jaccard 相似度，无法识别 USA vs United States 等地理别名

**现象**：`USA`（LS CategoryName）与 `United States`（TS CountryName）的 Jaccard 相似度低于 0.4，导致合法匹配被误否决。  
**根因**：`lsLocationVeto` 直接调用 `jaccardSimilarity`，未利用 `geoAliasGroups` 中已有的地理别名词典。  
**正确做法**：将 `lsLocationVeto` 中的 `jaccardSimilarity(catNorm, cntNorm) < 0.4` 替换为 `geoSimilarity(catNorm, cntNorm) < 0.4`，后者会先查地理别名词典，再回退到 Jaccard。

**关键位置**：`internal/matcher/ls_engine.go` → `lsLocationVeto`（约第 665 行）

---

## 关键耦合点

| 修改点 | 耦合模块 | 注意事项 |
|--------|---------|---------|
| `competitionTypeKeywords` 新增类型 | `CheckLeagueVeto`、`CalcFeaturePenalty` | 新类型必须同时在 Veto 和 Penalty 两处处理，否则只有检测无否决 |
| `staticLeagueAliasGroups` 新增别名组 | `leagueAliasIndex`（运行时构建） | 别名组中的 `CanonicalName` 必须是最完整的官方名，否则相似度基准偏低 |
| `leagueAbbrevCountryMap` 新增条目 | `lsLocationVetoByName`（遍历 map） | map 遍历顺序不确定，较短的缩写可能被较长的缩写覆盖；建议优先匹配最长缩写（当前实现为首次命中即返回，存在顺序依赖风险） |
| `tierPatterns` 新增正则 | `ExtractLeagueFeatures`、`tier_number_veto` | 新正则必须测试不会误匹配非层级数字（如年份 `2024`、球队编号） |

---

## 已知局限与后续优化建议

1. **`leagueAbbrevCountryMap` 首次命中即返回**：当联赛名称同时包含多个缩写时（如 `FNL Cup`），只有第一个命中的缩写生效。建议后续改为最长匹配策略。
2. **`lsLocationVetoByName` 使用 `strings.Contains`**：可能误匹配子串（如 `mls` 命中 `famls`）。建议后续改为完整词边界匹配（参考 `containsWholeWord`）。
3. **`staticLeagueAliasGroups` 需持续维护**：随着新赛事接入，需定期将高频误匹配的缩写联赛补充到词典中。

---

## 版本变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|---------|------|
| v1.0 | 2026-04-21 | 初始记录（四项修复：draft/all_star 类型、别名词典+30组、层级正则、地理约束知识图谱） | Manus Agent |
