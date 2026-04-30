package matcher

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

const (
	defaultEvidenceFirstCandidateLimit       = 8
	defaultEvidenceFirstMinHighConfEvents    = 3
	defaultEvidenceFirstMinTwoTeamAnchorRate = 0.60
	defaultEvidenceFirstMinCandidateGap      = 0.10
	defaultEvidenceFirstHighConfScore        = 0.85
)

// EvidenceFirstOptions 控制 Evidence-First 实验入口；默认不写回任何持久化资产。
type EvidenceFirstOptions struct {
	CandidateLimit       int                            `json:"candidate_limit,omitempty"`
	AllowWriteBack       bool                           `json:"allow_write_back"`
	ReviewOutputPath     string                         `json:"review_output_path,omitempty"`
	MatcherConfig        EvidenceEventMatcherConfig     `json:"matcher_config,omitempty"`
	AggregatorConfig     LeagueEvidenceAggregatorConfig `json:"aggregator_config,omitempty"`
	MinHighConfEvents    int                            `json:"min_high_conf_events,omitempty"`
	MinTwoTeamAnchorRate float64                        `json:"min_two_team_anchor_rate,omitempty"`
	MinCandidateGap      float64                        `json:"min_candidate_gap,omitempty"`
	HighConfScore        float64                        `json:"high_conf_score,omitempty"`
}

// EvidenceFirstWriteBackResult 记录安全写回门槛评估与实际写入结果。
type EvidenceFirstWriteBackResult struct {
	Enabled              bool     `json:"enabled"`
	Allowed              bool     `json:"allowed"`
	Reason               string   `json:"reason,omitempty"`
	TeamAliasUpserts     int      `json:"team_alias_upserts"`
	StrongMapUpserted    bool     `json:"strong_map_upserted"`
	BlockedReasons       []string `json:"blocked_reasons,omitempty"`
	MinHighConfEvents    int      `json:"min_high_conf_events"`
	MinTwoTeamAnchorRate float64  `json:"min_two_team_anchor_rate"`
	MinCandidateGap      float64  `json:"min_candidate_gap"`
	ActualHighConfEvents int      `json:"actual_high_conf_events"`
	ActualTwoTeamAnchor  float64  `json:"actual_two_team_anchor_rate"`
	ActualCandidateGap   float64  `json:"actual_candidate_gap"`
	HardVeto             bool     `json:"hard_veto"`
	DecisionStatus       string   `json:"decision_status"`
}

// EvidenceFirstKnownMapStatus 将 KnownMap RCR 验证结果暴露给 CLI/API 审核。
type EvidenceFirstKnownMapStatus struct {
	Checked         bool             `json:"checked"`
	TournamentID    string           `json:"tournament_id,omitempty"`
	TSCompetitionID string           `json:"ts_competition_id,omitempty"`
	Status          ValidationStatus `json:"status,omitempty"`
	RCR             float64          `json:"rcr,omitempty"`
	Suspect         bool             `json:"suspect"`
	ManualOverride  bool             `json:"manual_override"`
	Reason          string           `json:"reason,omitempty"`
}

// EvidenceFirstReview 是无需再次查库即可人工判断的审核快照。
type EvidenceFirstReview struct {
	GeneratedAt     time.Time                    `json:"generated_at"`
	SourceSide      string                       `json:"source_side"`
	TournamentID    string                       `json:"tournament_id"`
	Sport           string                       `json:"sport"`
	Tier            string                       `json:"tier,omitempty"`
	Source          LeagueEvidenceSource         `json:"source"`
	Decision        LeagueEvidenceDecision       `json:"decision"`
	KnownMap        EvidenceFirstKnownMapStatus  `json:"known_map"`
	WriteBack       EvidenceFirstWriteBackResult `json:"write_back"`
	CandidateCount  int                          `json:"candidate_count"`
	EventTotal      int                          `json:"event_total"`
	ResolvedMatches []ResolvedEventMatch         `json:"resolved_matches"`
	CandidateEdges  []EventEvidenceEdge          `json:"candidate_edges,omitempty"`
	Eliminated      []ConflictElimination        `json:"eliminated,omitempty"`
	TeamMappings    []TeamMappingResult          `json:"team_mappings,omitempty"`
	SelectedEvents  []LeagueEventExample         `json:"selected_events,omitempty"`
	Performance     EvidenceFirstPerformance     `json:"performance"`
}

// EvidenceFirstPerformance 记录端到端运行耗时与候选规模。
type EvidenceFirstPerformance struct {
	ElapsedMs          int64 `json:"elapsed_ms"`
	SourceEvents       int   `json:"source_events"`
	TSCompetitions     int   `json:"ts_competitions"`
	TSEvents           int   `json:"ts_events"`
	CandidateEdges     int   `json:"candidate_edges"`
	ResolvedMatchCount int   `json:"resolved_match_count"`
}

// EvidenceFirstResult 是 RunLeagueEvidenceFirst 的完整返回值。
type EvidenceFirstResult struct {
	SourceSide         string                       `json:"source_side"`
	TournamentID       string                       `json:"tournament_id"`
	Sport              string                       `json:"sport"`
	Tier               string                       `json:"tier,omitempty"`
	Source             LeagueEvidenceSource         `json:"source"`
	Competitions       []LeagueEvidenceCompetition  `json:"competitions"`
	CompetitionScores  map[string]float64           `json:"competition_scores,omitempty"`
	ConflictResolution ConflictResolutionResult     `json:"conflict_resolution"`
	Decision           LeagueEvidenceDecision       `json:"decision"`
	KnownMap           EvidenceFirstKnownMapStatus  `json:"known_map"`
	League             *LeagueMatchResult           `json:"league,omitempty"`
	Events             []ResolvedEventMatch         `json:"events"`
	Teams              []TeamMappingResult          `json:"teams"`
	Stats              UniversalMatchStats          `json:"stats"`
	WriteBack          EvidenceFirstWriteBackResult `json:"write_back"`
	Review             *EvidenceFirstReview         `json:"review,omitempty"`
}

type evidenceCompetitionCandidate struct {
	comp  db.TSCompetition
	score float64
	known bool
}

// RunLeagueEvidenceFirst 执行 Evidence-First 端到端实验流程：源侧加载、候选比赛池、P3 比赛证据、P4 联赛聚合、KnownMap 验证和可选安全写回。
func (e *UniversalEngine) RunLeagueEvidenceFirst(adapter SourceAdapter, tournamentID, sport, tier, tsCompetitionID string, opts EvidenceFirstOptions) (*EvidenceFirstResult, error) {
	if e == nil || e.TS == nil {
		return nil, fmt.Errorf("UniversalEngine.TS is required")
	}
	opts = normalizeEvidenceFirstOptions(opts)
	t0 := time.Now()
	prefix := fmt.Sprintf("[%s/evidence-first]", adapter.SourceSide())
	log.Printf("%s [1/6] 加载源侧联赛: %s", prefix, tournamentID)
	if err := adapter.LoadLeague(tournamentID, sport); err != nil {
		return nil, fmt.Errorf("LoadLeague: %w", err)
	}

	srcEvents, err := adapter.LoadEvents(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("LoadEvents: %w", err)
	}
	srcTeamNames, err := adapter.LoadTeamNames(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("LoadTeamNames: %w", err)
	}
	source := buildEvidenceSource(adapter, tournamentID, sport, srcEvents, srcTeamNames)

	log.Printf("%s [2/6] 生成 TS competition 候选...", prefix)
	candidateComps, scoreMap, err := e.selectEvidenceCompetitions(adapter, tournamentID, sport, tsCompetitionID, opts.CandidateLimit)
	if err != nil {
		return nil, err
	}
	if len(candidateComps) == 0 {
		return nil, fmt.Errorf("no TS competition candidates")
	}

	log.Printf("%s [3/6] 加载候选比赛池...", prefix)
	var eventCandidates []EvidenceEventCandidate
	tsTeamNames := make(map[string]string)
	competitions := make([]LeagueEvidenceCompetition, 0, len(candidateComps))
	tsEventCount := 0
	for _, cc := range candidateComps {
		comp := cc.comp
		competitions = append(competitions, NewLeagueEvidenceCompetitionFromTS(comp))
		events, evErr := e.TS.GetEvents(comp.ID, sport)
		if evErr != nil {
			log.Printf("%s   跳过候选 %s: GetEvents 失败: %v", prefix, comp.ID, evErr)
			continue
		}
		events = filterTSEventsBySourceWindow(srcEvents, events, 14*24*3600)
		teams, teamErr := e.TS.GetTeamNames(comp.ID, sport)
		if teamErr != nil {
			log.Printf("%s   候选 %s: GetTeamNames 失败: %v", prefix, comp.ID, teamErr)
		} else {
			for k, v := range teams {
				tsTeamNames[k] = v
			}
		}
		for _, ev := range events {
			eventCandidates = append(eventCandidates, EvidenceEventCandidate{
				CompetitionID:          comp.ID,
				CompetitionName:        comp.Name,
				Event:                  ev,
				CandidateScore:         cc.score,
				HomeTeamCandidateScore: 0.50,
				AwayTeamCandidateScore: 0.50,
				StrongConstraintOK:     true,
			})
		}
		tsEventCount += len(events)
	}
	log.Printf("%s   源比赛=%d, 候选联赛=%d, 候选 TS 比赛=%d", prefix, len(srcEvents), len(competitions), tsEventCount)

	log.Printf("%s [4/6] 比赛证据匹配（P3 两轮）...", prefix)
	eventMatcher := NewEvidenceEventMatcher(opts.MatcherConfig)
	conflict := eventMatcher.MatchTwoRound(srcEvents, eventCandidates, srcTeamNames, tsTeamNames)
	teamMappings := adapter.DeriveTeamMappings(resolvedToEventMatches(conflict.Matches), srcTeamNames, tsTeamNames)

	knownStatus := e.validateEvidenceKnownMap(adapter, tournamentID, sport, conflict.Matches)
	knownRCR := -1.0
	if knownStatus.Checked {
		knownRCR = knownStatus.RCR
	}

	log.Printf("%s [5/6] 联赛证据聚合（P4）...", prefix)
	aggregator := NewLeagueEvidenceAggregator(opts.AggregatorConfig)
	decision := aggregator.AggregateWithKnownMapRCR(source, conflict.Matches, competitions, knownRCR)
	league := leagueResultFromEvidence(source, decision)

	writeBack := e.applyEvidenceFirstWriteBack(adapter, sport, decision, teamMappings, opts)
	stats := computeEvidenceFirstStats(source, league, conflict.Matches, teamMappings, sport, tier, adapter.SourceSide(), time.Since(t0))

	result := &EvidenceFirstResult{
		SourceSide:         adapter.SourceSide(),
		TournamentID:       tournamentID,
		Sport:              sport,
		Tier:               tier,
		Source:             source,
		Competitions:       competitions,
		CompetitionScores:  scoreMap,
		ConflictResolution: conflict,
		Decision:           decision,
		KnownMap:           knownStatus,
		League:             league,
		Events:             conflict.Matches,
		Teams:              teamMappings,
		Stats:              stats,
		WriteBack:          writeBack,
	}
	result.Review = buildEvidenceFirstReview(result, opts, time.Since(t0), len(srcEvents), len(competitions), tsEventCount, len(conflict.Edges))
	if opts.ReviewOutputPath != "" {
		if err := writeEvidenceFirstReview(opts.ReviewOutputPath, result.Review); err != nil {
			return nil, err
		}
	}
	log.Printf("%s [6/6] 完成: status=%s selected=%s score=%.3f write_back=%v elapsed=%dms",
		prefix, decision.Status, decision.SelectedCompetitionID, decision.Score, writeBack.Allowed, stats.ElapsedMs)
	return result, nil
}

func normalizeEvidenceFirstOptions(opts EvidenceFirstOptions) EvidenceFirstOptions {
	if opts.CandidateLimit <= 0 {
		opts.CandidateLimit = defaultEvidenceFirstCandidateLimit
	}
	if opts.MinHighConfEvents <= 0 {
		opts.MinHighConfEvents = defaultEvidenceFirstMinHighConfEvents
	}
	if opts.MinTwoTeamAnchorRate <= 0 {
		opts.MinTwoTeamAnchorRate = defaultEvidenceFirstMinTwoTeamAnchorRate
	}
	if opts.MinCandidateGap <= 0 {
		opts.MinCandidateGap = defaultEvidenceFirstMinCandidateGap
	}
	if opts.HighConfScore <= 0 {
		opts.HighConfScore = defaultEvidenceFirstHighConfScore
	}
	if opts.AggregatorConfig.HighConfidenceMin <= 0 {
		opts.AggregatorConfig.HighConfidenceMin = opts.MinHighConfEvents
	}
	if opts.AggregatorConfig.CandidateGapThreshold <= 0 {
		opts.AggregatorConfig.CandidateGapThreshold = opts.MinCandidateGap
	}
	if opts.AggregatorConfig.HighConfidenceScore <= 0 {
		opts.AggregatorConfig.HighConfidenceScore = opts.HighConfScore
	}
	return opts
}

func (e *UniversalEngine) selectEvidenceCompetitions(adapter SourceAdapter, tournamentID, sport, tsCompetitionID string, limit int) ([]evidenceCompetitionCandidate, map[string]float64, error) {
	var comps []db.TSCompetition
	var err error
	if tsCompetitionID != "" {
		comp, compErr := e.TS.GetCompetition(tsCompetitionID, sport)
		if compErr == nil && comp != nil {
			comps = []db.TSCompetition{*comp}
		} else {
			comps = []db.TSCompetition{{ID: tsCompetitionID, Sport: sport}}
		}
	} else {
		switch sport {
		case "football":
			comps, err = e.TS.GetCompetitionsByFootball()
		case "basketball":
			comps, err = e.TS.GetCompetitionsByBasketball()
		default:
			return nil, nil, fmt.Errorf("不支持的运动类型: %s", sport)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("GetCompetitions(TS): %w", err)
		}
	}
	knownID, hasKnown := KnownLeagueMap[knownLeagueKey(sport, tournamentID)]
	candidates := make([]evidenceCompetitionCandidate, 0, len(comps))
	scoreMap := make(map[string]float64, len(comps))
	for _, comp := range comps {
		score := scoreEvidenceCompetition(adapter, comp)
		known := hasKnown && comp.ID == knownID
		if known && score < 1.0 {
			score = 1.0
		}
		candidates = append(candidates, evidenceCompetitionCandidate{comp: comp, score: round3(score), known: known})
		scoreMap[comp.ID] = round3(score)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].known != candidates[j].known {
			return candidates[i].known
		}
		if candidates[i].score == candidates[j].score {
			return candidates[i].comp.ID < candidates[j].comp.ID
		}
		return candidates[i].score > candidates[j].score
	})
	if tsCompetitionID == "" && limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, scoreMap, nil
}

func scoreEvidenceCompetition(adapter SourceAdapter, comp db.TSCompetition) float64 {
	switch a := adapter.(type) {
	case *SRSourceAdapter:
		if a.srTour != nil {
			return leagueNameScore(a.srTour, &comp)
		}
	}
	return 0.50
}

func filterTSEventsBySourceWindow(srcEvents []db.SREvent, tsEvents []db.TSEvent, marginSec int64) []db.TSEvent {
	if len(srcEvents) == 0 || len(tsEvents) == 0 {
		return tsEvents
	}
	minStart, maxStart := int64(0), int64(0)
	for _, ev := range srcEvents {
		if ev.StartUnix <= 0 {
			continue
		}
		if minStart == 0 || ev.StartUnix < minStart {
			minStart = ev.StartUnix
		}
		if ev.StartUnix > maxStart {
			maxStart = ev.StartUnix
		}
	}
	if minStart == 0 || maxStart == 0 {
		return tsEvents
	}
	lo, hi := minStart-marginSec, maxStart+marginSec
	filtered := make([]db.TSEvent, 0, len(tsEvents))
	for _, ev := range tsEvents {
		if ev.MatchTime >= lo && ev.MatchTime <= hi {
			filtered = append(filtered, ev)
		}
	}
	if len(filtered) == 0 {
		return tsEvents
	}
	return filtered
}

func buildEvidenceSource(adapter SourceAdapter, tournamentID, sport string, srcEvents []db.SREvent, srcTeamNames map[string]string) LeagueEvidenceSource {
	source := LeagueEvidenceSource{LeagueID: tournamentID, Sport: sport, TotalEvents: len(srcEvents), TotalTeams: len(srcTeamNames)}
	switch a := adapter.(type) {
	case *SRSourceAdapter:
		if a.srTour != nil {
			source.Name = a.srTour.Name
			source.CategoryName = a.srTour.CategoryName
			source.CountryName = a.srTour.CategoryName
		}
	case *LSSourceAdapter:
		if a.lsTour != nil {
			source.Name = a.lsTour.Name
			source.CategoryName = a.lsTour.CategoryName
			source.CountryName = a.lsTour.CategoryName
		}
	}
	if source.Name == "" {
		source.Name = tournamentID
	}
	source.Features = ExtractLeagueFeatures(source.Name)
	return source
}

func (e *UniversalEngine) validateEvidenceKnownMap(adapter SourceAdapter, tournamentID, sport string, matches []ResolvedEventMatch) EvidenceFirstKnownMapStatus {
	knownID, ok := KnownLeagueMap[knownLeagueKey(sport, tournamentID)]
	status := EvidenceFirstKnownMapStatus{Checked: ok, TournamentID: tournamentID, TSCompetitionID: knownID}
	if !ok {
		status.Reason = "no_known_map"
		return status
	}
	eventMatches := eventMatchesForKnownCompetition(matches, knownID)
	rcr := ComputeReverseConfirmRateSR(eventMatches)
	status.RCR = rcr
	if e.MapValidator != nil {
		validationStatus, _, validatorRCR := e.MapValidator.ValidateSR(tournamentID, knownID, sport, eventMatches)
		status.Status = validationStatus
		status.RCR = validatorRCR
		status.Suspect = validationStatus == ValidationStatusSuspect
		status.ManualOverride = validationStatus == ValidationStatusOverride
		status.Reason = string(validationStatus)
		return status
	}
	if rcr < defaultLeagueEvidenceKnownRCRMin {
		status.Status = ValidationStatusSuspect
		status.Suspect = true
		status.Reason = "known_map_rcr_below_threshold"
	} else {
		status.Status = ValidationStatusOK
		status.Reason = "known_map_rcr_ok"
	}
	return status
}

func eventMatchesForKnownCompetition(matches []ResolvedEventMatch, knownID string) []EventMatch {
	events := make([]EventMatch, len(matches))
	for i, m := range matches {
		events[i] = m.EventMatch
		if m.TSCompetitionID != knownID {
			events[i].Matched = false
			events[i].TSMatchID = ""
		}
	}
	return events
}

func (e *UniversalEngine) applyEvidenceFirstWriteBack(adapter SourceAdapter, sport string, decision LeagueEvidenceDecision, teams []TeamMappingResult, opts EvidenceFirstOptions) EvidenceFirstWriteBackResult {
	wb := EvidenceFirstWriteBackResult{
		Enabled:              opts.AllowWriteBack,
		MinHighConfEvents:    opts.MinHighConfEvents,
		MinTwoTeamAnchorRate: round3(opts.MinTwoTeamAnchorRate),
		MinCandidateGap:      round3(opts.MinCandidateGap),
		DecisionStatus:       string(decision.Status),
	}
	if len(decision.Candidates) > 0 {
		top := decision.Candidates[0]
		wb.ActualHighConfEvents = top.HighConfEvents
		wb.ActualTwoTeamAnchor = top.TwoTeamAnchorScore
		wb.ActualCandidateGap = top.CandidateGap
		wb.HardVeto = top.HardVeto
	}
	if !opts.AllowWriteBack {
		wb.Reason = "write_back_disabled"
		return wb
	}
	wb.BlockedReasons = evidenceWriteBackBlockedReasons(decision, opts)
	if len(wb.BlockedReasons) > 0 {
		wb.Reason = "safety_gate_blocked"
		return wb
	}
	wb.Allowed = true
	wb.Reason = "safety_gate_passed"
	if e.AliasStore != nil {
		for _, tm := range teams {
			if tm.TSTeamID == "" || tm.VoteCount < 2 || tm.Confidence < opts.HighConfScore {
				continue
			}
			if err := e.AliasStore.Upsert(adapter.SourceSide(), tm.SrcTeamID, tm.TSTeamID, tm.Confidence, sport, decision.SelectedCompetitionID); err == nil {
				wb.TeamAliasUpserts++
			}
		}
	}
	// 强映射资产 KnownLeagueMap 仍不在自动路径中静默覆盖；此处仅保留审计字段。
	wb.StrongMapUpserted = false
	return wb
}

func evidenceWriteBackBlockedReasons(decision LeagueEvidenceDecision, opts EvidenceFirstOptions) []string {
	reasons := []string{}
	if decision.Status != LeagueDecisionAutoConfirmed {
		reasons = append(reasons, "decision_not_auto_confirmed")
	}
	if len(decision.Candidates) == 0 {
		return append(reasons, "no_candidates")
	}
	top := decision.Candidates[0]
	if top.HighConfEvents < opts.MinHighConfEvents {
		reasons = append(reasons, "insufficient_high_conf_events")
	}
	if top.TwoTeamAnchorScore < opts.MinTwoTeamAnchorRate {
		reasons = append(reasons, "two_team_anchor_below_threshold")
	}
	if top.HardVeto {
		reasons = append(reasons, "hard_veto")
	}
	if top.CandidateGap < opts.MinCandidateGap {
		reasons = append(reasons, "candidate_gap_below_threshold")
	}
	return reasons
}

func leagueResultFromEvidence(source LeagueEvidenceSource, decision LeagueEvidenceDecision) *LeagueMatchResult {
	return &LeagueMatchResult{
		SrcID:           source.LeagueID,
		SrcName:         source.Name,
		SrcCategory:     source.CategoryName,
		TSCompetitionID: decision.SelectedCompetitionID,
		TSName:          decision.SelectedCompetitionName,
		Matched:         decision.Status == LeagueDecisionAutoConfirmed,
		MatchRule:       MatchRule("LEAGUE_EVIDENCE_FIRST"),
		Confidence:      decision.Score,
	}
}

func computeEvidenceFirstStats(source LeagueEvidenceSource, league *LeagueMatchResult, matches []ResolvedEventMatch, teams []TeamMappingResult, sport, tier, sourceSide string, elapsed time.Duration) UniversalMatchStats {
	result := &UniversalMatchResult{League: league, Teams: teams}
	for _, m := range matches {
		result.Events = append(result.Events, EventMatchResult{SrcEventID: m.SREventID, TSMatchID: m.TSMatchID, Matched: m.Matched, MatchRule: firstMatchRule(m.Rule, m.MatchRule), Confidence: maxFloat(m.Score, m.Confidence), TimeDiffSec: m.TimeDiffSec})
	}
	stats := computeUniversalStats(result, sport, tier, sourceSide, elapsed)
	if stats.SrcLeagueName == "" {
		stats.SrcLeagueName = source.Name
	}
	return stats
}

func buildEvidenceFirstReview(result *EvidenceFirstResult, opts EvidenceFirstOptions, elapsed time.Duration, sourceEvents, competitions, tsEvents, edges int) *EvidenceFirstReview {
	selectedEvents := []LeagueEventExample{}
	if len(result.Decision.Candidates) > 0 {
		selectedEvents = result.Decision.Candidates[0].TopEventExamples
	}
	return &EvidenceFirstReview{
		GeneratedAt:     time.Now(),
		SourceSide:      result.SourceSide,
		TournamentID:    result.TournamentID,
		Sport:           result.Sport,
		Tier:            result.Tier,
		Source:          result.Source,
		Decision:        result.Decision,
		KnownMap:        result.KnownMap,
		WriteBack:       result.WriteBack,
		CandidateCount:  len(result.Competitions),
		EventTotal:      sourceEvents,
		ResolvedMatches: result.Events,
		CandidateEdges:  result.ConflictResolution.Edges,
		Eliminated:      result.ConflictResolution.Eliminated,
		TeamMappings:    result.Teams,
		SelectedEvents:  selectedEvents,
		Performance:     EvidenceFirstPerformance{ElapsedMs: elapsed.Milliseconds(), SourceEvents: sourceEvents, TSCompetitions: competitions, TSEvents: tsEvents, CandidateEdges: edges, ResolvedMatchCount: len(result.Events)},
	}
}

func writeEvidenceFirstReview(path string, review *EvidenceFirstReview) error {
	payload, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal review: %w", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("write review %s: %w", path, err)
	}
	return nil
}
