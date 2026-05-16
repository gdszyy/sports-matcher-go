// Package matcher — Evidence-First P1 候选池生成器
//
// 本文件实现 Evidence-First 流水线的 P1 阶段：
//
//   P0（已冻结）：单 competition 的 TS 比赛集合 —— UniversalEngine.RunLeague
//                 在联赛匹配后只从一个 TS competition 拉事件，再交给 MatchEvents。
//   P1（本文件）：把"一组候选 TS competition + 各自的联赛先验分 + 联赛级强约束"
//                 直接转化为 []EvidenceEventCandidate，供 EvidenceEventMatcher 评分。
//   P2（后续）  ：球队级先验填充（HomeTeamCandidateScore/AwayTeamCandidateScore）。
//   P3（已落地）：EvidenceEventMatcher.MatchTwoRound 一对一冲突消解。
//
// 设计原则：
//   - 不做任何 SR↔TS 配对、打分、筛选 —— 那是 P3 EvidenceEventMatcher 的职责。
//   - 不依赖 *db.TSAdapter 本身，只依赖最小 TSEventLoader 接口，单测可注入 stub。
//   - 保持向后兼容：不改 event.go / evidence_event_matcher.go / universal_engine.go，
//     调用方按需 wire；P0 路径完全不动。
//
// 相关流程洞察：
//   - PI-006 Evidence-First 比赛级匹配流程：定义 EvidenceEventCandidate 的字段语义。
//   - PI-007 Evidence-First P0 基线冻结：P1 必须保证 P0 基线指标不回退（单 comp 输入退化为单 comp 输出）。

package matcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// TSEventLoader 是 CandidatePoolBuilder 依赖的最小数据访问接口。
// *db.TSAdapter 自然实现该接口；单测可用 stub 替换以避免 DB 依赖。
type TSEventLoader interface {
	GetEvents(competitionID, sport string) ([]db.TSEvent, error)
	GetTeamNames(competitionID, sport string) (map[string]string, error)
}

// BatchTSEventLoader 是 TSEventLoader 的可选超集（PI-006 v1.9）。
// 实现此接口的 Loader 能用单次 SQL IN(...) 同时拉多个 competition 的事件 /
// 球队名，把 P1 候选池构建的 N 次 DB round-trip 压缩成 1 次。
// *db.TSAdapter 实现了 BatchTSEventLoader；stub Loader 通常只实现单 comp 版本。
// CandidatePoolBuilder.Build 在内部用 type assertion 检测：有则用 batch，否则
// 回退到 N 个 goroutine 并行的 parallel-fetch 路径。
type BatchTSEventLoader interface {
	TSEventLoader
	GetEventsBatch(competitionIDs []string, sport string) (map[string][]db.TSEvent, error)
	GetTeamNamesBatch(competitionIDs []string, sport string) (map[string]map[string]string, error)
}

// TimeRangeBatchTSEventLoader 是 BatchTSEventLoader 的可选时间窗扩展（PI-006 v1.13）。
// 实现此接口的 Loader 能在 SQL 层用 WHERE match_time BETWEEN ... 把候选事件
// 数据量从"2 年 LIMIT 3000"压缩到 SR events 实际时间分布的 ±N 天窗口。
// *db.TSAdapter 实现了该接口；其他 Loader 通过 type assertion 自动降级到
// 不带时间窗的 BatchTSEventLoader / 并行 fetch 路径。
type TimeRangeBatchTSEventLoader interface {
	BatchTSEventLoader
	GetEventsBatchInRange(competitionIDs []string, sport string, timeMinUnix, timeMaxUnix int64) (map[string][]db.TSEvent, error)
}

// ContextBatchTSEventLoader 是 BatchTSEventLoader 的 ctx-aware 扩展（PI-006 v1.14）。
// 让长跑 SQL 能被 context.Done() 中断（配合 UniversalEngine.MaxRuntime 软超时
// + context.WithDeadline 实现真正的 SQL 取消）。
// *db.TSAdapter 同时实现 ContextBatchTSEventLoader 与 TimeRangeBatchTSEventLoader。
// CandidatePoolBuilder.BuildCtx / BuildWithTimeRangeCtx 优先使用此接口；
// stub Loader 通常只实现非 ctx 版本，自动回退到非取消语义。
type ContextBatchTSEventLoader interface {
	BatchTSEventLoader
	GetEventsBatchCtx(ctx context.Context, competitionIDs []string, sport string) (map[string][]db.TSEvent, error)
	GetTeamNamesBatchCtx(ctx context.Context, competitionIDs []string, sport string) (map[string]map[string]string, error)
}

// ContextTimeRangeBatchTSEventLoader 同时支持 ctx + 时间窗（v1.14）。
type ContextTimeRangeBatchTSEventLoader interface {
	ContextBatchTSEventLoader
	GetEventsBatchInRangeCtx(ctx context.Context, competitionIDs []string, sport string, timeMinUnix, timeMaxUnix int64) (map[string][]db.TSEvent, error)
}

// TSCompetitionCandidate 是 P1 候选池生成器的输入单元：
// 一个候选 TS 联赛 + 联赛级先验分 + 联赛级强约束结果。
//
// 典型来源：
//   - LeaguePriorScore：来自 KnownLeagueMap（命中=1.0）、LeagueAliasIndex（0.7~0.9）、
//     或纯算法名称相似度（0.4~0.7）。具体由调用方在 P0 联赛匹配阶段确定。
//   - StrongConstraintOK / StrongConstraintReason：来自 league_features.go 的
//     CheckLeagueVeto；六维强约束（性别/年龄/赛制/层级/区域/国家）任一不通过即 false。
type TSCompetitionCandidate struct {
	Competition            db.TSCompetition
	LeaguePriorScore       float64 // [0,1]
	StrongConstraintOK     bool
	StrongConstraintReason string
}

// CandidatePoolStats 用于日志与可观测性，不参与下游打分。
type CandidatePoolStats struct {
	CompetitionsScanned    int
	CompetitionsKept       int            // 至少有一个 TS 事件被纳入候选池的 competition 数量
	CompetitionsVetoed     int            // StrongConstraintOK=false 的（仍然写入候选池，由 P3 处理）
	TotalTSEvents          int            // 候选池总事件数
	PerCompetitionEventCnt map[string]int // competition_id -> 事件数
	Elapsed                time.Duration
}

// CandidatePoolResult 是 CandidatePoolBuilder.Build 的输出。
type CandidatePoolResult struct {
	// Candidates 可直接喂给 EvidenceEventMatcher.MatchTwoRound 作为 P3 输入。
	Candidates []EvidenceEventCandidate

	// TSTeamNames 是所有候选 competition 的 TS 球队名映射的 union（key=TSTeamID）。
	// 这是 P3 scoreEdge 需要的全局 TS 球队名查找表。
	TSTeamNames map[string]string

	Stats CandidatePoolStats
}

// CandidatePoolBuilder 把多个候选 TS competition 合成一份统一候选池。
// 该结构是无状态的（依赖通过 Loader 字段注入），可在多 goroutine 复用。
type CandidatePoolBuilder struct {
	Loader TSEventLoader
}

// NewCandidatePoolBuilder 创建候选池生成器。
// loader 必须非 nil；推荐传入 *db.TSAdapter，单测可传入 stub 实现。
func NewCandidatePoolBuilder(loader TSEventLoader) *CandidatePoolBuilder {
	return &CandidatePoolBuilder{Loader: loader}
}

// Build 拉取所有候选 competition 的事件，组装成统一候选池。
//
// 行为保证：
//  1. 不做任何 SR↔TS 配对或打分（那是 P3 的职责）；
//  2. 输出 len(Candidates) == sum(len(GetEvents(comp_i)))；
//  3. TSTeamNames 是所有候选 competition 的 team_id→name 的 union；
//  4. 当 candidates 长度为 1 时，输出与 P0 单 competition 路径在数据形态上等价
//     （只是包装成 EvidenceEventCandidate），保证 P0 基线不回退；
//  5. 任一 competition 的 GetEvents/GetTeamNames 失败 → 返回 wrapped error，已拉取的
//     candidates 不会被部分返回。
//
// sport 必须是 "football" 或 "basketball"，与 db.TSAdapter 接口一致。
func (b *CandidatePoolBuilder) Build(
	candidates []TSCompetitionCandidate,
	sport string,
) (*CandidatePoolResult, error) {
	t0 := time.Now()
	if b == nil || b.Loader == nil {
		return nil, fmt.Errorf("candidate pool: loader is nil")
	}
	if sport != "football" && sport != "basketball" {
		return nil, fmt.Errorf("candidate pool: unsupported sport: %s", sport)
	}

	result := &CandidatePoolResult{
		Candidates:  []EvidenceEventCandidate{},
		TSTeamNames: map[string]string{},
		Stats: CandidatePoolStats{
			PerCompetitionEventCnt: map[string]int{},
		},
	}
	result.Stats.CompetitionsScanned = len(candidates)

	// ── Fetch 候选 competition 数据 ──────────────────────────────────────
	// 优先级：
	//   1) Loader 实现 BatchTSEventLoader → 单次 SQL IN(...) 批量拉（v1.9 新增）；
	//   2) 否则并行 goroutine fetch（v1.8 路径）。
	// 保持输出顺序 deterministic（按 candidates 输入顺序），错误首次出现立即返回。
	type perCandResult struct {
		idx       int
		events    []db.TSEvent
		teamNames map[string]string
		err       error
	}
	results := make([]perCandResult, len(candidates))

	if batch, ok := b.Loader.(BatchTSEventLoader); ok {
		// ── batch 路径 ──
		compIDs := make([]string, 0, len(candidates))
		for _, cand := range candidates {
			if cand.Competition.ID != "" {
				compIDs = append(compIDs, cand.Competition.ID)
			}
		}
		if len(compIDs) > 0 {
			eventsMap, err := batch.GetEventsBatch(compIDs, sport)
			if err != nil {
				return nil, fmt.Errorf("candidate pool: GetEventsBatch: %w", err)
			}
			teamNamesMap, err := batch.GetTeamNamesBatch(compIDs, sport)
			if err != nil {
				return nil, fmt.Errorf("candidate pool: GetTeamNamesBatch: %w", err)
			}
			for i, cand := range candidates {
				if cand.Competition.ID == "" {
					results[i] = perCandResult{idx: i}
					continue
				}
				results[i] = perCandResult{
					idx:       i,
					events:    eventsMap[cand.Competition.ID],
					teamNames: teamNamesMap[cand.Competition.ID],
				}
			}
		}
	} else {
		// ── parallel-goroutine 回退路径 (v1.8) ──
		var wg sync.WaitGroup
		for i, cand := range candidates {
			if cand.Competition.ID == "" {
				results[i] = perCandResult{idx: i}
				continue
			}
			wg.Add(1)
			go func(idx int, compID string) {
				defer wg.Done()
				events, err := b.Loader.GetEvents(compID, sport)
				if err != nil {
					results[idx] = perCandResult{idx: idx, err: fmt.Errorf("candidate pool: GetEvents(%s, %s): %w", compID, sport, err)}
					return
				}
				teamNames, err := b.Loader.GetTeamNames(compID, sport)
				if err != nil {
					results[idx] = perCandResult{idx: idx, err: fmt.Errorf("candidate pool: GetTeamNames(%s, %s): %w", compID, sport, err)}
					return
				}
				results[idx] = perCandResult{idx: idx, events: events, teamNames: teamNames}
			}(i, cand.Competition.ID)
		}
		wg.Wait()
	}

	// 错误整体回滚（同旧版语义）：任一 candidate 报错就丢弃整批，不部分写回
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
	}

	// 按输入顺序合并（保持 TSTeamNames 的 first-write-wins 语义）
	for i, cand := range candidates {
		compID := cand.Competition.ID
		if compID == "" {
			continue
		}
		r := results[i]
		for tsTeamID, name := range r.teamNames {
			if tsTeamID == "" {
				continue
			}
			if _, exists := result.TSTeamNames[tsTeamID]; !exists {
				result.TSTeamNames[tsTeamID] = name
			}
		}
		eventCount := 0
		for _, ev := range r.events {
			if ev.MatchID == "" && ev.ID == "" {
				continue
			}
			result.Candidates = append(result.Candidates, EvidenceEventCandidate{
				CompetitionID:          compID,
				CompetitionName:        cand.Competition.Name,
				Event:                  ev,
				CandidateScore:         cand.LeaguePriorScore,
				HomeTeamCandidateScore: 0,
				AwayTeamCandidateScore: 0,
				StrongConstraintOK:     cand.StrongConstraintOK,
				StrongConstraintReason: cand.StrongConstraintReason,
			})
			eventCount++
		}
		result.Stats.PerCompetitionEventCnt[compID] = eventCount
		result.Stats.TotalTSEvents += eventCount
		if eventCount > 0 {
			result.Stats.CompetitionsKept++
		}
		if !cand.StrongConstraintOK {
			result.Stats.CompetitionsVetoed++
		}
	}

	result.Stats.Elapsed = time.Since(t0)
	return result, nil
}

// BuildWithTimeRange 是 Build 的时间窗版（PI-006 v1.13）。
// 当 Loader 实现 TimeRangeBatchTSEventLoader 时，使用 SQL WHERE
// match_time BETWEEN timeMinUnix AND timeMaxUnix 限定候选事件范围；
// 否则回退到 Build 的全量行为（warning：可能很慢）。
// EF 路径调用方应从 SR events 派生 timeMin/timeMax（典型 ±30 天）。
func (b *CandidatePoolBuilder) BuildWithTimeRange(
	candidates []TSCompetitionCandidate,
	sport string,
	timeMinUnix, timeMaxUnix int64,
) (*CandidatePoolResult, error) {
	timeRange, ok := b.Loader.(TimeRangeBatchTSEventLoader)
	if !ok || timeMinUnix <= 0 {
		return b.Build(candidates, sport)
	}
	t0 := time.Now()
	if sport != "football" && sport != "basketball" {
		return nil, fmt.Errorf("candidate pool: unsupported sport: %s", sport)
	}

	result := &CandidatePoolResult{
		Candidates:  []EvidenceEventCandidate{},
		TSTeamNames: map[string]string{},
		Stats: CandidatePoolStats{
			PerCompetitionEventCnt: map[string]int{},
		},
	}
	result.Stats.CompetitionsScanned = len(candidates)

	compIDs := make([]string, 0, len(candidates))
	for _, cand := range candidates {
		if cand.Competition.ID != "" {
			compIDs = append(compIDs, cand.Competition.ID)
		}
	}
	if len(compIDs) == 0 {
		result.Stats.Elapsed = time.Since(t0)
		return result, nil
	}

	eventsMap, err := timeRange.GetEventsBatchInRange(compIDs, sport, timeMinUnix, timeMaxUnix)
	if err != nil {
		return nil, fmt.Errorf("candidate pool: GetEventsBatchInRange: %w", err)
	}
	teamNamesMap, err := timeRange.GetTeamNamesBatch(compIDs, sport)
	if err != nil {
		return nil, fmt.Errorf("candidate pool: GetTeamNamesBatch: %w", err)
	}

	for _, cand := range candidates {
		compID := cand.Competition.ID
		if compID == "" {
			continue
		}
		for tsTeamID, name := range teamNamesMap[compID] {
			if tsTeamID == "" {
				continue
			}
			if _, exists := result.TSTeamNames[tsTeamID]; !exists {
				result.TSTeamNames[tsTeamID] = name
			}
		}
		eventCount := 0
		for _, ev := range eventsMap[compID] {
			if ev.MatchID == "" && ev.ID == "" {
				continue
			}
			result.Candidates = append(result.Candidates, EvidenceEventCandidate{
				CompetitionID:          compID,
				CompetitionName:        cand.Competition.Name,
				Event:                  ev,
				CandidateScore:         cand.LeaguePriorScore,
				StrongConstraintOK:     cand.StrongConstraintOK,
				StrongConstraintReason: cand.StrongConstraintReason,
			})
			eventCount++
		}
		result.Stats.PerCompetitionEventCnt[compID] = eventCount
		result.Stats.TotalTSEvents += eventCount
		if eventCount > 0 {
			result.Stats.CompetitionsKept++
		}
		if !cand.StrongConstraintOK {
			result.Stats.CompetitionsVetoed++
		}
	}

	result.Stats.Elapsed = time.Since(t0)
	return result, nil
}

// BuildCtx 是 Build 的 ctx-aware 版本（v1.14）。
// 若 Loader 实现 ContextBatchTSEventLoader 则用 ctx-aware SQL；否则回退到 Build。
// ctx 提前 done 时返回 ctx.Err() wrap 的错误。
func (b *CandidatePoolBuilder) BuildCtx(
	ctx context.Context,
	candidates []TSCompetitionCandidate,
	sport string,
) (*CandidatePoolResult, error) {
	ctxLoader, ok := b.Loader.(ContextBatchTSEventLoader)
	if !ok {
		return b.Build(candidates, sport)
	}
	t0 := time.Now()
	if sport != "football" && sport != "basketball" {
		return nil, fmt.Errorf("candidate pool: unsupported sport: %s", sport)
	}
	result := &CandidatePoolResult{
		Candidates:  []EvidenceEventCandidate{},
		TSTeamNames: map[string]string{},
		Stats:       CandidatePoolStats{PerCompetitionEventCnt: map[string]int{}},
	}
	result.Stats.CompetitionsScanned = len(candidates)
	compIDs := make([]string, 0, len(candidates))
	for _, cand := range candidates {
		if cand.Competition.ID != "" {
			compIDs = append(compIDs, cand.Competition.ID)
		}
	}
	if len(compIDs) == 0 {
		result.Stats.Elapsed = time.Since(t0)
		return result, nil
	}
	eventsMap, err := ctxLoader.GetEventsBatchCtx(ctx, compIDs, sport)
	if err != nil {
		return nil, fmt.Errorf("candidate pool: GetEventsBatchCtx: %w", err)
	}
	teamNamesMap, err := ctxLoader.GetTeamNamesBatchCtx(ctx, compIDs, sport)
	if err != nil {
		return nil, fmt.Errorf("candidate pool: GetTeamNamesBatchCtx: %w", err)
	}
	assembleCandidatePool(result, candidates, eventsMap, teamNamesMap)
	result.Stats.Elapsed = time.Since(t0)
	return result, nil
}

// BuildWithTimeRangeCtx 是 BuildWithTimeRange 的 ctx-aware 版本（v1.14）。
func (b *CandidatePoolBuilder) BuildWithTimeRangeCtx(
	ctx context.Context,
	candidates []TSCompetitionCandidate,
	sport string,
	timeMinUnix, timeMaxUnix int64,
) (*CandidatePoolResult, error) {
	ctxTR, ok := b.Loader.(ContextTimeRangeBatchTSEventLoader)
	if !ok || timeMinUnix <= 0 {
		return b.BuildCtx(ctx, candidates, sport)
	}
	t0 := time.Now()
	if sport != "football" && sport != "basketball" {
		return nil, fmt.Errorf("candidate pool: unsupported sport: %s", sport)
	}
	result := &CandidatePoolResult{
		Candidates:  []EvidenceEventCandidate{},
		TSTeamNames: map[string]string{},
		Stats:       CandidatePoolStats{PerCompetitionEventCnt: map[string]int{}},
	}
	result.Stats.CompetitionsScanned = len(candidates)
	compIDs := make([]string, 0, len(candidates))
	for _, cand := range candidates {
		if cand.Competition.ID != "" {
			compIDs = append(compIDs, cand.Competition.ID)
		}
	}
	if len(compIDs) == 0 {
		result.Stats.Elapsed = time.Since(t0)
		return result, nil
	}
	eventsMap, err := ctxTR.GetEventsBatchInRangeCtx(ctx, compIDs, sport, timeMinUnix, timeMaxUnix)
	if err != nil {
		return nil, fmt.Errorf("candidate pool: GetEventsBatchInRangeCtx: %w", err)
	}
	teamNamesMap, err := ctxTR.GetTeamNamesBatchCtx(ctx, compIDs, sport)
	if err != nil {
		return nil, fmt.Errorf("candidate pool: GetTeamNamesBatchCtx: %w", err)
	}
	assembleCandidatePool(result, candidates, eventsMap, teamNamesMap)
	result.Stats.Elapsed = time.Since(t0)
	return result, nil
}

// assembleCandidatePool 把 fetch 出来的 events/teamNames 装配到 CandidatePoolResult。
// BuildCtx / BuildWithTimeRangeCtx 公用，保证输出语义与 Build / BuildWithTimeRange 一致。
func assembleCandidatePool(
	result *CandidatePoolResult,
	candidates []TSCompetitionCandidate,
	eventsMap map[string][]db.TSEvent,
	teamNamesMap map[string]map[string]string,
) {
	for _, cand := range candidates {
		compID := cand.Competition.ID
		if compID == "" {
			continue
		}
		for tsTeamID, name := range teamNamesMap[compID] {
			if tsTeamID == "" {
				continue
			}
			if _, exists := result.TSTeamNames[tsTeamID]; !exists {
				result.TSTeamNames[tsTeamID] = name
			}
		}
		eventCount := 0
		for _, ev := range eventsMap[compID] {
			if ev.MatchID == "" && ev.ID == "" {
				continue
			}
			result.Candidates = append(result.Candidates, EvidenceEventCandidate{
				CompetitionID:          compID,
				CompetitionName:        cand.Competition.Name,
				Event:                  ev,
				CandidateScore:         cand.LeaguePriorScore,
				StrongConstraintOK:     cand.StrongConstraintOK,
				StrongConstraintReason: cand.StrongConstraintReason,
			})
			eventCount++
		}
		result.Stats.PerCompetitionEventCnt[compID] = eventCount
		result.Stats.TotalTSEvents += eventCount
		if eventCount > 0 {
			result.Stats.CompetitionsKept++
		}
		if !cand.StrongConstraintOK {
			result.Stats.CompetitionsVetoed++
		}
	}
}

// MergeTSTeamNames 把多份 TS 球队名映射合并为一份。常用于调用方已经按其他途径
// 准备好 TSTeamNames 时，希望与候选池的 TS 球队名 union。先到不会被覆盖。
func MergeTSTeamNames(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			if k == "" {
				continue
			}
			if _, ok := out[k]; !ok {
				out[k] = v
			}
		}
	}
	return out
}
