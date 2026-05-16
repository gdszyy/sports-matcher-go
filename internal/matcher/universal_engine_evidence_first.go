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
//   - 通过 Go-idiomatic optional interface（TopNAdapter）让各 adapter 选择性升级；
//   - EF 路径产出的 EventMatch / TeamMappingResult 与 P0 同形态，下游 Step 8 球员
//     匹配、自底向上、Stats 计算可无缝复用。
//
// 相关流程洞察：PI-006 v1.6 Evidence-First 比赛级匹配流程

package matcher

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// TopNAdapter 是支持 Evidence-First Top-N 联赛匹配的可选适配器接口。
// SRSourceAdapter 与 LSSourceAdapter 均已实现（v1.6 起），未来如有新链路
// 可按需追加，不必修改 SourceAdapter 主接口。
type TopNAdapter interface {
	MatchLeagueTopN(tsComps []db.TSCompetition, n int) []LeagueMatchCandidate
}

// MatchLeagueTopN 是 SRSourceAdapter 上的 Top-N 联赛匹配实现，
// 直接转发到包级函数 matcher.MatchLeagueTopN。
func (a *SRSourceAdapter) MatchLeagueTopN(tsComps []db.TSCompetition, n int) []LeagueMatchCandidate {
	return MatchLeagueTopNWithFlags(a.srTour, tsComps, n, a.NoKnownMap)
}

// MatchLeagueTopN 是 LSSourceAdapter 上的 Top-N 联赛匹配实现，
// 转发到包级函数 matcher.LSMatchLeagueTopN，并传递适配器的 NoKnownMap 标志。
// 这使得 LS 链路也能加入 Evidence-First 流水线，与 SR 链路保持对称。
func (a *LSSourceAdapter) MatchLeagueTopN(tsComps []db.TSCompetition, n int) []LeagueMatchCandidate {
	return LSMatchLeagueTopN(a.lsTour, tsComps, n, a.NoKnownMap)
}

// defaultRunLeagueTopN 是 RunLeague 在 EF 路径下未指定 EvidenceFirstTopN 时的默认值。
const defaultRunLeagueTopN = 5

// suspectConfidenceMultiplier 是 edges=0 时联赛 confidence 的降级系数（v1.12）。
// 0.5 是经验值：把高 conf (0.9+) 拉到 ≤0.5 让下游聚合 / 报表能识别为 SUSPECT。
const suspectConfidenceMultiplier = 0.5

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
// budgetExceeded 用 e.MaxRuntime 检查 elapsed 是否超时（PI-006 v1.11）。
// MaxRuntime<=0 表示不设上限，永远返回 false。
func (e *UniversalEngine) budgetExceeded(t0 time.Time) bool {
	if e.MaxRuntime <= 0 {
		return false
	}
	return time.Since(t0) > e.MaxRuntime
}

// markTruncated 把当前结果标记为截断并填上当前阶段。返回 result 以便链式 return。
func markTruncated(result *UniversalMatchResult, stage string) {
	result.Stats.Truncated = true
	result.Stats.TruncatedStage = stage
}

func (e *UniversalEngine) runLeagueEvidenceFirst(
	adapter SourceAdapter,
	topn TopNAdapter,
	tournamentID, sport, tier string,
	tsComps []db.TSCompetition,
	t0 time.Time,
) (*UniversalMatchResult, error) {
	prefix := fmt.Sprintf("[%s][EF]", adapter.SourceSide())
	result := &UniversalMatchResult{}

	// ── PI-006 v1.14: 用 MaxRuntime 派生 deadline context，让 SQL 真正可取消 ──
	ctx := context.Background()
	if e.MaxRuntime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, t0.Add(e.MaxRuntime))
		defer cancel()
	}

	n := e.EvidenceFirstTopN
	if n <= 0 {
		n = defaultRunLeagueTopN
	}
	leagueCandidates := topn.MatchLeagueTopN(tsComps, n)
	log.Printf("%s [1/4] 联赛 Top-N: %d 候选 (n=%d)", prefix, len(leagueCandidates), n)
	if len(leagueCandidates) == 0 {
		result.League = &LeagueMatchResult{Matched: false, MatchRule: RuleLeagueNoMatch}
		result.Stats = computeUniversalStats(result, sport, tier, adapter.SourceSide(), time.Since(t0))
		return result, nil
	}
	top1 := leagueCandidates[0]
	result.League = &LeagueMatchResult{
		TSCompetitionID: top1.TSCompetitionID,
		TSName:          top1.TSName,
		TSCountry:       top1.TSCountry,
		Matched:         true,
		MatchRule:       top1.Rule,
		Confidence:      top1.Score,
	}
	log.Printf("%s   Top-1: %s rule=%s score=%.3f (共 %d 候选)",
		prefix, top1.TSCompetitionID, top1.Rule, top1.Score, len(leagueCandidates))

	if e.budgetExceeded(t0) {
		log.Printf("%s ⏱ budget exceeded before P1; emit league-only result", prefix)
		markTruncated(result, "topn")
		result.Stats = computeUniversalStats(result, sport, tier, adapter.SourceSide(), time.Since(t0))
		result.Stats.Truncated = true
		result.Stats.TruncatedStage = "topn"
		return result, nil
	}

	// 先 load SR events，从中派生时间窗（PI-006 v1.13）。
	// 然后用 BuildWithTimeRange 把 P1 SQL fetch 限定到 ±30 天窗口，
	// 数据量从"2 年 LIMIT 3000"缩到典型"60 天 LIMIT 200"，
	// EPL 实测 P1 fetch 12s → ~3s。
	srcEvents, err := adapter.LoadEvents(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("LoadEvents: %w", err)
	}
	srcTeamNames, err := adapter.LoadTeamNames(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("LoadTeamNames: %w", err)
	}
	// 仅当 EvidenceFirstTimePadding>0 时才下推时间窗。
	// 推荐 180*24*3600 (180d) 兼顾速度与 L4b 跨季召回；0 (默认) = 完全保留 v1.12 行为。
	var timeMin, timeMax int64
	if e.EvidenceFirstTimePadding > 0 {
		paddingSec := int64(e.EvidenceFirstTimePadding / time.Second)
		timeMin, timeMax = deriveTimeRangeFromSREvents(srcEvents, paddingSec)
	}

	candInputs := ToTSCompetitionCandidates(leagueCandidates, sport)
	poolBuilder := NewCandidatePoolBuilder(e.TS)
	var pool *CandidatePoolResult
	if timeMin > 0 {
		pool, err = poolBuilder.BuildWithTimeRangeCtx(ctx, candInputs, sport, timeMin, timeMax)
	} else {
		pool, err = poolBuilder.BuildCtx(ctx, candInputs, sport)
	}
	if err != nil {
		return nil, fmt.Errorf("EF P1 Build: %w", err)
	}
	log.Printf("%s [2/4] P1 候选池: %d 候选事件 (跨 %d competition, %d 球队) [time window %d~%d]",
		prefix, len(pool.Candidates), pool.Stats.CompetitionsKept, len(pool.TSTeamNames), timeMin, timeMax)
	if e.budgetExceeded(t0) {
		log.Printf("%s ⏱ budget exceeded before P3; emit P1 truncated result", prefix)
		top1 := leagueCandidates[0]
		_ = top1
		result.Stats = computeUniversalStats(result, sport, tier, adapter.SourceSide(), time.Since(t0))
		result.Stats.Truncated = true
		result.Stats.TruncatedStage = "p2"
		return result, nil
	}

	enriched := EnrichTeamPriors(pool.Candidates, pool.TSTeamNames, srcTeamNames, nil)
	log.Printf("%s [3/4] 源侧: %d events, %d teams; P2 priors filled",
		prefix, len(srcEvents), len(srcTeamNames))

	efMatcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	efResult := efMatcher.MatchTwoRound(srcEvents, enriched, srcTeamNames, pool.TSTeamNames)
	eventMatches := resolvedToEventMatches(efResult.Matches)
	l1, l2, l3, l4, l5, l4b, l6, matched := countEventLevels(eventMatches)
	log.Printf("%s [4/4] EF Match: %d/%d [L1=%d L2=%d L3=%d L4=%d L5=%d L4b=%d L6=%d] edges=%d eliminated=%d",
		prefix, matched, len(eventMatches), l1, l2, l3, l4, l5, l4b, l6,
		len(efResult.Edges), len(efResult.Eliminated))

	// ── Team-First fallback (PI-006 v1.16) ─────────────────────────────
	// 若 EF 第一遍 edges=0 且开启了 EnableTeamFirstFallback：
	//   1. 用 SR 球队名跨 sport 查 TS team_ids
	//   2. 按 team_id 拉事件 → 多数表决反推 TS competition_id
	//   3. 用反推出的 competition_id 重跑 EF（一次性，避免无限循环）
	//   4. 若 second pass edges > 0 则替换结果，league.Rule = LEAGUE_TEAM_FIRST
	// 实证基础：PI-006 v1.16 Jordan League PoC 100% league 回灌准确。
	tfApplied := false
	if e.EnableTeamFirstFallback && len(efResult.Edges) == 0 &&
		result.League != nil && result.League.Matched &&
		result.League.MatchRule != RuleLeagueKnown {
		newLeague, newEfResult, newEventMatches, ok := e.tryTeamFirstFallback(
			ctx, prefix, srcEvents, srcTeamNames, sport, timeMin, timeMax,
			result.League.TSCompetitionID,
		)
		if ok {
			result.League = newLeague
			efResult = newEfResult
			eventMatches = newEventMatches
			tfApplied = true
			log.Printf("%s ✓ team-first fallback recovered: league=%s edges=%d",
				prefix, newLeague.TSCompetitionID, len(newEfResult.Edges))
		}
	}

	// ── SUSPECT 降级（PI-006 v1.12）─────────────────────────────────────
	// 若 EF 跑完产生 0 条候选边、且联赛标了 Matched=true、且不是 KnownLeagueMap
	// 命中（人工 curated 信任），则联赛级名称匹配几乎肯定错了 —— 把 confidence
	// 砍半并把 Rule 改成 LEAGUE_SUSPECT，让下游 / 监控能识别。
	// 实证证据：PI-006 v1.10 Case A (Premier Soccer League Zimbabwe → BPL) 和
	// Case C (Italy Serie B Women → Argentine Women's League) 都是这个模式。
	// 不触发 KnownMap：KnownLeagueMap 是人工映射，edges=0 可能是 TS 数据缺失。
	// 不触发 team-first 已修复的：上面 tfApplied=true 时跳过此降级。
	if !tfApplied && len(efResult.Edges) == 0 && result.League != nil && result.League.Matched && result.League.MatchRule != RuleLeagueKnown {
		originalConf := result.League.Confidence
		result.League.Confidence *= suspectConfidenceMultiplier
		originalRule := result.League.MatchRule
		result.League.MatchRule = RuleLeagueSuspect
		log.Printf("%s ⚠ league SUSPECT: edges=0 → rule %s→%s, conf %.3f→%.3f",
			prefix, originalRule, RuleLeagueSuspect, originalConf, result.League.Confidence)
	}

	teamMappings := adapter.DeriveTeamMappings(eventMatches, srcTeamNames, pool.TSTeamNames)
	log.Printf("%s   teamMappings: %d", prefix, len(teamMappings))

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

	result.Events = adapter.ConvertEvents(eventMatches)
	result.Teams = teamMappings

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

// deriveTimeRangeFromSREvents 计算 SR events 的时间分布并加 paddingSec 缓冲。
// 返回 (timeMinUnix, timeMaxUnix)；当 srcEvents 为空或全无效时返回 (0, 0)，
// 调用方应回退到无时间窗的 fetch 路径。
func deriveTimeRangeFromSREvents(srcEvents []db.SREvent, paddingSec int64) (int64, int64) {
	var minT, maxT int64
	found := false
	for _, e := range srcEvents {
		if e.StartUnix <= 0 {
			continue
		}
		if !found {
			minT = e.StartUnix
			maxT = e.StartUnix
			found = true
			continue
		}
		if e.StartUnix < minT {
			minT = e.StartUnix
		}
		if e.StartUnix > maxT {
			maxT = e.StartUnix
		}
	}
	if !found {
		return 0, 0
	}
	return minT - paddingSec, maxT + paddingSec
}

// getTeamFirstBuilder 是 e.tfBuilder 的 lazy initializer。线程安全。
func (e *UniversalEngine) getTeamFirstBuilder() *TeamFirstPoolBuilder {
	e.tfMu.Lock()
	defer e.tfMu.Unlock()
	if e.tfBuilder == nil {
		e.tfBuilder = NewTeamFirstPoolBuilder(e.TS)
	}
	return e.tfBuilder
}

// PreloadTeamFirstIndex 预加载某个 sport 的 TS 球队 token 索引到内存（v1.17）。
//
// 长跑批量场景（batch2 / ls-batch）启动时调用一次，后续 RunLeague 触发 team-first
// fallback 时全部走 in-memory 路径（~50ms），避免每次 SQL pre-filter 4-5s 启动成本。
//
// 单跑 match2 / ls-match 场景不建议预加载（一次性 cold start ~4s 反而更慢）。
//
// 并发安全；重复调用同一 sport 直接 no-op。
func (e *UniversalEngine) PreloadTeamFirstIndex(ctx context.Context, sport string) error {
	tfBuilder := e.getTeamFirstBuilder()
	if err := tfBuilder.PreloadIndex(ctx, sport); err != nil {
		return err
	}
	teamCnt, tokenCnt := tfBuilder.PreloadStats(sport)
	log.Printf("[EF] team-first index preloaded: sport=%s teams=%d tokens=%d", sport, teamCnt, tokenCnt)
	return nil
}

// tryTeamFirstFallback 用 SR 球队名直接查 TS team_ids → 拉事件 → 多数表决
// 反推 TS competition_id，再重跑 EF P3。成功返回 (newLeague, newEfResult,
// newEventMatches, true)；失败返回 (nil, nil, nil, false)。
//
// 关键限制：只跑一次，避免循环；只在 second pass edges > 0 时认定成功。
func (e *UniversalEngine) tryTeamFirstFallback(
	ctx context.Context,
	prefix string,
	srcEvents []db.SREvent,
	srcTeamNames map[string]string,
	sport string,
	timeMin, timeMax int64,
	currentTSCompetitionID string,
) (*LeagueMatchResult, ConflictResolutionResult, []EventMatch, bool) {
	tfBuilder := e.getTeamFirstBuilder()
	log.Printf("%s ⤴ team-first fallback: SR teams=%d, sport=%s", prefix, len(srcTeamNames), sport)
	// padding 默认 ±7 天（与 PoC 一致）
	if timeMin == 0 {
		timeMin, timeMax = deriveTimeRangeFromSREvents(srcEvents, 7*24*3600)
	}
	tfPool, err := tfBuilder.Build(ctx, srcTeamNames, sport, timeMin, timeMax, 3)
	if err != nil {
		log.Printf("%s   team-first build failed: %v", prefix, err)
		return nil, ConflictResolutionResult{}, nil, false
	}
	if len(tfPool.Candidates) == 0 {
		log.Printf("%s   team-first: 0 candidates", prefix)
		return nil, ConflictResolutionResult{}, nil, false
	}
	// 多数表决：按 competition_id 计票
	compVotes := make(map[string]int)
	for _, c := range tfPool.Candidates {
		compVotes[c.CompetitionID]++
	}
	// 找出 top competition
	var topComp string
	topCount := 0
	for cid, cnt := range compVotes {
		if cnt > topCount {
			topCount = cnt
			topComp = cid
		}
	}
	log.Printf("%s   team-first: %d candidates, top competition=%s (%d events)",
		prefix, len(tfPool.Candidates), topComp, topCount)
	if topComp == "" || topComp == currentTSCompetitionID {
		// team-first 同意第一遍的判断，不需要 fallback
		return nil, ConflictResolutionResult{}, nil, false
	}
	// 用 team-first 的候选池重跑 EF P3
	enriched := EnrichTeamPriors(tfPool.Candidates, tfPool.TSTeamNames, srcTeamNames, nil)
	efMatcher := NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	newEfResult := efMatcher.MatchTwoRound(srcEvents, enriched, srcTeamNames, tfPool.TSTeamNames)
	if len(newEfResult.Edges) == 0 {
		log.Printf("%s   team-first P3: still 0 edges, abort fallback", prefix)
		return nil, ConflictResolutionResult{}, nil, false
	}
	newLeague := &LeagueMatchResult{
		TSCompetitionID: topComp,
		TSName:          tfPool.TSTeamNames[topComp],
		Matched:         true,
		MatchRule:       RuleLeagueTeamFirst,
		Confidence:      0.70, // 中等先验：team-first 反推非 KnownMap，标 0.70
	}
	return newLeague, newEfResult, resolvedToEventMatches(newEfResult.Matches), true
}
