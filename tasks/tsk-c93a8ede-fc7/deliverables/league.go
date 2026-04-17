// Package matcher — 联赛匹配逻辑和已知联赛映射表
package matcher

import (
	"fmt"
	"strings"

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

// ─────────────────────────────────────────────────────────────────────────────
// 公共地区/国家匹配工具函数（SR 侧与 LS 侧共用）
// ─────────────────────────────────────────────────────────────────────────────

// internationalCategoryKeywords 洲际/国际赛事关键词列表
var internationalCategoryKeywords = []string{
	"world", "international", "europe", "europa", "asia", "africa",
	"america", "oceania", "concacaf", "conmebol", "afc", "caf",
	"uefa", "fifa", "south america", "north america", "central america",
}

// IsInternationalCategory 判断地区名称是否属于洲际/国际赛事（不应强制约束国家匹配）
// 该函数为公共函数，供 SR 侧和 LS 侧共用（替代 ls_engine.go 中的 lsInternationalCategory）
func IsInternationalCategory(name string) bool {
	norm := normalizeName(name)
	for _, kw := range internationalCategoryKeywords {
		if norm == kw || jaccardSimilarity(norm, kw) >= 0.8 {
			return true
		}
	}
	return false
}

// LocationVeto 判断两个地区名称是否明显不匹配（强约束否决）
// 返回 true 表示应否决该匹配（跨国误匹配）
// 该函数为公共函数，供 SR 侧和 LS 侧共用（替代 ls_engine.go 中的 lsLocationVeto）
func LocationVeto(srcCategory, tsCountry string) bool {
	// 如果任一侧为空，不否决（信息不足时保守处理）
	if srcCategory == "" || tsCountry == "" {
		return false
	}
	// 洲际/国际赛事不约束国家
	if IsInternationalCategory(srcCategory) || IsInternationalCategory(tsCountry) {
		return false
	}
	catNorm := normalizeName(srcCategory)
	cntNorm := normalizeName(tsCountry)
	// 相似度低于 0.4 时否决（避免如 Libya vs Laos 的跨国误匹配）
	return jaccardSimilarity(catNorm, cntNorm) < 0.4
}

// countryCodeVeto 通过结构化 CountryCode 字段判断是否应否决匹配
// 仅在两侧 CountryCode 均非空且不相等时否决（精确否决，优先级高于名称模糊匹配）
// 国际赛事代码（如 "INT"、"UEFA"、"FIFA"）豁免否决
func countryCodeVeto(srcCode, tsCode string) bool {
	if srcCode == "" || tsCode == "" {
		return false
	}
	srcUpper := strings.ToUpper(strings.TrimSpace(srcCode))
	tsUpper := strings.ToUpper(strings.TrimSpace(tsCode))
	if srcUpper == "" || tsUpper == "" {
		return false
	}
	// 国际/洲际代码豁免
	internationalCodes := map[string]bool{
		"INT": true, "UEFA": true, "FIFA": true, "AFC": true, "CAF": true,
		"CONCACAF": true, "CONMEBOL": true, "OFC": true, "FIBA": true,
	}
	if internationalCodes[srcUpper] || internationalCodes[tsUpper] {
		return false
	}
	return srcUpper != tsUpper
}

// locationScore 计算两侧地区/国家的综合相似度分数
// 整合 CategoryName（文本）和 CountryCode（结构化代码）两个维度
// 返回 (locSim float64, hardMatch bool)：
//   - locSim: 地区相似度分数 [0.0, 1.0]
//   - hardMatch: 是否通过结构化代码精确匹配（用于提升加分权重）
func locationScore(srcCategory, srcCode, tsCountry, tsCode string) (locSim float64, hardMatch bool) {
	// 1. 结构化 CountryCode 精确匹配（最高优先级）
	if srcCode != "" && tsCode != "" {
		srcUpper := strings.ToUpper(strings.TrimSpace(srcCode))
		tsUpper := strings.ToUpper(strings.TrimSpace(tsCode))
		if srcUpper != "" && tsUpper != "" {
			if srcUpper == tsUpper {
				return 1.0, true // 代码精确匹配
			}
			// 代码不同但都非空 → 返回低分（不直接否决，由 countryCodeVeto 处理）
			return 0.1, false
		}
	}

	// 2. 文本名称相似度（CategoryName vs CountryName）
	if srcCategory != "" && tsCountry != "" {
		catNorm := normalizeName(srcCategory)
		cntNorm := normalizeName(tsCountry)
		sim := jaccardSimilarity(catNorm, cntNorm)
		return sim, false
	}

	return 0.0, false
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

// leagueNameScore 计算联赛名称相似度（含多维国家/地区匹配）
//
// 改进（TODO-002 P0）：引入六维强约束一票否决，使用 nameSimilarityMax（Jaccard+JW）替代纯 Jaccard
// 改进（多维特征融合）：
//  1. 整合 CountryCode 结构化字段进行精确否决和精确加分
//  2. 将 LocationVeto 强否决应用于 SR 侧（与 LS 侧 lsLocationVeto 对齐）
//  3. 提升地区匹配加分权重：精确代码匹配时 0.30，文本匹配时 0.25（原为 0.20）
func leagueNameScore(sr *db.SRTournament, ts *db.TSCompetition) float64 {
	// ── 第一层：结构化 CountryCode 强否决（最高优先级，精确否决）────────────────
	// 当两侧 CountryCode 均非空且不相等时，直接否决（跨国精确判断）
	if countryCodeVeto(sr.CategoryCountryCode, ts.CountryCode) {
		return 0.0
	}

	// ── 第二层：地区名称强否决（与 LS 侧 lsLocationVeto 对齐）──────────────────
	// CategoryName（SR）vs CountryName（TS）Jaccard < 0.4 时否决
	if LocationVeto(sr.CategoryName, ts.CountryName) {
		return 0.0
	}

	// ── 第三层：六维强约束一票否决（性别、年龄段、区域分区、赛制类型、层级数字）──
	srFeatures := ExtractLeagueFeatures(sr.Name)
	tsFeatures := ExtractLeagueFeatures(ts.Name)

	base := nameSimilarityMax(sr.Name, ts.Name)
	confLevel := "low"
	if base >= 0.85 {
		confLevel = "hi"
	} else if base >= 0.70 {
		confLevel = "med"
	}
	veto := CheckLeagueVeto(srFeatures, tsFeatures, confLevel)
	if veto.Vetoed {
		return 0.0
	}

	// ── 第四层：多维地区/国家匹配加分 ──────────────────────────────────────────
	// 整合 CountryCode（结构化）和 CategoryName/CountryName（文本）两个维度
	locSim, hardMatch := locationScore(sr.CategoryName, sr.CategoryCountryCode, ts.CountryName, ts.CountryCode)
	if hardMatch {
		// 结构化代码精确匹配：更高权重加分（0.30）
		base = base*0.70 + 0.30*locSim
	} else if locSim >= 0.6 {
		// 文本名称相似度匹配：标准权重加分（0.25，原为 0.20）
		base = base*0.75 + 0.25*locSim
	}

	return base
}
