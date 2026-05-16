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
