// Package matcher — 联赛匹配逻辑和已知联赛映射表
package matcher

import (
	"fmt"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// KnownLeagueMap SR tournament_id → TS competition_id 已知映射
// key 格式: "<sport>:<tournament_id>"，避免不同运动类型的 ID 冲突
// 所有 TS ID 均经过数据库实际查询验证
//
// ⚠️ v1.18 决议：本表仅作为**生产链路的运营 override 层**，**严禁参与算法测评循环**。
//
//   - 算法测评必须用 `--strict-no-mapping` flag（main.go）或 Python 侧 evidence_first_strict_baseline.py
//     强制走纯算法路径，本表不会被查询。
//   - 任何"把推断结果回写本表"的便利化想法（v1.18 KnownMap writeback 提议已否决）都是禁止的，
//     mapping 会让算法效果失真（参见 PI-007 v1.4: strict 模式 SR Top-1 71.4% vs 默认 100%，
//     mapping 自证给算法效果造的虚是 28.6 pp）。
//   - 本表可在生产 API 中作为人工 override 使用；本表变更不算"算法改进"。
//
// 关联 CI 检查：`scripts/check_no_mapping_in_eval.sh` — 测评代码引用 KnownLeagueMap
// 直接命中路径会 fail（白名单需加 `// ALLOW-KNOWNMAP-IN-EVAL` 行尾注释）。
var KnownLeagueMap = map[string]string{
	// ── 足球热门 ──────────────────────────────────────────────────────────
	"football:sr:tournament:17":  "jednm9whz0ryox8", // Premier League (English Premier League)
	"football:sr:tournament:8":   "vl7oqdehlyr510j", // LaLiga (Spanish La Liga)
	"football:sr:tournament:35":  "gy0or5jhg6qwzv3", // Bundesliga
	"football:sr:tournament:23":  "4zp5rzghp5q82w1", // Serie A (Italian Serie A)
	"football:sr:tournament:34":  "yl5ergphnzr8k0o", // Ligue 1 (French Ligue 1)
	"football:sr:tournament:7":   "z8yomo4h7wq0j6l", // UEFA Champions League
	"football:sr:tournament:679": "56ypq3nh0xmd7oj", // UEFA Europa League

	// ── 英格兰联赛体系（PI-002 新增）────────────────────────────────────
	// SR 官方名称 "EFL League One" 在 TS 中对应 "League One"
	// 别名索引已内置处理该差异，此处补充已知 SR tournament_id 映射
	"football:sr:tournament:18":  "l965mkyh32r1ge4", // EFL Championship (English Football League Championship)
	"football:sr:tournament:19":  "8y39mp1hjzmojxg", // EFL League One → English Football League One (v1.29 drift fix)
	"football:sr:tournament:20":  "9k82rekhygrepzj", // EFL League Two → English Football League Two (v1.29 drift fix)
	"football:sr:tournament:21":  "z318q66hv8qo9jd", // National League → English National League (v1.29 drift fix)
	"football:sr:tournament:9":   "9vjxm8gh8gr6odg", // FA Cup (v1.29 drift fix)
	"football:sr:tournament:22":  "56ypq3nh5xmd7oj", // EFL Cup (League Cup / Carabao Cup)

	// ── 足球常规 ──────────────────────────────────────────────────────
	// Championship 已移至英格兰联赛体系分组)
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
	"basketball:sr:tournament:138": "jednm9ktd5ryox8", // EuroLeague (Basketball)

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
// 改进（TODO-002 P0）：引入六维强约束一票否决，使用 nameSimilarityMax（Jaccard+JW）替代纯 Jaccard
// 改进（PI-002）：引入别名感知相似度，解决官方名称与常用名称差异较大的问题
// 改进（优化建议 §3.5/3.6）：引入负向特征惩罚和地理别名词典
//
//	典型案例：SR "EFL League One" ↔ TS "League One" 通过别名索引直接命中
func leagueNameScore(sr *db.SRTournament, ts *db.TSCompetition) float64 {
	// 六维强约束一票否决（性别、年龄段、区域分区、赛制类型、层级数字）
	srFeatures := ExtractLeagueFeatures(sr.Name)
	tsFeatures := ExtractLeagueFeatures(ts.Name)

	// PI-002: 使用别名感知相似度替代纯名称相似度
	// leagueNameSimilarityWithAlias 在计算相似度前先做别名展开，
	// 若两侧名称映射到同一规范名称，直接返回 0.98 高置信度
	base := leagueNameSimilarityWithAlias(sr.Name, ts.Name)

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

	// 负向特征惩罚（优化建议 §3.6）
	penalty := CalcFeaturePenalty(srFeatures, tsFeatures)
	if penalty <= 0 {
		return 0.0
	}
	base *= penalty

	// 国家/地区名称匹配加分（优化建议 §3.5）+ alias canonical hit 后 country 二次约束（v1.22 P0-B）
	if sr.CategoryName != "" && ts.CountryName != "" {
		catNorm := normalizeName(sr.CategoryName)
		cntNorm := normalizeName(ts.CountryName)
		locSim := geoSimilarity(catNorm, cntNorm)
		// v1.22 P0-B: alias canonical hit (base ≥ 0.95) 但 country 完全不同（非 international）→
		// 强降到 0.55（低于 NAME_LOW 阈值），避免 Russian Premier League 被 alias 归到
		// English Premier League、Serie A Brazil 被归到 Italian Serie A 这类跨地域错配。
		// 与 SUSPECT 降级（v1.12）独立 —— 这是算法层防护，SUSPECT 是事件层防护。
		if base >= 0.95 && locSim < 0.4 &&
			!lsInternationalCategory(catNorm) && !lsInternationalCategory(cntNorm) {
			return 0.55
		}
		// v1.29 P0-2: alias canonical hit + country 高匹配 → 加分 0.015 让 country 一致的 ts_id 赢
		if base >= 0.95 && locSim >= 0.8 {
			boosted := base + 0.015
			if boosted > 0.999 {
				boosted = 0.999
			}
			return boosted
		}
		if locSim > 0.6 {
			base = base*0.8 + 0.2*locSim
		}
	}

	return base
}

// MatchLeagueWithFlags 是 MatchLeague 的扩展版本（v1.18）：
//
//   - noKnownMap=true 时跳过 KnownLeagueMap，强制走名称相似度路径。
//     用于"严格无 mapping 测评模式"（--strict-no-mapping），目的是
//     测算法本身的能力，避免人工 mapping 表自证有效性。
//
//   - noKnownMap=false 时行为与 MatchLeague 完全一致。
//
// 设计要点：本算法明确杜绝把推断结果回写 KnownLeagueMap（v1.18 决议）；
// 这个表只保留作为生产侧"运营 override 层"，不参与算法测评循环。
func MatchLeagueWithFlags(srTour *db.SRTournament, tsComps []db.TSCompetition, noKnownMap bool) *LeagueMatch {
	if noKnownMap {
		return matchLeagueByName(srTour, tsComps)
	}
	return MatchLeague(srTour, tsComps)
}

// matchLeagueByName 是 MatchLeague 跳过 KnownLeagueMap 的内部版本。
// 直接走名称相似度（与 MatchLeague 中 step 2 起的逻辑等价）。
func matchLeagueByName(srTour *db.SRTournament, tsComps []db.TSCompetition) *LeagueMatch {
	result := &LeagueMatch{
		SRTournamentID: srTour.ID,
		SRName:         srTour.Name,
		SRCategory:     srTour.CategoryName,
		Matched:        false,
		MatchRule:      RuleLeagueNoMatch,
	}
	bestScore := 0.0
	var bestComp *db.TSCompetition
	for i := range tsComps {
		score := leagueNameScore(srTour, &tsComps[i])
		if score > bestScore {
			bestScore = score
			bestComp = &tsComps[i]
		}
	}
	if bestComp == nil || bestScore < 0.55 {
		return result
	}
	result.TSCompetitionID = bestComp.ID
	result.TSName = bestComp.Name
	result.TSCountry = bestComp.CountryName
	result.Matched = true
	result.Confidence = bestScore
	if bestScore >= 0.85 {
		result.MatchRule = RuleLeagueNameHi
	} else if bestScore >= 0.70 {
		result.MatchRule = RuleLeagueNameMed
	} else {
		result.MatchRule = RuleLeagueNameLow
	}
	return result
}
