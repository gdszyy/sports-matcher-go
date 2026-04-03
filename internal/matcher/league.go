// Package matcher — 联赛匹配逻辑和已知联赛映射表
package matcher

import (
	"fmt"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// KnownLeagueMap SR tournament_id → TS competition_id 已知映射
// key 格式: "<sport>:<tournament_id>"，避免不同运动类型的 ID 冲突
// 所有 TS ID 均经过数据库实际查询验证
var KnownLeagueMap = map[string]string{
	// ── 足球热门 ──────────────────────────────────────────────────────────
	"football:sr:tournament:17":  "jednm9whz0ryox8", // Premier League (English Premier League)
	"football:sr:tournament:8":   "vl7oqdehlyr510j", // LaLiga (Spanish La Liga)
	"football:sr:tournament:35":  "gy0or5jhg6qwzv3", // Bundesliga
	"football:sr:tournament:23":  "4zp5rzghp5q82w1", // Serie A (Italian Serie A)
	"football:sr:tournament:34":  "yl5ergphnzr8k0o", // Ligue 1 (French Ligue 1)
	"football:sr:tournament:7":   "z8yomo4h7wq0j6l", // UEFA Champions League
	"football:sr:tournament:679": "56ypq3nh0xmd7oj", // UEFA Europa League

	// ── 足球常规 ──────────────────────────────────────────────────────────
	"football:sr:tournament:18":  "l965mkyh32r1ge4", // Championship (English Football League Championship)
	"football:sr:tournament:37":  "vl7oqdeheyr510j", // Eredivisie (Netherlands Eredivisie)
	"football:sr:tournament:238": "gx7lm7phpnm2wdk", // Liga Portugal 2 (Liga Portugal 主联赛 TS 无独立 ID，用 Liga Portugal 2 代替)
	"football:sr:tournament:52":  "8y39mp1h6jmojxg", // Super Lig (Turkish Super League)
	"football:sr:tournament:203": "8y39mp1hwxmojxg", // Russian Premier League
	"football:sr:tournament:11":  "9vjxm8gh22r6odg", // Belgian Pro League
	"football:sr:tournament:242": "kn54qllhg2qvy9d", // MLS (United States Major League Soccer)
	"football:sr:tournament:325": "4zp5rzgh9zq82w1", // Brasileiro Serie A (Brazilian Serie A)
	"football:sr:tournament:955": "z318q66hl1qo9jd", // J1 League (Japanese J1 League)
	"football:sr:tournament:572": "9k82rekh52repzj", // Chinese Super League (Chinese Football Super League)

	// ── 足球冷门 ──────────────────────────────────────────────────────────
	"football:sr:tournament:551": "e4wyrn4hoeq86pv", // Greek Super League
	"football:sr:tournament:44":  "l965mkyhg0r1ge4", // Allsvenskan (Sweden Allsvenskan)
	"football:sr:tournament:48":  "gy0or5jhj6qwzv3", // Eliteserien (Norwegian Eliteserien)
	"football:sr:tournament:63":  "z8yomo4h92q0j6l", // Veikkausliiga (Finnish Veikkausliiga)

	// ── 篮球热门 ──────────────────────────────────────────────────────────
	"basketball:sr:tournament:132": "49vjxm8xt4q6odg", // NBA (National Basketball Association)
	"basketball:sr:tournament:23":  "jednm9ktd5ryox8", // EuroLeague

	// ── 篮球常规 ──────────────────────────────────────────────────────────
	"basketball:sr:tournament:390": "kjw2r02t6xqz84o", // FIBA Basketball Champions League
	"basketball:sr:tournament:176": "v2y8m4ptx1ml074", // VTB United League
	"basketball:sr:tournament:131": "v2y8m4ptdeml074", // Liga ACB (Spain ACB League)
	"basketball:sr:tournament:53":  "x4zp5rzkt1r82w1", // Lega Basket Serie A
	"basketball:sr:tournament:54":  "0l965mk8tom1ge4", // Basketball Bundesliga

	// ── 篮球冷门 ──────────────────────────────────────────────────────────
	"basketball:sr:tournament:955": "ngy0or5gteqwzv3", // CBA (Chinese Basketball Association)
	"basketball:sr:tournament:551": "56ypq3kt0pymd7o", // NBL Australia (Australia NBL Blitz 暂用)
	"basketball:sr:tournament:572": "8y39mp4tgkmojxg", // Liga Argentina (Argentina Liga Nacional)
}

// knownLeagueKey 生成已知映射的 key
func knownLeagueKey(sport, tournamentID string) string {
	return fmt.Sprintf("%s:%s", sport, tournamentID)
}

// MatchLeague 联赛匹配：优先查已知映射，其次名称相似度
func MatchLeague(srTour *db.SRTournament, tsComps []db.TSCompetition) *LeagueMatch {
	result := &LeagueMatch{
		SRTournamentID: srTour.ID,
		SRName:         srTour.Name,
		SRCategory:     srTour.CategoryName,
		Matched:        false,
		MatchRule:      RuleLeagueNoMatch,
	}

	// 1. 已知映射（优先用 sport+id 组合 key）
	mapKey := knownLeagueKey(srTour.Sport, srTour.ID)
	if tsID, ok := KnownLeagueMap[mapKey]; ok {
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
		// 有映射但 tsComps 中没有该 ID（可能是单联赛模式直接注入了 tsComps）
		// 仍然标记为已知映射，TSName 留空
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
		score := leagueNameScore(srTour, &tsComps[i])
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

// leagueNameScore 计算联赛名称相似度（含国家加分）
func leagueNameScore(sr *db.SRTournament, ts *db.TSCompetition) float64 {
	srNorm := normalizeName(sr.Name)
	tsNorm := normalizeName(ts.Name)

	base := jaccardSimilarity(srNorm, tsNorm)

	// 国家/地区名称匹配加分
	if sr.CategoryName != "" && ts.CountryName != "" {
		catNorm := normalizeName(sr.CategoryName)
		cntNorm := normalizeName(ts.CountryName)
		if jaccardSimilarity(catNorm, cntNorm) > 0.6 {
			base = base*0.8 + 0.2
		}
	}

	return base
}
