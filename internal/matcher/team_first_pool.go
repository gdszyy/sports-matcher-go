// Package matcher — Evidence-First 球队优先候选池（PI-006 v1.17）
//
// 反向流水线：当 league name matching 不可靠时，跳过 Top-N 直接走球队 → 事件 → 联赛回灌。
//
// 用法：
//
//	1) 调用方 LoadTeamNames 拿到 SR 球队名清单
//	2) TeamFirstPoolBuilder.Build(srTeamNames, sport, tMin, tMax) 内部：
//	   a. 优先：若已 PreloadIndex(sport) → 内存 token 索引快速命中
//	   b. 次选：若 Loader 实现 TokenSearchTeamLoader → SQL REGEXP pre-filter
//	   c. 兜底：full-table load + 内存索引
//	3) 输出与 CandidatePoolBuilder.BuildCtx 同形态的 *CandidatePoolResult
//	   → 喂给 EvidenceEventMatcher.MatchTwoRound 完成 P3 评分
//
// v1.17 新增 PreloadIndex —— 长跑批量场景（batch2/ls-batch）在启动时一次性
// load 全量 TS 球队 + token 索引，后续 RunLeague 全部走 in-memory 路径，
// 省掉 SQL pre-filter 的 4-5s 启动成本。

package matcher

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// TeamFirstLoader 是球队优先路径依赖的最小数据访问接口。
// *db.TSAdapter 自然实现该接口。
type TeamFirstLoader interface {
	GetAllTeamsBriefCtx(ctx context.Context, sport string) ([]db.TSTeamBrief, error)
	GetEventsByTeamIDsInRangeCtx(ctx context.Context, sport string, teamIDs []string, timeMinUnix, timeMaxUnix int64) ([]db.TSEventWithComp, error)
}

// TokenSearchTeamLoader 是 TeamFirstLoader 的可选 SQL pre-filter 扩展。
// 用 token REGEXP 在 SQL 层把候选 TS 球队从 79K 全表压到 ~1500，避免内存
// load 全表的 cold start 成本（PoC 实测 4.4s）。
type TokenSearchTeamLoader interface {
	TeamFirstLoader
	SearchTeamsByTokensCtx(ctx context.Context, sport string, tokens []string, limit int) ([]db.TSTeamBrief, error)
}

// TeamFirstPoolBuilder 构建球队优先候选池。
type TeamFirstPoolBuilder struct {
	Loader TeamFirstLoader

	// 共享 TS 球队缓存。首次 Build 或 PreloadIndex 时 load，后续直接复用。
	// 缓存 key = sport，value = []TSTeamBrief。
	mu        sync.Mutex
	tsTeams   map[string][]db.TSTeamBrief
	teamIndex map[string]map[string][]int // sport -> token -> indexes (in tsTeams[sport])
}

// NewTeamFirstPoolBuilder 创建球队优先候选池生成器。
func NewTeamFirstPoolBuilder(loader TeamFirstLoader) *TeamFirstPoolBuilder {
	return &TeamFirstPoolBuilder{
		Loader: loader,
	}
}

// PreloadIndex 显式预加载某个 sport 的全量 TS 球队并建 token 倒排索引。
//
// 长跑批量场景（batch2/ls-batch）启动时调用一次，后续 Build 全部走内存路径，
// 不再触发 SQL pre-filter（节省 ~4-5s 启动成本 × N 个联赛）。
//
// 并发安全；重复调用同一 sport 直接 no-op。
func (b *TeamFirstPoolBuilder) PreloadIndex(ctx context.Context, sport string) error {
	if b == nil || b.Loader == nil {
		return fmt.Errorf("team-first: loader is nil")
	}
	if sport != "football" && sport != "basketball" {
		return fmt.Errorf("team-first: unsupported sport: %s", sport)
	}
	return b.ensureIndexed(ctx, sport)
}

// IsPreloaded 返回某个 sport 的内存索引是否已就绪。
func (b *TeamFirstPoolBuilder) IsPreloaded(sport string) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tsTeams == nil {
		return false
	}
	_, ok := b.tsTeams[sport]
	return ok
}

// PreloadStats 返回内存索引的简单统计（球队数 / token 数），主要给 log 用。
func (b *TeamFirstPoolBuilder) PreloadStats(sport string) (teamCnt, tokenCnt int) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tsTeams != nil {
		teamCnt = len(b.tsTeams[sport])
	}
	if b.teamIndex != nil {
		tokenCnt = len(b.teamIndex[sport])
	}
	return
}

// ensureIndexed 是 loadAndIndexTeams 的并发安全版本。
// 内部加锁，并对 sport 做"已加载即跳过"判定。
func (b *TeamFirstPoolBuilder) ensureIndexed(ctx context.Context, sport string) error {
	b.mu.Lock()
	if b.tsTeams != nil {
		if _, ok := b.tsTeams[sport]; ok {
			b.mu.Unlock()
			return nil
		}
	}
	b.mu.Unlock()

	// 跨锁 load —— 慢 IO 不阻塞其他 sport 的 read
	teams, err := b.Loader.GetAllTeamsBriefCtx(ctx, sport)
	if err != nil {
		return fmt.Errorf("team-first: load teams: %w", err)
	}
	// 建 token 索引：每个球队名拆 token，每个 token → 多个球队 index
	idx := make(map[string][]int, len(teams))
	for i, t := range teams {
		for _, tok := range tokenizeForIndex(t.Name) {
			idx[tok] = append(idx[tok], i)
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tsTeams == nil {
		b.tsTeams = make(map[string][]db.TSTeamBrief)
		b.teamIndex = make(map[string]map[string][]int)
	}
	// double-check：另一并发 caller 可能抢先写入
	if _, ok := b.tsTeams[sport]; !ok {
		b.tsTeams[sport] = teams
		b.teamIndex[sport] = idx
	}
	return nil
}

// findCandidateTSTeams 对每个 SR 球队名找 top-K 候选 TS team_ids（内存路径）。
// 算法：(1) 拆 SR 名为 token，从 teamIndex 拉候选索引集；(2) 对候选 candidate 用
// teamNameSimilarity 精确打分；(3) 取 top-K。
//
// 调用前必须确保 sport 已 ensureIndexed/PreloadIndex 过。
func (b *TeamFirstPoolBuilder) findCandidateTSTeams(sport, srName string, topK int) []db.TSTeamBrief {
	tokens := tokenizeForIndex(srName)
	if len(tokens) == 0 {
		return nil
	}
	b.mu.Lock()
	teams := b.tsTeams[sport]
	idx := b.teamIndex[sport]
	b.mu.Unlock()
	if len(teams) == 0 || idx == nil {
		return nil
	}
	candidateIdx := map[int]bool{}
	for _, tok := range tokens {
		for _, i := range idx[tok] {
			candidateIdx[i] = true
		}
	}
	if len(candidateIdx) == 0 {
		return nil
	}
	type scored struct {
		i     int
		score float64
	}
	scoredList := make([]scored, 0, len(candidateIdx))
	for i := range candidateIdx {
		sim := teamNameSimilarity(srName, teams[i].Name)
		if sim < 0.5 {
			continue
		}
		scoredList = append(scoredList, scored{i: i, score: sim})
	}
	sort.SliceStable(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})
	if len(scoredList) > topK {
		scoredList = scoredList[:topK]
	}
	out := make([]db.TSTeamBrief, 0, len(scoredList))
	for _, s := range scoredList {
		out = append(out, teams[s.i])
	}
	return out
}

// Build 主入口。输入 SR 球队名清单与 sport，输出与 CandidatePoolBuilder.BuildCtx
// 同形态的 *CandidatePoolResult。
//
// 参数：
//   - srTeamNames: map[srTeamID]srTeamName
//   - sport: "football" / "basketball"
//   - tMin, tMax: SR 事件时间窗（unix 秒），用作 TS 事件查询的过滤范围
//   - topKPerSR: 每个 SR 球队保留多少候选 TS 球队（推荐 3~5）
//
// 候选 team 来源优先级（v1.17）：
//   - 1. 已 PreloadIndex(sport) → 内存 token 倒排索引（最快，~50ms）
//   - 2. 未 preload 且 Loader 实现 TokenSearchTeamLoader → SQL REGEXP pre-filter（沙箱 4-5s）
//   - 3. 都没有 → 全表 load + 内存索引（适用于无 SQL 支持的 stub）
func (b *TeamFirstPoolBuilder) Build(
	ctx context.Context,
	srTeamNames map[string]string,
	sport string,
	tMin, tMax int64,
	topKPerSR int,
) (*CandidatePoolResult, error) {
	t0 := time.Now()
	if b == nil || b.Loader == nil {
		return nil, fmt.Errorf("team-first: loader is nil")
	}
	if sport != "football" && sport != "basketball" {
		return nil, fmt.Errorf("team-first: unsupported sport: %s", sport)
	}
	if topKPerSR <= 0 {
		topKPerSR = 3
	}

	tsTeamIDs := map[string]string{} // tsTeamID -> tsTeamName

	// 优先级 1：preload 命中 → 内存路径
	if b.IsPreloaded(sport) {
		for _, srName := range srTeamNames {
			if srName == "" {
				continue
			}
			cands := b.findCandidateTSTeams(sport, srName, topKPerSR)
			for _, c := range cands {
				if _, exists := tsTeamIDs[c.TeamID]; !exists {
					tsTeamIDs[c.TeamID] = c.Name
				}
			}
		}
	} else if tsLoader, ok := b.Loader.(TokenSearchTeamLoader); ok {
		// 优先级 2：SQL REGEXP pre-filter（v1.16+ 默认）
		tokenSet := map[string]bool{}
		for _, srName := range srTeamNames {
			for _, tok := range tokenizeForIndex(srName) {
				tokenSet[tok] = true
			}
		}
		tokens := make([]string, 0, len(tokenSet))
		for t := range tokenSet {
			tokens = append(tokens, t)
		}
		candidates, err := tsLoader.SearchTeamsByTokensCtx(ctx, sport, tokens, 5000)
		if err != nil {
			return nil, fmt.Errorf("team-first: SearchTeamsByTokensCtx: %w", err)
		}
		// 对每个 SR 球队找 top-K
		for _, srName := range srTeamNames {
			if srName == "" {
				continue
			}
			type scoredT struct {
				id, name string
				score    float64
			}
			scored := []scoredT{}
			for _, c := range candidates {
				sim := teamNameSimilarity(srName, c.Name)
				if sim < 0.5 {
					continue
				}
				scored = append(scored, scoredT{id: c.TeamID, name: c.Name, score: sim})
			}
			sort.SliceStable(scored, func(i, j int) bool {
				return scored[i].score > scored[j].score
			})
			if len(scored) > topKPerSR {
				scored = scored[:topKPerSR]
			}
			for _, s := range scored {
				if _, exists := tsTeamIDs[s.id]; !exists {
					tsTeamIDs[s.id] = s.name
				}
			}
		}
	} else {
		// 优先级 3：全表回退路径
		if err := b.ensureIndexed(ctx, sport); err != nil {
			return nil, err
		}
		for _, srName := range srTeamNames {
			if srName == "" {
				continue
			}
			cands := b.findCandidateTSTeams(sport, srName, topKPerSR)
			for _, c := range cands {
				if _, exists := tsTeamIDs[c.TeamID]; !exists {
					tsTeamIDs[c.TeamID] = c.Name
				}
			}
		}
	}

	result := &CandidatePoolResult{
		Candidates:  []EvidenceEventCandidate{},
		TSTeamNames: map[string]string{},
		Stats:       CandidatePoolStats{PerCompetitionEventCnt: map[string]int{}},
	}

	if len(tsTeamIDs) == 0 {
		result.Stats.Elapsed = time.Since(t0)
		return result, nil
	}

	// 2. 用 team_ids 拉事件
	teamIDList := make([]string, 0, len(tsTeamIDs))
	for id := range tsTeamIDs {
		teamIDList = append(teamIDList, id)
		result.TSTeamNames[id] = tsTeamIDs[id]
	}
	events, err := b.Loader.GetEventsByTeamIDsInRangeCtx(ctx, sport, teamIDList, tMin, tMax)
	if err != nil {
		return nil, fmt.Errorf("team-first: fetch events: %w", err)
	}

	// 3. 按 competition_id 分组并装配为 EvidenceEventCandidate
	competitionEvents := map[string][]db.TSEvent{}
	for _, ec := range events {
		competitionEvents[ec.CompetitionID] = append(competitionEvents[ec.CompetitionID], ec.Event)
	}
	for compID, evs := range competitionEvents {
		for _, ev := range evs {
			if ev.MatchID == "" && ev.ID == "" {
				continue
			}
			result.Candidates = append(result.Candidates, EvidenceEventCandidate{
				CompetitionID:          compID,
				CompetitionName:        "", // 球队优先路径暂不查 competition 名，下游 ResolvedEventMatch 会带 competition_id
				Event:                  ev,
				CandidateScore:         0.6, // 中等先验：通过球队 ID 找到的事件，league 未确认
				StrongConstraintOK:     true,
				StrongConstraintReason: "",
			})
		}
		result.Stats.PerCompetitionEventCnt[compID] = len(evs)
		result.Stats.TotalTSEvents += len(evs)
		result.Stats.CompetitionsKept++
	}
	result.Stats.CompetitionsScanned = result.Stats.CompetitionsKept

	result.Stats.Elapsed = time.Since(t0)
	return result, nil
}

// tokenizeForIndex 把球队名拆为小写 token 集合，用于倒排索引。
// 简单实现：按空格/连字符/点拆分，每个 token >= 3 字符才保留。
func tokenizeForIndex(name string) []string {
	if name == "" {
		return nil
	}
	// 复用 name.go 的 normalizeName，把变音/连字符/标点统一
	normalized := normalizeName(name)
	// 简单按空白拆
	tokens := []string{}
	cur := []rune{}
	for _, r := range normalized {
		if r == ' ' || r == '-' || r == '.' || r == '/' || r == ',' {
			if len(cur) >= 3 {
				tokens = append(tokens, string(cur))
			}
			cur = cur[:0]
			continue
		}
		cur = append(cur, r)
	}
	if len(cur) >= 3 {
		tokens = append(tokens, string(cur))
	}
	return tokens
}
