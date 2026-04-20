// Package matcher — 比赛匹配引擎（多策略匹配 + 联赛级队伍别名学习）
//
// 匹配策略说明（P2 重构后）：
//
//   标准匹配（σ=3600s）：
//     时间分 S_time = exp(-Δt² / 2σ²)，综合分 = 0.30*S_time + 0.70*S_name
//     名称阈值 ≥ 0.40，综合分阈值 ≥ 0.50
//     时差超过 3σ（10800s）时 S_time < 0.011，实际等效于原 L1 窗口
//
//   宽松匹配（σ=10800s）：
//     时间分 S_time = exp(-Δt² / 2σ²)，综合分 = 0.15*S_time + 0.85*S_name
//     名称阈值 ≥ 0.65，综合分阈值 ≥ 0.60
//     时差超过 3σ（32400s ≈ 9h）时 S_time < 0.011
//
//   超宽容差匹配（σ=43200s）：
//     时间分 S_time = exp(-Δt² / 2σ²)，综合分 = 0.0*S_time + 1.0*S_name
//     名称阈值 ≥ 0.75，综合分阈值 ≥ 0.70
//     同日（UTC）约束，解决时区/延期导致的跨日漏匹配
//
//   别名强匹配（σ=43200s，require_alias=true）：
//     时间分 S_time = exp(-Δt² / 2σ²)，综合分 = 0.0*S_time + 1.0*S_name
//     名称阈值 ≥ 0.85，综合分阈值 ≥ 0.80
//     时差上限 72h（259200s），要求至少一支队伍在 TeamAliasIndex 中命中
//
//   L5 唯一性匹配（无时间约束）：
//     名称阈值 ≥ 0.90，时差上限 30 天，TS 候选有且仅有一场
//
//   L4b 球队 ID 精确对兜底（无时间限制）：
//     需要外部传入 teamIDMap，仅在前四级均未命中时激活
//
// v4 变更：
//   - 将 levelConfigs 中的硬性时间分级替换为高斯衰减连续模糊时间窗口
//   - 新增 gaussianTimeFactor(timeDiff, sigma) 函数
//   - eventMatchConfig 新增 sigma 字段（高斯标准差，秒），maxTimeDiffSec 仅作截止上限
//   - 保留 TeamAliasIndex 联赛级别名学习机制（v3 引入）
package matcher

import (
	"math"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// ─────────────────────────────────────────────────────────────────────────────
// TeamAliasIndex — 联赛级队伍别名学习索引
// ─────────────────────────────────────────────────────────────────────────────

const (
	// aliasLearnThreshold 触发别名学习的最低名称相似度，防止低质量匹配污染索引
	aliasLearnThreshold = 0.50
	// aliasMinVotes 写入别名所需的最少投票次数（≥1 即可，防止单次误匹配）
	aliasMinVotes = 1
	// aliasApplyScore 别名命中时返回的相似度分数
	aliasApplyScore = 0.92
)

// TeamAliasIndex 在同一联赛的比赛匹配过程中，动态学习 SR→TS 队伍映射。
// 当 SR 队伍 A 与 TS 队伍 A_bar 在多场比赛中被高置信度匹配，则将其注册为别名对。
// 后续比赛在计算名称相似度时，若命中别名对，直接返回 aliasApplyScore，
// 解决 "Chelsea FC" vs "Chelsea" 等名称细微差异导致置信度偏低的问题。
type TeamAliasIndex struct {
	// votes[srTeamID][tsTeamID] = 投票次数
	votes map[string]map[string]int
	// alias[srTeamID] = tsTeamID（已确认的别名）
	alias map[string]string
	// aliasRev[tsTeamID] = srTeamID（反向索引）
	aliasRev map[string]string
}

// newTeamAliasIndex 创建空的别名索引
func newTeamAliasIndex() *TeamAliasIndex {
	return &TeamAliasIndex{
		votes:    make(map[string]map[string]int),
		alias:    make(map[string]string),
		aliasRev: make(map[string]string),
	}
}

// Learn 从一场已匹配的比赛中学习队伍别名。
// 参数 nameSim 是该场比赛的名称相似度（用于过滤低质量匹配）。
func (idx *TeamAliasIndex) Learn(
	srHomeID, srAwayID string,
	tsHomeID, tsAwayID string,
	srHomeName, srAwayName string,
	tsHomeName, tsAwayName string,
	nameSim float64,
) {
	if nameSim < aliasLearnThreshold {
		return
	}

	// 判断主客场顺序是否一致
	fwd := teamNameSimilarity(srHomeName, tsHomeName) + teamNameSimilarity(srAwayName, tsAwayName)
	rev := teamNameSimilarity(srHomeName, tsAwayName) + teamNameSimilarity(srAwayName, tsHomeName)

	var pairs [][2]string
	if fwd >= rev {
		pairs = [][2]string{{srHomeID, tsHomeID}, {srAwayID, tsAwayID}}
	} else {
		pairs = [][2]string{{srHomeID, tsAwayID}, {srAwayID, tsHomeID}}
	}

	for _, p := range pairs {
		srID, tsID := p[0], p[1]
		if srID == "" || tsID == "" {
			continue
		}
		if idx.votes[srID] == nil {
			idx.votes[srID] = make(map[string]int)
		}
		idx.votes[srID][tsID]++
		if idx.votes[srID][tsID] >= aliasMinVotes {
			idx.alias[srID] = tsID
			idx.aliasRev[tsID] = srID
		}
	}
}

// NameSimWithAlias 计算考虑别名的名称相似度。
// 若 (srTeamID, tsTeamID) 在别名索引中，直接返回 aliasApplyScore；
// 否则回退到原始 Jaccard 相似度。
func (idx *TeamAliasIndex) NameSimWithAlias(
	srTeamID, srName string,
	tsTeamID, tsName string,
) float64 {
	if srTeamID != "" && tsTeamID != "" {
		if idx.alias[srTeamID] == tsTeamID {
			return aliasApplyScore
		}
		if idx.aliasRev[tsTeamID] == srTeamID {
			return aliasApplyScore
		}
	}
	return teamNameSimilarity(srName, tsName)
}

// HasAlias 判断 SR 队伍是否已有注册别名
func (idx *TeamAliasIndex) HasAlias(srTeamID string) bool {
	_, ok := idx.alias[srTeamID]
	return ok
}

// GetTSID 获取 SR 队伍对应的 TS 队伍 ID（若无则返回空字符串）
func (idx *TeamAliasIndex) GetTSID(srTeamID string) string {
	return idx.alias[srTeamID]
}

// Len 返回已注册的别名对数量
func (idx *TeamAliasIndex) Len() int {
	return len(idx.alias)
}

// InjectAlias 直接注入一条已验证的别名对（绕过投票机制，直接写入 alias 映射）。
// 实现 db.AliasIndexLoader 接口，供 AliasStore.LoadIntoIndex 调用，
// 将持久化知识图谱中的历史别名在每次任务开始前注入内存索引。
func (idx *TeamAliasIndex) InjectAlias(srcTeamID, tsTeamID string) {
	if srcTeamID == "" || tsTeamID == "" {
		return
	}
	idx.alias[srcTeamID] = tsTeamID
	idx.aliasRev[tsTeamID] = srcTeamID
}

// ─────────────────────────────────────────────────────────────────────────────
// 高斯衰减时间评分
// ─────────────────────────────────────────────────────────────────────────────

// gaussianTimeFactor 计算基于高斯衰减的时间评分。
//
// 公式：S_time(Δt) = exp(-Δt² / (2σ²))
//
// 特性：
//   - Δt=0 时 S_time=1.0（完美）
//   - Δt=σ 时 S_time≈0.607
//   - Δt=2σ 时 S_time≈0.135
//   - Δt=3σ 时 S_time≈0.011（实际接近 0，等效于原硬性截止）
//
// 与旧版硬性分级的对比：
//   - 旧版：时差超过阈值直接截断（断崖效应）
//   - 新版：时差越大评分越低，但不硬性截断，由 maxTimeDiffSec 作为最终上限
func gaussianTimeFactor(timeDiffSec int64, sigma float64) float64 {
	if sigma <= 0 {
		return 0.0
	}
	dt := float64(timeDiffSec)
	return math.Exp(-(dt * dt) / (2.0 * sigma * sigma))
}

// ─────────────────────────────────────────────────────────────────────────────
// 匹配策略配置（P2 重构：硬性分级 → 高斯衰减连续窗口）
// ─────────────────────────────────────────────────────────────────────────────

// eventMatchConfig 匹配策略配置
type eventMatchConfig struct {
	// sigma 高斯衰减标准差（秒）。sigma=0 表示不使用时间评分（timeWeight 应为 0）。
	sigma float64
	// maxTimeDiffSec 时间差上限（秒）。超过此值直接跳过，-1 表示使用同日（UTC）约束。
	maxTimeDiffSec int64
	// nameThreshold 名称相似度最低门槛（使用别名增强后的值）
	nameThreshold float64
	// timeWeight 时间分在综合分中的权重
	timeWeight float64
	// nameWeight 名称分在综合分中的权重
	nameWeight float64
	// minScore 最低综合分
	minScore float64
	// requireAlias 是否要求至少一支队伍在别名索引中命中（别名强匹配专用）
	requireAlias bool
	// rule 匹配规则标识
	rule MatchRule
}

// levelConfigs P2 重构后的匹配策略列表（按宽松程度从严到宽排列）。
//
// 策略 1 — 标准匹配（原 L1/L2 合并）：
//   σ=3600s，时差上限 21600s（6h），名称阈值 0.40，综合分阈值 0.50
//   高斯衰减使得时差越大时间分越低，自然区分"精确时间"和"宽松时间"场景，
//   无需人工划分 L1/L2 边界。
//
// 策略 2 — 宽松匹配（原 L2 高名称要求部分）：
//   σ=10800s（3h），时差上限 32400s（9h），名称阈值 0.65，综合分阈值 0.60
//
// 策略 3 — 超宽容差匹配（原 L3 同日）：
//   σ=43200s（12h），同日（UTC）约束，名称阈值 0.75，综合分阈值 0.70
//   专门处理时区差异（如 UTC+8 vs UTC）导致的跨日漏匹配。
//
// 策略 4 — 别名强匹配（原 L4）：
//   σ=43200s（12h），时差上限 259200s（72h），名称阈值 0.85，综合分阈值 0.80
//   require_alias=true，防止跨联赛误匹配。
var levelConfigs = []eventMatchConfig{
	{
		// 策略 1：标准匹配（σ=3600s，时差上限 6h）
		// 等效于原 L1（精确时间）+ L2（宽松时间）的连续版本。
		// 时差 0s → S_time=1.0；时差 3600s → S_time≈0.607；时差 7200s → S_time≈0.135
		sigma:          3600,
		maxTimeDiffSec: 21600,
		nameThreshold:  0.40,
		timeWeight:     0.30,
		nameWeight:     0.70,
		minScore:       0.50,
		requireAlias:   false,
		rule:           RuleEventL1,
	},
	{
		// 策略 2：宽松匹配（σ=10800s，时差上限 9h）
		// 专为名称高度一致但时间偏移较大（如直播延迟、时区偏移）的场景设计。
		// 时差 0s → S_time=1.0；时差 10800s → S_time≈0.607；时差 21600s → S_time≈0.135
		sigma:          10800,
		maxTimeDiffSec: 32400,
		nameThreshold:  0.65,
		timeWeight:     0.15,
		nameWeight:     0.85,
		minScore:       0.60,
		requireAlias:   false,
		rule:           RuleEventL2,
	},
	{
		// 策略 3：超宽容差匹配（σ=43200s，同日 UTC 约束）
		// 专为跨日时区差异（如 UTC vs UTC+8）设计，时间权重为 0，完全依赖名称。
		// 同日约束（UTC）防止跨日误匹配。
		sigma:          43200,
		maxTimeDiffSec: -1, // 同日（UTC）约束
		nameThreshold:  0.75,
		timeWeight:     0.0,
		nameWeight:     1.0,
		minScore:       0.70,
		requireAlias:   false,
		rule:           RuleEventL3,
	},
	{
		// 策略 4：别名强匹配（σ=43200s，时差上限 72h，require_alias=true）
		// 专为赛程延期（>24h）场景设计，要求别名索引命中防止误匹配。
		// 时差 0s → S_time=1.0；时差 43200s → S_time≈0.607；时差 86400s → S_time≈0.135
		sigma:          43200,
		maxTimeDiffSec: 259200, // 72 小时
		nameThreshold:  0.85,
		timeWeight:     0.0,
		nameWeight:     1.0,
		minScore:       0.80,
		requireAlias:   true,
		rule:           RuleEventL4,
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// MatchEvents — 主入口
// ─────────────────────────────────────────────────────────────────────────────

// MatchEvents 对 SR 比赛列表执行多策略匹配（策略 1/2/3/4 + L5 唯一性 + L4b 球队ID兜底）。
//
// teamIDMap: SR team_id → TS team_id，用于激活 L4b（为空则跳过 L4b）。
// 策略 4（别名强匹配）由内部 TeamAliasIndex 驱动，无需外部传入。
func MatchEvents(
	srEvents []db.SREvent,
	tsEvents []db.TSEvent,
	srTeamNames map[string]string,
	tsTeamNames map[string]string,
	teamIDMap map[string]string,
) []EventMatch {
	// @section:init_state - 初始化匹配状态（usedTSIDs、results、aliasIdx）
	usedTSIDs := make(map[string]bool)
	results := make([]EventMatch, 0, len(srEvents))
	aliasIdx := newTeamAliasIndex() // 联赛级别名索引，随匹配过程动态学习

	// @section:multi_level_match - 逐条 SR 比赛执行策略 1/2/3/4 + L5 + L4b 匹配
	for _, sr := range srEvents {
		em := EventMatch{
			SREventID:   sr.ID,
			SRStartTime: sr.StartTime,
			SRStartUnix: sr.StartUnix,
			SRHomeName:  sr.HomeName,
			SRHomeID:    sr.HomeID,
			SRAwayName:  sr.AwayName,
			SRAwayID:    sr.AwayID,
			Matched:     false,
			MatchRule:   RuleEventNoMatch,
		}

		// @section:strategy_1_to_4 - 策略 1/2/3/4 逐级尝试（策略 4 依赖 aliasIdx）
		matched := false
		for _, cfg := range levelConfigs {
			if m, ok := tryMatchLevel(sr, tsEvents, srTeamNames, tsTeamNames, cfg, usedTSIDs, aliasIdx); ok {
				em = m
				usedTSIDs[m.TSMatchID] = true

				// 从成功匹配的比赛中学习队伍别名
				nameSim := (teamNameSimilarity(sr.HomeName, tsTeamNames[m.TSHomeID]) +
					teamNameSimilarity(sr.AwayName, tsTeamNames[m.TSAwayID])) / 2.0
				aliasIdx.Learn(
					sr.HomeID, sr.AwayID,
					m.TSHomeID, m.TSAwayID,
					sr.HomeName, sr.AwayName,
					tsTeamNames[m.TSHomeID], tsTeamNames[m.TSAwayID],
					nameSim,
				)

				matched = true
				break
			}
		}

		// @section:l5_unique_match - L5 无时间约束唯一性匹配（策略 1~4 均未命中时激活）
		// L5: 无时间约束的唯一性匹配（策略 1~4 均未命中时激活）
		// 规则：名称相似度 ≥ 0.90 且主客场顺序一致，且 TS 侧满足条件的候选有且仅有一场。
		// 时差上限：30 天（防止跨赛季误匹配）。
		if !matched {
			if m, ok := tryMatchL5(sr, tsEvents, srTeamNames, tsTeamNames, usedTSIDs, aliasIdx); ok {
				em = m
				usedTSIDs[m.TSMatchID] = true
				matched = true
			}
		}

		// @section:l4b_team_id_fallback - L4b 球队ID 精确对兜底
		// L4b: 球队 ID 精确对兜底（仅当 teamIDMap 非空且前四级未命中）
		if !matched && len(teamIDMap) > 0 {
			if m, ok := tryMatchL4b(sr, tsEvents, teamIDMap, usedTSIDs); ok {
				em = m
				usedTSIDs[m.TSMatchID] = true
			}
		}

		results = append(results, em)
	}

	return results
}

// ─────────────────────────────────────────────────────────────────────────────
// tryMatchLevel — 通用匹配策略执行（支持高斯衰减时间评分 + 别名增强）
// ─────────────────────────────────────────────────────────────────────────────

// tryMatchLevel 尝试某一匹配策略（支持高斯衰减时间评分 + 别名增强）
func tryMatchLevel(
	sr db.SREvent,
	tsEvents []db.TSEvent,
	srTeamNames, tsTeamNames map[string]string,
	cfg eventMatchConfig,
	usedTSIDs map[string]bool,
	aliasIdx *TeamAliasIndex,
) (EventMatch, bool) {
	bestScore := -1.0
	var bestTS *db.TSEvent
	var bestTimeDiff int64

	srDay := unixToUTCDay(sr.StartUnix)

	for i := range tsEvents {
		ts := &tsEvents[i]
		if usedTSIDs[ts.ID] {
			continue
		}

		timeDiff := sr.StartUnix - ts.MatchTime
		if timeDiff < 0 {
			timeDiff = -timeDiff
		}

		// 时间窗口检查
		if cfg.maxTimeDiffSec >= 0 {
			// 硬性上限截止（高斯衰减的最终兜底，防止极端偏移误匹配）
			if timeDiff > cfg.maxTimeDiffSec {
				continue
			}
		} else {
			// 同日（UTC）约束（策略 3 专用）
			tsDay := unixToUTCDay(ts.MatchTime)
			if srDay != tsDay {
				continue
			}
		}

		// 别名强匹配额外约束：至少一支队伍在别名索引中命中（策略 4 专用）
		if cfg.requireAlias {
			homeAliasHit := aliasIdx.HasAlias(sr.HomeID) &&
				(aliasIdx.GetTSID(sr.HomeID) == ts.HomeID || aliasIdx.GetTSID(sr.HomeID) == ts.AwayID)
			awayAliasHit := aliasIdx.HasAlias(sr.AwayID) &&
				(aliasIdx.GetTSID(sr.AwayID) == ts.HomeID || aliasIdx.GetTSID(sr.AwayID) == ts.AwayID)
			if !homeAliasHit && !awayAliasHit {
				continue
			}
		}

		// 使用别名增强的名称相似度（仅正向：主对主、客对客）
		// 禁止主客场反转匹配：若正向得分不满足阈值，直接跳过，不尝试反向。
		// 反向匹配会导致主客场标注错误的比赛被错误命中，降低数据质量。
		homeSimFwd := aliasIdx.NameSimWithAlias(sr.HomeID, sr.HomeName, ts.HomeID, tsTeamNames[ts.HomeID])
		awaySimFwd := aliasIdx.NameSimWithAlias(sr.AwayID, sr.AwayName, ts.AwayID, tsTeamNames[ts.AwayID])
		nameSim := (homeSimFwd + awaySimFwd) / 2.0

		if nameSim < cfg.nameThreshold {
			continue
		}

		// 高斯衰减时间评分（P2 新增）
		// 当 timeWeight=0 时（策略 3/4），时间分不参与综合分计算，但高斯值仍可用于排序
		timeFactor := 0.0
		if cfg.timeWeight > 0 && cfg.sigma > 0 {
			timeFactor = gaussianTimeFactor(timeDiff, cfg.sigma)
		}
		score := cfg.timeWeight*timeFactor + cfg.nameWeight*nameSim

		if score >= cfg.minScore && score > bestScore {
			bestScore = score
			bestTS = ts
			bestTimeDiff = timeDiff
		}
	}

	if bestTS == nil {
		return EventMatch{}, false
	}

	em := buildEventMatch(sr, *bestTS, tsTeamNames, cfg.rule, bestScore, bestTimeDiff)
	return em, true
}

// ─────────────────────────────────────────────────────────────────────────────
// tryMatchL5 — 无时间约束的唯一性匹配
// ─────────────────────────────────────────────────────────────────────────────

const (
	l5NameThreshold = 0.90
	l5MaxTimeDiff   = 30 * 24 * 3600 // 30 天，防止跨赛季误匹配
)

// tryMatchL5 尝试 L5 级别匹配：无时间约束 + 唯一性排他。
// 在 TS 侧所有未使用的比赛中，找到名称高度一致（≥0.90）且主客场顺序正确的候选。
// 满足条件的候选有且仅有一场时才匹配（防止同赛季两回合混淆）。
// 时差上限 30 天，防止跨赛季误匹配。
func tryMatchL5(
	sr db.SREvent,
	tsEvents []db.TSEvent,
	srTeamNames, tsTeamNames map[string]string,
	usedTSIDs map[string]bool,
	aliasIdx *TeamAliasIndex,
) (EventMatch, bool) {
	type candidate struct {
		score    float64
		ts       db.TSEvent
		timeDiff int64
	}
	var candidates []candidate

	for i := range tsEvents {
		ts := &tsEvents[i]
		if usedTSIDs[ts.ID] {
			continue
		}

		// 时差上限预筛：超过 30 天直接跳过
		timeDiff := sr.StartUnix - ts.MatchTime
		if timeDiff < 0 {
			timeDiff = -timeDiff
		}
		if ts.MatchTime > 0 && timeDiff > l5MaxTimeDiff {
			continue
		}

		// 使用别名增强的名称相似度（仅正向：主对主、客对客）
		// 禁止主客场反转：不计算反向得分，仅当正向得分满足阈值时才加入候选。
		homeFwd := aliasIdx.NameSimWithAlias(sr.HomeID, sr.HomeName, ts.HomeID, tsTeamNames[ts.HomeID])
		awayFwd := aliasIdx.NameSimWithAlias(sr.AwayID, sr.AwayName, ts.AwayID, tsTeamNames[ts.AwayID])
		fwdScore := (homeFwd + awayFwd) / 2.0

		// 名称阈值：仅正向得分满足阈值时才加入候选（禁止反向匹配）
		if fwdScore >= l5NameThreshold {
			candidates = append(candidates, candidate{fwdScore, *ts, timeDiff})
		}
	}

	// 唯一性排他：有且仅有一个候选才匹配
	if len(candidates) != 1 {
		return EventMatch{}, false
	}

	c := candidates[0]
	em := buildEventMatch(sr, c.ts, tsTeamNames, RuleEventL5, c.score, c.timeDiff)
	return em, true
}

// ─────────────────────────────────────────────────────────────────────────────
// tryMatchL4b — 球队 ID 精确对兜底（原 L4，重命名为 L4b）
// ─────────────────────────────────────────────────────────────────────────────

// tryMatchL4b L4b: 通过球队 ID 对精确查找（跨时间窗口兜底，无时间限制）。
// 仅在前四级（策略 1/2/3/4）均未命中时激活，需要外部传入 teamIDMap。
func tryMatchL4b(
	sr db.SREvent,
	tsEvents []db.TSEvent,
	teamIDMap map[string]string,
	usedTSIDs map[string]bool,
) (EventMatch, bool) {
	tsHomeID, homeOK := teamIDMap[sr.HomeID]
	tsAwayID, awayOK := teamIDMap[sr.AwayID]
	if !homeOK || !awayOK {
		return EventMatch{}, false
	}

	bestScore := -1.0
	var bestTS *db.TSEvent
	var bestTimeDiff int64

	for i := range tsEvents {
		ts := &tsEvents[i]
		if usedTSIDs[ts.ID] {
			continue
		}
		if ts.HomeID != tsHomeID || ts.AwayID != tsAwayID {
			continue
		}

		timeDiff := sr.StartUnix - ts.MatchTime
		if timeDiff < 0 {
			timeDiff = -timeDiff
		}

		// 基础置信度 0.75，时间越近加分越高（最高 +0.10）
		// 使用高斯衰减替代原线性加成，σ=3600s（1h）
		timeBonus := 0.10 * gaussianTimeFactor(timeDiff, 3600)
		score := 0.75 + timeBonus

		if score > bestScore {
			bestScore = score
			bestTS = ts
			bestTimeDiff = timeDiff
		}
	}

	if bestTS == nil {
		return EventMatch{}, false
	}

	em := buildEventMatch(sr, *bestTS, nil, RuleEventL4b, bestScore, bestTimeDiff)
	return em, true
}

// ─────────────────────────────────────────────────────────────────────────────
// buildEventMatch — 构建 EventMatch 结果
// ─────────────────────────────────────────────────────────────────────────────

// buildEventMatch 构建 EventMatch 结果
func buildEventMatch(
	sr db.SREvent,
	ts db.TSEvent,
	tsTeamNames map[string]string,
	rule MatchRule,
	score float64,
	timeDiff int64,
) EventMatch {
	tsHomeName := ""
	tsAwayName := ""
	if tsTeamNames != nil {
		tsHomeName = tsTeamNames[ts.HomeID]
		tsAwayName = tsTeamNames[ts.AwayID]
	}

	return EventMatch{
		SREventID:   sr.ID,
		SRStartTime: sr.StartTime,
		SRStartUnix: sr.StartUnix,
		SRHomeName:  sr.HomeName,
		SRHomeID:    sr.HomeID,
		SRAwayName:  sr.AwayName,
		SRAwayID:    sr.AwayID,

		TSMatchID:   ts.ID,
		TSMatchTime: ts.MatchTime,
		TSHomeName:  tsHomeName,
		TSHomeID:    ts.HomeID,
		TSAwayName:  tsAwayName,
		TSAwayID:    ts.AwayID,

		Matched:     true,
		MatchRule:   rule,
		Confidence:  math.Round(score*1000) / 1000,
		TimeDiffSec: timeDiff,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 工具函数
// ─────────────────────────────────────────────────────────────────────────────

// unixToUTCDay 将 Unix 时间戳转换为 UTC 日期字符串 "2006-01-02"
func unixToUTCDay(unix int64) string {
	return time.Unix(unix, 0).UTC().Format("2006-01-02")
}
