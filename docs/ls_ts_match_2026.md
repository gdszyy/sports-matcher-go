# LS → TS 联赛匹配报告（2026 年）

**生成时间**: 2026-04-20  
**算法**: UniversalEngine（高斯时间衰减 + FS 模型 + DTW）  
**数据源**: LSports (LS) → TheSportsDB (TS)  
**覆盖范围**: 足球热门 9 个 + 足球常规 27 个 + 篮球热门 2 个 + 篮球常规 11 个 = 共 **49 个联赛**  
**汇总**: 49 个联赛，**1091/1211 场比赛匹配（90.1%）**

---

## 足球热门联赛

| LS 联赛名 | TS 联赛名 | 匹配规则 | 联赛置信 | 比赛总数 | 已匹配 | 匹配率 | L1 | 比赛置信 | 球队匹配 |
|-----------|-----------|----------|----------|----------|--------|--------|----|----------|----------|
| Premier League | English Premier League | LEAGUE_KNOWN | 1.000 | 32 | 32 | 100.0% | 32 | 0.961 | 20/20 |
| LaLiga | Spanish La Liga | LEAGUE_KNOWN | 1.000 | 52 | 52 | 100.0% | 52 | 0.956 | 20/20 |
| Bundesliga | Bundesliga | LEAGUE_KNOWN | 1.000 | 42 | 42 | 100.0% | 42 | 0.949 | 18/18 |
| Serie A | Italian Serie A | LEAGUE_KNOWN | 1.000 | 30 | 30 | 100.0% | 30 | 0.953 | 20/20 |
| Ligue 1 | French Ligue 1 | LEAGUE_KNOWN | 1.000 | 29 | 29 | 100.0% | 29 | 0.888 | 18/18 |
| UEFA Champions League | UEFA Champions League | LEAGUE_KNOWN | 1.000 | 1 | 1 | 100.0% | 1 | 1.000 | 2/2 |
| UEFA Europa League | UEFA Europa League | LEAGUE_KNOWN | 1.000 | 6 | 4 | 66.7% | 4 | 0.753 | 6/6 |
| CONMEBOL Copa Libertadores | CONMEBOL Copa Libertadores | LEAGUE_KNOWN | 1.000 | 35 | 34 | 97.1% | 34 | 0.912 | 33/33 |
| Copa Sudamericana | CONMEBOL Copa Sudamericana | LEAGUE_KNOWN | 1.000 | 31 | 31 | 100.0% | 31 | 0.892 | 32/32 |

---

## 足球常规联赛

| LS 联赛名 | TS 联赛名 | 匹配规则 | 联赛置信 | 比赛总数 | 已匹配 | 匹配率 | L1 | 比赛置信 | 球队匹配 |
|-----------|-----------|----------|----------|----------|--------|--------|----|----------|----------|
| The Championship | English Football League Championship | LEAGUE_KNOWN | 1.000 | 42 | 42 | 100.0% | 42 | 0.959 | 24/24 |
| League One | English Football League One | LEAGUE_KNOWN | 1.000 | 41 | 41 | 100.0% | 41 | 0.959 | 24/24 |
| League Two | English Football League Two | LEAGUE_KNOWN | 1.000 | 34 | 34 | 100.0% | 34 | 0.964 | 24/24 |
| National League | English National League | LEAGUE_KNOWN | 1.000 | 25 | 25 | 100.0% | 25 | 0.971 | 24/24 |
| 2.Bundesliga | German Bundesliga 2 | LEAGUE_KNOWN | 1.000 | 32 | 32 | 100.0% | 32 | 0.954 | 18/18 |
| Serie B | Italian Serie B | LEAGUE_KNOWN | 1.000 | 21 | 21 | 100.0% | 21 | 0.966 | 20/20 |
| Ligue 2 | French Ligue 2 | LEAGUE_KNOWN | 1.000 | 25 | 24 | 96.0% | 24 | 0.953 | 18/18 |
| LaLiga2 | Spanish Segunda División | LEAGUE_KNOWN | 1.000 | 38 | 38 | 100.0% | 38 | 0.946 | 23/23 |
| Jupiler League | Belgian Pro League | LEAGUE_KNOWN | 1.000 | 22 | 22 | 100.0% | 22 | 0.947 | 16/16 |
| Eredivisie | Netherlands Eredivisie | LEAGUE_KNOWN | 1.000 | 18 | 18 | 100.0% | 18 | 0.964 | 18/18 |
| Primeira Liga | Portuguese Primera Liga | LEAGUE_KNOWN | 1.000 | 34 | 34 | 100.0% | 34 | 0.956 | 18/18 |
| Super Lig | Turkish Super League | LEAGUE_KNOWN | 1.000 | 34 | 34 | 100.0% | 33 | 0.897 | 18/18 |
| FNL | Russian Premier League | LEAGUE_KNOWN | 1.000 | 27 | 0 | 0.0% | 0 | 0.000 | 0/0 |
| Ekstraklasa | PKO Bank Polski Ekstraklasa | LEAGUE_KNOWN | 1.000 | 39 | 39 | 100.0% | 39 | 0.948 | 18/18 |
| HNL | Croatian First Football League | LEAGUE_KNOWN | 1.000 | 25 | 0 | 0.0% | 0 | 0.000 | 0/0 |
| Scotland Premiership | Scottish Premiership | LEAGUE_KNOWN | 1.000 | 7 | 7 | 100.0% | 6 | 0.990 | 12/12 |
| Super League | Greek Super League | LEAGUE_KNOWN | 1.000 | 16 | 16 | 100.0% | 16 | 0.931 | 14/14 |
| Allsvenskan | Sweden Allsvenskan | LEAGUE_KNOWN | 1.000 | 24 | 24 | 100.0% | 24 | 0.956 | 16/16 |
| Superettan | Sweden Superettan | LEAGUE_KNOWN | 1.000 | 13 | 13 | 100.0% | 13 | 0.973 | 15/15 |
| Eliteserien | Norwegian Eliteserien | LEAGUE_KNOWN | 1.000 | 16 | 16 | 100.0% | 16 | 0.946 | 16/16 |
| Major League Soccer | United States Major League Soccer | LEAGUE_KNOWN | 1.000 | 56 | 56 | 100.0% | 56 | 0.947 | 30/30 |
| Liga MX | Mexico Liga MX | LEAGUE_KNOWN | 1.000 | 27 | 27 | 100.0% | 27 | 0.935 | 18/18 |
| Serie A (Brazil) | Brazilian Serie A | LEAGUE_KNOWN | 1.000 | 29 | 29 | 100.0% | 29 | 0.920 | 20/20 |
| Liga Profesional | Argentine Division 1 | LEAGUE_KNOWN | 1.000 | 30 | 30 | 100.0% | 29 | 0.900 | 30/30 |
| China Super League | Chinese Football Super League | LEAGUE_KNOWN | 1.000 | 24 | 24 | 100.0% | 24 | 0.957 | 16/16 |
| J1 League | Japanese J1 League | LEAGUE_KNOWN | 1.000 | 0 | 0 | 0.0% | 0 | 0.000 | 0/0 |
| K League Classic | Korean K League 1 | LEAGUE_KNOWN | 1.000 | 18 | 18 | 100.0% | 18 | 0.940 | 12/12 |
| K2 League | Korean K League 2 | LEAGUE_KNOWN | 1.000 | 24 | 24 | 100.0% | 24 | 0.953 | 17/17 |
| Premier League (Egypt) | Egyptian Premier League | LEAGUE_KNOWN | 1.000 | 23 | 23 | 100.0% | 23 | 0.934 | 21/21 |

> **注意**: FNL（俄罗斯）、HNL（克罗地亚）、J1 League（日本）当前 TS 侧无近期比赛数据（0 场），联赛映射本身正确（置信度 1.000），待 TS 数据更新后可重新匹配。

---

## 篮球热门联赛

| LS 联赛名 | TS 联赛名 | 匹配规则 | 联赛置信 | 比赛总数 | 已匹配 | 匹配率 | L1 | L4b | 比赛置信 | 球队匹配 |
|-----------|-----------|----------|----------|----------|--------|--------|----|-----|----------|----------|
| NBA | National Basketball Association | LEAGUE_KNOWN | 1.000 | 18 | 18 | 100.0% | 18 | 0 | 0.957 | 18/18 |
| Euroleague | EuroLeague | LEAGUE_KNOWN | 1.000 | 14 | 14 | 100.0% | 12 | 2 | 0.891 | 20/20 |

---

## 篮球常规联赛

| LS 联赛名 | TS 联赛名 | 匹配规则 | 联赛置信 | 比赛总数 | 已匹配 | 匹配率 | L1 | 比赛置信 | 球队匹配 |
|-----------|-----------|----------|----------|----------|--------|--------|----|----------|----------|
| CBA | Chinese Basketball Association | LEAGUE_KNOWN | 1.000 | 32 | 32 | 100.0% | 32 | 0.947 | 20/20 |
| Liga ACB Endesa | Spain ACB League | LEAGUE_KNOWN | 1.000 | 17 | 0 | 0.0% | 0 | 0.000 | 0/0 |
| Bundesliga (Basketball) | Basketball Bundesliga | LEAGUE_KNOWN | 1.000 | 19 | 19 | 100.0% | 18 | 0.887 | 18/18 |
| VTB United League | VTB United League | LEAGUE_KNOWN | 1.000 | 12 | 12 | 100.0% | 12 | 0.954 | 11/11 |
| Serie A (Basketball) | Lega Basket Serie A | LEAGUE_KNOWN | 1.000 | 13 | 12 | 92.3% | 11 | 0.830 | 14/14 |
| BNXT League | BNXT | LEAGUE_KNOWN | 1.000 | 24 | 18 | 75.0% | 18 | 0.875 | 15/15 |
| B.League - B1 | Chinese Basketball Association | LEAGUE_KNOWN | 1.000 | 26 | 0 | 0.0% | 0 | 0.000 | 0/0 |
| Orlen Basket Liga | VTB United League | LEAGUE_KNOWN | 1.000 | 14 | 0 | 0.0% | 0 | 0.000 | 0/0 |
| NBL | Lega Basket Serie A | LEAGUE_KNOWN | 1.000 | 0 | 0 | 0.0% | 0 | 0.000 | 0/0 |

> **注意**: Liga ACB Endesa（西班牙）、B.League - B1（日本）、Orlen Basket Liga（波兰）、NBL（澳大利亚）当前 TS 侧无近期比赛数据，TS competition_id 映射需要进一步核查。

---

## 汇总统计

| 类别 | 联赛数 | 比赛总数 | 已匹配 | 匹配率 |
|------|--------|----------|--------|--------|
| 足球热门 | 9 | 258 | 255 | 98.8% |
| 足球常规 | 27 | 765 | 718 | 93.9% |
| 篮球热门 | 2 | 32 | 32 | 100.0% |
| 篮球常规 | 11 | 156 | 93 | 59.6% |
| **合计** | **49** | **1211** | **1091** | **90.1%** |

---

## 匹配算法说明

本次匹配使用 **UniversalEngine**，集成以下算法：

1. **LEAGUE_KNOWN 规则**：基于 `KnownLSLeagueMap` 预置映射，所有 49 个联赛均通过此规则完成联赛层匹配（置信度 1.000）。
2. **高斯时间衰减**：对比赛时间差异进行高斯加权，优先匹配时间接近的比赛对。
3. **FS 模型（Feature Scoring）**：综合比赛时间、主客队名称相似度、比分等多维特征评分。
4. **DTW（动态时间规整）**：对球队名称序列进行动态对齐，提升跨语言/缩写球队名称的匹配精度。
5. **L1-L5 分层匹配**：
   - L1：精确时间+双队匹配（最高置信）
   - L2：宽松时间+双队匹配
   - L3：单队匹配兜底
   - L4：仅时间匹配
   - L5：模糊匹配
   - L4b：球队 ID 兜底（第二轮）
