// Package matcher — 联赛匹配逻辑和已知联赛映射表
package matcher

import (
	"fmt"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// KnownLeagueMap 已物理清空 (v2.0)。
//
// v1.18 决议（杜绝任何 mapping）→ v2.0 实施。
// 之前的 36 条 entries 全部从代码移除。生产 MatchLeague 不再走 KnownMap 短路，
// 完全依赖名称相似度 + alias 字典 + country 推断算法。
//
// 完整 git history 可恢复历史 entries：git log -p internal/matcher/league.go
// 但**不应**恢复 — mapping 让算法效果失真（PI-006 v1.18, v1.34, v1.40）。
//
// 变量 reference 保留是为了暂时不破坏所有引用代码（v2.1 会进一步删除）。
var KnownLeagueMap = map[string]string{}

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
	// v1.30 P0: 用 EffectiveCountry 补全（ts.CountryName="" 时从 ts.Name 推断，解决 ts_bb_competition
	// 无 host_country 字段的 basketball 场景）
	effectiveTSCountry := EffectiveCountry(ts.CountryName, ts.Name)
	if sr.CategoryName != "" && effectiveTSCountry != "" {
		catNorm := normalizeName(sr.CategoryName)
		cntNorm := normalizeName(effectiveTSCountry)
		locSim := geoSimilarity(catNorm, cntNorm)
		// v1.22 P0-B: alias canonical hit (base ≥ 0.95) 但 country 完全不同（非 international）→
		// 强降到 0.55（低于 NAME_LOW 阈值），避免 Russian Premier League 被 alias 归到
		// English Premier League、Serie A Brazil 被归到 Italian Serie A 这类跨地域错配。
		// 与 SUSPECT 降级（v1.12）独立 —— 这是算法层防护，SUSPECT 是事件层防护。
		if base >= 0.95 && locSim < 0.4 &&
			!lsInternationalCategory(catNorm) && !lsInternationalCategory(cntNorm) {
			return 0.55
		}
		// v1.37/v1.38: alias default_country vs src.category 二次约束（扩 base>=0.70）
		// 攻 Premier League (Egypt) → Laos Premier League / Primera Division Apertura (Argentina) → Spanish La Liga
		// 即便非 alias canonical hit (base<0.95)，只要 base>=NAME_MED 且 ts.country 空且 alias default 跟 src.cat 不一致 → 降到 0.55
		if base >= 0.70 && ts.CountryName == "" && !lsInternationalCategory(catNorm) {
			defCountry := GetLeagueAliasIndex().GetDefaultCountry(ts.Name)
			if defCountry != "" && !lsInternationalCategory(normalizeName(defCountry)) {
				if geoSimilarity(catNorm, normalizeName(defCountry)) < 0.6 {
					return 0.55
				}
			}
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
