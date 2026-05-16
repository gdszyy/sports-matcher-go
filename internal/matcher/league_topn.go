// Package matcher — 联赛级 Top-N 候选匹配
//
// 本文件提供 MatchLeague 的 Top-N 变体：返回排名前 N 的 TS competition 候选，
// 而不是单一胜者。这是 Evidence-First 流水线的入口：
//
//	srTour + tsComps  →  []LeagueMatchCandidate (Top-N)
//	                  →  []TSCompetitionCandidate (P1 输入)
//	                  →  CandidatePoolBuilder.Build → []EvidenceEventCandidate
//	                  →  EnrichTeamPriors → 填充球队级先验
//	                  →  EvidenceEventMatcher.MatchTwoRound → 一对一冲突消解
//
// 设计原则：
//   - 不破坏既有 MatchLeague 接口（单胜者路径仍走 MatchLeague）；
//   - KnownLeagueMap 命中始终占据 Top-1，score=1.0，rule=RuleLeagueKnown；
//   - 其余候选按 leagueNameScore 降序排，分数低于 minScore 的丢弃；
//   - 每条候选携带强约束结果（已通过 CheckLeagueVeto 计算），方便 P1 直接消费。
//
// 相关流程洞察：
//   - PI-006 Evidence-First 比赛级匹配流程（v1.3）
//   - PI-001 通用匹配算法设计与五级降级规则

package matcher

import (
	"sort"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// LeagueMatchCandidate 是 Top-N 联赛匹配返回的单条候选。
type LeagueMatchCandidate struct {
	TSCompetitionID        string    `json:"ts_competition_id"`
	TSName                 string    `json:"ts_name"`
	TSCountry              string    `json:"ts_country"`
	Score                  float64   `json:"score"`
	Rule                   MatchRule `json:"rule"`
	StrongConstraintOK     bool      `json:"strong_constraint_ok"`
	StrongConstraintReason string    `json:"strong_constraint_reason,omitempty"`
}

const defaultLeagueTopN = 5
const defaultLeagueTopNMinScore = 0.55

// MatchLeagueTopN 返回排名前 n 的 TS competition 候选（按 score 降序）。
//
// 行为保证：
//  1. n <= 0 → 默认 n=5；
//  2. KnownLeagueMap 命中 → 该候选占 Top-1，score=1.0，Rule=RuleLeagueKnown，
//     即使该 tsID 不在 tsComps 中也保留（与 MatchLeague 语义一致）；
//  3. 其余候选按 leagueNameScore 降序，score >= 0.55 的进入候选集；
//  4. StrongConstraintOK / StrongConstraintReason 来自 CheckLeagueVeto。
//
// 复杂度：O(N_tsComps × leagueNameScore) + O(N log N) 排序。
func MatchLeagueTopN(srTour *db.SRTournament, tsComps []db.TSCompetition, n int) []LeagueMatchCandidate {
	if srTour == nil {
		return nil
	}
	if n <= 0 {
		n = defaultLeagueTopN
	}

	var candidates []LeagueMatchCandidate

	// 1) KnownLeagueMap 命中 → Top-1
	mapKey := knownLeagueKey(srTour.Sport, srTour.ID)
	knownTSID, hasKnown := KnownLeagueMap[mapKey]
	if hasKnown {
		known := LeagueMatchCandidate{
			TSCompetitionID:    knownTSID,
			Score:              1.0,
			Rule:               RuleLeagueKnown,
			StrongConstraintOK: true,
		}
		for _, comp := range tsComps {
			if comp.ID == knownTSID {
				known.TSName = comp.Name
				known.TSCountry = comp.CountryName
				break
			}
		}
		candidates = append(candidates, known)
	}

	// 2) 名称相似度兜底
	type scored struct {
		comp  db.TSCompetition
		score float64
		veto  LeagueVetoResult
	}
	var pool []scored
	srFeatures := ExtractLeagueFeatures(srTour.Name)
	for i := range tsComps {
		if hasKnown && tsComps[i].ID == knownTSID {
			continue
		}
		score := leagueNameScore(srTour, &tsComps[i])
		if score < defaultLeagueTopNMinScore {
			continue
		}
		tsFeatures := ExtractLeagueFeatures(tsComps[i].Name)
		confLevel := "low"
		if score >= 0.85 {
			confLevel = "hi"
		} else if score >= 0.70 {
			confLevel = "med"
		}
		veto := CheckLeagueVeto(srFeatures, tsFeatures, confLevel)
		pool = append(pool, scored{comp: tsComps[i], score: score, veto: veto})
	}
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].score == pool[j].score {
			return pool[i].comp.ID < pool[j].comp.ID
		}
		return pool[i].score > pool[j].score
	})
	for _, p := range pool {
		rule := RuleLeagueNameLow
		if p.score >= 0.85 {
			rule = RuleLeagueNameHi
		} else if p.score >= 0.70 {
			rule = RuleLeagueNameMed
		}
		candidates = append(candidates, LeagueMatchCandidate{
			TSCompetitionID:        p.comp.ID,
			TSName:                 p.comp.Name,
			TSCountry:              p.comp.CountryName,
			Score:                  p.score,
			Rule:                   rule,
			StrongConstraintOK:     !p.veto.Vetoed,
			StrongConstraintReason: string(p.veto.Reason),
		})
	}

	if len(candidates) > n {
		candidates = candidates[:n]
	}
	return candidates
}

// ToTSCompetitionCandidate 把 LeagueMatchCandidate 转换为 P1 候选池生成器的输入单元。
func (c LeagueMatchCandidate) ToTSCompetitionCandidate(sport string) TSCompetitionCandidate {
	return TSCompetitionCandidate{
		Competition: db.TSCompetition{
			ID:          c.TSCompetitionID,
			Name:        c.TSName,
			CountryName: c.TSCountry,
			Sport:       sport,
		},
		LeaguePriorScore:       c.Score,
		StrongConstraintOK:     c.StrongConstraintOK,
		StrongConstraintReason: c.StrongConstraintReason,
	}
}

// ToTSCompetitionCandidates 批量转换。
func ToTSCompetitionCandidates(cands []LeagueMatchCandidate, sport string) []TSCompetitionCandidate {
	out := make([]TSCompetitionCandidate, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.ToTSCompetitionCandidate(sport))
	}
	return out
}

// MatchLeagueTopNWithFlags 是 MatchLeagueTopN 的扩展版本（v1.18）：
//
//   - noKnownMap=true 时跳过 KnownLeagueMap，候选全部来自名称相似度。
//     用于严格无 mapping 测评模式 —— 让 EF 拿到不被 KnownMap 短路的候选集。
//
//   - noKnownMap=false 时行为与 MatchLeagueTopN 完全一致（包括 KnownMap Top-1）。
//
// 与 MatchLeagueWithFlags 配套，构成 SR 链路的"严格测评"开关。
func MatchLeagueTopNWithFlags(srTour *db.SRTournament, tsComps []db.TSCompetition, n int, noKnownMap bool) []LeagueMatchCandidate {
	if !noKnownMap {
		return MatchLeagueTopN(srTour, tsComps, n)
	}
	if srTour == nil {
		return nil
	}
	if n <= 0 {
		n = defaultLeagueTopN
	}
	type scored struct {
		comp  db.TSCompetition
		score float64
		veto  LeagueVetoResult
	}
	var pool []scored
	srFeatures := ExtractLeagueFeatures(srTour.Name)
	for i := range tsComps {
		score := leagueNameScore(srTour, &tsComps[i])
		if score < defaultLeagueTopNMinScore {
			continue
		}
		tsFeatures := ExtractLeagueFeatures(tsComps[i].Name)
		confLevel := "low"
		if score >= 0.85 {
			confLevel = "hi"
		} else if score >= 0.70 {
			confLevel = "med"
		}
		veto := CheckLeagueVeto(srFeatures, tsFeatures, confLevel)
		pool = append(pool, scored{comp: tsComps[i], score: score, veto: veto})
	}
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].score == pool[j].score {
			return pool[i].comp.ID < pool[j].comp.ID
		}
		return pool[i].score > pool[j].score
	})
	candidates := make([]LeagueMatchCandidate, 0, len(pool))
	for _, p := range pool {
		rule := RuleLeagueNameLow
		if p.score >= 0.85 {
			rule = RuleLeagueNameHi
		} else if p.score >= 0.70 {
			rule = RuleLeagueNameMed
		}
		candidates = append(candidates, LeagueMatchCandidate{
			TSCompetitionID:        p.comp.ID,
			TSName:                 p.comp.Name,
			TSCountry:              p.comp.CountryName,
			Score:                  p.score,
			Rule:                   rule,
			StrongConstraintOK:     !p.veto.Vetoed,
			StrongConstraintReason: string(p.veto.Reason),
		})
	}
	if len(candidates) > n {
		candidates = candidates[:n]
	}
	return candidates
}
