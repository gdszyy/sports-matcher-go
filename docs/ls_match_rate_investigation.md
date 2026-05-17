# LS 匹配率"差"的真相 — KnownLSLeagueMap 占位 GT 调查 (v1.34)

## 缘起

v1.33 真实池 strict 评估显示：
- SR football Top-1: 92.3%
- **LS football Top-1: 78.0%** (-14.3 pp 比 SR)
- SR basketball Top-1: 40.0%
- **LS basketball Top-1: 30.4%** (-9.6 pp 比 SR)
- **LS 整体: 60.9% vs SR 77.8% (-16.9 pp)**

直觉上"LS 算法比 SR 弱"。本调查推翻这一判断。

## 25 条 LS Top-1 误匹配分类

| 类型 | 数量 | 例子 |
|------|------|------|
| 🔴 **GT 占位 (placeholder)** | **13** | `Basketligan (Sweden) → Basketball Bundesliga (closest)` |
| 真算法 bug（同 alias canonical 多 ts_id 抢占） | 5 | `FNL → Russian Football National League 2` |
| 真算法 bug（字面相似干扰） | 4 | `Ekstraklasa → Poland Mloda Ekstraklasa` |
| 真算法 bug（同 country 多 ts_id） | 3 | `Liga ACB Endesa → Spain Liga EBA` |

**13/25 = 52% 是 GT 本身脏数据**，不是算法错。

## 占位 GT 的真实形态

`KnownLSLeagueMap` 注释里**显式标注 `(closest)` 或 `(alt id)`**：

```go
// internal/matcher/ls_engine.go
"basketball:36":    "0l965mk8tom1ge4", // Basketligan (Sweden) → Basketball Bundesliga (closest)
"basketball:841":   "0l965mk8tom1ge4", // Korisliiga (Finland) → Basketball Bundesliga (closest)
"basketball:12855": "0l965mk8tom1ge4", // Basketligaen (Denmark) → Basketball Bundesliga (closest)
"basketball:7666":  "49vjxm8xt4q6odg", // BSN (Puerto Rico) → National Basketball Association (closest)
"basketball:4379":  "49vjxm8xt4q6odg", // NBL (New Zealand) → National Basketball Association (closest)
"basketball:1834":  "x4zp5rzkt1r82w1", // NBL (Australia) → Lega Basket Serie A (closest)
"basketball:15301": "x4zp5rzkt1r82w1", // NBB (Brazil) → Lega Basket Serie A (closest)
"basketball:2558":  "x4zp5rzkt1r82w1", // Super League (Israel) → Lega Basket Serie A (closest)
"basketball:25357": "v2y8m4ptx1ml074", // Orlen Basket Liga (Poland) → VTB United League (closest)
"basketball:19164": "v2y8m4ptx1ml074", // Liga Nacional de Básquet (Argentina) → VTB United League (closest)
"basketball:34184": "ngy0or5gteqwzv3", // B.League - B1 (Japan) → Chinese Basketball Association (closest)
"basketball:21272": "7p4jwq25t6q0veo", // LNB Elite (France) → Ligue Nationale de Basket Pro A (closest)
"basketball:132":   ...,              // NBA (alt id)
```

含义："生产 TS DB **没有**对应国家的 basketball 联赛 ts_id，先用最接近的占位"。

## v1.34 修复

`wide_baseline.py` 加 `GT_PLACEHOLDER` 排除：注释含 `(closest)` 或 `(alt id)` → 不计入 evaluable。

## 排除 12 条占位后 LS 真实算法能力

| 指标 | v1.33 (含脏 GT) | **v1.34 (纯算法)** | Δ |
|------|----------------|-------------------|---|
| LS 整体 evaluable | 64 | **52** | -12 (排除 placeholder) |
| **LS basketball Top-1** | 30.4% (7/23) | **63.6% (7/11)** | **+33.2 pp** |
| **LS basketball Top-7** | 56.5% | **100.0%** | **+43.5 pp** |
| **LS 整体 Top-1** | 60.9% | **75.0%** | **+14.1 pp** |
| LS 整体 Top-5 | 76.6% | **94.2%** | **+17.6 pp** |
| LS 整体 Top-7 | 81.2% | **96.2%** | **+15 pp** |
| LS football Top-1 | 78.0% | 78.0% (不变) | 0 |

**LS basketball Top-7 = 100%** — 排除占位 GT 后所有真 GT 算法都能"看见"。

## 算法对比修正后真实数字

| 维度 | SR | LS |
|------|----|----|
| football Top-1 | 92.3% | **78.0%** |
| football Top-7 | 96.2% | **95.1%** |
| basketball Top-1 | 40.0% | **63.6%** |
| basketball Top-7 | 70.0% | **100.0%** |
| 整体 Top-1 | 77.8% | **75.0%** |
| 整体 Top-7 | 88.9% | **96.2%** |

**结论**：LS 整体 Top-7 (96.2%) > SR Top-7 (88.9%)，LS basketball 真实能力反而**强于** SR basketball！

之前"LS 匹配率差"是评估集脏数据制造的假象。

## v1.35+ 真实可攻方向

排除 placeholder 后 LS 12 条真算法 bug：

| 类型 | 例 | 攻击方法 |
|------|-----|---------|
| Alias canonical 多 ts_id 内字面更像兄弟 | FNL → Russian Football National League 2 (而非 Russian Premier League) | tier veto 或 cross-canonical name 字面排序 |
| 同 group 字面更像 prefix 联赛 | Ekstraklasa → Poland Mloda Ekstraklasa | "Mloda"（青年队？）pattern 识别为 youth → Gender 类 veto |
| 同 country 字面相似干扰 | Liga ACB Endesa → Spain Liga EBA | alias group 拆细 |
| 缩写本地化变体 | HNL → Croatian Football League（GT 是 Croatia First Football League 同义但 ts_id 不同）| 数据层 GT 注释更新 |

## 后续

1. **KnownLSLeagueMap 应当审查 12 条 placeholder**：要么删除（不评估），要么补真实 GT ts_id（如生产 DB 增加新联赛）
2. 后续测评一律排除 `(closest)`/`(alt id)` 标注的条目
3. 基础设施 `_placeholder_re` 已加入 `wide_baseline.py`
