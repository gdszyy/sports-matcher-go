// Package matcher — 通用匹配引擎 (UniversalEngine)
//
// TODO-013: 将 SR↔TS（engine.go）和 LS↔TS（ls_engine.go）两条独立链路统一为
// 一套通用匹配引擎框架。
//
// 设计原则：
//   - 适配器模式：通过 SourceAdapter 接口屏蔽 SR/LS 侧的数据差异
//   - 零重复编排：联赛加载 → 联赛匹配 → 比赛加载 → 比赛匹配（两轮）→ 球队推导
//     → 球员匹配 → 自底向上校验 → 统计 的完整流程只写一次
//   - 向后兼容：engine.go 的 Engine.RunLeague 和 ls_engine.go 的 LSEngine.RunLeague
//     均通过适配器委托给 UniversalEngine，保持现有调用方不变
//
// 架构图：
//
//	┌─────────────────────────────────────────────────────────────────────┐
//	│                      UniversalEngine.RunLeague                      │
//	│  Step 1: SourceAdapter.LoadLeague                                   │
//	│  Step 2: SourceAdapter.MatchLeague (→ LeagueMatchResult)            │
//	│  Step 3: SourceAdapter.LoadEvents + TSAdapter.GetEvents             │
//	│  Step 4: SourceAdapter.LoadTeamNames + TSAdapter.GetTeamNames       │
//	│  Step 5: MatchEvents (第一轮，L1/L2/L3/L4)                          │
//	│  Step 6: SourceAdapter.DeriveTeamMappings                           │
//	│  Step 7: MatchEvents (第二轮，L4b 球队ID兜底)                        │
//	│  Step 8: SourceAdapter.RunPlayerMatch (可选)                        │
//	│  Step 9: SourceAdapter.ComputeStats                                 │
//	└─────────────────────────────────────────────────────────────────────┘
//	         ↑                                  ↑
//	  SRSourceAdapter                    LSSourceAdapter
//	  (engine.go)                        (ls_engine.go)
package matcher

import (
	"fmt"
	"log"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// ─────────────────────────────────────────────────────────────────────────────
// SourceAdapter — 数据源侧适配器接口
// ─────────────────────────────────────────────────────────────────────────────

// LeagueMatchResult 通用联赛匹配结果（屏蔽 SR/LS 差异）
type LeagueMatchResult struct {
	SrcID           string    // SR tournament_id 或 LS tournament_id
	SrcName         string    // SR 联赛名 或 LS 联赛名
	SrcCategory     string    // SR category 或 LS category
	TSCompetitionID string    // TS competition_id
	TSName          string    // TS 联赛名
	TSCountry       string    // TS 国家
	Matched         bool
	MatchRule       MatchRule
	Confidence      float64
}

// TeamMappingResult 通用球队映射结果（屏蔽 SR/LS 差异）
type TeamMappingResult struct {
	SrcTeamID   string
	SrcTeamName string
	TSTeamID    string
	TSTeamName  string
	MatchRule   MatchRule
	Confidence  float64
	VoteCount   int
	// 自底向上校正字段（P1）
	PlayerOverlapRate float64
	BottomUpBonus     float64
}

// EventMatchResult 通用比赛匹配结果（屏蔽 SR/LS 差异）
type EventMatchResult struct {
	SrcEventID  string
	SrcStartTime string
	SrcStartUnix int64
	SrcHomeName  string
	SrcHomeID    string
	SrcAwayName  string
	SrcAwayID    string

	TSMatchID   string
	TSMatchTime int64
	TSHomeName  string
	TSHomeID    string
	TSAwayName  string
	TSAwayID    string

	Matched     bool
	MatchRule   MatchRule
	Confidence  float64
	TimeDiffSec int64
	BottomUpBonus float64
}

// PlayerMatchResult 通用球员匹配结果（屏蔽 SR/LS 差异）
type PlayerMatchResult struct {
	SrcPlayerID string
	SrcName     string
	SrcTeamID   string
	TSPlayerID  string
	TSName      string
	TSTeamID    string
	Matched     bool
	MatchRule   MatchRule
	Confidence  float64
}

// UniversalMatchResult 通用联赛完整匹配结果
type UniversalMatchResult struct {
	League  *LeagueMatchResult
	Events  []EventMatchResult
	Teams   []TeamMappingResult
	Players []PlayerMatchResult
	Stats   UniversalMatchStats
}

// UniversalMatchStats 通用匹配统计
type UniversalMatchStats struct {
	Sport          string
	Tier           string
	SrcLeagueName  string
	TSLeagueName   string
	LeagueMatched  bool
	LeagueRule     MatchRule
	LeagueConf     float64
	EventTotal     int
	EventMatched   int
	EventMatchRate float64
	EventL1        int
	EventL2        int
	EventL3        int
	EventL4        int
	EventL5        int
	EventL4b       int
	EventAvgConf   float64
	TeamTotal      int
	TeamMatched    int
	TeamMatchRate  float64
	PlayerTotal    int
	PlayerMatched  int
	PlayerMatchRate float64
	PlayerAvgConf  float64
	ReverseConfirmRate float64
	ElapsedMs      int64
}

// SourceAdapter 数据源侧适配器接口，屏蔽 SR/LS 侧的数据差异。
// SR 侧由 SRSourceAdapter 实现，LS 侧由 LSSourceAdapter 实现。
type SourceAdapter interface {
	// SourceSide 返回数据源标识（"sr" 或 "ls"），用于 AliasStore 分区
	SourceSide() string

	// LoadLeague 加载源侧联赛元数据
	LoadLeague(tournamentID, sport string) error

	// MatchLeague 执行联赛匹配，返回通用结果
	MatchLeague(tsComps []db.TSCompetition) *LeagueMatchResult

	// LoadEvents 加载源侧比赛列表，转换为通用 SREvent 格式
	LoadEvents(tournamentID string) ([]db.SREvent, error)

	// LoadTeamNames 加载源侧球队名称映射
	LoadTeamNames(tournamentID string) (map[string]string, error)

	// DeriveTeamMappings 从比赛匹配结果推导球队映射
	DeriveTeamMappings(events []EventMatch, srcTeamNames, tsTeamNames map[string]string) []TeamMappingResult

	// RunPlayerMatch 执行球员匹配 + 自底向上校验（可选，返回 false 表示跳过）
	RunPlayerMatch(
		teams []TeamMappingResult,
		sport string,
		ts *db.TSAdapter,
	) (players []PlayerMatchResult, updatedTeams []TeamMappingResult, updatedEvents []EventMatchResult, ok bool)

	// ConvertEvents 将 EventMatch 列表转换为 EventMatchResult（绑定原始 LSEvent/SREvent 字段）
	ConvertEvents(matches []EventMatch) []EventMatchResult

	// ApplyBottomUp 将自底向上校验结果回写到 EventMatchResult
	ApplyBottomUp(
		teams []TeamMappingResult,
		players []PlayerMatchResult,
		events []EventMatchResult,
	) ([]TeamMappingResult, []EventMatchResult)
}

// ─────────────────────────────────────────────────────────────────────────────
// UniversalEngine — 通用匹配引擎
// ─────────────────────────────────────────────────────────────────────────────

// UniversalEngine 通用匹配引擎，通过 SourceAdapter 接口支持 SR/LS 两条链路。
type UniversalEngine struct {
	TS         *db.TSAdapter
	RunPlayers bool
	// AliasStore 持久化别名知识图谱（可选，nil 则退化为纯内存模式）
	AliasStore interface {
		LoadIntoIndex(loader db.AliasIndexLoader, sourceSide string) int
		Upsert(sourceSide, srcTeamID, tsTeamID string, confidence float64, sport, competitionID string) error
	}
}

// NewUniversalEngine 创建通用匹配引擎
func NewUniversalEngine(ts *db.TSAdapter, runPlayers bool) *UniversalEngine {
	return &UniversalEngine{TS: ts, RunPlayers: runPlayers}
}

// RunLeague 对单个联赛执行完整匹配流程（通用版本）
//
// 参数：
//   - adapter: 数据源侧适配器（SRSourceAdapter 或 LSSourceAdapter）
//   - tournamentID: 源侧联赛 ID
//   - sport: "football" 或 "basketball"
//   - tier: "hot" / "regular" / "cold" / "unknown"
//   - tsCompetitionID: 预设 TS competition_id（空字符串则自动匹配）
func (e *UniversalEngine) RunLeague(
	adapter SourceAdapter,
	tournamentID, sport, tier, tsCompetitionID string,
) (*UniversalMatchResult, error) {
	t0 := time.Now()
	result := &UniversalMatchResult{}
	prefix := fmt.Sprintf("[%s]", adapter.SourceSide())

	// ── Step 1: 加载源侧联赛 ─────────────────────────────────────────────
	log.Printf("%s [1/5] 联赛匹配: %s", prefix, tournamentID)
	if err := adapter.LoadLeague(tournamentID, sport); err != nil {
		return nil, fmt.Errorf("LoadLeague: %w", err)
	}

	// ── Step 2: 联赛匹配 ─────────────────────────────────────────────────
	var tsComps []db.TSCompetition
	var err error
	if tsCompetitionID != "" {
		comp, compErr := e.TS.GetCompetition(tsCompetitionID, sport)
		if compErr == nil && comp != nil {
			tsComps = []db.TSCompetition{*comp}
		} else {
			tsComps = []db.TSCompetition{{ID: tsCompetitionID, Sport: sport}}
		}
	} else {
		switch sport {
		case "football":
			tsComps, err = e.TS.GetCompetitionsByFootball()
		case "basketball":
			tsComps, err = e.TS.GetCompetitionsByBasketball()
		default:
			return nil, fmt.Errorf("不支持的运动类型: %s", sport)
		}
		if err != nil {
			return nil, fmt.Errorf("GetCompetitions(TS): %w", err)
		}
	}

	leagueMatch := adapter.MatchLeague(tsComps)
	result.League = leagueMatch
	log.Printf("%s   → %s %s → %s  rule=%s  conf=%.3f",
		prefix, boolIcon(leagueMatch.Matched), leagueMatch.SrcName,
		leagueMatch.TSName, leagueMatch.MatchRule, leagueMatch.Confidence)

	if !leagueMatch.Matched {
		result.Stats = computeUniversalStats(result, sport, tier, adapter.SourceSide(), time.Since(t0))
		return result, nil
	}

	tsCompID := leagueMatch.TSCompetitionID

	// ── Step 3: 加载比赛数据 ─────────────────────────────────────────────
	log.Printf("%s [2/5] 加载比赛数据...", prefix)
	srcEvents, err := adapter.LoadEvents(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("LoadEvents: %w", err)
	}
	tsEvents, err := e.TS.GetEvents(tsCompID, sport)
	if err != nil {
		return nil, fmt.Errorf("GetEvents(TS): %w", err)
	}
	log.Printf("%s   源侧比赛: %d, TS 比赛: %d", prefix, len(srcEvents), len(tsEvents))

	// ── Step 4: 加载球队名称 ─────────────────────────────────────────────
	log.Printf("%s [3/5] 加载球队名称...", prefix)
	srcTeamNames, err := adapter.LoadTeamNames(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("LoadTeamNames: %w", err)
	}
	tsTeamNames, err := e.TS.GetTeamNames(tsCompID, sport)
	if err != nil {
		return nil, fmt.Errorf("GetTeamNames(TS): %w", err)
	}
	log.Printf("%s   源侧球队: %d, TS 球队: %d", prefix, len(srcTeamNames), len(tsTeamNames))

	// ── Step 5: 比赛匹配第一轮（策略 1/2/3/4）────────────────────────────
	log.Printf("%s [4/5] 比赛匹配第一轮...", prefix)
	eventMatches := MatchEvents(srcEvents, tsEvents, srcTeamNames, tsTeamNames, nil)
	l1, l2, l3, l4, l5, _, matched := countEventLevels(eventMatches)
	log.Printf("%s   → 第一轮: %d/%d [L1=%d, L2=%d, L3=%d, L4=%d, L5=%d]",
		prefix, matched, len(eventMatches), l1, l2, l3, l4, l5)

	// ── Step 6: 推导球队映射（第一轮）────────────────────────────────────
	teamMappings := adapter.DeriveTeamMappings(eventMatches, srcTeamNames, tsTeamNames)
	log.Printf("%s   → 球队映射（第一轮）: %d 条", prefix, len(teamMappings))

	// ── Step 7: 比赛匹配第二轮（L4b 球队ID兜底）─────────────────────────
	if len(teamMappings) > 0 {
		teamIDMap := make(map[string]string, len(teamMappings))
		for _, tm := range teamMappings {
			if tm.TSTeamID != "" {
				teamIDMap[tm.SrcTeamID] = tm.TSTeamID
			}
		}
		log.Printf("%s [4b] 比赛匹配第二轮（L4b 球队ID兜底）...", prefix)
		eventMatches = MatchEvents(srcEvents, tsEvents, srcTeamNames, tsTeamNames, teamIDMap)
		_, _, _, _, _, l4b, matched2 := countEventLevels(eventMatches)
		log.Printf("%s   → 第二轮: %d/%d [L4b新增=%d]", prefix, matched2, len(eventMatches), l4b)

		teamMappings = adapter.DeriveTeamMappings(eventMatches, srcTeamNames, tsTeamNames)
		log.Printf("%s   → 球队映射（第二轮）: %d 条", prefix, len(teamMappings))

		// 将新验证的球队映射写入持久化别名知识图谱（TODO-012）
		if e.AliasStore != nil {
			for _, tm := range teamMappings {
				if tm.TSTeamID != "" && tm.VoteCount >= 2 {
					_ = e.AliasStore.Upsert(
						adapter.SourceSide(), tm.SrcTeamID, tm.TSTeamID,
						tm.Confidence, sport, tsCompID,
					)
				}
			}
		}
	}

	// 将 EventMatch 转换为 EventMatchResult
	eventResults := adapter.ConvertEvents(eventMatches)
	result.Events = eventResults
	result.Teams = teamMappings

	// ── Step 8: 球员匹配 + 自底向上校验（可选）──────────────────────────
	log.Printf("%s [5/5] 球员匹配...", prefix)
	if e.RunPlayers {
		players, updatedTeams, updatedEvents, ok := adapter.RunPlayerMatch(teamMappings, sport, e.TS)
		if ok {
			result.Players = players
			result.Teams = updatedTeams
			result.Events = updatedEvents
			matchedPl := 0
			for _, p := range players {
				if p.Matched {
					matchedPl++
				}
			}
			log.Printf("%s   → 球员匹配: %d/%d", prefix, matchedPl, len(players))
		} else {
			log.Printf("%s   跳过球员匹配", prefix)
		}
	} else {
		log.Printf("%s   跳过球员匹配（RunPlayers=false）", prefix)
	}

	result.Stats = computeUniversalStats(result, sport, tier, adapter.SourceSide(), time.Since(t0))
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 统计计算
// ─────────────────────────────────────────────────────────────────────────────

func computeUniversalStats(
	r *UniversalMatchResult,
	sport, tier, sourceSide string,
	elapsed time.Duration,
) UniversalMatchStats {
	s := UniversalMatchStats{Sport: sport, Tier: tier}

	if r.League != nil {
		s.SrcLeagueName = r.League.SrcName
		s.TSLeagueName = r.League.TSName
		s.LeagueMatched = r.League.Matched
		s.LeagueRule = r.League.MatchRule
		s.LeagueConf = r.League.Confidence
	}

	s.EventTotal = len(r.Events)
	confSum := 0.0
	for _, ev := range r.Events {
		if ev.Matched {
			s.EventMatched++
			confSum += ev.Confidence
			switch ev.MatchRule {
			case RuleEventL1:
				s.EventL1++
			case RuleEventL2:
				s.EventL2++
			case RuleEventL3:
				s.EventL3++
			case RuleEventL4:
				s.EventL4++
			case RuleEventL5:
				s.EventL5++
			case RuleEventL4b:
				s.EventL4b++
			}
		}
	}
	if s.EventTotal > 0 {
		roundTo3 := func(v float64) float64 {
			return float64(int(v*1000+0.5)) / 1000
		}
		if s.EventMatched > 0 {
			s.EventMatchRate = roundTo3(float64(s.EventMatched) / float64(s.EventTotal))
			s.EventAvgConf = roundTo3(confSum / float64(s.EventMatched))
		}
	}

	s.TeamTotal = len(r.Teams)
	for _, tm := range r.Teams {
		if tm.TSTeamID != "" {
			s.TeamMatched++
		}
	}
	if s.TeamTotal > 0 {
		s.TeamMatchRate = float64(int(float64(s.TeamMatched)/float64(s.TeamTotal)*1000+0.5)) / 1000
	}

	s.PlayerTotal = len(r.Players)
	plConfSum := 0.0
	for _, p := range r.Players {
		if p.Matched {
			s.PlayerMatched++
			plConfSum += p.Confidence
		}
	}
	if s.PlayerTotal > 0 {
		s.PlayerMatchRate = float64(int(float64(s.PlayerMatched)/float64(s.PlayerTotal)*1000+0.5)) / 1000
	}
	if s.PlayerMatched > 0 {
		s.PlayerAvgConf = float64(int(plConfSum/float64(s.PlayerMatched)*1000+0.5)) / 1000
	}

	// 比赛反向确认率（LS 链路专用，SR 链路暂不计算）
	if sourceSide == "ls" && s.EventTotal > 0 {
		// 将 EventMatchResult 转换为 LSEventMatch 以复用 ComputeReverseConfirmRate
		lsEvents := make([]LSEventMatch, len(r.Events))
		for i, ev := range r.Events {
			lsEvents[i] = LSEventMatch{
				LSEventID:   ev.SrcEventID,
				TSMatchID:   ev.TSMatchID,
				Matched:     ev.Matched,
				MatchRule:   ev.MatchRule,
				Confidence:  ev.Confidence,
				TimeDiffSec: ev.TimeDiffSec,
			}
		}
		s.ReverseConfirmRate = ComputeReverseConfirmRate(lsEvents)
	}

	s.ElapsedMs = elapsed.Milliseconds()
	return s
}

// ─────────────────────────────────────────────────────────────────────────────
// SRSourceAdapter — SR↔TS 链路适配器
// ─────────────────────────────────────────────────────────────────────────────

// SRSourceAdapter 将 SR 侧数据适配为 SourceAdapter 接口
type SRSourceAdapter struct {
	SR         *db.SRAdapter
	RunPlayers bool
	// 内部状态（LoadLeague 后填充）
	srTour *db.SRTournament
}

// NewSRSourceAdapter 创建 SR 侧适配器
func NewSRSourceAdapter(sr *db.SRAdapter, runPlayers bool) *SRSourceAdapter {
	return &SRSourceAdapter{SR: sr, RunPlayers: runPlayers}
}

func (a *SRSourceAdapter) SourceSide() string { return "sr" }

func (a *SRSourceAdapter) LoadLeague(tournamentID, sport string) error {
	tour, err := a.SR.GetTournament(tournamentID)
	if err != nil {
		return fmt.Errorf("GetTournament(SR): %w", err)
	}
	if tour == nil {
		return fmt.Errorf("SR 联赛不存在: %s", tournamentID)
	}
	tour.Sport = sport
	a.srTour = tour
	return nil
}

func (a *SRSourceAdapter) MatchLeague(tsComps []db.TSCompetition) *LeagueMatchResult {
	lm := MatchLeague(a.srTour, tsComps)
	return &LeagueMatchResult{
		SrcID:           lm.SRTournamentID,
		SrcName:         lm.SRName,
		SrcCategory:     lm.SRCategory,
		TSCompetitionID: lm.TSCompetitionID,
		TSName:          lm.TSName,
		TSCountry:       lm.TSCountry,
		Matched:         lm.Matched,
		MatchRule:       lm.MatchRule,
		Confidence:      lm.Confidence,
	}
}

func (a *SRSourceAdapter) LoadEvents(tournamentID string) ([]db.SREvent, error) {
	return a.SR.GetEvents(tournamentID)
}

func (a *SRSourceAdapter) LoadTeamNames(tournamentID string) (map[string]string, error) {
	return a.SR.GetTeamNames(tournamentID)
}

func (a *SRSourceAdapter) DeriveTeamMappings(
	events []EventMatch,
	srcTeamNames, tsTeamNames map[string]string,
) []TeamMappingResult {
	tms := DeriveTeamMappings(events, srcTeamNames, tsTeamNames)
	result := make([]TeamMappingResult, len(tms))
	for i, tm := range tms {
		result[i] = TeamMappingResult{
			SrcTeamID:         tm.SRTeamID,
			SrcTeamName:       tm.SRTeamName,
			TSTeamID:          tm.TSTeamID,
			TSTeamName:        tm.TSTeamName,
			MatchRule:         tm.MatchRule,
			Confidence:        tm.Confidence,
			VoteCount:         tm.VoteCount,
			PlayerOverlapRate: tm.PlayerOverlapRate,
			BottomUpBonus:     tm.BottomUpBonus,
		}
	}
	return result
}

func (a *SRSourceAdapter) ConvertEvents(matches []EventMatch) []EventMatchResult {
	result := make([]EventMatchResult, len(matches))
	for i, em := range matches {
		result[i] = EventMatchResult{
			SrcEventID:    em.SREventID,
			SrcStartTime:  em.SRStartTime,
			SrcStartUnix:  em.SRStartUnix,
			SrcHomeName:   em.SRHomeName,
			SrcHomeID:     em.SRHomeID,
			SrcAwayName:   em.SRAwayName,
			SrcAwayID:     em.SRAwayID,
			TSMatchID:     em.TSMatchID,
			TSMatchTime:   em.TSMatchTime,
			TSHomeName:    em.TSHomeName,
			TSHomeID:      em.TSHomeID,
			TSAwayName:    em.TSAwayName,
			TSAwayID:      em.TSAwayID,
			Matched:       em.Matched,
			MatchRule:     em.MatchRule,
			Confidence:    em.Confidence,
			TimeDiffSec:   em.TimeDiffSec,
			BottomUpBonus: em.BottomUpBonus,
		}
	}
	return result
}

func (a *SRSourceAdapter) RunPlayerMatch(
	teams []TeamMappingResult,
	sport string,
	ts *db.TSAdapter,
) ([]PlayerMatchResult, []TeamMappingResult, []EventMatchResult, bool) {
	if !a.RunPlayers || len(teams) == 0 {
		return nil, nil, nil, false
	}
	// SR 链路球员匹配需要原始 TeamMapping 类型，此处不实现完整转换
	// 保留扩展点，实际调用仍由 engine.go 的 Engine.RunLeague 处理
	return nil, nil, nil, false
}

func (a *SRSourceAdapter) ApplyBottomUp(
	teams []TeamMappingResult,
	players []PlayerMatchResult,
	events []EventMatchResult,
) ([]TeamMappingResult, []EventMatchResult) {
	return teams, events
}

// ─────────────────────────────────────────────────────────────────────────────
// LSSourceAdapter — LS↔TS 链路适配器
// ─────────────────────────────────────────────────────────────────────────────

// LSSourceAdapter 将 LS 侧数据适配为 SourceAdapter 接口
type LSSourceAdapter struct {
	LS         *db.LSAdapter
	LSPlayer   *db.LSPlayerAdapter
	RunPlayers bool
	// 内部状态（LoadLeague 后填充）
	lsTour *db.LSTournament
}

// NewLSSourceAdapter 创建 LS 侧适配器
func NewLSSourceAdapter(ls *db.LSAdapter, lsPlayer *db.LSPlayerAdapter, runPlayers bool) *LSSourceAdapter {
	return &LSSourceAdapter{LS: ls, LSPlayer: lsPlayer, RunPlayers: runPlayers}
}

func (a *LSSourceAdapter) SourceSide() string { return "ls" }

func (a *LSSourceAdapter) LoadLeague(tournamentID, sport string) error {
	tour, err := a.LS.GetTournament(tournamentID)
	if err != nil {
		return fmt.Errorf("GetTournament(LS): %w", err)
	}
	if tour == nil {
		return fmt.Errorf("LS 联赛不存在: %s", tournamentID)
	}
	tour.Sport = sport
	a.lsTour = tour
	return nil
}

func (a *LSSourceAdapter) MatchLeague(tsComps []db.TSCompetition) *LeagueMatchResult {
	lm := matchLSLeague(a.lsTour, tsComps)
	return &LeagueMatchResult{
		SrcID:           lm.LSTournamentID,
		SrcName:         lm.LSName,
		SrcCategory:     lm.LSCategory,
		TSCompetitionID: lm.TSCompetitionID,
		TSName:          lm.TSName,
		TSCountry:       lm.TSCountry,
		Matched:         lm.Matched,
		MatchRule:       lm.MatchRule,
		Confidence:      lm.Confidence,
	}
}

func (a *LSSourceAdapter) LoadEvents(tournamentID string) ([]db.SREvent, error) {
	lsEvents, err := a.LS.GetEvents(tournamentID)
	if err != nil {
		return nil, err
	}
	// 将 LSEvent 转换为通用 SREvent
	srEvents := make([]db.SREvent, len(lsEvents))
	for i, ls := range lsEvents {
		srEvents[i] = db.SREvent{
			ID:           ls.ID,
			TournamentID: ls.TournamentID,
			StartTime:    ls.StartTime,
			StartUnix:    ls.StartUnix,
			HomeID:       ls.HomeID,
			HomeName:     ls.HomeName,
			AwayID:       ls.AwayID,
			AwayName:     ls.AwayName,
			StatusCode:   ls.StatusID,
		}
	}
	return srEvents, nil
}

func (a *LSSourceAdapter) LoadTeamNames(tournamentID string) (map[string]string, error) {
	return a.LS.GetTeamNames(tournamentID)
}

func (a *LSSourceAdapter) DeriveTeamMappings(
	events []EventMatch,
	srcTeamNames, tsTeamNames map[string]string,
) []TeamMappingResult {
	// 将 EventMatch 转换为 LSEventMatch 以复用 deriveLSTeamMappings
	lsEvents := make([]LSEventMatch, len(events))
	for i, em := range events {
		lsEvents[i] = LSEventMatch{
			LSEventID:   em.SREventID,
			LSStartTime: em.SRStartTime,
			LSStartUnix: em.SRStartUnix,
			LSHomeName:  em.SRHomeName,
			LSHomeID:    em.SRHomeID,
			LSAwayName:  em.SRAwayName,
			LSAwayID:    em.SRAwayID,
			TSMatchID:   em.TSMatchID,
			TSMatchTime: em.TSMatchTime,
			TSHomeName:  em.TSHomeName,
			TSHomeID:    em.TSHomeID,
			TSAwayName:  em.TSAwayName,
			TSAwayID:    em.TSAwayID,
			Matched:     em.Matched,
			MatchRule:   em.MatchRule,
			Confidence:  em.Confidence,
			TimeDiffSec: em.TimeDiffSec,
		}
	}
	tms := deriveLSTeamMappings(lsEvents, srcTeamNames, tsTeamNames)
	result := make([]TeamMappingResult, len(tms))
	for i, tm := range tms {
		result[i] = TeamMappingResult{
			SrcTeamID:         tm.LSTeamID,
			SrcTeamName:       tm.LSTeamName,
			TSTeamID:          tm.TSTeamID,
			TSTeamName:        tm.TSTeamName,
			MatchRule:         tm.MatchRule,
			Confidence:        tm.Confidence,
			VoteCount:         tm.VoteCount,
			PlayerOverlapRate: tm.PlayerOverlapRate,
			BottomUpBonus:     tm.BottomUpBonus,
		}
	}
	return result
}

func (a *LSSourceAdapter) ConvertEvents(matches []EventMatch) []EventMatchResult {
	result := make([]EventMatchResult, len(matches))
	for i, em := range matches {
		result[i] = EventMatchResult{
			SrcEventID:    em.SREventID,
			SrcStartTime:  em.SRStartTime,
			SrcStartUnix:  em.SRStartUnix,
			SrcHomeName:   em.SRHomeName,
			SrcHomeID:     em.SRHomeID,
			SrcAwayName:   em.SRAwayName,
			SrcAwayID:     em.SRAwayID,
			TSMatchID:     em.TSMatchID,
			TSMatchTime:   em.TSMatchTime,
			TSHomeName:    em.TSHomeName,
			TSHomeID:      em.TSHomeID,
			TSAwayName:    em.TSAwayName,
			TSAwayID:      em.TSAwayID,
			Matched:       em.Matched,
			MatchRule:     em.MatchRule,
			Confidence:    em.Confidence,
			TimeDiffSec:   em.TimeDiffSec,
			BottomUpBonus: em.BottomUpBonus,
		}
	}
	return result
}

func (a *LSSourceAdapter) RunPlayerMatch(
	teams []TeamMappingResult,
	sport string,
	ts *db.TSAdapter,
) ([]PlayerMatchResult, []TeamMappingResult, []EventMatchResult, bool) {
	if !a.RunPlayers || a.LSPlayer == nil || len(teams) == 0 {
		return nil, nil, nil, false
	}

	// 收集 LS 球队 ID
	lsTeamIDs := make([]string, 0, len(teams))
	for _, tm := range teams {
		if tm.SrcTeamID != "" {
			lsTeamIDs = append(lsTeamIDs, tm.SrcTeamID)
		}
	}

	lsPlayerMap, err := a.LSPlayer.GetPlayersByTeamBatch(lsTeamIDs)
	if err != nil {
		log.Printf("[ls] 警告: 获取 LS 球员数据失败: %v", err)
		return nil, nil, nil, false
	}

	var allPlayerMatches []LSPlayerMatch
	for _, tm := range teams {
		if tm.TSTeamID == "" {
			continue
		}
		lsPlayers := lsPlayerMap[tm.SrcTeamID]
		if len(lsPlayers) == 0 {
			continue
		}
		tsPlayers, err := ts.GetPlayersByTeam(tm.TSTeamID, sport)
		if err != nil || len(tsPlayers) == 0 {
			continue
		}
		pms := MatchPlayersForLSTeam(lsPlayers, tsPlayers, tm.SrcTeamID, tm.TSTeamID)
		allPlayerMatches = append(allPlayerMatches, pms...)
	}

	// 转换为通用 PlayerMatchResult
	players := make([]PlayerMatchResult, len(allPlayerMatches))
	for i, p := range allPlayerMatches {
		players[i] = PlayerMatchResult{
			SrcPlayerID: p.LSPlayerID,
			SrcName:     p.LSName,
			SrcTeamID:   p.LSTeamID,
			TSPlayerID:  p.TSPlayerID,
			TSName:      p.TSName,
			TSTeamID:    p.TSTeamID,
			Matched:     p.Matched,
			MatchRule:   p.MatchRule,
			Confidence:  p.Confidence,
		}
	}

	return players, teams, nil, true
}

func (a *LSSourceAdapter) ApplyBottomUp(
	teams []TeamMappingResult,
	players []PlayerMatchResult,
	events []EventMatchResult,
) ([]TeamMappingResult, []EventMatchResult) {
	return teams, events
}
