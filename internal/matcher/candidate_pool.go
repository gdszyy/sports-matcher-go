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
	"fmt"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// TSEventLoader 是 CandidatePoolBuilder 依赖的最小数据访问接口。
// *db.TSAdapter 自然实现该接口；单测可用 stub 替换以避免 DB 依赖。
type TSEventLoader interface {
	GetEvents(competitionID, sport string) ([]db.TSEvent, error)
	GetTeamNames(competitionID, sport string) (map[string]string, error)
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

	for _, cand := range candidates {
		compID := cand.Competition.ID
		if compID == "" {
			// 没有 ID 的 candidate 直接跳过（防御性，正常调用方不应该这样）
			continue
		}

		events, err := b.Loader.GetEvents(compID, sport)
		if err != nil {
			return nil, fmt.Errorf("candidate pool: GetEvents(%s, %s): %w", compID, sport, err)
		}
		teamNames, err := b.Loader.GetTeamNames(compID, sport)
		if err != nil {
			return nil, fmt.Errorf("candidate pool: GetTeamNames(%s, %s): %w", compID, sport, err)
		}

		// 合并 TS 球队名（union；不同 competition 的同 ID 极少冲突，后到覆盖前到即可）
		for tsTeamID, name := range teamNames {
			if tsTeamID == "" {
				continue
			}
			if _, exists := result.TSTeamNames[tsTeamID]; !exists {
				result.TSTeamNames[tsTeamID] = name
			}
		}

		// 把该 competition 的每个事件包装为 EvidenceEventCandidate
		// HomeTeamCandidateScore / AwayTeamCandidateScore 在 P1 暂留 0，P2 阶段再填充。
		eventCount := 0
		for _, ev := range events {
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
