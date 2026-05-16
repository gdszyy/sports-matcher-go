// Package matcher — 联赛级 Top-N 候选匹配（LS 版）
//
// 本文件是 league_topn.go 的 LS 版镜像：把 LSports 源侧的联赛匹配输出从
// 单胜者升级为排名前 N 的候选列表，让 LSSourceAdapter 也能加入
// Evidence-First 流水线。
//
// 与 SR 版（MatchLeagueTopN）的差异：
//   - 入参为 *db.LSTournament，KnownMap 查 KnownLSLeagueMap；
//   - 打分用 lsLeagueNameScore（除了通用 CheckLeagueVeto + 负向惩罚 +
//     国家加分外，还跑 LS 特有的 lsLocationVeto / lsLocationVetoByName）；
//   - 多一个 noKnownMap 参数，对应 LSSourceAdapter.NoKnownMap，用于
//     "纯算法验证"模式（命令行 --no-known-map）。
//
// 设计原则：
//   - 与 MatchLeagueTopN 输出同一个 LeagueMatchCandidate 结构，
//     方便复用 ToTSCompetitionCandidates 接力到 P1；
//   - 不破坏 P0：既有 matchLSLeague / matchLSLeagueAlgoOnly 保留；
//   - 强约束 reason 与 SR 版一致：CheckLeagueVeto 的 Reason 字段；LS 特有
//     的 location veto 用更明确的字符串显式标识。
//
// 相关流程洞察：PI-006 v1.6 Evidence-First 比赛级匹配流程

package matcher

import (
	"sort"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// lsLocationVetoReason / lsLocationVetoByNameReason 标识 LS 特有的两类
// 地理强约束触发原因，供 LeagueMatchCandidate.StrongConstraintReason 使用。
const (
	lsLocationVetoReason       = "ls_location_veto"
	lsLocationVetoByNameReason = "ls_location_veto_by_name"
)

// LSMatchLeagueTopN 返回排名前 n 的 TS competition 候选（LS 版）。
//
// 行为保证：
//  1. n <= 0 → 使用默认 n=5；
//  2. 若 noKnownMap=false 且 KnownLSLeagueMap 命中 → Top-1，score=1.0，
//     Rule=RuleLeagueKnown；即使该 tsID 不在 tsComps 中也保留；
//  3. 其余候选通过 lsLeagueNameScore 计算分数，score >= 0.55 入选（与
//     matchLSLeague 同口径，不复用 matchLSLeagueAlgoOnly 的 0.40 阈值，
//     因为 Top-N 需要更高的"可信"门槛防止误污染 P1 候选池）；
//  4. 排序：score 降序，同分按 ts.ID 字典序；
//  5. StrongConstraintOK / Reason：先检查 LS 特有 location vetos，再调
//     通用 CheckLeagueVeto；任一触发即 StrongConstraintOK=false。
//
// 复杂度：O(N_tsComps × lsLeagueNameScore) + O(N log N) 排序。
func LSMatchLeagueTopN(
	lsTour *db.LSTournament,
	tsComps []db.TSCompetition,
	n int,
	noKnownMap bool,
) []LeagueMatchCandidate {
	if lsTour == nil {
		return nil
	}
	if n <= 0 {
		n = defaultLeagueTopN
	}

	var candidates []LeagueMatchCandidate

	// 1) KnownLSLeagueMap 命中 → Top-1
	var knownTSID string
	hasKnown := false
	if !noKnownMap {
		mapKey := lsLeagueKey(lsTour.Sport, lsTour.ID)
		if tsID, ok := KnownLSLeagueMap[mapKey]; ok {
			knownTSID = tsID
			hasKnown = true
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
	}

	// 2) 名称相似度兜底（含 LS 特有 location veto + 通用 CheckLeagueVeto）
	type scored struct {
		comp                  db.TSCompetition
		score                 float64
		lsVetoReason          string // 非空表示 LS 特有 veto 触发
		commonVeto            LeagueVetoResult
	}
	var pool []scored
	lsFeatures := ExtractLeagueFeatures(lsTour.Name)
	for i := range tsComps {
		if hasKnown && tsComps[i].ID == knownTSID {
			continue
		}
		// 先判 LS 特有的两类 location veto（与 lsLeagueNameScore 内部一致）
		lsVR := ""
		if lsLocationVeto(lsTour.CategoryName, tsComps[i].CountryName) {
			lsVR = lsLocationVetoReason
		} else if lsLocationVetoByName(lsTour.Name, tsComps[i].CountryName) {
			lsVR = lsLocationVetoByNameReason
		}
		if lsVR != "" {
			// LS location veto 触发：lsLeagueNameScore 会返回 0，跳过此候选
			continue
		}
		score := lsLeagueNameScore(lsTour, &tsComps[i])
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
		veto := CheckLeagueVeto(lsFeatures, tsFeatures, confLevel)
		pool = append(pool, scored{comp: tsComps[i], score: score, commonVeto: veto})
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
			StrongConstraintOK:     !p.commonVeto.Vetoed,
			StrongConstraintReason: string(p.commonVeto.Reason),
		})
	}

	if len(candidates) > n {
		candidates = candidates[:n]
	}
	return candidates
}
