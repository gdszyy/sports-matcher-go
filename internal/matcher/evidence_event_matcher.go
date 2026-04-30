package matcher

import (
	"math"
	"sort"

	"github.com/gdszyy/sports-matcher/internal/db"
)

const (
	ReasonTimeWindow       = "TIME_WINDOW"
	ReasonAliasHit         = "ALIAS_HIT"
	ReasonTeamIDFallback   = "TEAM_ID_FALLBACK"
	ReasonSideReversed     = "SIDE_REVERSED"
	ReasonFSModel          = "FS_MODEL"
	ReasonDTWOffset        = "DTW_OFFSET"
	ReasonCandidatePrior   = "P2_CANDIDATE_PRIOR"
	ReasonStrongConstraint = "STRONG_CONSTRAINT"
	ReasonConflictTSUsed   = "CONFLICT_TS_USED"
	ReasonConflictSource   = "CONFLICT_SOURCE_USED"
	ReasonBelowThreshold   = "BELOW_AUTO_THRESHOLD"
	ReasonGuardVeto        = "GUARD_VETO"
)

const (
	defaultEvidenceAutoConfirmThreshold = 0.75
	sideReversedPenalty                 = 0.12
	teamIDAnchorBonus                   = 0.08
	aliasHitBonus                       = 0.04
	candidatePriorWeight                = 0.10
	fsScoreWeight                       = 0.08
)

// EvidenceEventCandidate 是 P2 比赛候选池传给 P3 的最小候选单元。
// TSEvent 本身不携带 competition_id，因此这里显式保留 competition_id、competition_name
// 以及 P2 已计算的联赛/球队候选先验分，供 EvidenceEventMatcher 进行比赛级证据融合。
type EvidenceEventCandidate struct {
	CompetitionID          string     `json:"competition_id"`
	CompetitionName        string     `json:"competition_name,omitempty"`
	Event                  db.TSEvent `json:"event"`
	CandidateScore         float64    `json:"candidate_score,omitempty"`
	HomeTeamCandidateScore float64    `json:"home_team_candidate_score,omitempty"`
	AwayTeamCandidateScore float64    `json:"away_team_candidate_score,omitempty"`
	StrongConstraintOK     bool       `json:"strong_constraint_ok"`
	StrongConstraintReason string     `json:"strong_constraint_reason,omitempty"`
}

// EvidenceEventMatcherConfig 控制 Evidence-First 比赛匹配适配层的阈值与可选模型。
type EvidenceEventMatcherConfig struct {
	AutoConfirmThreshold float64
	FSModel              *FSModel
	UseDTW               bool
}

// EvidenceEventMatcher 将 P2 多 competition 比赛候选池转化为高置信、一对一的比赛匹配结果。
type EvidenceEventMatcher struct {
	cfg EvidenceEventMatcherConfig
}

// NewEvidenceEventMatcher 创建 Evidence-First 比赛匹配器。
func NewEvidenceEventMatcher(cfg EvidenceEventMatcherConfig) *EvidenceEventMatcher {
	if cfg.AutoConfirmThreshold <= 0 {
		cfg.AutoConfirmThreshold = defaultEvidenceAutoConfirmThreshold
	}
	if cfg.FSModel == nil {
		cfg.FSModel = NewFSModel()
	}
	return &EvidenceEventMatcher{cfg: cfg}
}

// EventEvidenceEdge 表示一个源侧比赛到一个 TS 候选比赛的可解释证据边。
type EventEvidenceEdge struct {
	SREventID              string    `json:"sr_event_id"`
	SRStartUnix            int64     `json:"sr_start_unix"`
	SRHomeID               string    `json:"sr_home_id"`
	SRHomeName             string    `json:"sr_home_name"`
	SRAwayID               string    `json:"sr_away_id"`
	SRAwayName             string    `json:"sr_away_name"`
	TSMatchID              string    `json:"ts_match_id"`
	TSEventID              string    `json:"ts_event_id,omitempty"`
	TSCompetitionID        string    `json:"ts_competition_id"`
	TSCompetitionName      string    `json:"ts_competition_name,omitempty"`
	TSMatchTime            int64     `json:"ts_match_time"`
	TSHomeID               string    `json:"ts_home_id"`
	TSHomeName             string    `json:"ts_home_name"`
	TSAwayID               string    `json:"ts_away_id"`
	TSAwayName             string    `json:"ts_away_name"`
	Score                  float64   `json:"score"`
	Rule                   MatchRule `json:"rule"`
	ReasonCodes            []string  `json:"reason_codes"`
	TimeDiffSec            int64     `json:"time_diff_sec"`
	CorrectedTimeDiffSec   int64     `json:"corrected_time_diff_sec,omitempty"`
	DTWOffsetSec           int64     `json:"dtw_offset_sec,omitempty"`
	HomeScore              float64   `json:"home_score"`
	AwayScore              float64   `json:"away_score"`
	NameScore              float64   `json:"name_score"`
	TimeScore              float64   `json:"time_score"`
	FSScore                float64   `json:"fs_score"`
	CandidateScore         float64   `json:"candidate_score,omitempty"`
	HomeTeamCandidateScore float64   `json:"home_team_candidate_score,omitempty"`
	AwayTeamCandidateScore float64   `json:"away_team_candidate_score,omitempty"`
	SideReversed           bool      `json:"side_reversed"`
	AliasHomeHit           bool      `json:"alias_home_hit"`
	AliasAwayHit           bool      `json:"alias_away_hit"`
	TeamIDAnchor           bool      `json:"team_id_anchor"`
}

// ResolvedEventMatch 是冲突消解后的比赛级匹配输出。
type ResolvedEventMatch struct {
	EventMatch
	TSCompetitionID   string                `json:"ts_competition_id,omitempty"`
	TSCompetitionName string                `json:"ts_competition_name,omitempty"`
	Rule              MatchRule             `json:"rule"`
	ReasonCodes       []string              `json:"reason_codes,omitempty"`
	ConflictInfo      []ConflictElimination `json:"conflict_info,omitempty"`
	SideReversed      bool                  `json:"side_reversed,omitempty"`
	Score             float64               `json:"score"`
	Evidence          *EventEvidenceEdge    `json:"evidence,omitempty"`
}

// ConflictElimination 记录一个被一对一冲突消解淘汰的候选。
type ConflictElimination struct {
	LoserSREventID  string  `json:"loser_sr_event_id"`
	LoserTSMatchID  string  `json:"loser_ts_match_id"`
	LostToSREventID string  `json:"lost_to_sr_event_id"`
	LostToTSMatchID string  `json:"lost_to_ts_match_id"`
	WinnerScore     float64 `json:"winner_score"`
	LoserScore      float64 `json:"loser_score"`
	ScoreGap        float64 `json:"score_gap"`
	Reason          string  `json:"reason"`
}

// ConflictResolutionResult 汇总候选边、自动确认结果、被淘汰候选和第二轮 teamIDMap。
type ConflictResolutionResult struct {
	Matches      []ResolvedEventMatch  `json:"matches"`
	Edges        []EventEvidenceEdge   `json:"edges"`
	Eliminated   []ConflictElimination `json:"eliminated"`
	TeamIDMap    map[string]string     `json:"team_id_map,omitempty"`
	DTWOffsetSec int64                 `json:"dtw_offset_sec,omitempty"`
	DTWApplied   bool                  `json:"dtw_applied"`
}

// MatchTwoRound 执行 Evidence-First 两轮比赛匹配：第一轮学习 teamIDMap，第二轮启用 L4b 队伍 ID 兜底。
func (m *EvidenceEventMatcher) MatchTwoRound(
	srEvents []db.SREvent,
	candidates []EvidenceEventCandidate,
	srTeamNames, tsTeamNames map[string]string,
) ConflictResolutionResult {
	first := m.Match(srEvents, candidates, srTeamNames, tsTeamNames, nil)
	baseEvents := resolvedToEventMatches(first.Matches)
	teamMappings := DeriveTeamMappings(baseEvents, srTeamNames, tsTeamNames)
	teamIDMap := make(map[string]string, len(teamMappings))
	for _, tm := range teamMappings {
		if tm.SRTeamID != "" && tm.TSTeamID != "" {
			teamIDMap[tm.SRTeamID] = tm.TSTeamID
		}
	}
	second := m.Match(srEvents, candidates, srTeamNames, tsTeamNames, teamIDMap)
	second.TeamIDMap = teamIDMap
	return second
}

// Match 计算候选证据边并执行一对一冲突消解。
func (m *EvidenceEventMatcher) Match(
	srEvents []db.SREvent,
	candidates []EvidenceEventCandidate,
	srTeamNames, tsTeamNames map[string]string,
	teamIDMap map[string]string,
) ConflictResolutionResult {
	if m == nil {
		m = NewEvidenceEventMatcher(EvidenceEventMatcherConfig{})
	}
	aliasIdx := newTeamAliasIndex()
	dtwOffset, dtwApplied := m.estimateDTWOffset(srEvents, candidates)
	edges := make([]EventEvidenceEdge, 0, len(srEvents)*maxInt(1, len(candidates)/maxInt(1, len(srEvents))))
	for _, sr := range srEvents {
		for _, cand := range candidates {
			if !cand.StrongConstraintOK && cand.StrongConstraintReason != "" {
				continue
			}
			if edge, ok := m.scoreEdge(sr, cand, srTeamNames, tsTeamNames, teamIDMap, aliasIdx, dtwOffset, dtwApplied); ok {
				edges = append(edges, edge)
			}
		}
	}
	result := m.resolveConflicts(srEvents, edges)
	result.Edges = edges
	result.DTWOffsetSec = dtwOffset
	result.DTWApplied = dtwApplied
	return result
}

func (m *EvidenceEventMatcher) scoreEdge(
	sr db.SREvent,
	cand EvidenceEventCandidate,
	srTeamNames, tsTeamNames map[string]string,
	teamIDMap map[string]string,
	aliasIdx *TeamAliasIndex,
	dtwOffset int64,
	dtwApplied bool,
) (EventEvidenceEdge, bool) {
	ts := cand.Event
	if ts.ID == "" && ts.MatchID == "" {
		return EventEvidenceEdge{}, false
	}
	tsHomeName := firstNonEmpty(tsTeamNames[ts.HomeID], ts.HomeName)
	tsAwayName := firstNonEmpty(tsTeamNames[ts.AwayID], ts.AwayName)
	homeFwd := aliasIdx.NameSimWithAlias(sr.HomeID, sr.HomeName, ts.HomeID, tsHomeName)
	awayFwd := aliasIdx.NameSimWithAlias(sr.AwayID, sr.AwayName, ts.AwayID, tsAwayName)
	homeRev := aliasIdx.NameSimWithAlias(sr.HomeID, sr.HomeName, ts.AwayID, tsAwayName)
	awayRev := aliasIdx.NameSimWithAlias(sr.AwayID, sr.AwayName, ts.HomeID, tsHomeName)
	fwdName := (homeFwd + awayFwd) / 2.0
	revName := (homeRev + awayRev) / 2.0
	sideReversed := revName > fwdName
	homeScore, awayScore, nameScore := homeFwd, awayFwd, fwdName
	if sideReversed {
		homeScore, awayScore, nameScore = homeRev, awayRev, revName
	}
	if nameScore < 0.40 && !hasTeamIDAnchor(sr, ts, teamIDMap, sideReversed) {
		return EventEvidenceEdge{}, false
	}
	timeDiff := absInt64(sr.StartUnix - ts.MatchTime)
	correctedDiff := timeDiff
	if dtwApplied {
		correctedDiff = absInt64(sr.StartUnix + dtwOffset - ts.MatchTime)
	}
	rule, levelScore, timeScore, ok := bestEvidenceLevel(nameScore, correctedDiff, sideReversed, cand.StrongConstraintOK)
	teamAnchor := hasTeamIDAnchor(sr, ts, teamIDMap, sideReversed)
	if !ok && teamAnchor {
		rule = RuleEventL4b
		timeScore = gaussianTimeFactor(correctedDiff, 3600)
		levelScore = 0.75 + 0.10*timeScore
		ok = true
	}
	if !ok {
		return EventEvidenceEdge{}, false
	}
	fsCmp := CompareEventPair(homeScore, awayScore, correctedDiff, true, true)
	fsScore := m.cfg.FSModel.ScoreNormalized(fsCmp)
	prior := normalizePrior(cand.CandidateScore, cand.HomeTeamCandidateScore, cand.AwayTeamCandidateScore)
	score := levelScore*(1.0-candidatePriorWeight-fsScoreWeight) + prior*candidatePriorWeight + fsScore*fsScoreWeight
	reasons := []string{ReasonTimeWindow, ReasonFSModel}
	if prior > 0 {
		reasons = append(reasons, ReasonCandidatePrior)
	}
	if cand.StrongConstraintOK {
		reasons = append(reasons, ReasonStrongConstraint)
	}
	if dtwApplied {
		reasons = append(reasons, ReasonDTWOffset)
	}
	if sideReversed {
		score -= sideReversedPenalty
		reasons = append(reasons, ReasonSideReversed)
	}
	homeAlias := aliasIdx.HasAlias(sr.HomeID) && (aliasIdx.GetTSID(sr.HomeID) == ts.HomeID || aliasIdx.GetTSID(sr.HomeID) == ts.AwayID)
	awayAlias := aliasIdx.HasAlias(sr.AwayID) && (aliasIdx.GetTSID(sr.AwayID) == ts.HomeID || aliasIdx.GetTSID(sr.AwayID) == ts.AwayID)
	if homeAlias || awayAlias {
		score += aliasHitBonus
		reasons = append(reasons, ReasonAliasHit)
	}
	if teamAnchor {
		score += teamIDAnchorBonus
		reasons = append(reasons, ReasonTeamIDFallback)
	}
	if score > 1.0 {
		score = 1.0
	}
	if score < 0 {
		score = 0
	}
	return EventEvidenceEdge{
		SREventID: sr.ID, SRStartUnix: sr.StartUnix, SRHomeID: sr.HomeID, SRHomeName: sr.HomeName,
		SRAwayID: sr.AwayID, SRAwayName: sr.AwayName, TSMatchID: tsMatchKey(ts), TSEventID: ts.ID,
		TSCompetitionID: cand.CompetitionID, TSCompetitionName: cand.CompetitionName, TSMatchTime: ts.MatchTime,
		TSHomeID: ts.HomeID, TSHomeName: tsHomeName, TSAwayID: ts.AwayID, TSAwayName: tsAwayName,
		Score: round3(score), Rule: rule, ReasonCodes: dedupReasonCodes(reasons), TimeDiffSec: timeDiff,
		CorrectedTimeDiffSec: correctedDiff, DTWOffsetSec: dtwOffset, HomeScore: round3(homeScore), AwayScore: round3(awayScore),
		NameScore: round3(nameScore), TimeScore: round3(timeScore), FSScore: round3(fsScore), CandidateScore: cand.CandidateScore,
		HomeTeamCandidateScore: cand.HomeTeamCandidateScore, AwayTeamCandidateScore: cand.AwayTeamCandidateScore,
		SideReversed: sideReversed, AliasHomeHit: homeAlias, AliasAwayHit: awayAlias, TeamIDAnchor: teamAnchor,
	}, true
}

func (m *EvidenceEventMatcher) resolveConflicts(srEvents []db.SREvent, edges []EventEvidenceEdge) ConflictResolutionResult {
	sorted := append([]EventEvidenceEdge(nil), edges...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Score == sorted[j].Score {
			if sorted[i].SREventID == sorted[j].SREventID {
				return sorted[i].TSMatchID < sorted[j].TSMatchID
			}
			return sorted[i].SREventID < sorted[j].SREventID
		}
		return sorted[i].Score > sorted[j].Score
	})
	usedSource := map[string]EventEvidenceEdge{}
	usedTS := map[string]EventEvidenceEdge{}
	selected := map[string]EventEvidenceEdge{}
	var eliminated []ConflictElimination
	for _, e := range sorted {
		if e.Score < m.cfg.AutoConfirmThreshold {
			eliminated = append(eliminated, elimination(e, EventEvidenceEdge{}, ReasonBelowThreshold))
			continue
		}
		if winner, ok := usedSource[e.SREventID]; ok {
			eliminated = append(eliminated, elimination(e, winner, ReasonConflictSource))
			continue
		}
		if winner, ok := usedTS[e.TSMatchID]; ok {
			eliminated = append(eliminated, elimination(e, winner, ReasonConflictTSUsed))
			continue
		}
		usedSource[e.SREventID] = e
		usedTS[e.TSMatchID] = e
		selected[e.SREventID] = e
	}
	matches := make([]ResolvedEventMatch, 0, len(srEvents))
	for _, sr := range srEvents {
		if e, ok := selected[sr.ID]; ok {
			matches = append(matches, resolvedFromEdge(sr, e, eliminationsForWinner(e, eliminated)))
		} else {
			matches = append(matches, ResolvedEventMatch{EventMatch: EventMatch{SREventID: sr.ID, SRStartTime: sr.StartTime, SRStartUnix: sr.StartUnix, SRHomeName: sr.HomeName, SRHomeID: sr.HomeID, SRAwayName: sr.AwayName, SRAwayID: sr.AwayID, Matched: false, MatchRule: RuleEventNoMatch}, Rule: RuleEventNoMatch})
		}
	}
	return ConflictResolutionResult{Matches: matches, Eliminated: eliminated}
}

func bestEvidenceLevel(nameScore float64, timeDiff int64, sideReversed bool, strongOK bool) (MatchRule, float64, float64, bool) {
	bestScore := -1.0
	bestRule := RuleEventNoMatch
	bestTime := 0.0
	for _, cfg := range levelConfigs {
		if cfg.requireAlias {
			continue
		}
		if cfg.maxTimeDiffSec >= 0 && timeDiff > cfg.maxTimeDiffSec {
			continue
		}
		if cfg.maxTimeDiffSec < 0 && timeDiff > 24*3600 {
			continue
		}
		if nameScore < cfg.nameThreshold {
			continue
		}
		timeFactor := 0.0
		if cfg.timeWeight > 0 && cfg.sigma > 0 {
			timeFactor = gaussianTimeFactor(timeDiff, cfg.sigma)
		}
		score := cfg.timeWeight*timeFactor + cfg.nameWeight*nameScore
		if score >= cfg.minScore && score > bestScore {
			bestScore, bestRule, bestTime = score, cfg.rule, timeFactor
		}
	}
	if bestScore >= 0 {
		return bestRule, bestScore, bestTime, true
	}
	if strongOK && nameScore >= 0.85 && timeDiff <= 72*3600 {
		timeFactor := gaussianTimeFactor(timeDiff, 43200)
		return RuleEventL4, 0.80 + 0.10*timeFactor, timeFactor, true
	}
	if !sideReversed && nameScore >= l5NameThreshold && timeDiff <= l5MaxTimeDiff {
		return RuleEventL5, nameScore, 0, true
	}
	return RuleEventNoMatch, 0, 0, false
}

func (m *EvidenceEventMatcher) estimateDTWOffset(srEvents []db.SREvent, candidates []EvidenceEventCandidate) (int64, bool) {
	if !m.cfg.UseDTW || len(srEvents) == 0 || len(candidates) == 0 {
		return 0, false
	}
	srDTW := make([]DTWEvent, 0, len(srEvents))
	for _, sr := range srEvents {
		srDTW = append(srDTW, DTWEvent{ID: sr.ID, StartUnix: sr.StartUnix, HomeName: sr.HomeName, AwayName: sr.AwayName, HomeID: sr.HomeID, AwayID: sr.AwayID})
	}
	tsSeen := map[string]bool{}
	tsDTW := make([]DTWEvent, 0, len(candidates))
	for _, cand := range candidates {
		key := tsMatchKey(cand.Event)
		if key == "" || tsSeen[key] {
			continue
		}
		tsSeen[key] = true
		tsDTW = append(tsDTW, DTWEvent{ID: key, StartUnix: cand.Event.MatchTime, HomeName: cand.Event.HomeName, AwayName: cand.Event.AwayName, HomeID: cand.Event.HomeID, AwayID: cand.Event.AwayID})
	}
	_, offset, applied := NewEventDTWMatcher().TryCorrect(srDTW, tsDTW)
	if !applied {
		return 0, false
	}
	return offset.OffsetSec, true
}

func resolvedFromEdge(sr db.SREvent, e EventEvidenceEdge, conflicts []ConflictElimination) ResolvedEventMatch {
	em := EventMatch{SREventID: sr.ID, SRStartTime: sr.StartTime, SRStartUnix: sr.StartUnix, SRHomeName: sr.HomeName, SRHomeID: sr.HomeID, SRAwayName: sr.AwayName, SRAwayID: sr.AwayID, TSMatchID: e.TSMatchID, TSMatchTime: e.TSMatchTime, TSHomeName: e.TSHomeName, TSHomeID: e.TSHomeID, TSAwayName: e.TSAwayName, TSAwayID: e.TSAwayID, Matched: true, MatchRule: e.Rule, Confidence: e.Score, TimeDiffSec: e.CorrectedTimeDiffSec}
	return ResolvedEventMatch{EventMatch: em, TSCompetitionID: e.TSCompetitionID, TSCompetitionName: e.TSCompetitionName, Rule: e.Rule, ReasonCodes: e.ReasonCodes, ConflictInfo: conflicts, SideReversed: e.SideReversed, Score: e.Score, Evidence: &e}
}

func resolvedToEventMatches(matches []ResolvedEventMatch) []EventMatch {
	out := make([]EventMatch, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.EventMatch)
	}
	return out
}

func elimination(loser, winner EventEvidenceEdge, reason string) ConflictElimination {
	return ConflictElimination{LoserSREventID: loser.SREventID, LoserTSMatchID: loser.TSMatchID, LostToSREventID: winner.SREventID, LostToTSMatchID: winner.TSMatchID, WinnerScore: winner.Score, LoserScore: loser.Score, ScoreGap: round3(winner.Score - loser.Score), Reason: reason}
}

func eliminationsForWinner(w EventEvidenceEdge, all []ConflictElimination) []ConflictElimination {
	var out []ConflictElimination
	for _, e := range all {
		if e.LostToSREventID == w.SREventID && e.LostToTSMatchID == w.TSMatchID {
			out = append(out, e)
		}
	}
	return out
}

func hasTeamIDAnchor(sr db.SREvent, ts db.TSEvent, teamIDMap map[string]string, reversed bool) bool {
	if len(teamIDMap) == 0 {
		return false
	}
	if !reversed {
		return teamIDMap[sr.HomeID] == ts.HomeID && teamIDMap[sr.AwayID] == ts.AwayID
	}
	return teamIDMap[sr.HomeID] == ts.AwayID && teamIDMap[sr.AwayID] == ts.HomeID
}

func tsMatchKey(ts db.TSEvent) string {
	if ts.MatchID != "" {
		return ts.MatchID
	}
	return ts.ID
}

func normalizePrior(scores ...float64) float64 {
	sum := 0.0
	cnt := 0
	for _, s := range scores {
		if s > 0 {
			if s > 1 {
				s = 1
			}
			sum += s
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	return sum / float64(cnt)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func dedupReasonCodes(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, r := range in {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}
