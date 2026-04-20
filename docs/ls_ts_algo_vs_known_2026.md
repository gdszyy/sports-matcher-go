# LS→TS 联赛匹配：纯算法（--no-known-map）vs LEAGUE_KNOWN 对比报告

**测试日期**: 2026-04-20  
**测试范围**: 49 个联赛（足球 36 个 + 篮球 13 个）  
**算法版本**: 高斯时间衰减 + FS 模型 + DTW + 名称相似度（leagueNameSimilarityWithAlias）

---

## 总体结果对比

| 模式 | 联赛数 | 比赛总数 | 已匹配 | 匹配率 |
|------|--------|----------|--------|--------|
| **LEAGUE_KNOWN**（有映射表） | 49 | 1211 | 1091 | **90.1%** |
| **纯算法**（--no-known-map） | 49 | 1211 | 484 | **40.0%** |

> 结论：LEAGUE_KNOWN 模式比纯算法高出 **50.1 个百分点**，验证了映射表的核心价值。

---

## 逐联赛对比（足球）

### 足球热门联赛

| LS 联赛名 | 算法匹配到 TS | 规则 | 置信度 | 算法匹配率 | KNOWN 匹配率 | 算法是否正确 |
|-----------|--------------|------|--------|------------|-------------|------------|
| Premier League | English Premier League | LEAGUE_NAME_HI | 0.980 | 100% | 100% | ✅ 正确 |
| LaLiga | Spanish Segunda División | LEAGUE_NAME_HI | 0.979 | — | 100% | ❌ 错误（匹配到二级联赛） |
| Bundesliga | Bundesliga | LEAGUE_NAME_HI | 0.985 | 100% | 100% | ✅ 正确 |
| Serie A | Italian Serie A | LEAGUE_NAME_HI | 0.985 | — | 100% | ✅ 正确（联赛对，但比赛数0） |
| Ligue 1 | French Ligue 2 | LEAGUE_NAME_HI | 0.979 | — | 100% | ❌ 错误（匹配到二级联赛） |
| UEFA Champions League | CAF Champions League | LEAGUE_NAME_HI | — | — | 100% | ❌ 错误（跨洲际） |
| UEFA Europa League | UEFA Europa League | LEAGUE_NAME_HI | — | — | 100% | ✅ 正确 |
| CONMEBOL Copa Libertadores | CONMEBOL Copa America | LEAGUE_NAME_HI | 0.899 | 0% | 100% | ❌ 错误（不同赛事） |
| Copa Sudamericana | Copa de la Reina Women | LEAGUE_NAME_MED | 0.825 | — | 100% | ❌ 错误（女子赛事） |

### 足球常规联赛（典型错误）

| LS 联赛名 | 算法匹配到 TS | 是否正确 | 问题原因 |
|-----------|--------------|----------|----------|
| The Championship | English Football League Championship | ✅ 正确 | 名称相似度高 |
| League One | English Football League One | ✅ 正确 | 名称相似度高 |
| League Two | English Football League Two | ✅ 正确 | 名称相似度高 |
| National League | Football Association Community Shield | ❌ 错误 | "National"+"League" 词频干扰 |
| 2.Bundesliga | Bundesliga | ❌ 错误 | 匹配到一级联赛（忽略"2."前缀） |
| Ligue 2 | French Ligue 2 | ✅ 正确 | 名称相似度高 |
| LaLiga2 | Spanish Segunda División | ✅ 正确 | 别名映射正确 |
| Jupiler League | Israel C League | ❌ 错误 | 国家/地区约束未生效 |
| Eredivisie | Netherlands Derde Divisie | ❌ 错误 | 匹配到三级联赛 |
| Primeira Liga | Liga Portugal 2 | ❌ 错误 | 匹配到二级联赛 |
| FNL | Finalissima | ❌ 错误 | 缩写歧义 |
| Ekstraklasa | KSA WL | ❌ 错误 | 完全错误 |
| HNL | HNFL | ❌ 错误 | 缩写相似但不同联赛 |
| Super League | Super 8 | ❌ 错误 | "Super"词频干扰 |
| Superettan | Super 8 | ❌ 错误 | 同上 |
| Eliteserien | BPL | ❌ 错误 | 完全错误 |
| Major League Soccer | MLS ASG（全明星赛） | ❌ 错误 | 匹配到全明星赛而非正赛 |
| Liga MX | LIFA C | ❌ 错误 | 完全错误 |
| Serie A（巴西） | Italian Serie C5 | ❌ 错误 | 跨国同名联赛冲突 |
| J1 League | Japanese J2 League | ❌ 错误 | 匹配到二级联赛 |
| K League Classic | KUC | ❌ 错误 | 完全错误 |
| K2 League | KH WF League | ❌ 错误 | 完全错误 |
| Premier League（埃及） | BPL | ❌ 错误 | 跨国同名联赛冲突 |
| China Super League | Chinese Football Super League | ✅ 正确 | 名称相似度高 |
| Allsvenskan | Sweden Allsvenskan | ✅ 正确 | 名称相似度高 |
| Scotland Premiership | Scottish Premiership | ✅ 正确 | 名称相似度高 |

### 篮球联赛

| LS 联赛名 | 算法匹配到 TS | 是否正确 | 问题原因 |
|-----------|--------------|----------|----------|
| NBA | National Basketball Association | ✅ 正确 | 名称相似度高 |
| Euroleague | EuroLeague | ✅ 正确 | 名称相似度高 |
| CBA | CBA Draft | ❌ 错误 | 匹配到选秀而非正赛 |
| Liga ACB Endesa | Liga de Baloncesto | ✅ 正确（联赛对） | 但比赛数为0（TS无数据） |
| Bundesliga（篮球） | German All Star | ❌ 错误 | 匹配到全明星赛 |
| VTB United League | VTB United League | ✅ 正确 | 名称完全一致 |
| Serie A（意大利篮球） | Italian Regional league | ❌ 错误 | 匹配到地区联赛 |
| BNXT League | B1 League | ❌ 错误 | 缩写相似但不同联赛 |
| B.League - B1 | B1 League | ✅ 正确 | 名称相似度高 |
| Orlen Basket Liga | Jordan Basketball League | ❌ 错误 | 完全错误 |
| NBL（澳大利亚） | ENBL | ❌ 错误 | 缩写歧义 |

---

## 算法问题根因分析

### 1. 同名联赛跨国冲突（最高频问题）

"Serie A"、"Premier League"、"Bundesliga"、"Super League" 等通用名称在多个国家/地区都有同名联赛，算法无法通过名称区分，导致跨国误匹配。

**影响联赛**: LaLiga（→二级）、Serie A（巴西→意大利）、Premier League（埃及→英格兰）、Bundesliga（篮球→全明星）

### 2. 级别数字/前缀识别不足

"2.Bundesliga"、"J1 League"（vs J2 League）、"Ligue 1"（vs Ligue 2）等联赛的级别数字在相似度计算中权重不足，导致匹配到同国家的不同级别联赛。

**影响联赛**: 2.Bundesliga、J1 League、Ligue 1、Primeira Liga、Eredivisie

### 3. 缩写歧义

"FNL"（俄罗斯足球甲级联赛）→ "Finalissima"，"HNL" → "HNFL"，"NBL" → "ENBL"，"BNXT" → "B1 League"。缩写在不同语境下有不同含义，纯名称相似度无法区分。

### 4. 赛事类型混淆

"Copa Libertadores" → "Copa America"（不同赛事），"Major League Soccer" → "MLS ASG"（全明星赛），"CBA" → "CBA Draft"（选秀），"Bundesliga" → "German All Star"（全明星赛）。

### 5. 国家/地区约束失效

"Jupiler League"（比利时）→ "Israel C League"（以色列），"Eliteserien"（挪威）→ "BPL"（孟加拉国）。地理约束 `lsLocationVeto` 未能正确识别这些联赛的归属国。

---

## 结论与建议

纯算法（名称相似度）在以下场景表现良好：
- 名称高度唯一的联赛（NBA、Euroleague、VTB United League、China Super League）
- 英格兰联赛体系（Championship、League One、League Two）
- 名称包含国家前缀的联赛（Sweden Allsvenskan、Scottish Premiership）

纯算法在以下场景存在系统性缺陷：
- **同名跨国联赛**：需要引入国家/地区强约束
- **级别数字识别**：需要提升数字权重或引入层级约束
- **缩写歧义**：需要缩写词典扩展
- **赛事类型区分**：需要引入赛事类型特征（正赛/杯赛/全明星/选秀）

**建议**: 维持 `KnownLSLeagueMap` 作为主要匹配机制，纯算法作为兜底。对于新增联赛，可先用纯算法生成候选列表（Top-5），再人工确认后加入映射表。

---

*报告生成时间: 2026-04-20*
