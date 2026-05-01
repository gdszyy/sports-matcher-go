# Evidence-First P0 失败案例追踪

## 1. 文档目的

本文档记录 Evidence-First P0 基线下已经可追踪的失败案例。案例来源包括 `docs/ls_ts_algo_vs_known_2026.md` 中的 LS 纯算法误匹配、`docs/league_match_evaluation_rule.md` 与 `docs/league_guard_keywords.json` 中的强约束样例，以及 `docs/tests/evidence_first_baseline_metrics.json` 中本次离线评估输出的失败原因。每个案例均保留源联赛、错误 TS 联赛、触发规则、误判原因和建议修复方向，供 P1–P5 回归时逐项核对。

> P0 不修复这些失败，也不改动核心匹配算法。后续阶段若修复某类失败，应在保持 SR GT 加权指标稳定的前提下更新本文件的状态与对比结果。

## 2. 联赛名称误吸附案例

联赛名称误吸附指旧流程或纯名称算法把通用词、杯赛词、商业冠名词当作主要证据，忽略国家、级别、性别或赛事类型约束，从而把源联赛吸附到名称相似但语义错误的 TS 联赛。

| 案例 ID | 源链路 | 源联赛 | 错误 TS 联赛 | 触发规则 | 误判原因 | 建议修复方向 |
|---------|--------|--------|--------------|----------|----------|--------------|
| FC-001 | LS→TS 纯算法 | `National League` | `Football Association Community Shield` | `LEAGUE_NAME_HI` / 名称相似 | `National`、`League` 等高频词压过赛事类型；Community Shield 是杯/超级杯性质，不是常规联赛 | P1 提取 `competition_type`，将 shield/super cup/cup 与 league 正赛区分；P2 输出候选时保留类型解释 |
| FC-002 | LS→TS 纯算法 | `CONMEBOL Copa Libertadores` | `CONMEBOL Copa America` | `LEAGUE_NAME_HI` | 同为 CONMEBOL + Copa，但一个是俱乐部洲际杯，一个是国家队洲际杯 | P1 增加 club/national 与 continental scope 强约束；P4 若比赛球队类型证据冲突则降级 |
| FC-003 | LS→TS 纯算法 | `Copa Sudamericana` | `Copa de la Reina Women` | `LEAGUE_NAME_MED` | `Copa` 词面相似，但后者为女子赛事且区域/组织完全不同 | P1 将 Women/gender 和 confederation/region 作为强约束；P2 候选保留性别 veto reason |
| FC-004 | LS→TS 纯算法 | `Major League Soccer` | `MLS ASG` | `LEAGUE_NAME_HI` | 缩写 MLS 命中全明星赛，赛事类型从常规联赛误转为 All-Star | P1 引入 `all_star` 赛事类型 veto；P2 输出正赛与表演赛候选分层 |
| FC-005 | LS→TS 纯算法 | `CBA` | `CBA Draft` | `LEAGUE_NAME_HI` | CBA 字面完全命中，但 Draft 不是联赛正赛 | P1 将 draft 作为强赛事类型特征；P4 对 draft/all-star/cup 与 league 不一致直接人工复核 |

## 3. 跨级别误匹配案例

跨级别误匹配指源联赛与目标 TS 联赛在同一国家或同一名称族内相似，但层级数字或级别语义不同。该类错误尤其容易在 `2.`、`J1/J2`、`Ligue 1/Ligue 2`、`Serie A/Serie B` 中出现。

| 案例 ID | 源链路 | 源联赛 | 错误 TS 联赛 | 触发规则 | 误判原因 | 建议修复方向 |
|---------|--------|--------|--------------|----------|----------|--------------|
| FC-006 | LS→TS 纯算法 | `LaLiga` | `Spanish Segunda División` | `LEAGUE_NAME_HI` | 西班牙一级与二级联赛别名相近，算法未把 division level 作为强约束 | P1 抽取 `tier=1/2` 并强约束；P2 对同国同名族候选输出 level diff |
| FC-007 | LS→TS 纯算法 | `Ligue 1` | `French Ligue 2` | `LEAGUE_NAME_HI` | 数字差异未被充分加权，名称主体高度相似 | P1 提升显式数字与 ordinal token 权重；P4 level conflict 直接降级 |
| FC-008 | LS→TS 纯算法 | `2.Bundesliga` | `Bundesliga` | `LEAGUE_NAME_HI` | `2.` 前缀被归一化弱化，导致二级联赛被吸附到一级联赛 | P1 保留前缀级别 token；候选分中增加 same-tier bonus / cross-tier penalty |
| FC-009 | LS→TS 纯算法 | `Primeira Liga` | `Liga Portugal 2` | `LEAGUE_NAME_HI` | 葡萄牙一级与二级命名接近，level 约束未生效 | P1 建立国家联赛层级词典；P2 输出 `LEVEL_MISMATCH` |
| FC-010 | LS→TS 纯算法 | `J1 League` | `Japanese J2 League` | `LEAGUE_NAME_HI` | J1/J2 缩写只差数字，名称相似度过高 | P1 针对 J/K/B 等短缩写联赛保留数字级别；P5 将 J1/J2 作为固定回归样本 |
| FC-011 | SR→TS 补充样本 | `National 2` | `French Championnat National 2`（候选） | `LEAGUE_KNOWN` 补充候选 | 当前可匹配但无 GT，无法证明候选是否完整正确 | P1–P2 保留为二级/低级别样本，补充人工 GT 后再进入 Precision/Recall 计算 |

## 4. 跨国同名误匹配案例

跨国同名误匹配指 `Premier League`、`Serie A`、`Super League`、`NBL` 等通用名称在多个国家或运动中复用，名称相似度无法独立决定联赛身份。

| 案例 ID | 源链路 | 源联赛 | 错误 TS 联赛 | 触发规则 | 误判原因 | 建议修复方向 |
|---------|--------|--------|--------------|----------|----------|--------------|
| FC-012 | LS→TS 纯算法 | `Serie A`（巴西足球） | `Italian Serie C5` / 意大利 Serie 系列 | `LEAGUE_NAME_HI` | `Serie A` 跨国家复用，国家约束缺失或权重不足 | P1 将 country/region 作为强 veto；P2 候选保留国家证据，P4 汇总时检查球队国家分布 |
| FC-013 | LS→TS 纯算法 | `Premier League`（埃及） | `BPL` 或其他 Premier League 族 | `LEAGUE_NAME_HI` | Premier League 为跨国通用名称，源侧国家未绑定到 TS 国家 | P1 建立 country alias，Egypt 与 England/Bangladesh 等不一致时 veto |
| FC-014 | LS→TS 纯算法 | `Jupiler League`（比利时） | `Israel C League` | `LEAGUE_NAME_MED` | 地理约束未识别 Jupiler League 的比利时归属 | P1 扩展 sponsor/商业冠名到国家映射；P2 候选输出 sponsor-derived country |
| FC-015 | LS→TS 纯算法 | `Eliteserien`（挪威） | `BPL` | `LEAGUE_NAME_LOW/MED` | 完全不同国家，名称弱相似仍进入候选 | P1 对低相似跨国候选设置硬 veto；P2 限制无国家证据候选 Top-N 权重 |
| FC-016 | LS→TS 纯算法 | `NBL`（澳大利亚篮球） | `ENBL` | `LEAGUE_NAME_HI` | NBL/ENBL 缩写相似但所属区域与赛事体系不同 | P1 建立缩写词典与 sport+country 上下文；P4 对缩写候选要求更高 RCR |

## 5. 强约束未覆盖案例

强约束未覆盖指现有流程没有把年龄、性别、赛事类型、国家、层级或商业冠名归属作为足够强的约束。P0 评估集已将这些案例纳入 `high_ambiguity=true`，但部分样本无 KnownMap 或无 GT，后续应优先补齐候选池与人工标注。

| 案例 ID | 源链路 | 源联赛 | 错误 TS 联赛或风险 TS 联赛 | 触发规则 | 误判原因 | 建议修复方向 |
|---------|--------|--------|----------------------------|----------|----------|--------------|
| FC-017 | SR→TS 补充样本 | `U19 DFB Nachwuchsliga` | 未配置 TS competition | `no_known_map` | 旧流程依赖单一 KnownMap，U19 样本无法产生可解释候选 | P1 抽取 age_group=U19；P2 在 TS 候选中只保留 U19/青年队同类赛事 |
| FC-018 | SR→TS 补充样本 | `U19 Division de Honor Juvenil` | `Spanish U19 League`（候选） | `LEAGUE_KNOWN` 补充候选；无 GT | 年龄组一致但缺少人工 GT，无法验证事件级正确性 | 补充人工 GT；P3 输出比赛级 reason code；P4 以 RCR 聚合验证 |
| FC-019 | LS→TS 补充样本 | `Bundesliga U19` | 未配置 TS competition | `no_known_map` | Bundesliga 主体词会吸附成人 Bundesliga，年龄约束必须先行 | P1 年龄组强 veto；若 TS 候选缺 U19，应输出 `NO_AGE_COMPATIBLE_CANDIDATE` |
| FC-020 | SR→TS 补充样本 | `NCAA Women, Regular Season` | `Women's National Collegiate Athletic Association`（候选） | `LEAGUE_KNOWN` 补充候选；无 GT | 女子标签一致但事件级 GT 缺失；若后续候选混入男子 NCAA 则风险极高 | P1 gender 强约束；P2 候选解释必须包含 `GENDER_MATCH` |
| FC-021 | LS→TS 补充样本 | `IPBL. Pro Division - Women` | 未配置 TS competition | `no_known_map` | 商业冠名 + Women，名称主体弱，旧流程无法稳定定位 | P1 同时抽取 sponsor 与 gender；P2 允许人工复核队列而非自动确认 |
| FC-022 | LS→TS 补充样本 | `NBL 1 - Women` | 未配置 TS competition | `no_known_map` | NBL 缩写 + Women + 数字级别三重歧义 | P1 缩写词典需绑定 gender/tier/country；P5 固化该样本 |
| FC-023 | LS→TS 补充样本 | `Meiji Yasuda J2/J3 100 Year Vision League` | 未配置 TS competition | `no_known_map` | 商业冠名和 J2/J3 级别信息复杂，名称相似度易丢失核心级别 | P1 sponsor token 降权但保留 J2/J3 层级；P2 输出 level-compatible candidates |
| FC-024 | LS→TS 补充样本 | `BNXT League` | `B1 League`（历史纯算法误吸附） | `LEAGUE_NAME_HI` | BNXT 与 B1 字面相似，但国家/区域/赛事体系不同 | P1 缩写白名单与区域约束；P4 缩写候选需足够 RCR 支撑 |

## 6. 与基线指标文件的追踪关系

`docs/tests/evidence_first_baseline_metrics.json` 中每个联赛样本都包含 `source`、`source_league_id`、`source_league_name`、`ts_competition_id`、`ts_league_name`、`league_rule`、`failure_reason`、`ambiguity_tags`、规则分布、球队匹配率与 RCR。后续阶段应在不删除历史案例的前提下增加以下字段或并行输出，以便把本文件的失败案例自动化验证：

| 需要补充的追踪字段 | 用途 | 预计阶段 |
|--------------------|------|----------|
| `candidate_rank` / `candidate_score` | 判断错误 TS 联赛是否仍进入 Top-N | P2 |
| `strong_constraint_reason` | 验证 gender/age/level/country/cup veto 是否触发 | P1–P2 |
| `event_reason_codes` | 将比赛证据边与联赛级失败原因关联 | P3 |
| `competition_rcr_by_candidate` | 对多 competition 候选计算反向确认率 | P4 |
| `manual_review_required` | 标记不可自动确认但应进入人工复核队列的样本 | P4–P5 |

## 7. P1 输入条件

P1 开始前，应至少保留以下输入不变：`python/evidence_first_baseline_eval.py`、`docs/tests/evidence_first_baseline_metrics.json`、`docs/evidence_first_baseline_report.md` 与本文档。P1 的核心任务是把本文列出的失败案例转化为可机器判定的强约束和候选解释，而不是直接扩大 KnownMap 绕过问题。
