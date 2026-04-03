// Package matcher — 比赛匹配引擎（四级降级 + L4 球队ID兜底）
package matcher

import (
	"math"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// eventMatchConfig 各级匹配配置
type eventMatchConfig struct {
	maxTimeDiffSec int64   // 时间窗口（秒），-1 表示同日
	nameThreshold  float64 // 名称相似度最低门槛
	timeWeight     float64 // 时间分在综合分中的权重
	nameWeight     float64 // 名称分在综合分中的权重
	minScore       float64 // 最低综合分
	rule           MatchRule
}

var levelConfigs = []eventMatchConfig{
	{
		maxTimeDiffSec: 300,   // ≤5min
		nameThreshold:  0.40,
		timeWeight:     0.30,
		nameWeight:     0.70,
		minScore:       0.50,
		rule:           RuleEventL1,
	},
	{
		maxTimeDiffSec: 21600, // ≤6h
		nameThreshold:  0.65,
		timeWeight:     0.15,
		nameWeight:     0.85,
		minScore:       0.60,
		rule:           RuleEventL2,
	},
	{
		maxTimeDiffSec: -1,   // 同日
		nameThreshold:  0.75,
		timeWeight:     0.0,
		nameWeight:     1.0,
		minScore:       0.70,
		rule:           RuleEventL3,
	},
}

// MatchEvents 对 SR 比赛列表执行四级匹配
// teamIDMap: SR team_id → TS team_id，用于激活 L4（为空则跳过 L4）
func MatchEvents(
	srEvents []db.SREvent,
	tsEvents []db.TSEvent,
	srTeamNames map[string]string,
	tsTeamNames map[string]string,
	teamIDMap map[string]string,
) []EventMatch {
	usedTSIDs := make(map[string]bool)
	results := make([]EventMatch, 0, len(srEvents))

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

		// L1 / L2 / L3 逐级尝试
		matched := false
		for _, cfg := range levelConfigs {
			if m, ok := tryMatchLevel(sr, tsEvents, srTeamNames, tsTeamNames, cfg, usedTSIDs); ok {
				em = m
				usedTSIDs[m.TSMatchID] = true
				matched = true
				break
			}
		}

		// L4: 球队ID精确对兜底（仅当 teamIDMap 非空且前三级未命中）
		if !matched && len(teamIDMap) > 0 {
			if m, ok := tryMatchL4(sr, tsEvents, teamIDMap, usedTSIDs); ok {
				em = m
				usedTSIDs[m.TSMatchID] = true
			}
		}

		results = append(results, em)
	}

	return results
}

// tryMatchLevel 尝试某一级别的匹配
func tryMatchLevel(
	sr db.SREvent,
	tsEvents []db.TSEvent,
	srTeamNames, tsTeamNames map[string]string,
	cfg eventMatchConfig,
	usedTSIDs map[string]bool,
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
			// L3: 同日检查
			tsDay := unixToUTCDay(ts.MatchTime)
			if srDay != tsDay {
				continue
			}
		}

		// 名称相似度
		homeNameSim := teamNameSimilarity(sr.HomeName, tsTeamNames[ts.HomeID])
		awayNameSim := teamNameSimilarity(sr.AwayName, tsTeamNames[ts.AwayID])
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

// tryMatchL4 L4: 通过球队ID对精确查找（跨时间窗口兜底）
func tryMatchL4(
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

	em := buildEventMatch(sr, *bestTS, nil, RuleEventL4, bestScore, bestTimeDiff)
	return em, true
}

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

// unixToUTCDay 将 Unix 时间戳转换为 UTC 日期字符串 "2006-01-02"
func unixToUTCDay(unix int64) string {
	return time.Unix(unix, 0).UTC().Format("2006-01-02")
}
