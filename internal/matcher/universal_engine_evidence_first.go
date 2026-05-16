// Package matcher — UniversalEngine 的 Evidence-First 入口
//
// 本文件提供 Evidence-First 流水线在 UniversalEngine 中的 opt-in wire 实现。
// 当 UniversalEngine.UseEvidenceFirst=true 且 adapter 实现了 TopNAdapter 接口时，
// RunLeague 会切换到 Evidence-First 路径：
//
//	MatchLeagueTopN  →  ToTSCompetitionCandidates  →  CandidatePoolBuilder.Build (P1)
//	                →  EnrichTeamPriors (P2)
//	                →  EvidenceEventMatcher.MatchTwoRound (P3)
//
// 否则继续走原 P0 单胜者路径（adapter.MatchLeague + MatchEvents）。
//
// 设计原则：
//   - 不修改既有 SourceAdapter 接口（向后兼容）；
//   - 通过 Go-idiomatic optional interface（TopNAdapter）让 SR adapter 选择性升级，
//     LS adapter 无需变更即可继续工作；
//   - EF 路径产出的 EventMatch / TeamMappingResult 与 P0 同形态，下游 Step 8 球员
//     匹配、Step 8b 自底向上、Stats 计算可无缝复用。
//
// 相关流程洞察：PI-006 v1.4 Evidence-First 比赛级匹配流程

package matcher

import (
	"fmt"
	"log"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// TopNAdapter 是支持 Evidence-First Top-N 联赛匹配的可选适配器接口。
// SRSourceAdapter 实现了它；LSSourceAdapter 暂不实现，将自动降级回 P0 路径。
type TopNAdapter interface {
	MatchLeagueTopN(tsComps []db.TSCompetition, n int) []LeagueMatchCandidate
}

// MatchLeagueTopN 是 SRSourceAdapter 上的 Top-N 联赛匹配实现，
// 直接转发到包级函数 matcher.MatchLeagueTopN。
func (a *SRSourceAdapter) MatchLeagueTopN(tsComps []db.TSCompetition, n int) []LeagueMatchCandidate {
	return MatchLeagueTopN(a.srTour, tsComps, n)
}

// defaultRunLeagueTopN 是 RunLeague 在 EF 路径下未指定 EvidenceFirstTopN 时的默认值。
const defaultRunLeagueTopN = 5

// runLeagueEvidenceFirst 是 RunLeague 的 Evidence-First 分支。
//
// 调用前提：
//   - e.UseEvidenceFirst == true；
//   - adapter 实现了 TopNAdapter；
//   - tsComps 非空（由 RunLeague 提前 load）；
//   - tournamentID/sport/tier 与 RunLeague 一致。
//
// 输出与原 RunLeague 同形态的 *UniversalMatchResult，调用方（即 RunLeague）
// 拿到结果直接返回即可，不需要再跑 P0 路径。
func (e *UniversalEngine) runLeagueEvidenceFirst(
	adapter SourceAdapter,
	topn TopNAdapter,
	tournamentID, sport, tier string,
	tsComps []db.TSCompetition,
	t0 time.Time,
) (*UniversalMatchResult, error) {
	prefix := fmt.Sprintf("[%s][EF]", adapter.SourceSide())
	result := &UniversalMatchResult{}

	// ── EF Step 1: 联赛 Top-N ─────────────────────────────────────────────
	n := e.EvidenceFirstTopN
	if n <= 0 {
		n = defaultRunLeagueTopN
	}
	leagueCandidates := topn.MatchLeagueTopN(tsComps, n)
	log.Printf("%s [1/4] 联赛 Top-N: %d 候选 (n=%d)", prefix, len(leagueCandidates), n)
	if len(leagueCandidates) == 0 {
		// 无联赛候选，等价于"联赛不匹配"
		result.League = &LeagueMatchResult{Matched: false, MatchRule: RuleLeagueNoMatch}
		result.Stats = computeUniversalStats(result, sport, tier, adapter.SourceSide(), time.Since(t0))
		return result, nil
	}
	// Top-1 作为对外展示的"联赛匹配结果"
	top1 := leagueCandidates[0]
	result.League = &LeagueMatchResult{
		TSCompetitionID: top1.TSCompetitionID,
		TSName:          top1.TSName,
		TSCountry:       top1.TSCountry,
		Matched:         true,
		MatchRule:       top1.Rule,
		Confidence:      top1.Score,
	}
	log.Printf("%s   → Top-1: %s rule=%s score=%.3f (共 %d 候选)",
		prefix, top1.TSCompetitionID, top1.Rule, top1.Score, len(leagueCandidates))

	// ── EF Step 2: P1 候选池构建 ──────────────────────────────────────────
	candInputs := ToTSCompetitionCandidates(leagueCandidates, sport)
	poolBuilder := NewCandidatePoolBuilder(e.TS)
	pool, err := poolBuilder.Build(candInputs, sport)
	if err != nil {
		return nil, fmt.Errorf("EF P1 Build: %w", err)
	}
	log.Printf("%s [2/4] P1 候选池: %d 候选事件 (跨 %d competition, %d 球队)",
		prefix, len(pool.Candidates), pool.Stats.CompetitionsKept, len(pool.TSTeamNames))

	// ── EF Step 3: 加载源侧事件与球队，P2 球队级先验填充 ─────────────────
	srcEvents, err := adapter.LoadEvents(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("LoadEvents: %w", err)
	}
	srcTeamNames, err := adapter.LoadTeamNames(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("LoadTeamNames: %w", err)
	}
	enriched := EnrichTeamPriors(pool.Candidates, pool.TSTeamNames, srcTeamNames, nil)
	log.Printf("%s [3/4] 源侧: %d events, %d teams; P2 priors 已填充",
		prefix, len(srcEvents), len(srcTeamNames))

	// ── EF Step 4: P3 比赛级匹配（两轮 L4b）──────────────────────────────
	efMatcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	efResult := efMatcher.MatchTwoRound(srcEvents, enriched, srcTeamNames, pool.TSTeamNames)
	eventMatches := resolvedToEventMatches(efResult.Matches)
	l1, l2, l3, l4, l5, l4b, l6, matched := countEventLevels(eventMatches)
	log.Printf("%s [4/4] EF Match: %d/%d [L1=%d L2=%d L3=%d L4=%d L5=%d L4b=%d L6=%d] edges=%d eliminated=%d",
		prefix, matched, len(eventMatches), l1, l2, l3, l4, l5, l4b, l6,
		len(efResult.Edges), len(efResult.Eliminated))

	// ── 球队映射（与 P0 路径一致，复用 adapter.DeriveTeamMappings）────────
	teamMappings := adapter.DeriveTeamMappings(eventMatches, srcTeamNames, pool.TSTeamNames)
	log.Printf("%s   → 球队映射: %d 条", prefix, len(teamMappings))

	// 持久化别名（P0 一致），用 Top-1 competition 作为分组 key
	if e.AliasStore != nil {
		for _, tm := range teamMappings {
			if tm.TSTeamID != "" && tm.VoteCount >= 2 {
				_ = e.AliasStore.Upsert(
					adapter.SourceSide(), tm.SrcTeamID, tm.TSTeamID,
					tm.Confidence, sport, top1.TSCompetitionID,
				)
			}
		}
	}

	// 转换为通用 EventMatchResult
	result.Events = adapter.ConvertEvents(eventMatches)
	result.Teams = teamMappings

	// ── Step 8: 球员匹配（与 P0 一致）─────────────────────────────────────
	if e.RunPlayers {
		players, updatedTeams, updatedEvents, ok := adapter.RunPlayerMatch(teamMappings, sport, e.TS)
		if ok {
			result.Players = players
			result.Teams = updatedTeams
			result.Events = updatedEvents
		}
	}

	result.Stats = computeUniversalStats(result, sport, tier, adapter.SourceSide(), time.Since(t0))
	return result, nil
}
