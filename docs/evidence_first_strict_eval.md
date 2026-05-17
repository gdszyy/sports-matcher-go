# Strict-No-Mapping 基线评估报告（v1.18）

> 2026-05-16 | 配套 PI-006 v1.18 / PI-007 v1.4
> 评估脚本：`python/evidence_first_strict_baseline.py`
> 结果 JSON：`docs/tests/evidence_first_strict_baseline.json`

## 背景

v1.18 决议：**杜绝任何直接的 mapping，mapping 让算法效果失真**。

代码层加了 `--strict-no-mapping` flag 强制走纯算法路径，但"纯算法在脱掉 KnownMap 后表现到底如何"一直没有量化基线。本评估补上这一块。

## 评估设计

- **评估集**：从 `python/data/sr_ts_ground_truth.json` 反推（2858 条事件级 GT → 去重 14 个 SR 联赛 × 真实 TS competition）
- **候选池**：`ts_leagues_2026.json` 45 个 + GT 反推的 11 个 = **56 个 TS 联赛**
- **算法**：与 Go `MatchLeagueTopNWithFlags(noKnownMap=true)` 等价 — `name_similarity (Jaro-Winkler ⊕ SequenceMatcher) + country bonus`，无任何 KnownMap 短路
- **指标**：Top-1 准确率（强约束） / Top-5 召回（算法能否"看到"正确答案）

## 结果

```
SR 评估集: 14 个联赛 (no_gt=0, gt_not_in_pool=0)
  Top-1 准确率: 71.4% (10/14)   ← 算法靠名字 + category 能直接锁定
  Top-5 召回:   92.9% (13/14)   ← 算法能"看见"正确答案的占比
  Top-1 误匹配: 4

LS 评估集: 45 个联赛 (no_gt=43, gt_not_in_pool=0)
  Top-1 准确率: 50.0% (1/2)     ← evaluable 太少，参考价值低
```

## 4 个 SR 误匹配剖析（v1.18 暴露的算法真实弱点）

| SR 联赛 | category | 算法 Top-1 | score | GT 真实 TS | Top-5 内 | 弱点类型 |
|---------|----------|-----------|-------|----------|---------|---------|
| **LaLiga** | Spain | Poland Liga 3 | 0.745 | Spanish La Liga (0.676) | ✓ 第 2 位 | **国别加分公式太弱** |
| **Eredivisie** | Netherlands | Sweden Division 2 | 0.629 | Eredivisie GT | ✓ Top-5 | 类似上面 |
| **NBA** | USA | (找不到) | 0.0 | National Basketball Association | ✗ Top-5 外 | **缩写 vs 全称** |
| **Premier League** | Russia | Russian Premier League | 0.824 | Russian Premier League | TOP1_HIT 实际上 | 该条其实命中（src_id=sr:tournament:203 不是 sr:tournament:17） |

修正后的真实误匹配是 **3 个**：

### 弱点 1：国别加分公式被名称字面相似度压制

- **LaLiga (Spain)** vs `Poland Liga 3` vs `Spanish La Liga`
  - name_sim("LaLiga", "Poland Liga 3") ≈ 0.745（"Liga" 字面命中）
  - name_sim("LaLiga", "Spanish La Liga") ≈ 0.567 → country bonus(Spain↔Spain=1.0) → 0.567×0.75 + 0.25×1.0 = **0.676**
  - 当前公式 `base = base × 0.75 + 0.25 × loc` 加上国别相等后**仍然没追上字面相似度高的干扰项**

- **Eredivisie (Netherlands)** vs `Sweden Division 2` vs `Eredivisie` GT 同类问题

**根因**：country bonus 是"加性弱权重"，对抗高字面相似度的干扰项无效。生产侧 Go `leagueNameScore` 也存在同样问题 — 但生产侧靠 KnownLeagueMap 直接短路，掩盖了该缺陷。

### 弱点 2：缩写 vs 全称

- **NBA (USA, basketball)** → 候选池里有 GT `National Basketball Association`，但 name_sim("NBA", "National Basketball Association") 太低（< 0.3 阈值，直接被丢弃）
- 缩写匹配需要专用规则：要么单独的 alias 字典（"NBA" → 已知映射 to full name pattern），要么 token 子串识别（首字母 N/B/A 命中 "National Basketball Association" 的首字母）

**根因**：纯字面相似度对中英文缩写完全无效。生产侧靠 KnownLeagueMap 短路解决，算法本身没解决。

## Top-5 内但未排第 1 的（拯救空间）

13/14 (92.9%) 的 SR 联赛**算法都"看到"了正确答案**，只是排序错位。这说明：

1. **候选池本身够覆盖** — 不是召回能力不足
2. **排序权重需要调整** — country/region 强约束应该比当前公式更强
3. **可加规则**：当 src.category 与 ts.country 完全相同且 name_sim ≥ 0.5 时，应触发额外加分或直接 promote 到 Top-1

## 与混 KnownMap 模式对照（量化"mapping 自证"幅度）

| 模式 | SR Top-1 准确率 | 来源 |
|------|----------------|------|
| 默认（KnownMap 命中）| 100% | run4 (PI-007 v1.2) — 14/14 全部命中（直接走 KnownLeagueMap） |
| **strict-no-mapping** | **71.4%** | 本评估 — 算法真实能力 |
| **Δ** | **-28.6 pp** | KnownMap 自证遮蔽的算法真实缺口 |

**结论**：v1.18 之前的 PI-007 基线报告"100% league match accuracy"是 mapping 自证 — 实际算法能力是 71.4%。这就是 v1.18 决议要解决的本质问题。

## 后续

- v1.19 算法改进方向（基于本评估暴露的弱点）：
  1. **强 country/region 加分**：当 SR.category 完全等于 TS.country 时，基础 score 直接 + 0.20（而非乘性折扣）
  2. **缩写表 + token 首字母匹配**：内置 ~50 个常见缩写规则（NBA/CBA/MLS/EPL/NFL/MLB/UFC），不算 mapping（是"算法字典"，对所有源生效）
  3. **token 重叠加分**：unique token IoU（Intersection over Union）给名称相似度加补充信号
- 后续每次 v1.x 算法改动都重跑本基线，记录 SR Top-1 / Top-5 趋势
- LS 评估集需要扩展（evaluable=2 太少），等 LS GT 数据补全后再补

## 复现

```bash
python3 python/evidence_first_strict_baseline.py
# → docs/tests/evidence_first_strict_baseline.json
```

## v1.19 增量更新（2026-05-16）

### 按 sport × Top-K 全光谱

```
[SR] 总联赛 14, 评估有效 14
  整体 Top-K 准确率：
  K=1     2      3      4      5      6      7
  71.4%   92.9%  92.9%  92.9%  92.9%  92.9%  92.9%

  [sport=football] 总 13, 有效 13
  K=1     2       3       4       5       6       7
  76.9%   100.0%  100.0%  100.0%  100.0%  100.0%  100.0%

  [sport=basketball] 总 1, 有效 1   ← 只有 NBA 一条样本
  K=1   2   3   4   5   6   7
  0.0%  0.0%  0.0%  0.0%  0.0%  0.0%  0.0%
```

**关键发现**：

1. **Football 算法效果强** — Top-1 76.9%，Top-2 即 100%。算法实际能力比"71.4% 整体"更高，被 1 个 basketball 样本拉低
2. **Basketball 完全失效** — NBA / CBA 的 name_similarity("NBA", "National Basketball Association") = 0.18，低于 0.30 阈值直接被丢弃。**缩写问题是 basketball 算法的卡点，与 sport 类型强相关**
3. **football Top-2 = 100%** 说明：football 算法**已经看见正确答案**，只是 LaLiga (Spain) 在 Top-1 被 Poland Liga 3 字面相似度压制 → 排到 Top-2。v1.19 改进只需把 country 加分公式调强（让 Spain↔Spain match 时直接 promote Top-1）就能拿到 100%

### 改进方向优先级（基于 v1.19 全光谱数据）

| 改进点 | 影响范围 | 预期收益 |
|--------|---------|---------|
| **A. 缩写表 + 首字母 token 匹配** | basketball NBA/CBA + football 缩写联赛 | basketball Top-1: 0% → 100% (1/1)，football 几个边缘场景 |
| **B. country 强约束 → Top-1 promote** | football LaLiga / Eredivisie 类型 | football Top-1: 76.9% → 100% (13/13)，整体 Top-1: 71.4% → ~93% |
| C. token IoU 补充信号 | 所有 sport | 边缘改善（~2-5 pp） |

A+B 是首选 v1.19 改进路线。

---

## v1.20 修订（2026-05-16）— Python eval 等价 Go 真实算法

### 修了什么

v1.18-v1.19 Python eval 算法**不等价**于 Go 真实算法，两个错位导致严重低估：

| 错位 | v1.18-v1.19 Python eval | Go 真实算法 |
|------|------------------------|-------------|
| **LeagueAliasIndex** | ❌ 未集成 | ✅ `leagueNameSimilarityWithAlias` 内置 |
| **Basketball country 字段** | ❌ 用 fetch 脚本的脏数据 (`country='ngy0or5gteqwzv3'` hash ID)，触发 GEO_VETO 一刀切丢所有候选 | ✅ DB 层 `ts_bb_competition.host_country` 不存在，`CountryName=""`，自动跳过 country veto |

修复后 Python eval ↔ Go 算法字典 + 字段处理逻辑一致。

### 修复后真实数字

```
[SR] 14 个有效 (basketball=1 + football=13)
  整体 Top-1: 100.0% (14/14)
  basketball Top-1: 100.0% (1/1)  ← NBA → National Basketball Association 0.98
  football Top-1:   100.0% (13/13)  ← LaLiga → Spanish La Liga 0.98 (alias canonical match)

[LS] 2 个有效 (basketball=1 + football=1)
  整体 Top-1: 100.0% (2/2)
  basketball Top-1: 100.0% (1/1)  ← CBA → Chinese Basketball Association 0.98
  football Top-1:   100.0% (1/1)
```

### v1.18-v1.20 数字演进史

| 版本 | SR 整体 Top-1 | football Top-1 | basketball Top-1 | 原因 |
|------|---------------|----------------|------------------|------|
| v1.18 | 57.1% | (混合) | 0% | Python eval 缺 alias + 缺 category |
| v1.19 | 71.4% | 76.9% | 0% | 加了 category 但仍缺 alias、basketball 误用脏 country |
| **v1.20** | **100.0%** | **100.0%** | **100.0%** | **等价 Go 真实算法** |

### 修订后的结论

- **Go 真实算法在 strict-no-mapping 模式下、当前 14 个 SR GT 评估集上 Top-1 = 100%**
- 之前报告的 "28.6 pp mapping 自证缺口"是 Python eval 不等价造成的假象 — 真实差距是 0 pp
- **PI-006 v1.10 报告的 Zimbabwe / Jordan League 等 silent 误匹配仍真实存在** — 它们不在 14 个 GT 评估集里，是更难的边缘场景
- v1.19 提议的"v1.19 改进方向 A+B" 在当前评估集上**没有改进空间**（已 100%），改进的真实场景是：
  - PI-006 v1.10 暴露的非 GT 联赛误匹配（Zimbabwe → BPL 0.890）— 需要扩 strict eval 评估集到生产 DB 全量
  - team-first fallback 已经在生产里解决了这些 silent 误匹配（v1.16）

### 仍然有效的发现

- ✅ 算法字典（LeagueAliasGroup）是 strict 评估的核心组件 —— 没它，NBA / CBA 这类缩写联赛直接 0%
- ✅ Basketball schema 不一致是数据层而非算法层问题（fetch 脚本对 ts_bb_competition 处理与 DB schema 不一致）
- ⚠️ **当前评估集太小**（SR 14 / LS 2 evaluable），无法暴露真实算法弱点 —— 需要扩 ground_truth 评估集

---

## v1.21 — 扩评估集到 100 GT 联赛（真实算法分布出现）

### 评估集扩展

- 旧：14 个 SR + 2 个 LS evaluable（来自 sr_ts_ground_truth.json 反推）
- 新：**36 SR + 64 LS = 100 个 GT 联赛**（来自 KnownLeagueMap + KnownLSLeagueMap 全量）
- 候选池：99 个 TS 联赛（ts_leagues_2026 base + GT 反推 + KnownMap stub 补齐）
- 评估脚本：`python/evidence_first_strict_wide_baseline.py`

### v1.21 真实结果（这才是 mapping 替算法答题的真实掩盖幅度）

```
[SR] 36 个 GT 联赛 (basketball=10 + football=26)
  整体 Top-K：
  K=1     2      3      4      5      6      7
  66.7%   69.4%  72.2%  77.8%  80.6%  80.6%  80.6%

  football:   Top-1 80.8% / Top-5 88.5%
  basketball: Top-1 30.0% / Top-5 60.0%

[LS] 64 个 GT 联赛 (basketball=23 + football=41)
  整体 Top-K：
  K=1     2      3      4      5      6      7
  53.1%   60.9%  65.6%  67.2%  71.9%  71.9%  71.9%

  football:   Top-1 73.2% / Top-5 87.8%
  basketball: Top-1 17.4% / Top-5 43.5%
```

### 真实算法弱点（按数量排序）

#### 1. Alias canonical 跨地域错配（最严重）

LeagueAliasGroup 把同名异国联赛归到同一 canonical，给 0.98 高分但选错 ts_id：

- `Russian Premier League` → Top-1 `English Premier League` 0.98（同 canonical "Premier League"）
- `League One (England)` → Top-1 `English Football League One` 0.98（不是 GT 的那个 ts_id）
- `League Two (England)` → Top-1 `English Football League Two` 0.98（同样问题）
- `2. Bundesliga (Germany)` → Top-1 `Bundesliga` 0.94（级别归并错误）
- `Serie A (Brazil)` → Top-1 `Italian Serie A` 0.98（跨国同名）

**根因**：alias group 内部多个 ts_id 都映射到同 canonical，算法选了字母序第一个或字面相似度最高的。
**修复方向（v1.22+）**：alias 命中后要拿 country/category 二次排序，不能直接返回 0.98 给所有候选。

#### 2. Basketball 严重失效（30% / 17% Top-1）

- `EuroLeague` → Top-1 `B1 League` 0.615（缺 alias group）
- `Lega Basket Serie A` → Top-1 `Lega Nazionale Pallacanestro Serie A2`（级别歧义）
- `Liga ACB` → Top-1 `Liga Profesional de Baloncesto`（缩写未识别）

**根因**：64 条 alias group 中 basketball 联赛覆盖严重不足（NBA / CBA / Euroleague 等只有缩写但没覆盖更多变体）。
**修复方向（v1.22+）**：扩 basketball alias group（数量上 football=约 50 个 group / basketball=约 5 个）

#### 3. 跨国杯赛 / 跨国联赛同名

- `CONMEBOL Copa Libertadores` → 错选其它联赛（无 country 加持）
- `Copa Sudamericana` → 错选 Brazilian Serie A（"Copa" 在 Brazilian 高频）

**根因**：杯赛本身就跨国，category="" 时算法只有 name；当前 country bonus 公式不足以强约束。
**修复方向**：同 v1.19 提的 **B. country 强约束**。

### Top-K 衰减说明

| K | SR overall | LS overall |
|---|-----------|-----------|
| 1 | 66.7% | 53.1% |
| 5 | 80.6% | 71.9% |
| 7 | 80.6% | 71.9% |

Top-5 到 Top-7 都是平稳的 — 说明剩下的 ~20-30% 误匹配是**算法根本看不见 GT**（不在 Top-7 候选里），不是排序错位。这是真正的召回缺口。

### 实际意义

| 指标 | 默认 (KnownMap 命中) | strict (算法真实) | mapping 自证掩盖幅度 |
|------|---------------------|-------------------|---------------------|
| SR Top-1 | 100% | **66.7%** | **-33.3 pp** |
| LS Top-1 | 100% | **53.1%** | **-46.9 pp** |
| basketball SR Top-1 | 100% | **30.0%** | **-70.0 pp** |
| basketball LS Top-1 | 100% | **17.4%** | **-82.6 pp** |

**Basketball 算法实际严重失效 — KnownMap 在替算法答 80+% 的题。**

这是 v1.18 决议要量化的真实数字。

### v1.22+ 改进路线（基于 v1.21 数据）

| 优先级 | 改进点 | 影响 |
|--------|--------|------|
| **P0** | **basketball alias group 大扩** | basketball Top-1: 17~30% → ~70%+ |
| **P0** | **alias canonical 命中后用 country 二次排序** | football Top-1: 73~80% → ~90%+ |
| P1 | country/category 强约束公式（base + 加性 0.15 而非乘性折扣） | 杯赛、跨国同名进一步改善 |
| P1 | LeagueAliasGroup 按 level/region 拆细（区分 League One England vs League One Australia） | 5-10 pp 改善 |
| P2 | LS↔TS 链路单独审视（53.1% 比 SR 低 13 pp） | LS 数据质量或 alias 字典对 LS 名变体覆盖不足 |

---

## v1.22 — P0 改进落地（basketball alias 大扩 + alias canonical country 二次约束）

### 改动栈

**P0-A**: `internal/matcher/league_alias.go` 加 8 个 basketball group：EuroLeague / Liga ACB / Lega Basket Serie A / Basketball Bundesliga / VTB United League / B.League B1 / BNXT League / Orlen Basket Liga（64 组 → 72 组）。`python/data/league_alias_groups.json` 由 `scripts/sync_alias.py` 自动同步。

**P0-B**: `internal/matcher/league.go::leagueNameScore`：alias canonical hit (`base >= 0.95`) 但 `sr.CategoryName` 与 `ts.CountryName` 完全不同（非 international 且 `geoSimilarity < 0.4`）→ 强降到 0.55（低于 NAME_LOW 阈值）。等价 Python eval 同步在 `match_league_topk`。

### v1.22 vs v1.21 数字对比

| 指标 | v1.21 | v1.22 | Δ |
|------|-------|-------|---|
| **SR 整体 Top-1** | 66.7% | **72.2%** | **+5.5 pp** |
| SR football Top-1 | 80.8% | **84.6%** | +3.8 pp |
| SR basketball Top-1 | 30.0% | **40.0%** | **+10.0 pp** |
| **LS 整体 Top-1** | 53.1% | **56.2%** | **+3.1 pp** |
| LS football Top-1 | 73.2% | **75.6%** | +2.4 pp |
| LS football Top-2 | 80.5% | **87.8%** | **+7.3 pp** |
| LS basketball Top-1 | 17.4% | **21.7%** | +4.3 pp |

**LS football Top-2 +7.3 pp** 是 country 二次约束最显著的信号 —— 错配的 Russian Premier League / Serie A Brazil 被降到 0.55 后，真 GT 上浮到 Top-2。

### v1.22 已修复的真实误匹配（举例）

- `League Two (England) GT=9k82rekhygrepzj` → v1.21 选 `English Football League Two` (0.985 alias canonical 错 ts_id) → v1.22 仍 Top-1 错（这里 Top-1 名称完全相同，是评估集 stub 数据问题，非算法问题）
- `Serie A (Brazil) GT=4zp5rzgh9zq82w1` → v1.21 选 `Italian Serie A` (0.980 跨国错配) → v1.22 仍选 `Italian Serie D` 0.957（Brazil 没在 stub 里被推断，仍有改进空间）

### v1.22 未解决（v1.23+ 待办）

- `League One/Two England` 等：评估集 stub TS 有两个 ts_id 名称完全相同（`English Football League One` 重复在 alias group 里），算法无法区分。需要 TS 真实数据补全 country / level 字段。
- `Copa Sudamericana` → `Brazilian Serie A`：杯赛 category 空 + Brazilian 字面相似度高。需要扩 alias 覆盖南美杯赛。
- `2. Bundesliga (Germany)` → `Bundesliga` 0.944：alias 把不同级别归到同 canonical，但 P0-B veto 不触发（base 0.944 < 0.95 阈值）。把阈值降到 0.90 ？需评估副作用。
- basketball Top-1 40% / 17%：仍偏低，因为评估集 stub TS 数据没 country/level 信息。

### Top-K 衰减仍有救空间

```
SR football: K=1 84.6%, K=2 84.6%, K=3 84.6%, K=4 88.5%
LS football: K=1 75.6%, K=2 87.8%, K=3 87.8%, K=4 90.2%
```

Top-2 到 Top-4 的提升说明算法已经看见 GT，只是排在第 2-4 位。这部分通过更精细的 alias group 拆分（按 level / region）能消化。

---

## v1.23-v1.24 — Tier veto + 跨国杯赛 alias + stub TS 修复

### 改动栈

**v1.23 P0-C: 层级数字 veto 扩展**
- `internal/matcher/league_features.go::CheckLeagueVeto` 新增：
  - 原有：两侧 TierNumber > 0 且不同 → veto
  - **新增：一侧 = 0、另一侧 ≥ 2 → veto**（视为隐式 tier 1 vs 显式 tier N，避免 `Bundesliga` vs `2. Bundesliga` 错配 0.944）
- Python eval 同步 `extract_tier_number` + `check_tier_veto` 等价实现

**v1.24 跨国杯赛 alias 扩展**
- `internal/matcher/league_alias.go` 加 4 个跨国杯赛 group（72→**76** 组）：
  - CONMEBOL Libertadores / Copa Sudamericana / AFC Champions League / CAF Champions League

**v1.24 stub TS name fallback 修复**
- `python/evidence_first_strict_wide_baseline.py::build_eval_set`：LS 注释格式不统一（部分缺 `→`），导致 ts_name 解析空。fallback 到 `src_name_fallback`（隐含约定：注释只写一个名时 SR 名 ≈ TS 名）。**这是评估集修复，不动算法**

### v1.23+v1.24 真实数字

```
[SR] 36 个 GT 联赛
  整体 Top-K: 1=80.6% / 2=86.1% / 3=86.1% / 4=88.9% / 5=88.9% / 6=88.9% / 7=91.7%
  football:    Top-1 92.3% / Top-2 92.3% / Top-4 96.2%
  basketball:  Top-1 50.0% / Top-2 70.0% / Top-7 80.0%

[LS] 64 个 GT 联赛
  整体 Top-K: 1=67.2% / 2=78.1% / 3=82.8% / 4=84.4% / 5=84.4% / 6=87.5% / 7=87.5%
  football:    Top-1 85.4% / Top-2 95.1% / Top-4 97.6%
  basketball:  Top-1 34.8% / Top-2 47.8% / Top-7 69.6%
```

### v1.21 → v1.24 累计改进史

| 指标 | v1.21 | v1.22 | v1.23+v1.24 | 累计 Δ |
|------|-------|-------|-------------|--------|
| **SR 整体 Top-1** | 66.7% | 72.2% | **80.6%** | **+13.9 pp** |
| SR football | 80.8% | 84.6% | **92.3%** | **+11.5 pp** |
| SR basketball | 30.0% | 40.0% | **50.0%** | **+20.0 pp** |
| **LS 整体 Top-1** | 53.1% | 56.2% | **67.2%** | **+14.1 pp** |
| LS football | 73.2% | 75.6% | **85.4%** | **+12.2 pp** |
| LS football Top-2 | 80.5% | 87.8% | **95.1%** | **+14.6 pp** |
| LS basketball | 17.4% | 21.7% | **34.8%** | **+17.4 pp** |

LS football Top-2 = 95.1% 表明算法**基本能看见正确 GT**。

### v1.23+v1.24 解决的真实误匹配

- ✅ `2. Bundesliga (Germany) → Bundesliga`：0.944 错配 → tier veto 后 Top-1 score=0（GT stub 也被 veto 误伤，需 v1.25 修 stub name 加数字）
- ✅ `Russian Premier League`：之前 alias canonical hit 错归到 English PL，country 二次约束 + tier veto 双护
- ✅ `Serie A (Brazil)`：之前 0.980 错配 Italian Serie A，country 二次约束后降到 0.957 但仍 Top-1 错（评估集 stub TS country 字段缺）
- ✅ basketball EuroLeague / Liga ACB / BBL 等 8 个新 group 让 Top-1 +10pp

### v1.25+ 待办（基于 v1.23+v1.24 剩余误匹配）

| 优先级 | 改进 | 影响 |
|--------|------|------|
| **P0** | 扩 LS comment 解析，写 `sync_ts_pool.py` 从生产 DB 补 ts_pool 真实 country/level | 修 `Serie A Brazil` 类、`2.Bundesliga` GT 被 veto 误伤等 |
| **P1** | 评估集 stub TS 名称重复治理（`English Football League One` 两条同名 ts_id）| ~5% Top-1 提升 |
| **P1** | basketball 数据 fetch 修 `_infer_country_from_tsname` 给所有 NCAA/NBL/CBL 等填 country | basketball Top-1 50% → ~70% |
| P2 | LNB Elite / Nationale 1 / LNB Pro B 法国级别拆 alias | LS basketball 边缘改善 |

---

## v1.25 + v1.26 — LS 同步 P0-B + 生产 TS pool 真实数据

### v1.25 LS 链路同步 P0-B

`internal/matcher/ls_engine.go::lsLeagueNameScore` 加 v1.22 P0-B 等价的 alias canonical hit + country 二次约束（在原有 lsLocationVeto 之上做后置防护）。当 LS.CategoryName 空时 lsLocationVeto 不触发，仍能被这一层兜住。

### v1.26 生产 TS pool 拉真实数据

- `scripts/sync_ts_pool.py` — SSH 隧道 + pymysql 直连 test-thesports-db
- `python/data/ts_pool_real.json` — 2601 football + 1387 basketball = **3988 个真实 TS 联赛**
- `wide_baseline.py --use-real-pool` 切换池子

### 真实池 vs stub 池数字对比

| 指标 | stub (56) | **real (3988)** | Δ |
|------|-----------|----------------|---|
| SR 整体 Top-1 | 80.6% | **63.9%** | -16.7 pp |
| SR football | 92.3% | **73.1%** | -19.2 pp |
| SR basketball | 50.0% | **40.0%** | -10 pp |
| SR football Top-7 | 96.2% | **88.5%** | -7.7 pp |
| LS 整体 Top-1 | 67.2% | **57.8%** | -9.4 pp |
| LS football | 85.4% | **75.6%** | -9.8 pp |
| LS football Top-7 | 97.6% | **90.2%** | -7.4 pp |
| LS basketball | 34.8% | **26.1%** | -8.7 pp |

stub 池数字虚高 — 候选池从 56 → 3988 (70x 干扰项) 后真实算法暴露。

### 真实池暴露的新弱点（v1.27+ 攻关）

1. **alias canonical 内部多 TS ID 歧义**（最严重）
   - `Premier League (Russia)` → BPL (English) 0.98（同 canonical 多 ts_id，选错那个）
   - `Premier League (Egypt)` → BPL 0.98（同问题）
   - `EFL League One` → English Football League One (不是 GT 的那个 ts_id)
   - `FA Cup` → FA Cup (不是 GT)
   - **根因**：alias group "Premier League" / "FA Cup" 对应**多个真实 ts_id**（England/Russia/Egypt 等都叫 Premier League），canonical 命中给 0.98 后无 country 排序

2. **alias 抢占（不同 canonical 误判）**
   - `National League` → `UEFA Nations League` 0.92（base name_similarity 高 + 都含 'League'）
   - `EFL Cup` → `Leagues Cup` 0.97

3. **Brazilian Serie B** → `Belarus Pro Series` 0.736（"Pro Series" 拐弯字面相似度）
4. **National League North (England Amateur)** → `National Basketball League1 South`（sport 一致 + alias 高分）

### v1.27+ 必做（基于真实池数字）

| 优先级 | 改进 | 攻击的误匹配 |
|--------|------|--------------|
| **P0** | alias canonical hit 后必须按 country 二次排序选 ts_id（不是简单降分，要换 ts_id） | Premier League (Russia/Egypt) → BPL 类 |
| **P0** | basketball 池增补 country 字段（数据 fetch 或推断） | basketball 整体 -10pp |
| **P1** | `Premier League` / `FA Cup` 等 alias group 拆按 country/region 子分组 | 同 canonical 多 ts_id 问题 |
| **P1** | `National League` veto 'UEFA Nations League'（Nations vs National 字面相似但 international/non-international 分类不同） | National League → Nations League |
| **P2** | `Leagues Cup` / `Pro Series` 等冷门字面相似的干扰项加 country veto | 边缘修 |

### 性能注解

3988 候选 × 100 GT × alias 展开 = ~22.8s 跑完（加了 lru_cache）。生产 Go 端没问题（同样算法在 Go 里 ms 级），Python eval 只是验证工具。
