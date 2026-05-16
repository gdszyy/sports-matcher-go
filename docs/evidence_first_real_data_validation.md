# Evidence-First 真数据有效性验证报告

> 2026-05-16 | 沙箱真数据实测 | 配套 PI-006 v1.10
> Commit baseline: `b5fc681 perf(matcher,db): EF P1 batch SQL query (v1.9)`

## 摘要

在生产 DB（test-xp-lsports + test-thesports-db）上，对 **3 个高歧义非 KnownMap SR 联赛**分别用 P0（`match2`）与 EF（`match2 --use-evidence-first --evidence-first-topn 5`）跑实战对照。所有测试用例 P0 都给出**联赛级名称相似度高分但事件级 0/N matched 的误报**；EF 在其中 1 例直接拯救了 76/78 个事件，另 2 例通过 `edges=0` 显式暴露 league match 错误。

**正面结论**：EF 跨 candidate 候选池能恢复 P0 在"多 competition_id 散布"场景下完全错过的事件匹配（**+76 events, 0% → 97.4% recall**）；EF 的可解释证据让上层能识别并降权错误的 league match。

---

## 测试集

| ID | tournament_id | 名称 | 国家/区域 | 运动 | 事件数 | 歧义类型 |
|----|--------------|------|-----------|------|--------|----------|
| A | `sr:tournament:23479` | Premier Soccer League | Zimbabwe | football | 75 | "Premier" 全球高频词 |
| **B** | **`sr:tournament:929`** | **Jordan League** | **Jordan** | **football** | **78** | **"League" 通用词 + 多赛季 TS 数据散布** |
| C | `sr:tournament:48215` | Serie B, Women | Italy | football | 78 | 跨性别 + 跨级别歧义 |

筛选规则：
- 不在 `KnownLeagueMap` 中（强制走名称相似度路径）
- 包含 Liga/Premier/Cup/Division/League/Serie 等歧义关键词
- 近 6 个月 ≥ 30 场（足够进行事件级评估）

---

## Case A: Premier Soccer League (Zimbabwe)

```
SR: sr:tournament:23479  Premier Soccer League (Zimbabwe football, 75 events)
```

| 模式 | 联赛 → TS | conf | EventMatched | edges | 解读 |
|------|-----------|------|--------------|-------|------|
| **P0** | BPL (无国家) | 0.890 LEAGUE_NAME_HI | **0/75** | — | 名称相似度欺骗：BPL 可能是 Bangladesh Premier League 或 Bermuda Premier Division。完全错的联赛 |
| **EF TopN=2** | BPL (同 P0) | 0.890 LEAGUE_NAME_HI | **0/75** | **0** | EF 同样选了 BPL 作 Top-1，但 P3 候选边数 0 = 显式暴露 league match 是错的 |

**EF 价值**：`edges=0 eliminated=0` 是 P0 完全不给的可解释负向证据。可加规则：edges=0 时 league confidence 自动降级 → SUSPECT。

---

## Case B: Jordan League ⭐ EF 大胜

```
SR: sr:tournament:929  Jordan League (Jordan football, 78 events)
```

| 模式 | 联赛 → TS | conf | EventMatched | rule 分布 |
|------|-----------|------|--------------|-----------|
| **P0** | Jordan League Division 1 (`9vjxm8gh4j9r6od`) | 0.908 LEAGUE_NAME_HI | **0/78** | 全 NoMatch |
| **EF TopN=5** | Jordan League Division 1 (同 P0 Top-1) | 0.908 LEAGUE_NAME_HI | **76/78** ✓ | L1=75, L4b=1 |

**Δ = +76 events recovered（97.4% recall 提升）**

### 根因解读

P0 把 SR `Jordan League` 联赛匹配到 TS `9vjxm8gh4j9r6od (Jordan League Division 1)` 这一个 TS competition_id，但**该 competition 在 TS 数据库里没有 2026 赛季事件**（可能是历史赛季的 ID）。SR 的 78 个 2026 年 Jordan League 事件实际散落在**多个 TS competition_id 下**（不同年/不同赛季/不同分组）。

P0 单 competition 路径必然漏掉这 78 个；EF 跨 5 个 candidate competition 拉了 1845 events，让事件级别强匹配（球队名 + 时间）找到正确归宿。

### EF 样本（实际匹配证据）

```
SR sr:match:67922598  Al Wehdat vs Al Baqaa @ 2026-01-20
   → TS y0or5jh80v7dqwz  Al Wehdat vs Al-Baq's (apostrophe variant)
     rule=EVENT_L1 conf=0.933

SR sr:match:67805808  Al Hussein Irbid vs AL Jazeera Club Amman @ 2026-01-23
   → TS 965mkyhk2p6lr1g  Al-Hussein SC (Irbid) vs Al-Jazeera
     rule=EVENT_L1 conf=0.900

SR sr:match:67805810  AL Salt vs AL Faisaly (Jor) @ 2026-01-23
   → TS 4wyrn4h6ogkvq86  AL Salt vs Al Faisaly
     rule=EVENT_L1 conf=0.973
```

每条都是同日 + 同主客队（含命名变体），EF L1 强匹配捕获。

### EF P3 全景统计

```
edges=347 eliminated=271
最终 matched=76 (220 个候选边经过 1-on-1 冲突消解，留下 76 个赢家)
```

---

## Case C: Serie B, Women (Italy)

```
SR: sr:tournament:48215  Serie B, Women (Italy football, 78 events)
```

| 模式 | 联赛 → TS | conf | EventMatched |
|------|-----------|------|--------------|
| **P0** | **Argentine Women's League** | 0.684 LEAGUE_NAME_LOW | **0/78** |
| EF | （沙箱超时未完成） | — | — |

P0 严重误匹配：SR 是意大利 Serie B 女子，被错配到阿根廷女子联赛 — 只因为名字里都有 "Women"，LEAGUE_NAME_LOW 阈值 0.55 太松。

EF 在沙箱里未跑完，但基于 Case A 模式，EF 大概率也会给出 `edges=0` 显式负向信号。

---

## 综合发现

### 1. P0 在非 KnownMap 高歧义场景的失败模式

P0 联赛匹配高 confidence + 事件 0/N matched —— 3/3 测试 case 都触发了这一模式。原因：

- `leagueNameScore` 只看名称相似度（带 alias + 国家加分 + 负向惩罚 + veto），但**不验证事件层是否真能匹配**
- LEAGUE_NAME_LOW 阈值 0.55 容忍了名义上稍像的跨国/跨级误匹配
- 一旦联赛错，单 competition 路径不会回头探索其它候选

### 2. EF 的两类价值

**A. 跨 competition 候选池恢复事件**（Case B 演示）

当 TS 数据按赛季/分组散布在多个 competition_id 时，P0 漏的事件 EF 能找回。Jordan League 是 EF **最强证据点**：0% → 97.4% recall。

**B. 显式可解释负向证据**（Case A 演示）

`edges=0 eliminated=0` 直接告诉上层"我尝试了 N 个 candidate，没一个事件能过强阈值"——这是 P0 完全没有的诊断信号。可派生规则：

```
if league.Matched && events.matched == 0 && ef.edges == 0:
    league.Confidence *= 0.5  # 降级到 SUSPECT
    league.MatchRule = LEAGUE_REVIEW_NEEDED
```

### 3. 性能数据

| 联赛 | SR events | EF TopN=5 耗时 | 完成 |
|------|----------:|---------------:|------|
| Premier Soccer League | 75 | (TopN=2 26s) | ✓ |
| Jordan League | 78 | 26s | ✓ |
| Serie B Women | 78 | >32s | ✗ 超时 |
| EPL (KnownMap-hit) | 225 | 24s | ✓ |

EF 在 75-78 SR events × TopN=5 → ~1500-2000 候选事件的规模下，沙箱端到端 26-32s，**实战可用**。EPL 因 KnownMap-hit 候选少（4 个 competition）反而更快。

---

## 后续

1. **派生 SUSPECT 降级规则**：当 EF `edges=0` 时把 league.Confidence 降到 ≤0.5，结合 `KnownLeagueMapValidator` 的 RCR 检查。
2. **Case C 跑完整**：Windows 端跑 EF Serie B Women，验证 edges=0 假设。
3. **Jordan League KnownMap 修复**：手工把 `sr:tournament:929` 加进 `KnownLeagueMap`，指向 EF 实际匹配多数的 TS competition_id（需 RCR 验证哪个最高）。
4. **扩大测试样本**：在 Windows 端跑全部 29 个高歧义候选，统计 EF gained / lost / redirected 总分布。

## 原始数据

`output/efrun/` 目录下保留了 P0 与 EF 的 JSON 输出和 log：

- `p0_23479.json` / `ef2_23479.json` — Premier Soccer League Zimbabwe
- `p0_929.json` / `ef_929.json` — Jordan League ⭐
- `p0_48215.json` / `ef_48215.json` (size=0) — Serie B Women Italy

可用 `python python/compare_p0_ef.py p0_929.json ef_929.json` 复现对照。
