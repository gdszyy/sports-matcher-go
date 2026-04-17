// Package matcher — LSports ↔ TheSports 匹配引擎
package matcher

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// LSEngine LSports ↔ TheSports 匹配引擎
type LSEngine struct {
	LS         *db.LSAdapter
	TS         *db.TSAdapter
}

// NewLSEngine 创建 LS ↔ TS 匹配引擎
func NewLSEngine(ls *db.LSAdapter, ts *db.TSAdapter) *LSEngine {
	return &LSEngine{LS: ls, TS: ts}
}

// RunLeague 对单个 LSports 联赛执行完整 LS ↔ TS 匹配流程
// lsTournamentID: LSports tournament_id（整数字符串，如 "8363"）
// sport: football / basketball
// tier: hot / regular / cold / unknown
// tsCompetitionID: 预设 TheSports competition_id（可选，空字符串则自动匹配）
func (e *LSEngine) RunLeague(lsTournamentID, sport, tier, tsCompetitionID string) (*LSMatchResult, error) {
	t0 := time.Now()
	result := &LSMatchResult{}

	// ── Step 1: 加载 LSports 联赛 ────────────────────────────────────────
	log.Printf("[LS] [1/4] 联赛匹配: %s", lsTournamentID)
	lsTour, err := e.LS.GetTournament(lsTournamentID)
	if err != nil {
		return nil, fmt.Errorf("GetTournament(LS): %w", err)
	}
	if lsTour == nil {
		return nil, fmt.Errorf("LSports 联赛不存在: %s", lsTournamentID)
	}
	lsTour.Sport = sport

	// ── Step 2: 联赛匹配 ─────────────────────────────────────────────────
	var tsComps []db.TSCompetition
	if tsCompetitionID != "" {
		comp, err := e.TS.GetCompetition(tsCompetitionID, sport)
		if err == nil && comp != nil {
			tsComps = []db.TSCompetition{*comp}
		} else {
			// 即使查不到也允许继续，后续会用 tsCompetitionID 直接查比赛
			tsComps = []db.TSCompetition{{ID: tsCompetitionID, Name: "", Sport: sport}}
		}
	} else {
		switch sport {
		case "football":
			tsComps, err = e.TS.GetCompetitionsByFootball()
		case "basketball":
			tsComps, err = e.TS.GetCompetitionsByBasketball()
		default:
			return nil, fmt.Errorf("不支持的运动类型: %s", sport)
		}
		if err != nil {
			return nil, fmt.Errorf("GetCompetitions(TS): %w", err)
		}
	}

	leagueMatch := matchLSLeague(lsTour, tsComps)
	result.League = leagueMatch
	log.Printf("[LS]   → %s %s → %s  rule=%s  conf=%.3f",
		boolIcon(leagueMatch.Matched), leagueMatch.LSName, leagueMatch.TSName,
		leagueMatch.MatchRule, leagueMatch.Confidence)

	if !leagueMatch.Matched {
		result.Stats = computeLSStats(result, sport, tier, time.Since(t0))
		return result, nil
	}

	tsCompID := leagueMatch.TSCompetitionID

	// ── Step 3: 加载比赛数据 ─────────────────────────────────────────────
	log.Printf("[LS] [2/4] 加载比赛数据...")
	lsEvents, err := e.LS.GetEvents(lsTournamentID)
	if err != nil {
		return nil, fmt.Errorf("GetEvents(LS): %w", err)
	}
	tsEvents, err := e.TS.GetEvents(tsCompID, sport)
	if err != nil {
		return nil, fmt.Errorf("GetEvents(TS): %w", err)
	}
	log.Printf("[LS]   LS 比赛: %d, TS 比赛: %d", len(lsEvents), len(tsEvents))

	// ── Step 4: 加载球队名称 ─────────────────────────────────────────────
	log.Printf("[LS] [3/4] 加载球队名称...")
	lsTeamNames, err := e.LS.GetTeamNames(lsTournamentID)
	if err != nil {
		return nil, fmt.Errorf("GetTeamNames(LS): %w", err)
	}
	tsTeamNames, err := e.TS.GetTeamNames(tsCompID, sport)
	if err != nil {
		return nil, fmt.Errorf("GetTeamNames(TS): %w", err)
	}
	log.Printf("[LS]   LS 球队: %d, TS 球队: %d", len(lsTeamNames), len(tsTeamNames))

	// ── Step 5: 比赛匹配（第一轮：L1/L2/L3/L4）──────────────────────────
	log.Printf("[LS] [4/4] 比赛匹配第一轮（L1/L2/L3/L4）...")
	eventMatches := matchLSEvents(lsEvents, tsEvents, lsTeamNames, tsTeamNames, nil)
	l1, l2, l3, l4, l5, _, matched := countLSEventLevels(eventMatches)
	log.Printf("[LS]   → 第一轮: %d/%d [L1=%d, L2=%d, L3=%d, L4=%d, L5=%d]",
		matched, len(eventMatches), l1, l2, l3, l4, l5)

	// 推导球队映射（第一轮）
	teamMappings := deriveLSTeamMappings(eventMatches, lsTeamNames, tsTeamNames)
	log.Printf("[LS]   → 球队映射（第一轮）: %d 条", len(teamMappings))

	// ── Step 6: 比赛匹配（第二轮：L4b 球队ID兜底）───────────────────────
	if len(teamMappings) > 0 {
		teamIDMap := make(map[string]string, len(teamMappings))
		for _, tm := range teamMappings {
			if tm.TSTeamID != "" {
				teamIDMap[tm.LSTeamID] = tm.TSTeamID
			}
		}
		log.Printf("[LS] [4b] 比赛匹配第二轮（L4b 球队ID兜底）...")
		eventMatches = matchLSEvents(lsEvents, tsEvents, lsTeamNames, tsTeamNames, teamIDMap)
		_, _, _, _, _, l4b, matched2 := countLSEventLevels(eventMatches)
		log.Printf("[LS]   → 第二轮: %d/%d [L4b新增=%d]", matched2, len(eventMatches), l4b)

		teamMappings = deriveLSTeamMappings(eventMatches, lsTeamNames, tsTeamNames)
		log.Printf("[LS]   → 球队映射（第二轮）: %d 条", len(teamMappings))
	}

	result.Events = eventMatches
	result.Teams = teamMappings
	result.Stats = computeLSStats(result, sport, tier, time.Since(t0))
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 联赛匹配
// ─────────────────────────────────────────────────────────────────────────────

// KnownLSLeagueMap LS tournament_id → TS competition_id 已知映射
// key 格式: "sport:ls_tournament_id"，如 "football:8363"
var KnownLSLeagueMap = map[string]string{
	// ── 足球热门 ──────────────────────────────────────────────────────────
	// LS tournament_id 与 SR 完全不同，需要根据 LSports 数据库实际值配置
	"football:67":    "jednm9whz0ryox8", // Premier League (England)
	"football:8363":  "vl7oqdehlyr510j", // LaLiga (Spain)
	"football:61":    "yl5ergphnzr8k0o", // Ligue 1 (France)
	"football:66":    "gy0or5jhg6qwzv3", // 2.Bundesliga (Germany) → Bundesliga in TS
	// UEFA 赛事
	"football:32644": "z8yomo4h7wq0j6l", // UEFA Champions League
	"football:30444": "56ypq3nh0xmd7oj", // UEFA Europa League
	// ── 篮球热门 ──────────────────────────────────────────────────────────
	"basketball:132": "49vjxm8xt4q6odg", // NBA
}

func lsLeagueKey(sport, tournamentID string) string {
	return fmt.Sprintf("%s:%s", sport, tournamentID)
}

// matchLSLeague 联赛匹配：优先查已知映射，其次名称相似度
func matchLSLeague(lsTour *db.LSTournament, tsComps []db.TSCompetition) *LSLeagueMatch {
	result := &LSLeagueMatch{
		LSTournamentID: lsTour.ID,
		LSName:         lsTour.Name,
		LSCategory:     lsTour.CategoryName,
		Matched:        false,
		MatchRule:      RuleLeagueNoMatch,
	}

	// 1. 已知映射
	mapKey := lsLeagueKey(lsTour.Sport, lsTour.ID)
	if tsID, ok := KnownLSLeagueMap[mapKey]; ok {
		for _, comp := range tsComps {
			if comp.ID == tsID {
				result.TSCompetitionID = comp.ID
				result.TSName = comp.Name
				result.TSCountry = comp.CountryName
				result.Matched = true
				result.MatchRule = RuleLeagueKnown
				result.Confidence = 1.0
				return result
			}
		}
		// 有映射但 tsComps 中没有该 ID
		result.TSCompetitionID = tsID
		result.Matched = true
		result.MatchRule = RuleLeagueKnown
		result.Confidence = 1.0
		return result
	}

	// 2. 名称相似度匹配（兜底）
	bestScore := 0.0
	var bestComp *db.TSCompetition
	for i := range tsComps {
		score := lsLeagueNameScore(lsTour, &tsComps[i])
		if score > bestScore {
			bestScore = score
			bestComp = &tsComps[i]
		}
	}
	if bestComp != nil && bestScore >= 0.85 {
		result.TSCompetitionID = bestComp.ID
		result.TSName = bestComp.Name
		result.TSCountry = bestComp.CountryName
		result.Matched = true
		result.MatchRule = RuleLeagueNameHi
		result.Confidence = bestScore
	} else if bestComp != nil && bestScore >= 0.70 {
		result.TSCompetitionID = bestComp.ID
		result.TSName = bestComp.Name
		result.TSCountry = bestComp.CountryName
		result.Matched = true
		result.MatchRule = RuleLeagueNameMed
		result.Confidence = bestScore
	} else if bestComp != nil && bestScore >= 0.55 {
		result.TSCompetitionID = bestComp.ID
		result.TSName = bestComp.Name
		result.TSCountry = bestComp.CountryName
		result.Matched = true
		result.MatchRule = RuleLeagueNameLow
		result.Confidence = bestScore
	}
	return result
}

// lsInternationalCategory 判断地区名称是否属于洲际/国际赛事（不应强制约束国家匹配）
func lsInternationalCategory(name string) bool {
	norm := normalizeName(name)
	international := []string{
		"world", "international", "europe", "europa", "asia", "africa",
		"america", "oceania", "concacaf", "conmebol", "afc", "caf",
		"uefa", "fifa", "south america", "north america", "central america",
	}
	for _, kw := range international {
		if norm == kw || jaccardSimilarity(norm, kw) >= 0.8 {
			return true
		}
	}
	return false
}

// lsLocationVeto 判断两个地区名称是否明显不匹配（强约束否决）
// 返回 true 表示应否决该匹配（跨国误匹配）
func lsLocationVeto(lsCategory, tsCountry string) bool {
	// 如果任一侧为空，不否决（信息不足时保守处理）
	if lsCategory == "" || tsCountry == "" {
		return false
	}
	// 洲际/国际赛事不约束国家
	if lsInternationalCategory(lsCategory) || lsInternationalCategory(tsCountry) {
		return false
	}
	catNorm := normalizeName(lsCategory)
	cntNorm := normalizeName(tsCountry)
	// 相似度低于 0.4 时否决（避免如 Libya vs Laos 的跨国误匹配）
	return jaccardSimilarity(catNorm, cntNorm) < 0.4
}

// lsLeagueNameScore 计算 LS 联赛与 TS 联赛的名称相似度
// 改进（TODO-002 P0）：引入六维强约束一票否决，使用 nameSimilarityMax（Jaccard+JW）替代纯 Jaccard
func lsLeagueNameScore(ls *db.LSTournament, ts *db.TSCompetition) float64 {
	// 强约束：地区明显不匹配时直接否决
	if lsLocationVeto(ls.CategoryName, ts.CountryName) {
		return 0.0
	}

	// 六维强约束一票否决（性别、年龄段、区域分区、赛制类型、层级数字）
	lsFeatures := ExtractLeagueFeatures(ls.Name)
	tsFeatures := ExtractLeagueFeatures(ts.Name)
	// 名称相似度用于确定置信度等级（hi/med/low）
	base := nameSimilarityMax(ls.Name, ts.Name)
	confLevel := "low"
	if base >= 0.85 {
		confLevel = "hi"
	} else if base >= 0.70 {
		confLevel = "med"
	}
	veto := CheckLeagueVeto(lsFeatures, tsFeatures, confLevel)
	if veto.Vetoed {
		return 0.0
	}

	// 国家/地区名称匹配加分（同国加权提升置信度）
	if ls.CategoryName != "" && ts.CountryName != "" {
		catNorm := normalizeName(ls.CategoryName)
		cntNorm := normalizeName(ts.CountryName)
		locSim := jaccardSimilarity(catNorm, cntNorm)
		if locSim >= 0.6 {
			// 同国联赛：名称相似度加权提升
			base = base*0.75 + 0.25*locSim
		}
	}
	return base
}

// ─────────────────────────────────────────────────────────────────────────────
// 比赛匹配（复用 SR↔TS 的多级匹配框架，适配 LSEvent → TSEvent）
// ─────────────────────────────────────────────────────────────────────────────

// matchLSEvents 对 LS 比赛列表执行多级匹配，返回 LSEventMatch 列表
func matchLSEvents(
	lsEvents []db.LSEvent,
	tsEvents []db.TSEvent,
	lsTeamNames, tsTeamNames map[string]string,
	teamIDMap map[string]string, // LS competitor_id → TS team_id（L4b 用）
) []LSEventMatch {
	// 将 LSEvent 转换为通用 SREvent（复用现有匹配逻辑）
	srEvents := make([]db.SREvent, len(lsEvents))
	for i, ls := range lsEvents {
		srEvents[i] = db.SREvent{
			ID:           ls.ID,
			TournamentID: ls.TournamentID,
			StartTime:    ls.StartTime,
			StartUnix:    ls.StartUnix,
			HomeID:       ls.HomeID,
			HomeName:     ls.HomeName,
			AwayID:       ls.AwayID,
			AwayName:     ls.AwayName,
			StatusCode:   ls.StatusID,
		}
	}

	// 调用通用匹配引擎
	srTeamNames := lsTeamNames // LS competitor names 与 SR team names 结构相同
	eventMatches := MatchEvents(srEvents, tsEvents, srTeamNames, tsTeamNames, teamIDMap)

	// 将 EventMatch 转换回 LSEventMatch
	result := make([]LSEventMatch, len(eventMatches))
	for i, em := range eventMatches {
		result[i] = LSEventMatch{
			LSEventID:   em.SREventID,
			LSStartTime: em.SRStartTime,
			LSStartUnix: em.SRStartUnix,
			LSHomeName:  em.SRHomeName,
			LSHomeID:    em.SRHomeID,
			LSAwayName:  em.SRAwayName,
			LSAwayID:    em.SRAwayID,
			TSMatchID:   em.TSMatchID,
			TSMatchTime: em.TSMatchTime,
			TSHomeName:  em.TSHomeName,
			TSHomeID:    em.TSHomeID,
			TSAwayName:  em.TSAwayName,
			TSAwayID:    em.TSAwayID,
			Matched:     em.Matched,
			MatchRule:   em.MatchRule,
			Confidence:  em.Confidence,
			TimeDiffSec: em.TimeDiffSec,
		}
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// 球队映射推导
// ─────────────────────────────────────────────────────────────────────────────

// deriveLSTeamMappings 从比赛匹配结果推导球队映射
func deriveLSTeamMappings(
	events []LSEventMatch,
	lsTeamNames, tsTeamNames map[string]string,
) []LSTeamMapping {
	type vote struct {
		tsID  string
		conf  float64
		count int
	}
	votes := make(map[string]map[string]*vote) // lsID → tsID → vote

	for _, ev := range events {
		if !ev.Matched || ev.TSHomeID == "" {
			continue
		}
		// 主队
		if ev.LSHomeID != "" && ev.TSHomeID != "" {
			if votes[ev.LSHomeID] == nil {
				votes[ev.LSHomeID] = make(map[string]*vote)
			}
			v := votes[ev.LSHomeID][ev.TSHomeID]
			if v == nil {
				v = &vote{tsID: ev.TSHomeID}
				votes[ev.LSHomeID][ev.TSHomeID] = v
			}
			v.conf += ev.Confidence
			v.count++
		}
		// 客队
		if ev.LSAwayID != "" && ev.TSAwayID != "" {
			if votes[ev.LSAwayID] == nil {
				votes[ev.LSAwayID] = make(map[string]*vote)
			}
			v := votes[ev.LSAwayID][ev.TSAwayID]
			if v == nil {
				v = &vote{tsID: ev.TSAwayID}
				votes[ev.LSAwayID][ev.TSAwayID] = v
			}
			v.conf += ev.Confidence
			v.count++
		}
	}

	var mappings []LSTeamMapping
	for lsID, tsVotes := range votes {
		// 选票数最多（相同则取置信度最高）的 TS 队伍
		var best *vote
		for _, v := range tsVotes {
			if best == nil || v.count > best.count ||
				(v.count == best.count && v.conf > best.conf) {
				best = v
			}
		}
		if best == nil {
			continue
		}
		avgConf := best.conf / float64(best.count)
		mappings = append(mappings, LSTeamMapping{
			LSTeamID:   lsID,
			LSTeamName: lsTeamNames[lsID],
			TSTeamID:   best.tsID,
			TSTeamName: tsTeamNames[best.tsID],
			MatchRule:  RuleTeamDerived,
			Confidence: math.Round(avgConf*1000) / 1000,
			VoteCount:  best.count,
		})
	}
	return mappings
}

// ─────────────────────────────────────────────────────────────────────────────
// 统计
// ─────────────────────────────────────────────────────────────────────────────

func countLSEventLevels(events []LSEventMatch) (l1, l2, l3, l4, l5, l4b, matched int) {
	for _, ev := range events {
		if !ev.Matched {
			continue
		}
		matched++
		switch ev.MatchRule {
		case RuleEventL1:
			l1++
		case RuleEventL2:
			l2++
		case RuleEventL3:
			l3++
		case RuleEventL4:
			l4++
		case RuleEventL5:
			l5++
		case RuleEventL4b:
			l4b++
		}
	}
	return
}

func computeLSStats(r *LSMatchResult, sport, tier string, elapsed time.Duration) LSMatchStats {
	s := LSMatchStats{Sport: sport, Tier: tier}

	if r.League != nil {
		s.LeagueLSName = r.League.LSName
		s.LeagueTSName = r.League.TSName
		s.LeagueMatched = r.League.Matched
		s.LeagueRule = r.League.MatchRule
		s.LeagueConf = r.League.Confidence
	}

	s.EventTotal = len(r.Events)
	confSum := 0.0
	for _, ev := range r.Events {
		if ev.Matched {
			s.EventMatched++
			confSum += ev.Confidence
			switch ev.MatchRule {
			case RuleEventL1:
				s.EventL1++
			case RuleEventL2:
				s.EventL2++
			case RuleEventL3:
				s.EventL3++
			case RuleEventL4:
				s.EventL4++
			case RuleEventL5:
				s.EventL5++
			case RuleEventL4b:
				s.EventL4b++
			}
		}
	}
	if s.EventMatched > 0 {
		s.EventMatchRate = math.Round(float64(s.EventMatched)/float64(s.EventTotal)*1000) / 1000
		s.EventAvgConf = math.Round(confSum/float64(s.EventMatched)*1000) / 1000
	}

	s.TeamTotal = len(r.Teams)
	for _, tm := range r.Teams {
		if tm.TSTeamID != "" {
			s.TeamMatched++
		}
	}
	if s.TeamTotal > 0 {
		s.TeamMatchRate = math.Round(float64(s.TeamMatched)/float64(s.TeamTotal)*1000) / 1000
	}

	s.ElapsedMs = elapsed.Milliseconds()
	return s
}
