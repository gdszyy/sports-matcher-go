package matcher

import (
	"math"
	"sort"
	"strings"

	"github.com/gdszyy/sports-matcher/internal/db"
)

const (
	defaultLeagueEvidenceAutoThreshold   = 0.85
	defaultLeagueEvidenceReviewThreshold = 0.60
	defaultLeagueEvidenceGapThreshold    = 0.10
	defaultLeagueEvidenceHighConfMin     = 3
	defaultLeagueEvidenceKnownRCRMin     = 0.30
	defaultLeagueEvidenceHighConfScore   = 0.85
)

// LeagueDecisionStatus 表示 Evidence-First P4 的联赛级最终决策状态。
type LeagueDecisionStatus string

const (
	LeagueDecisionAutoConfirmed  LeagueDecisionStatus = "AUTO_CONFIRMED"
	LeagueDecisionReviewRequired LeagueDecisionStatus = "REVIEW_REQUIRED"
	LeagueDecisionRejected       LeagueDecisionStatus = "REJECTED"
	LeagueDecisionKnownSuspect   LeagueDecisionStatus = "KNOWN_SUSPECT"
)

// LeagueEvidenceWeights 集中定义 P4 聚合评分权重，便于后续调参。
type LeagueEvidenceWeights struct {
	EventCoverage     float64 `json:"event_coverage"`
	HighConfidence    float64 `json:"high_confidence_events"`
	TeamCoverage      float64 `json:"team_coverage"`
	TwoTeamAnchor     float64 `json:"two_team_anchor"`
	TemporalOverlap   float64 `json:"temporal_overlap"`
	Location          float64 `json:"location"`
	LeagueNameKeyword float64 `json:"league_name_keyword"`
}

// DefaultLeagueEvidenceWeights 是 P4 默认聚合权重。
var DefaultLeagueEvidenceWeights = LeagueEvidenceWeights{
	EventCoverage:     0.35,
	HighConfidence:    0.20,
	TeamCoverage:      0.20,
	TwoTeamAnchor:     0.10,
	TemporalOverlap:   0.05,
	Location:          0.05,
	LeagueNameKeyword: 0.05,
}

// LeagueEvidenceAggregatorConfig 控制 P4 聚合阈值与权重。
type LeagueEvidenceAggregatorConfig struct {
	Weights               LeagueEvidenceWeights
	AutoConfirmThreshold  float64
	ReviewThreshold       float64
	CandidateGapThreshold float64
	HighConfidenceMin     int
	HighConfidenceScore   float64
	KnownMapRCRThreshold  float64
}

// LeagueEvidenceAggregator 按 TS competition_id 聚合 P3 比赛证据并给出联赛级决策。
type LeagueEvidenceAggregator struct {
	cfg LeagueEvidenceAggregatorConfig
}

// NewLeagueEvidenceAggregator 创建 P4 联赛证据聚合器。
func NewLeagueEvidenceAggregator(cfg LeagueEvidenceAggregatorConfig) *LeagueEvidenceAggregator {
	if cfg.Weights == (LeagueEvidenceWeights{}) {
		cfg.Weights = DefaultLeagueEvidenceWeights
	}
	if cfg.AutoConfirmThreshold <= 0 {
		cfg.AutoConfirmThreshold = defaultLeagueEvidenceAutoThreshold
	}
	if cfg.ReviewThreshold <= 0 {
		cfg.ReviewThreshold = defaultLeagueEvidenceReviewThreshold
	}
	if cfg.CandidateGapThreshold <= 0 {
		cfg.CandidateGapThreshold = defaultLeagueEvidenceGapThreshold
	}
	if cfg.HighConfidenceMin <= 0 {
		cfg.HighConfidenceMin = defaultLeagueEvidenceHighConfMin
	}
	if cfg.HighConfidenceScore <= 0 {
		cfg.HighConfidenceScore = defaultLeagueEvidenceHighConfScore
	}
	if cfg.KnownMapRCRThreshold <= 0 {
		cfg.KnownMapRCRThreshold = defaultLeagueEvidenceKnownRCRMin
	}
	cfg.Weights = normalizeLeagueEvidenceWeights(cfg.Weights)
	return &LeagueEvidenceAggregator{cfg: cfg}
}

// LeagueEvidenceSource 描述源侧联赛特征与覆盖率分母。
type LeagueEvidenceSource struct {
	LeagueID     string         `json:"league_id,omitempty"`
	Name         string         `json:"name"`
	CategoryName string         `json:"category_name,omitempty"`
	CountryName  string         `json:"country_name,omitempty"`
	CountryCode  string         `json:"country_code,omitempty"`
	Sport        string         `json:"sport,omitempty"`
	Features     LeagueFeatures `json:"features,omitempty"`
	TotalEvents  int            `json:"total_events,omitempty"`
	TotalTeams   int            `json:"total_teams,omitempty"`
}

// LeagueEvidenceCompetition 描述 TS competition 元数据。
type LeagueEvidenceCompetition struct {
	CompetitionID   string         `json:"competition_id"`
	CompetitionName string         `json:"competition_name"`
	CountryName     string         `json:"country_name,omitempty"`
	CountryCode     string         `json:"country_code,omitempty"`
	Sport           string         `json:"sport,omitempty"`
	Features        LeagueFeatures `json:"features,omitempty"`
	TotalEvents     int            `json:"total_events,omitempty"`
}

// LeagueEvidenceCandidate 是单个 TS competition 的聚合候选与审核证据行。
type LeagueEvidenceCandidate struct {
	CompetitionID          string                `json:"competition_id"`
	CompetitionName        string                `json:"competition_name"`
	Score                  float64               `json:"score"`
	EventCoverageScore     float64               `json:"event_coverage_score"`
	HighConfEventScore     float64               `json:"high_conf_event_score"`
	TeamCoverageScore      float64               `json:"team_coverage_score"`
	TwoTeamAnchorScore     float64               `json:"two_team_anchor_score"`
	TemporalOverlapScore   float64               `json:"temporal_overlap_score"`
	LocationScore          float64               `json:"location_score"`
	LeagueNameKeywordScore float64               `json:"league_name_keyword_score"`
	CandidateGap           float64               `json:"candidate_gap"`
	Coverage               float64               `json:"coverage"`
	MatchedEvents          int                   `json:"matched_events"`
	HighConfEvents         int                   `json:"high_conf_events"`
	TeamCoverage           float64               `json:"team_coverage"`
	LocationResult         string                `json:"location_result"`
	KeywordResult          string                `json:"keyword_result"`
	VetoReason             string                `json:"veto_reason,omitempty"`
	VetoDetail             string                `json:"veto_detail,omitempty"`
	HardVeto               bool                  `json:"hard_veto"`
	TopEventExamples       []LeagueEventExample  `json:"top_event_examples,omitempty"`
	FeatureScores          LeagueEvidenceWeights `json:"feature_scores"`
}

// LeagueEventExample 是审核证据表中的 top event examples。
type LeagueEventExample struct {
	SREventID   string    `json:"sr_event_id"`
	TSMatchID   string    `json:"ts_match_id"`
	Score       float64   `json:"score"`
	Rule        MatchRule `json:"rule"`
	TimeDiffSec int64     `json:"time_diff_sec,omitempty"`
	SRHomeName  string    `json:"sr_home_name,omitempty"`
	SRAwayName  string    `json:"sr_away_name,omitempty"`
	TSHomeName  string    `json:"ts_home_name,omitempty"`
	TSAwayName  string    `json:"ts_away_name,omitempty"`
	ReasonCodes []string  `json:"reason_codes,omitempty"`
}

// LeagueEvidenceDecision 汇总候选排序、最终状态和审核原因。
type LeagueEvidenceDecision struct {
	Status                  LeagueDecisionStatus      `json:"status"`
	SelectedCompetitionID   string                    `json:"selected_competition_id,omitempty"`
	SelectedCompetitionName string                    `json:"selected_competition_name,omitempty"`
	Score                   float64                   `json:"score"`
	CandidateGap            float64                   `json:"candidate_gap"`
	Reason                  string                    `json:"reason,omitempty"`
	KnownMapRCR             float64                   `json:"known_map_rcr,omitempty"`
	Candidates              []LeagueEvidenceCandidate `json:"candidates"`
}

type leagueEvidenceBucket struct {
	meta    LeagueEvidenceCompetition
	matches []ResolvedEventMatch
}

// NewLeagueEvidenceCompetitionFromTS 将现有 db.TSCompetition 转为 P4 元数据。
func NewLeagueEvidenceCompetitionFromTS(comp db.TSCompetition) LeagueEvidenceCompetition {
	return LeagueEvidenceCompetition{
		CompetitionID:   comp.ID,
		CompetitionName: comp.Name,
		CountryName:     comp.CountryName,
		Sport:           comp.Sport,
		Features:        ExtractLeagueFeatures(comp.Name),
	}
}

// Aggregate 按 TS competition_id 聚合 P3 ResolvedEventMatch，输出候选排序与最终联赛决策。
func (a *LeagueEvidenceAggregator) Aggregate(source LeagueEvidenceSource, matches []ResolvedEventMatch, competitions []LeagueEvidenceCompetition) LeagueEvidenceDecision {
	return a.AggregateWithKnownMapRCR(source, matches, competitions, -1)
}

// AggregateWithKnownMapRCR 在 Aggregate 基础上接收 KnownMap 反向确认率；当 RCR < 0.30 时降为 KNOWN_SUSPECT。
func (a *LeagueEvidenceAggregator) AggregateWithKnownMapRCR(source LeagueEvidenceSource, matches []ResolvedEventMatch, competitions []LeagueEvidenceCompetition, knownMapRCR float64) LeagueEvidenceDecision {
	if a == nil {
		a = NewLeagueEvidenceAggregator(LeagueEvidenceAggregatorConfig{})
	}
	source = normalizeLeagueEvidenceSource(source, matches)
	buckets := a.buildBuckets(matches, competitions)
	candidates := make([]LeagueEvidenceCandidate, 0, len(buckets))
	for _, b := range buckets {
		cand := a.scoreBucket(source, matches, b)
		candidates = append(candidates, cand)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].CompetitionID < candidates[j].CompetitionID
		}
		return candidates[i].Score > candidates[j].Score
	})
	for i := range candidates {
		nextScore := 0.0
		if i+1 < len(candidates) {
			nextScore = candidates[i+1].Score
		}
		candidates[i].CandidateGap = round3(candidates[i].Score - nextScore)
	}
	decision := LeagueEvidenceDecision{Status: LeagueDecisionRejected, Candidates: candidates, KnownMapRCR: knownMapRCR}
	if len(candidates) == 0 {
		decision.Reason = "no_competition_evidence"
		return decision
	}
	top := candidates[0]
	decision.SelectedCompetitionID = top.CompetitionID
	decision.SelectedCompetitionName = top.CompetitionName
	decision.Score = top.Score
	decision.CandidateGap = top.CandidateGap
	if knownMapRCR >= 0 && knownMapRCR < a.cfg.KnownMapRCRThreshold {
		decision.Status = LeagueDecisionKnownSuspect
		decision.Reason = "known_map_rcr_below_threshold"
		return decision
	}
	if top.HardVeto {
		decision.Status = LeagueDecisionRejected
		decision.Reason = top.VetoReason
		return decision
	}
	if top.Score < a.cfg.ReviewThreshold {
		decision.Status = LeagueDecisionRejected
		decision.Reason = "score_below_review_threshold"
		return decision
	}
	if top.Score >= a.cfg.AutoConfirmThreshold && top.HighConfEvents >= a.cfg.HighConfidenceMin && top.CandidateGap >= a.cfg.CandidateGapThreshold {
		decision.Status = LeagueDecisionAutoConfirmed
		decision.Reason = "score_high_conf_events_and_gap_satisfied"
		return decision
	}
	decision.Status = LeagueDecisionReviewRequired
	switch {
	case top.Score < a.cfg.AutoConfirmThreshold:
		decision.Reason = "score_in_review_band"
	case top.HighConfEvents < a.cfg.HighConfidenceMin:
		decision.Reason = "insufficient_high_conf_events"
	case top.CandidateGap < a.cfg.CandidateGapThreshold:
		decision.Reason = "candidate_gap_below_threshold"
	default:
		decision.Reason = "review_required"
	}
	return decision
}

func (a *LeagueEvidenceAggregator) buildBuckets(matches []ResolvedEventMatch, competitions []LeagueEvidenceCompetition) map[string]leagueEvidenceBucket {
	buckets := make(map[string]leagueEvidenceBucket)
	for _, comp := range competitions {
		if comp.CompetitionID == "" {
			continue
		}
		comp = normalizeLeagueEvidenceCompetition(comp)
		buckets[comp.CompetitionID] = leagueEvidenceBucket{meta: comp}
	}
	for _, m := range matches {
		if !m.Matched || m.TSCompetitionID == "" {
			continue
		}
		b := buckets[m.TSCompetitionID]
		if b.meta.CompetitionID == "" {
			b.meta = LeagueEvidenceCompetition{CompetitionID: m.TSCompetitionID, CompetitionName: m.TSCompetitionName, Features: ExtractLeagueFeatures(m.TSCompetitionName)}
		}
		if b.meta.CompetitionName == "" {
			b.meta.CompetitionName = firstNonEmpty(m.TSCompetitionName, m.TSCompetitionID)
		}
		b.matches = append(b.matches, m)
		buckets[m.TSCompetitionID] = b
	}
	return buckets
}

func (a *LeagueEvidenceAggregator) scoreBucket(source LeagueEvidenceSource, allMatches []ResolvedEventMatch, b leagueEvidenceBucket) LeagueEvidenceCandidate {
	meta := normalizeLeagueEvidenceCompetition(b.meta)
	matched := len(b.matches)
	highConf := countHighConfEvents(b.matches, a.cfg.HighConfidenceScore)
	coverage := safeRatio(matched, source.TotalEvents)
	eventCoverageScore := clamp01(coverage)
	highConfScore := clamp01(float64(highConf) / float64(a.cfg.HighConfidenceMin))
	teamCoverage := computeTeamCoverage(b.matches, source.TotalTeams)
	twoTeamAnchor := computeTwoTeamAnchorScore(b.matches)
	temporal := computeTemporalOverlapScore(b.matches)
	locationScore, locationResult, locationVetoReason, locationVetoDetail := evaluateLeagueEvidenceLocation(source, meta)
	keywordScore, keywordResult := evaluateLeagueEvidenceKeyword(source, meta)
	veto := CheckLeagueVeto(source.Features, meta.Features, "low")
	hardVeto := veto.Vetoed || locationVetoReason != ""
	vetoReason := ""
	vetoDetail := ""
	if veto.Vetoed {
		vetoReason = string(veto.Reason)
		vetoDetail = veto.Detail
	} else if locationVetoReason != "" {
		vetoReason = locationVetoReason
		vetoDetail = locationVetoDetail
	}
	featureScores := LeagueEvidenceWeights{
		EventCoverage:     eventCoverageScore,
		HighConfidence:    highConfScore,
		TeamCoverage:      teamCoverage,
		TwoTeamAnchor:     twoTeamAnchor,
		TemporalOverlap:   temporal,
		Location:          locationScore,
		LeagueNameKeyword: keywordScore,
	}
	score := a.weightedScore(featureScores)
	if hardVeto {
		score = 0
	}
	return LeagueEvidenceCandidate{
		CompetitionID:          meta.CompetitionID,
		CompetitionName:        firstNonEmpty(meta.CompetitionName, meta.CompetitionID),
		Score:                  round3(score),
		EventCoverageScore:     round3(eventCoverageScore),
		HighConfEventScore:     round3(highConfScore),
		TeamCoverageScore:      round3(teamCoverage),
		TwoTeamAnchorScore:     round3(twoTeamAnchor),
		TemporalOverlapScore:   round3(temporal),
		LocationScore:          round3(locationScore),
		LeagueNameKeywordScore: round3(keywordScore),
		Coverage:               round3(coverage),
		MatchedEvents:          matched,
		HighConfEvents:         highConf,
		TeamCoverage:           round3(teamCoverage),
		LocationResult:         locationResult,
		KeywordResult:          keywordResult,
		VetoReason:             vetoReason,
		VetoDetail:             vetoDetail,
		HardVeto:               hardVeto,
		TopEventExamples:       topLeagueEventExamples(b.matches, 3),
		FeatureScores:          featureScores,
	}
}

func (a *LeagueEvidenceAggregator) weightedScore(s LeagueEvidenceWeights) float64 {
	w := a.cfg.Weights
	return clamp01(w.EventCoverage*s.EventCoverage + w.HighConfidence*s.HighConfidence + w.TeamCoverage*s.TeamCoverage + w.TwoTeamAnchor*s.TwoTeamAnchor + w.TemporalOverlap*s.TemporalOverlap + w.Location*s.Location + w.LeagueNameKeyword*s.LeagueNameKeyword)
}

func normalizeLeagueEvidenceWeights(w LeagueEvidenceWeights) LeagueEvidenceWeights {
	sum := w.EventCoverage + w.HighConfidence + w.TeamCoverage + w.TwoTeamAnchor + w.TemporalOverlap + w.Location + w.LeagueNameKeyword
	if sum <= 0 {
		return DefaultLeagueEvidenceWeights
	}
	if math.Abs(sum-1.0) < 0.000001 {
		return w
	}
	return LeagueEvidenceWeights{EventCoverage: w.EventCoverage / sum, HighConfidence: w.HighConfidence / sum, TeamCoverage: w.TeamCoverage / sum, TwoTeamAnchor: w.TwoTeamAnchor / sum, TemporalOverlap: w.TemporalOverlap / sum, Location: w.Location / sum, LeagueNameKeyword: w.LeagueNameKeyword / sum}
}

func normalizeLeagueEvidenceSource(source LeagueEvidenceSource, matches []ResolvedEventMatch) LeagueEvidenceSource {
	if source.Features == (LeagueFeatures{}) && source.Name != "" {
		source.Features = ExtractLeagueFeatures(source.Name)
	}
	if source.TotalEvents <= 0 {
		seen := map[string]bool{}
		for _, m := range matches {
			if m.SREventID != "" {
				seen[m.SREventID] = true
			}
		}
		source.TotalEvents = len(seen)
	}
	if source.TotalTeams <= 0 {
		teams := map[string]bool{}
		for _, m := range matches {
			addNonEmpty(teams, firstNonEmpty(m.SRHomeID, m.SRHomeName))
			addNonEmpty(teams, firstNonEmpty(m.SRAwayID, m.SRAwayName))
		}
		source.TotalTeams = len(teams)
	}
	return source
}

func normalizeLeagueEvidenceCompetition(comp LeagueEvidenceCompetition) LeagueEvidenceCompetition {
	if comp.Features == (LeagueFeatures{}) && comp.CompetitionName != "" {
		comp.Features = ExtractLeagueFeatures(comp.CompetitionName)
	}
	return comp
}

func countHighConfEvents(matches []ResolvedEventMatch, threshold float64) int {
	count := 0
	for _, m := range matches {
		if m.Matched && maxFloat(m.Score, m.Confidence) >= threshold {
			count++
		}
	}
	return count
}

func computeTeamCoverage(matches []ResolvedEventMatch, totalTeams int) float64 {
	if totalTeams <= 0 {
		return 0
	}
	teams := map[string]bool{}
	for _, m := range matches {
		if !m.Matched {
			continue
		}
		addNonEmpty(teams, firstNonEmpty(m.SRHomeID, m.SRHomeName))
		addNonEmpty(teams, firstNonEmpty(m.SRAwayID, m.SRAwayName))
	}
	return clamp01(float64(len(teams)) / float64(totalTeams))
}

func computeTwoTeamAnchorScore(matches []ResolvedEventMatch) float64 {
	if len(matches) == 0 {
		return 0
	}
	anchored := 0
	for _, m := range matches {
		if !m.Matched {
			continue
		}
		if m.Evidence != nil && m.Evidence.TeamIDAnchor {
			anchored++
			continue
		}
		if m.SRHomeID != "" && m.SRAwayID != "" && m.TSHomeID != "" && m.TSAwayID != "" {
			anchored++
		}
	}
	return clamp01(float64(anchored) / float64(len(matches)))
}

func computeTemporalOverlapScore(matches []ResolvedEventMatch) float64 {
	if len(matches) == 0 {
		return 0
	}
	total := 0.0
	for _, m := range matches {
		if !m.Matched {
			continue
		}
		diff := absInt64(m.TimeDiffSec)
		if m.Evidence != nil && m.Evidence.CorrectedTimeDiffSec != 0 {
			diff = absInt64(m.Evidence.CorrectedTimeDiffSec)
		}
		total += gaussianTimeFactor(diff, 12*3600)
	}
	return clamp01(total / float64(len(matches)))
}

func evaluateLeagueEvidenceLocation(source LeagueEvidenceSource, comp LeagueEvidenceCompetition) (float64, string, string, string) {
	srcCode := normalizeCountryCode(source.CountryCode)
	tsCode := normalizeCountryCode(comp.CountryCode)
	if srcCode != "" && tsCode != "" {
		if srcCode == tsCode {
			return 1.0, "country_code_match", "", ""
		}
		return 0.0, "country_code_conflict", "country_code_conflict", srcCode + " vs " + tsCode
	}
	sourceLoc := firstNonEmpty(source.CountryName, source.CategoryName)
	if sourceLoc != "" && comp.CountryName != "" {
		if lsInternationalCategory(sourceLoc) || lsInternationalCategory(comp.CountryName) {
			return 0.75, "international_or_continental", "", ""
		}
		sim := geoSimilarity(normalizeName(sourceLoc), normalizeName(comp.CountryName))
		if sim < 0.4 {
			return 0.0, "location_text_conflict", "location_text_conflict", sourceLoc + " vs " + comp.CountryName
		}
		return sim, "location_text_match", "", ""
	}
	if lsLocationVetoByName(source.Name, comp.CountryName) {
		return 0.0, "league_name_location_conflict", "location_name_conflict", source.Name + " vs " + comp.CountryName
	}
	if sourceLoc == "" && comp.CountryName == "" {
		return 0.5, "location_unknown", "", ""
	}
	return 0.6, "location_partial", "", ""
}

func evaluateLeagueEvidenceKeyword(source LeagueEvidenceSource, comp LeagueEvidenceCompetition) (float64, string) {
	nameScore := leagueNameSimilarityWithAlias(source.Name, comp.CompetitionName)
	featureScore := featureKeywordAgreement(source.Features, comp.Features)
	score := clamp01(nameScore*0.70 + featureScore*0.30)
	result := "weak_name_feature"
	if nameScore >= 0.95 {
		result = "alias_or_name_high"
	} else if featureScore >= 0.95 {
		result = "keyword_features_aligned"
	} else if score < 0.40 {
		result = "keyword_weak"
	}
	return score, result
}

func featureKeywordAgreement(a, b LeagueFeatures) float64 {
	checks := 0
	matches := 0
	if a.Gender != GenderUnknown || b.Gender != GenderUnknown {
		checks++
		if a.Gender == b.Gender {
			matches++
		}
	}
	if a.AgeGroup != "" || b.AgeGroup != "" {
		checks++
		if a.AgeGroup == b.AgeGroup {
			matches++
		}
	}
	if a.Region != "" || b.Region != "" {
		checks++
		if a.Region == b.Region {
			matches++
		}
	}
	if a.CompetitionType != "" || b.CompetitionType != "" {
		checks++
		if a.CompetitionType == b.CompetitionType {
			matches++
		}
	}
	if a.TierNumber > 0 || b.TierNumber > 0 {
		checks++
		if a.TierNumber == b.TierNumber {
			matches++
		}
	}
	if checks == 0 {
		return 0.5
	}
	return float64(matches) / float64(checks)
}

func topLeagueEventExamples(matches []ResolvedEventMatch, limit int) []LeagueEventExample {
	sorted := append([]ResolvedEventMatch(nil), matches...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return maxFloat(sorted[i].Score, sorted[i].Confidence) > maxFloat(sorted[j].Score, sorted[j].Confidence)
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	out := make([]LeagueEventExample, 0, len(sorted))
	for _, m := range sorted {
		out = append(out, LeagueEventExample{SREventID: m.SREventID, TSMatchID: m.TSMatchID, Score: round3(maxFloat(m.Score, m.Confidence)), Rule: firstMatchRule(m.Rule, m.MatchRule), TimeDiffSec: m.TimeDiffSec, SRHomeName: m.SRHomeName, SRAwayName: m.SRAwayName, TSHomeName: m.TSHomeName, TSAwayName: m.TSAwayName, ReasonCodes: m.ReasonCodes})
	}
	return out
}

func firstMatchRule(values ...MatchRule) MatchRule {
	for _, v := range values {
		if v != "" && v != RuleEventNoMatch {
			return v
		}
	}
	return RuleEventNoMatch
}

func addNonEmpty(m map[string]bool, v string) {
	if v != "" {
		m[v] = true
	}
}

func safeRatio(num, den int) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizeCountryCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
