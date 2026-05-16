# Team-First PoC 结果（PI-006 v1.16）

> 2026-05-16 | 沙箱真数据实测 | 目标：sr:tournament:929 Jordan League

## 问题

P0 联赛匹配 `Jordan League → Jordan League Division 1 (9vjxm8gh4j9r6od)` 看似置信 0.908，
但事件匹配 0/78。EF TopN=5 通过跨 5 个候选 competition 救回 76/78，但**Top-1 仍然
是错的** — 真实赛季其实在 `p3glrw7hwjlqdyj` 下。

## 假设

**SR 球队名比联赛名稳定得多**（联赛名带语言/缩写/赛季/层级噪音，球队名相对纯净）。
直接用 SR 球队名查 TS team_id，再按 team_id 拉事件，能绕过 league name matching
的歧义。

## PoC 步骤

```
SR:tournament:929
   ↓ load SR teams (11 队)
   ↓ load SR events time range (2026-01-20 ~ 2026-04-26)

TS 库：
   ↓ pre-filter ts_fb_team WHERE LOWER(name) REGEXP '(hussein|irbid|jazeera|...)' → 1599 候选 TS 球队
   ↓ Python jaccard fuzzy match → top-3 per SR team
   ↓ 25 个 unique TS team_ids

按 team_id 拉事件：
   SELECT ... FROM ts_fb_match
   WHERE (home_team_id IN (...) OR away_team_id IN (...))
     AND match_time BETWEEN tMin AND tMax
   ↓ 103 events

事件配对（PoC 简化版：SR home/away → TS Top-1 team_id + ±3h）：
   16/78 matched
```

## 关键数据

| 指标 | 结果 |
|------|------|
| SR 球队成功匹配 TS team_id | **11/11 (100%)** |
| Unique TS team_ids | 25 |
| TS 候选事件总数 | **103** (vs EF TopN=5 的 1845 — **94% 减少**) |
| **TS competition_id 分布**（16 个 matched 事件） | **100% 集中 `p3glrw7hwjlqdyj`** |
| 其他出现的 competition_id（fetch 阶段）| `p3glrw7hwjlqdyj:69` `j1l4rjnh66nm7vx:18` `4zp5rzghl0q82w1:5` `56ypq3nh7pdmd7o:5` `9k82rekhwgnrepz:3` |

## 核心结论

**Team-first 路径找到了正确的 TS competition_id `p3glrw7hwjlqdyj`，准确率 100%。**

P0/EF Top-1 都选错的 `9k82rekhwgnrepz`（同 9vjxm8gh4j9r6od）只覆盖 3 个事件 — 是历史
赛季 ID。**真实 2026 Jordan League 赛季是 `p3glrw7hwjlqdyj`（69 个事件）**。

## PoC 局限（事件匹配只 16/78）

- 用 SR→TS team Top-1 + 精确 (home, away) 配对太严格
- 应该展开 top-3 / top-5 候选、允许 (home, away) 双向
- 应该接 EF P3 的完整 scoreEdge（含 alias、levelConfigs、L4b 兜底）

## 推荐落地（PI-006 v1.16）

**Team-first 不替代 EF，而是先于 EF 修正 league 选择**：

```
旧 EF：
   league.MatchLeagueTopN → 5 candidates (Top-1 可能错)
   ↓
   P1 candidate pool
   ↓
   P3 match → 用 Top-N 全展开救援

新 EF（v1.16）：
   league.MatchLeagueTopN → 5 candidates
   ↓
   if league.MatchRule == LEAGUE_NAME_LOW/MED OR
      previous EF run got SUSPECT/edges=0:
       ▶ 启用 team-first：
         1. SR teams → fuzzy match TS teams (sport-wide)
         2. team_ids 拉事件
         3. event-level vote → 找出最多 events 集中的 TS competition_id
         4. 把它作为 league.Top-1 注入 EF
   ↓
   P1 候选池 (用更对的 league)
   ↓
   P3 match (完整 76/78)
```

这把 team-first 当作 **"league 选择的 second opinion"**，不破坏既有 EF 算法。

## 实现路径

1. 已就位的 Go 层组件（v1.15→v1.16 阶段）：
   - `db.GetAllTeamsBriefCtx` ✓
   - `db.GetEventsByTeamIDsInRangeCtx` ✓
   - `matcher.TeamFirstPoolBuilder` ✓（PoC 跑通逻辑）

2. 缺：
   - `runLeagueEvidenceFirst` 内增加"league 回灌"分支：
     ```go
     if league.MatchRule == RuleLeagueNameLow || (efResult.Edges == 0 && first_pass):
         tfPool := teamFirstBuilder.Build(srTeamNames, sport, tMin, tMax, 3)
         topComp := pickMostFrequentComp(tfPool.Candidates)
         if topComp != "" && topComp != current_league.TSCompetitionID:
             // 重跑一次 EF，用 topComp 作为 Top-1
             league.TSCompetitionID = topComp
             league.MatchRule = RuleLeagueTeamFirst  // 新 rule
             retry EF
     ```
   - 加新 `RuleLeagueTeamFirst` 常量
   - PI-006 v1.16 changelog
   - 单测

## 沙箱实测数据存档

- `output/efrun/poc.log` 含完整 stdout
- 复现：`python3 python/team_first_poc.py`（需 setup_tunnel + pymysql）
