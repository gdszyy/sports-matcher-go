// Package matcher — 比赛匹配引擎（五级降级 + 联赛级队伍别名学习）
//
// 匹配级别说明：
//   L1 — 精确时间（时差≤5min）+ 名称验证（≥0.40）
//   L2 — 宽松时间（时差≤6h）  + 强名称验证（≥0.65）
//   L3 — 同日对阵（时差≤24h） + 严格名称验证（≥0.75）
//   L4 — 超宽时间（时差≤72h） + 别名强匹配（≥0.85），require_alias=true
//        专门处理 SR/TS 时间戳来源不同（UTC vs 本地时区、赛程延期）导致时差超过 24h 的漏匹配。
//        要求至少一支队伍已在联赛级 TeamAliasIndex 中注册，防止跨联赛误匹配。
//   L4b — 球队 ID 精确对兜底（无时间限制），仅当 teamIDMap 非空时激活。
//
// v3 新增：
//   - TeamAliasIndex：联赛级队伍别名学习。L1/L2/L3/L4 匹配成功后自动将
//     (sr_team_id → ts_team_id) 写入索引；后续比赛若两队均在索引中，
//     直接返回高置信度分数（aliasApplyScore），不再依赖字面名称相似度。
//   - L4 级别：超宽时间窗口（72h）+ 别名强匹配，解决时区/延期导致的漏匹配。
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

// ─────────────────────────────────────────────────────────────────────────────
// 匹配级别配置
// ─────────────────────────────────────────────────────────────────────────────

// eventMatchConfig 各级匹配配置
type eventMatchConfig struct {
	maxTimeDiffSec int64   // 时间窗口（秒），-1 表示同日
	nameThreshold  float64 // 名称相似度最低门槛（使用别名增强后的值）
	timeWeight     float64 // 时间分在综合分中的权重
	nameWeight     float64 // 名称分在综合分中的权重
	minScore       float64 // 最低综合分
	requireAlias   bool    // 是否要求至少一支队伍在别名索引中命中（L4 专用）
	rule           MatchRule
}

var levelConfigs = []eventMatchConfig{
	{
		// L1: 精确时间（≤5min）+ 名称验证
		maxTimeDiffSec: 300,
		nameThreshold:  0.40,
		timeWeight:     0.30,
		nameWeight:     0.70,
		minScore:       0.50,
		requireAlias:   false,
		rule:           RuleEventL1,
	},
	{
		// L2: 宽松时间（≤6h）+ 强名称验证
		maxTimeDiffSec: 21600,
		nameThreshold:  0.65,
		timeWeight:     0.15,
		nameWeight:     0.85,
		minScore:       0.60,
		requireAlias:   false,
		rule:           RuleEventL2,
	},
	{
		// L3: 同日对阵（≤24h）+ 严格名称验证
		maxTimeDiffSec: -1, // 同日
		nameThreshold:  0.75,
		timeWeight:     0.0,
		nameWeight:     1.0,
		minScore:       0.70,
		requireAlias:   false,
		rule:           RuleEventL3,
	},
	{
		// L4: 超宽时间（≤72h）+ 别名强匹配
		// 专门处理 SR/TS 时间戳来源不同（UTC vs 本地时区、赛程延期）导致时差超过 24h 的漏匹配。
		// require_alias=true：要求至少一支队伍在 TeamAliasIndex 中命中，防止误匹配。
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

// MatchEvents 对 SR 比赛列表执行五级匹配（L1/L2/L3/L4 + L4b 球队ID兜底）。
//
// teamIDMap: SR team_id → TS team_id，用于激活 L4b（为空则跳过 L4b）。
// L4（超宽时间+别名）由内部 TeamAliasIndex 驱动，无需外部传入。
func MatchEvents(
	srEvents []db.SREvent,
	tsEvents []db.TSEvent,
	srTeamNames map[string]string,
	tsTeamNames map[string]string,
	teamIDMap map[string]string,
) []EventMatch {
	usedTSIDs := make(map[string]bool)
	results := make([]EventMatch, 0, len(srEvents))
	aliasIdx := newTeamAliasIndex() // 联赛级别名索引，随匹配过程动态学习

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

		// L1 / L2 / L3 / L4 逐级尝试（L4 依赖 aliasIdx）
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
// tryMatchLevel — L1/L2/L3/L4 通用匹配逻辑
// ─────────────────────────────────────────────────────────────────────────────

// tryMatchLevel 尝试某一级别的匹配（支持别名增强）
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
			if timeDiff > cfg.maxTimeDiffSec {
				continue
			}
		} else {
			// L3: 同日检查（UTC）
			tsDay := unixToUTCDay(ts.MatchTime)
			if srDay != tsDay {
				continue
			}
		}

		// L4 额外约束：至少一支队伍在别名索引中命中
		if cfg.requireAlias {
			homeAliasHit := aliasIdx.HasAlias(sr.HomeID) &&
				(aliasIdx.GetTSID(sr.HomeID) == ts.HomeID || aliasIdx.GetTSID(sr.HomeID) == ts.AwayID)
			awayAliasHit := aliasIdx.HasAlias(sr.AwayID) &&
				(aliasIdx.GetTSID(sr.AwayID) == ts.HomeID || aliasIdx.GetTSID(sr.AwayID) == ts.AwayID)
			if !homeAliasHit && !awayAliasHit {
				continue
			}
		}

		// 使用别名增强的名称相似度
		homeSimFwd := aliasIdx.NameSimWithAlias(sr.HomeID, sr.HomeName, ts.HomeID, tsTeamNames[ts.HomeID])
		homeSimRev := aliasIdx.NameSimWithAlias(sr.HomeID, sr.HomeName, ts.AwayID, tsTeamNames[ts.AwayID])
		awaySimFwd := aliasIdx.NameSimWithAlias(sr.AwayID, sr.AwayName, ts.AwayID, tsTeamNames[ts.AwayID])
		awaySimRev := aliasIdx.NameSimWithAlias(sr.AwayID, sr.AwayName, ts.HomeID, tsTeamNames[ts.HomeID])

		homeNameSim := math.Max(homeSimFwd, homeSimRev)
		awayNameSim := math.Max(awaySimFwd, awaySimRev)
		nameSim := (homeNameSim + awayNameSim) / 2.0

		if nameSim < cfg.nameThreshold {
			continue
		}

		// 综合分
		timeFactor := 0.0
		if cfg.maxTimeDiffSec > 0 {
			timeFactor = 1.0 - float64(timeDiff)/float64(cfg.maxTimeDiffSec)
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
// tryMatchL4b — 球队 ID 精确对兜底（原 L4，重命名为 L4b）
// ─────────────────────────────────────────────────────────────────────────────

// tryMatchL4b L4b: 通过球队 ID 对精确查找（跨时间窗口兜底，无时间限制）。
// 仅在前四级（L1/L2/L3/L4）均未命中时激活，需要外部传入 teamIDMap。
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
		timeBonus := 0.0
		if timeDiff <= 10800 { // ≤3h
			timeBonus = 0.10 * (1.0 - float64(timeDiff)/10800.0)
		}
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
