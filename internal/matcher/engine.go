// Package matcher — 主匹配引擎（完整流程编排）
package matcher

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// Engine 匹配引擎，持有数据库适配器
type Engine struct {
	SR         *db.SRAdapter
	TS         *db.TSAdapter
	RunPlayers bool
}

// NewEngine 创建匹配引擎
func NewEngine(sr *db.SRAdapter, ts *db.TSAdapter, runPlayers bool) *Engine {
	return &Engine{SR: sr, TS: ts, RunPlayers: runPlayers}
}

// RunLeague 对单个联赛执行完整匹配流程
func (e *Engine) RunLeague(tournamentID, sport, tier, tsCompetitionID string) (*MatchResult, error) {
	t0 := time.Now()
	result := &MatchResult{}

	// ── Step 1: 加载 SR 联赛 ─────────────────────────────────────────────
	log.Printf("  [1/5] 联赛匹配: %s", tournamentID)
	srTour, err := e.SR.GetTournament(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("GetTournament: %w", err)
	}
	if srTour == nil {
		return nil, fmt.Errorf("SR 联赛不存在: %s", tournamentID)
	}
	srTour.Sport = sport

	// ── Step 2: 联赛匹配 ─────────────────────────────────────────────────
	var tsComps []db.TSCompetition
	if tsCompetitionID != "" {
		// 预设 TS ID，注入映射表（使用 sport:tournament_id 格式的 key）
		mapKey := fmt.Sprintf("%s:%s", sport, tournamentID)
		KnownLeagueMap[mapKey] = tsCompetitionID
		comp, err := e.TS.GetCompetition(tsCompetitionID, sport)
		if err == nil && comp != nil {
			tsComps = []db.TSCompetition{*comp}
		}
	} else {
		switch sport {
		case "football":
			tsComps, err = e.TS.GetCompetitionsByFootball()
		case "basketball":
			tsComps, err = e.TS.GetCompetitionsByBasketball()
		}
		if err != nil {
			return nil, fmt.Errorf("GetCompetitions: %w", err)
		}
	}

	leagueMatch := MatchLeague(srTour, tsComps)
	result.League = leagueMatch
	log.Printf("    → %s %s → %s  rule=%s  conf=%.3f",
		boolIcon(leagueMatch.Matched), leagueMatch.SRName, leagueMatch.TSName,
		leagueMatch.MatchRule, leagueMatch.Confidence)

	if !leagueMatch.Matched {
		result.Stats = computeStats(result, sport, tier, time.Since(t0))
		return result, nil
	}

	tsCompID := leagueMatch.TSCompetitionID

	// ── Step 3: 加载比赛数据 ─────────────────────────────────────────────
	log.Printf("  [2/5] 加载比赛数据...")
	srEvents, err := e.SR.GetEvents(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("GetEvents(SR): %w", err)
	}
	tsEvents, err := e.TS.GetEvents(tsCompID, sport)
	if err != nil {
		return nil, fmt.Errorf("GetEvents(TS): %w", err)
	}
	log.Printf("    SR 比赛: %d, TS 比赛: %d", len(srEvents), len(tsEvents))

	// ── Step 4: 加载球队名称 ─────────────────────────────────────────────
	log.Printf("  [3/5] 加载球队名称...")
	srTeamNames, err := e.SR.GetTeamNames(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("GetTeamNames(SR): %w", err)
	}
	tsTeamNames, err := e.TS.GetTeamNames(tsCompID, sport)
	if err != nil {
		return nil, fmt.Errorf("GetTeamNames(TS): %w", err)
	}
	log.Printf("    SR 球队: %d, TS 球队: %d", len(srTeamNames), len(tsTeamNames))

	// ── Step 5: 比赛匹配第一轮（L1/L2/L3/L4）────────────────────────────
	// L4（超宽时间+别名）由 MatchEvents 内部的 TeamAliasIndex 驱动，无需外部传入 teamIDMap。
	log.Printf("  [4/5] 比赛匹配第一轮（L1/L2/L3/L4）...")
	eventMatches := MatchEvents(srEvents, tsEvents, srTeamNames, tsTeamNames, nil)
	l1, l2, l3, l4, l5, _, _, matched := countEventLevels(eventMatches)
	log.Printf("    → 第一轮: %d/%d [L1=%d, L2=%d, L3=%d, L4=%d, L5=%d]", matched, len(eventMatches), l1, l2, l3, l4, l5)

	// 推导球队映射（第一轮）
	teamMappings := DeriveTeamMappings(eventMatches, srTeamNames, tsTeamNames)
	log.Printf("    → 球队映射（第一轮）: %d 条", len(teamMappings))

	// ── Step 6: 比赛匹配第二轮（L4b 球队ID兜底）─────────────────────────
	// 将第一轮推导的球队映射作为 teamIDMap 传入，激活 L4b 球队 ID 精确对兜底。
	// 注意：L4（超宽时间+别名）已在第一轮内部由 TeamAliasIndex 驱动，无需第二轮重跑。
	if len(teamMappings) > 0 {
		teamIDMap := make(map[string]string, len(teamMappings))
		for _, tm := range teamMappings {
			if tm.TSTeamID != "" {
				teamIDMap[tm.SRTeamID] = tm.TSTeamID
			}
		}
		log.Printf("  [4b] 比赛匹配第二轮（L4b 球队ID兜底）...")
		eventMatches = MatchEvents(srEvents, tsEvents, srTeamNames, tsTeamNames, teamIDMap)
		_, _, _, _, _, l4b, l6, matched := countEventLevels(eventMatches)
		log.Printf("    → 第二轮: %d/%d [L4b新增=%d, L6新增=%d]", matched, len(eventMatches), l4b, l6)

		// 重新推导球队映射（包含 L4/L4b 场次贡献）
		teamMappings = DeriveTeamMappings(eventMatches, srTeamNames, tsTeamNames)
		log.Printf("    → 球队映射（第二轮）: %d 条", len(teamMappings))
	}

	result.Events = eventMatches
	result.Teams = teamMappings

	// ── Step 7: 球员匹配 + 自底向上反向验证 ────────────────────────────
	if e.RunPlayers && len(teamMappings) > 0 {
		log.Printf("  [5/5] 球员匹配...")
		var allPlayerMatches []PlayerMatch
		srTeamPlayerCounts := make(map[string]int)

		for _, tm := range teamMappings {
			srPlayers, err := e.SR.GetPlayersByTeam(tm.SRTeamID)
			if err != nil || len(srPlayers) == 0 {
				continue
			}
			tsPlayers, err := e.TS.GetPlayersByTeam(tm.TSTeamID, sport)
			if err != nil {
				continue
			}
			srTeamPlayerCounts[tm.SRTeamID] = len(srPlayers)

			pms := MatchPlayersForTeam(srPlayers, tsPlayers, tm.SRTeamID, tm.TSTeamID)
			allPlayerMatches = append(allPlayerMatches, pms...)
		}

		matchedPl := 0
		for _, p := range allPlayerMatches {
			if p.Matched {
				matchedPl++
			}
		}
		log.Printf("    → 球员匹配: %d/%d", matchedPl, len(allPlayerMatches))
		result.Players = allPlayerMatches

		// 自底向上校正
		log.Printf("  [自底向上] 反向验证校正置信度...")
		result.Teams, result.Events = ApplyBottomUp(
			result.Teams, result.Players, result.Events, srTeamPlayerCounts,
		)
	} else {
		log.Printf("  [5/5] 跳过球员匹配")
	}

	result.Stats = computeStats(result, sport, tier, time.Since(t0))
	return result, nil
}

// computeStats 计算统计摘要
func computeStats(r *MatchResult, sport, tier string, elapsed time.Duration) MatchStats {
	s := MatchStats{
		Sport: sport,
		Tier:  tier,
	}

	if r.League != nil {
		s.LeagueSRName = r.League.SRName
		s.LeagueTSName = r.League.TSName
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
			case RuleEventL6:
				s.EventL6++
			}
		}
	}
	if s.EventMatched > 0 {
		s.EventMatchRate = math.Round(float64(s.EventMatched)/float64(s.EventTotal)*1000) / 1000
		s.EventAvgConf = math.Round(confSum/float64(s.EventMatched)*1000) / 1000
	}

	s.TeamTotal = len(r.Teams)
	for _, tm := range r.Teams {
		if tm.TSTeamID != "" {
			s.TeamMatched++
		}
	}
	if s.TeamTotal > 0 {
		s.TeamMatchRate = math.Round(float64(s.TeamMatched)/float64(s.TeamTotal)*1000) / 1000
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
		s.PlayerMatchRate = math.Round(float64(s.PlayerMatched)/float64(s.PlayerTotal)*1000) / 1000
	}
	if s.PlayerMatched > 0 {
		s.PlayerAvgConf = math.Round(plConfSum/float64(s.PlayerMatched)*1000) / 1000
	}

	s.ElapsedMs = elapsed.Milliseconds()
	return s
}

// countEventLevels 统计各级匹配数量
func countEventLevels(events []EventMatch) (l1, l2, l3, l4, l5, l4b, l6, matched int) {
	for _, ev := range events {
		if !ev.Matched {
			continue
		}
		matched++
		switch ev.MatchRule {
		case RuleEventL1:
			l1++
		case RuleEventL2:
			l2++
		case RuleEventL3:
			l3++
		case RuleEventL4:
			l4++
		case RuleEventL5:
			l5++
		case RuleEventL4b:
			l4b++
		case RuleEventL6:
			l6++
		}
	}
	return l1, l2, l3, l4, l5, l4b, l6, matched
}

func boolIcon(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}
