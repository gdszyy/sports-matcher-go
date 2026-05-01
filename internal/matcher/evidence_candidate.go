package matcher

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

const (
	ReasonNameSimHigh           = "NAME_SIM_HIGH"
	ReasonNameSimMedium         = "NAME_SIM_MEDIUM"
	ReasonCountryMatch          = "COUNTRY_MATCH"
	ReasonCountryMissing        = "COUNTRY_MISSING"
	ReasonRecentMatchWindow     = "RECENT_MATCH_WINDOW"
	ReasonInternationalFallback = "INTERNATIONAL_FALLBACK"
	ReasonGuardVetoAge          = "GUARD_VETO_AGE"
	ReasonGuardVetoGender       = "GUARD_VETO_GENDER"
	ReasonGuardVetoReserve      = "GUARD_VETO_RESERVE"
	ReasonGuardVetoCountry      = "GUARD_VETO_COUNTRY"
	ReasonGuardVetoLeague       = "GUARD_VETO_LEAGUE_KEYWORD"
	ReasonEventTeamPair         = "EVENT_TEAM_PAIR"
	ReasonCandidateTruncated    = "CANDIDATE_TRUNCATED"
)

const (
	evidencePriorityAlias = iota
	evidencePriorityNameCountry
	evidencePriorityNameRecentMatch
	evidencePriorityInternationalFallback
)

// EvidenceCandidateOptions 控制 Evidence-First P2 候选召回的规模、时间窗口和守门阈值。
type EvidenceCandidateOptions struct {
	SourceSide string
	Sport      string

	LeagueName    string
	LeagueCountry string

	TeamTopK              int
	InternationalTeamTopK int
	EventCandidateLimit   int
	RecentWindow          time.Duration
	EventWindowPadding    time.Duration

	NameHighThreshold   float64
	NameMediumThreshold float64
	FallbackThreshold   float64

	AliasStore     *db.AliasStore
	TeamAliasIndex *TeamAliasIndex
}

// EvidenceTeamCandidateQuery 是 P2 对 P1 TS 队伍候选查询接口的最小依赖。
type EvidenceTeamCandidateQuery struct {
	Sport             string
	Names             []string
	Country           string
	AllowCrossCountry bool
	AliasTeamIDs      []string
	MinMatchTime      int64
	MaxMatchTime      int64
	Limit             int
}

// EvidenceQueriedTeam 表示 P1 查询接口返回的一条 TS 队伍候选。
type EvidenceQueriedTeam struct {
	TeamID          string
	TeamName        string
	CompetitionID   string
	CompetitionName string
	Country         string
	LastMatchTime   int64
	RecentMatch     bool
	Source          string
}

// EvidenceEventCandidateQuery 是 P2 对 P1 TS 比赛候选查询接口的最小依赖。
type EvidenceEventCandidateQuery struct {
	Sport        string
	TeamIDs      []string
	MinMatchTime int64
	MaxMatchTime int64
	Limit        int
}

// EvidenceQueriedEvent 表示 P1 查询接口返回的一条 TS 比赛候选。
type EvidenceQueriedEvent struct {
	CompetitionID   string
	CompetitionName string
	Country         string
	Event           db.TSEvent
}

// EvidenceTSCandidateQuerier 抽象 P1 提供的 TS 队伍候选与比赛候选查询接口。
type EvidenceTSCandidateQuerier interface {
	QueryTeamCandidates(ctx context.Context, q EvidenceTeamCandidateQuery) ([]EvidenceQueriedTeam, error)
	QueryEventCandidates(ctx context.Context, q EvidenceEventCandidateQuery) ([]EvidenceQueriedEvent, error)
}

// EvidenceSourceTeam 是源侧单联赛的小规模队伍集合元素。
type EvidenceSourceTeam struct {
	ID   string
	Name string
}

// TeamCandidateEvidence 是 P2 输出的 TS 队伍候选及解释信息。
type TeamCandidateEvidence struct {
	SourceTeamID       string   `json:"source_team_id"`
	SourceTeamName     string   `json:"source_team_name"`
	TSTeamID           string   `json:"ts_team_id"`
	TSTeamName         string   `json:"ts_team_name"`
	CompetitionID      string   `json:"ts_competition_id,omitempty"`
	CompetitionName    string   `json:"ts_competition_name,omitempty"`
	Country            string   `json:"country,omitempty"`
	Priority           int      `json:"priority"`
	Rank               int      `json:"rank"`
	Score              float64  `json:"score"`
	NameScore          float64  `json:"name_score"`
	Source             string   `json:"source"`
	Vetoed             bool     `json:"vetoed"`
	VetoReason         string   `json:"veto_reason,omitempty"`
	StrongConstraintOK bool     `json:"strong_constraint_ok"`
	ReasonCodes        []string `json:"reason_codes"`
}

// EventCandidateEvidence 是 P2 输出的 TS 比赛候选及其可解释先验。
type EventCandidateEvidence struct {
	SourceEventID          string     `json:"source_event_id"`
	CompetitionID          string     `json:"ts_competition_id"`
	CompetitionName        string     `json:"ts_competition_name,omitempty"`
	Event                  db.TSEvent `json:"event"`
	CandidateScore         float64    `json:"candidate_score"`
	HomeTeamCandidateScore float64    `json:"home_team_candidate_score"`
	AwayTeamCandidateScore float64    `json:"away_team_candidate_score"`
	StrongConstraintOK     bool       `json:"strong_constraint_ok"`
	Source                 string     `json:"source"`
	Truncated              bool       `json:"truncated,omitempty"`
	ReasonCodes            []string   `json:"reason_codes"`
}

// EvidenceCandidateResult 汇总 P2 的队伍候选、比赛候选和规模控制信息。
type EvidenceCandidateResult struct {
	TeamsBySourceTeamID        map[string][]TeamCandidateEvidence `json:"teams_by_source_team_id"`
	Events                     []EventCandidateEvidence           `json:"events"`
	P3EventCandidates          []EvidenceEventCandidate           `json:"p3_event_candidates"`
	TruncatedEventCandidates   bool                               `json:"truncated_event_candidates"`
	DroppedEventCandidateCount int                                `json:"dropped_event_candidate_count"`
}

// EvidenceCandidateGenerator 将源侧单联赛队伍集合转化为 TS 队伍候选与 TS 比赛候选。
type EvidenceCandidateGenerator struct {
	querier EvidenceTSCandidateQuerier
	options EvidenceCandidateOptions
}

// NewEvidenceCandidateGenerator 创建 Evidence-First P2 候选生成器。
func NewEvidenceCandidateGenerator(querier EvidenceTSCandidateQuerier, options EvidenceCandidateOptions) *EvidenceCandidateGenerator {
	return &EvidenceCandidateGenerator{querier: querier, options: normalizeEvidenceCandidateOptions(options)}
}

// Generate 基于源侧队伍和比赛时间范围生成 P2 候选池；本阶段只召回和解释，不做最终联赛确认。
func (g *EvidenceCandidateGenerator) Generate(ctx context.Context, teams []EvidenceSourceTeam, events []db.SREvent) (*EvidenceCandidateResult, error) {
	if g == nil || g.querier == nil {
		return &EvidenceCandidateResult{TeamsBySourceTeamID: map[string][]TeamCandidateEvidence{}}, nil
	}
	teams = mergeSourceTeams(teams, events)
	minTime, maxTime := sourceEventTimeRange(events, g.options.EventWindowPadding)
	international := isEvidenceInternational(g.options.LeagueName, g.options.LeagueCountry)

	result := &EvidenceCandidateResult{TeamsBySourceTeamID: make(map[string][]TeamCandidateEvidence)}
	allTeamIDs := make(map[string]struct{})
	teamEvidenceByTSID := make(map[string][]TeamCandidateEvidence)

	for _, team := range teams {
		query := EvidenceTeamCandidateQuery{
			Sport:             g.options.Sport,
			Names:             evidenceNameVariants(team.Name),
			Country:           g.options.LeagueCountry,
			AllowCrossCountry: international || strings.TrimSpace(g.options.LeagueCountry) == "",
			AliasTeamIDs:      g.aliasTeamIDs(team.ID),
			MinMatchTime:      minTime,
			MaxMatchTime:      maxTime,
			Limit:             g.teamQueryLimit(international),
		}
		queried, err := g.querier.QueryTeamCandidates(ctx, query)
		if err != nil {
			return nil, err
		}
		cands := g.scoreTeamCandidates(team, queried, international)
		cands = g.limitTeamCandidates(cands, international)
		result.TeamsBySourceTeamID[team.ID] = cands
		for _, cand := range cands {
			if cand.TSTeamID == "" || cand.Vetoed {
				continue
			}
			allTeamIDs[cand.TSTeamID] = struct{}{}
			teamEvidenceByTSID[cand.TSTeamID] = append(teamEvidenceByTSID[cand.TSTeamID], cand)
		}
	}

	if len(events) == 0 || len(allTeamIDs) == 0 {
		return result, nil
	}
	eventQuery := EvidenceEventCandidateQuery{Sport: g.options.Sport, TeamIDs: sortedKeys(allTeamIDs), MinMatchTime: minTime, MaxMatchTime: maxTime, Limit: g.options.EventCandidateLimit * 2}
	queriedEvents, err := g.querier.QueryEventCandidates(ctx, eventQuery)
	if err != nil {
		return nil, err
	}
	result.Events = g.scoreEventCandidates(events, queriedEvents, teamEvidenceByTSID, international)
	sort.SliceStable(result.Events, func(i, j int) bool {
		if result.Events[i].CandidateScore == result.Events[j].CandidateScore {
			return result.Events[i].Event.ID < result.Events[j].Event.ID
		}
		return result.Events[i].CandidateScore > result.Events[j].CandidateScore
	})
	if len(result.Events) > g.options.EventCandidateLimit {
		result.TruncatedEventCandidates = true
		result.DroppedEventCandidateCount = len(result.Events) - g.options.EventCandidateLimit
		result.Events = result.Events[:g.options.EventCandidateLimit]
		for i := range result.Events {
			result.Events[i].Truncated = true
			result.Events[i].ReasonCodes = appendReason(result.Events[i].ReasonCodes, ReasonCandidateTruncated)
		}
	}
	result.P3EventCandidates = toP3EventCandidates(result.Events)
	return result, nil
}

func normalizeEvidenceCandidateOptions(o EvidenceCandidateOptions) EvidenceCandidateOptions {
	if o.SourceSide == "" {
		o.SourceSide = "sr"
	}
	if o.Sport == "" {
		o.Sport = "football"
	}
	if o.TeamTopK <= 0 {
		o.TeamTopK = 8
	}
	if o.InternationalTeamTopK <= 0 {
		o.InternationalTeamTopK = 15
	}
	if o.EventCandidateLimit <= 0 {
		o.EventCandidateLimit = 300
	}
	if o.RecentWindow <= 0 {
		o.RecentWindow = 30 * 24 * time.Hour
	}
	if o.EventWindowPadding <= 0 {
		o.EventWindowPadding = 72 * time.Hour
	}
	if o.NameHighThreshold <= 0 {
		o.NameHighThreshold = 0.86
	}
	if o.NameMediumThreshold <= 0 {
		o.NameMediumThreshold = 0.68
	}
	if o.FallbackThreshold <= 0 {
		o.FallbackThreshold = 0.78
	}
	return o
}

func mergeSourceTeams(teams []EvidenceSourceTeam, events []db.SREvent) []EvidenceSourceTeam {
	seen := map[string]bool{}
	out := make([]EvidenceSourceTeam, 0, len(teams)+len(events)*2)
	add := func(id, name string) {
		key := id
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(name))
		}
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, EvidenceSourceTeam{ID: id, Name: name})
	}
	for _, t := range teams {
		add(t.ID, t.Name)
	}
	for _, ev := range events {
		add(ev.HomeID, ev.HomeName)
		add(ev.AwayID, ev.AwayName)
	}
	return out
}

func sourceEventTimeRange(events []db.SREvent, padding time.Duration) (int64, int64) {
	if len(events) == 0 {
		return 0, 0
	}
	minTime, maxTime := int64(math.MaxInt64), int64(0)
	for _, ev := range events {
		if ev.StartUnix <= 0 {
			continue
		}
		if ev.StartUnix < minTime {
			minTime = ev.StartUnix
		}
		if ev.StartUnix > maxTime {
			maxTime = ev.StartUnix
		}
	}
	if minTime == int64(math.MaxInt64) {
		return 0, 0
	}
	pad := int64(padding.Seconds())
	return minTime - pad, maxTime + pad
}

func (g *EvidenceCandidateGenerator) aliasTeamIDs(sourceTeamID string) []string {
	ids := map[string]struct{}{}
	if g.options.TeamAliasIndex != nil {
		if id := g.options.TeamAliasIndex.GetTSID(sourceTeamID); id != "" {
			ids[id] = struct{}{}
		}
	}
	if g.options.AliasStore != nil {
		if id, ok := g.options.AliasStore.Lookup(g.options.SourceSide, sourceTeamID); ok && id != "" {
			ids[id] = struct{}{}
		}
	}
	return sortedKeys(ids)
}

func (g *EvidenceCandidateGenerator) teamQueryLimit(international bool) int {
	limit := g.options.TeamTopK * 3
	if international {
		limit = g.options.InternationalTeamTopK * 3
	}
	if limit < 20 {
		limit = 20
	}
	return limit
}

func (g *EvidenceCandidateGenerator) scoreTeamCandidates(source EvidenceSourceTeam, queried []EvidenceQueriedTeam, international bool) []TeamCandidateEvidence {
	merged := map[string]TeamCandidateEvidence{}
	for _, q := range queried {
		if q.TeamID == "" {
			continue
		}
		aliasHit := containsString(g.aliasTeamIDs(source.ID), q.TeamID)
		nameScore := teamNameSimilarity(source.Name, q.TeamName)
		if normScore := normalizedTeamSimilarity(source.Name, q.TeamName); normScore > nameScore {
			nameScore = normScore
		}
		priority := evidencePriorityNameRecentMatch
		reasons := []string{}
		score := nameScore
		if aliasHit {
			priority = evidencePriorityAlias
			score = 1.0
			reasons = appendReason(reasons, ReasonAliasHit)
		} else if sameCountry(g.options.LeagueCountry, q.Country) {
			priority = evidencePriorityNameCountry
			score += 0.05
			reasons = appendReason(reasons, ReasonCountryMatch)
		} else if international || strings.TrimSpace(g.options.LeagueCountry) == "" || strings.TrimSpace(q.Country) == "" {
			priority = evidencePriorityInternationalFallback
			score -= 0.08
			reasons = appendReason(reasons, ReasonInternationalFallback)
			if strings.TrimSpace(g.options.LeagueCountry) == "" || strings.TrimSpace(q.Country) == "" {
				reasons = appendReason(reasons, ReasonCountryMissing)
			}
		} else {
			priority = evidencePriorityNameCountry
			score -= 0.35
		}
		if nameScore >= g.options.NameHighThreshold {
			reasons = appendReason(reasons, ReasonNameSimHigh)
		} else if nameScore >= g.options.NameMediumThreshold {
			reasons = appendReason(reasons, ReasonNameSimMedium)
		}
		if q.RecentMatch || withinRecentWindow(q.LastMatchTime, g.options.RecentWindow) {
			reasons = appendReason(reasons, ReasonRecentMatchWindow)
			score += 0.03
		}
		vetoed, vetoReason, vetoCode := g.applyTeamGuards(source.Name, q, international)
		if vetoed {
			score = math.Min(score, 0.30)
			reasons = appendReason(reasons, vetoCode)
		}
		if !aliasHit && priority == evidencePriorityInternationalFallback && nameScore < g.options.FallbackThreshold {
			continue
		}
		if !aliasHit && nameScore < g.options.NameMediumThreshold && !q.RecentMatch {
			continue
		}
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		cand := TeamCandidateEvidence{SourceTeamID: source.ID, SourceTeamName: source.Name, TSTeamID: q.TeamID, TSTeamName: q.TeamName, CompetitionID: q.CompetitionID, CompetitionName: q.CompetitionName, Country: q.Country, Priority: priority, Score: round3(score), NameScore: round3(nameScore), Source: q.Source, Vetoed: vetoed, VetoReason: vetoReason, StrongConstraintOK: !vetoed, ReasonCodes: reasons}
		if old, ok := merged[q.TeamID]; !ok || compareTeamCandidate(cand, old) < 0 {
			merged[q.TeamID] = cand
		}
	}
	out := make([]TeamCandidateEvidence, 0, len(merged))
	for _, cand := range merged {
		out = append(out, cand)
	}
	sort.SliceStable(out, func(i, j int) bool { return compareTeamCandidate(out[i], out[j]) < 0 })
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

func compareTeamCandidate(a, b TeamCandidateEvidence) int {
	if a.Vetoed != b.Vetoed {
		if !a.Vetoed {
			return -1
		}
		return 1
	}
	if a.Priority != b.Priority {
		if a.Priority < b.Priority {
			return -1
		}
		return 1
	}
	if a.Score != b.Score {
		if a.Score > b.Score {
			return -1
		}
		return 1
	}
	if a.NameScore != b.NameScore {
		if a.NameScore > b.NameScore {
			return -1
		}
		return 1
	}
	if a.TSTeamID < b.TSTeamID {
		return -1
	}
	if a.TSTeamID > b.TSTeamID {
		return 1
	}
	return 0
}

func (g *EvidenceCandidateGenerator) limitTeamCandidates(cands []TeamCandidateEvidence, international bool) []TeamCandidateEvidence {
	limit := g.options.TeamTopK
	if international {
		limit = g.options.InternationalTeamTopK
	}
	if len(cands) <= limit {
		return cands
	}
	out := append([]TeamCandidateEvidence(nil), cands[:limit]...)
	for i := range out {
		out[i].ReasonCodes = appendReason(out[i].ReasonCodes, ReasonCandidateTruncated)
	}
	return out
}

func (g *EvidenceCandidateGenerator) applyTeamGuards(sourceName string, q EvidenceQueriedTeam, international bool) (bool, string, string) {
	src := extractTeamGuardProfile(sourceName + " " + g.options.LeagueName)
	ts := extractTeamGuardProfile(q.TeamName + " " + q.CompetitionName)
	if src.gender != "" || ts.gender != "" {
		if src.gender != ts.gender {
			return true, src.gender + " vs " + ts.gender, ReasonGuardVetoGender
		}
	}
	if src.age != "" || ts.age != "" {
		if src.age != ts.age {
			return true, src.age + " vs " + ts.age, ReasonGuardVetoAge
		}
	}
	if src.reserve != ts.reserve {
		return true, "reserve mismatch", ReasonGuardVetoReserve
	}
	if !international && strings.TrimSpace(g.options.LeagueCountry) != "" && strings.TrimSpace(q.Country) != "" && !sameCountry(g.options.LeagueCountry, q.Country) {
		return true, g.options.LeagueCountry + " vs " + q.Country, ReasonGuardVetoCountry
	}
	if !international && q.CompetitionName != "" && g.options.LeagueName != "" {
		veto := CheckLeagueVeto(ExtractLeagueFeatures(g.options.LeagueName), ExtractLeagueFeatures(q.CompetitionName), "med")
		if veto.Vetoed {
			switch veto.Reason {
			case VetoGender:
				return true, veto.Detail, ReasonGuardVetoGender
			case VetoAge:
				return true, veto.Detail, ReasonGuardVetoAge
			default:
				return true, veto.Detail, ReasonGuardVetoLeague
			}
		}
	}
	return false, "", ""
}

func (g *EvidenceCandidateGenerator) scoreEventCandidates(sourceEvents []db.SREvent, queried []EvidenceQueriedEvent, teamEvidence map[string][]TeamCandidateEvidence, international bool) []EventCandidateEvidence {
	out := []EventCandidateEvidence{}
	for _, ev := range queried {
		homeEvs := teamEvidence[ev.Event.HomeID]
		awayEvs := teamEvidence[ev.Event.AwayID]
		if len(homeEvs) == 0 || len(awayEvs) == 0 {
			continue
		}
		for _, src := range sourceEvents {
			home := bestEvidenceForSource(homeEvs, src.HomeID)
			away := bestEvidenceForSource(awayEvs, src.AwayID)
			if home == nil || away == nil {
				continue
			}
			if !international && (!home.StrongConstraintOK || !away.StrongConstraintOK) {
				continue
			}
			timeScore := evidenceTimeScore(abs64(src.StartUnix - ev.Event.MatchTime))
			score := (home.Score*0.40 + away.Score*0.40 + timeScore*0.20)
			reasons := mergeReasons(home.ReasonCodes, away.ReasonCodes)
			reasons = appendReason(reasons, ReasonEventTeamPair)
			if abs64(src.StartUnix-ev.Event.MatchTime) <= int64(g.options.EventWindowPadding.Seconds()) {
				reasons = appendReason(reasons, ReasonRecentMatchWindow)
			}
			out = append(out, EventCandidateEvidence{SourceEventID: src.ID, CompetitionID: ev.CompetitionID, CompetitionName: ev.CompetitionName, Event: ev.Event, CandidateScore: round3(score), HomeTeamCandidateScore: home.Score, AwayTeamCandidateScore: away.Score, StrongConstraintOK: home.StrongConstraintOK && away.StrongConstraintOK, Source: "team_pair", ReasonCodes: reasons})
		}
	}
	return out
}

func bestEvidenceForSource(cands []TeamCandidateEvidence, sourceTeamID string) *TeamCandidateEvidence {
	var best *TeamCandidateEvidence
	for i := range cands {
		if cands[i].SourceTeamID != sourceTeamID {
			continue
		}
		if best == nil || compareTeamCandidate(cands[i], *best) < 0 {
			best = &cands[i]
		}
	}
	return best
}

func toP3EventCandidates(events []EventCandidateEvidence) []EvidenceEventCandidate {
	out := make([]EvidenceEventCandidate, 0, len(events))
	for _, e := range events {
		out = append(out, EvidenceEventCandidate{CompetitionID: e.CompetitionID, CompetitionName: e.CompetitionName, Event: e.Event, CandidateScore: e.CandidateScore, HomeTeamCandidateScore: e.HomeTeamCandidateScore, AwayTeamCandidateScore: e.AwayTeamCandidateScore, StrongConstraintOK: e.StrongConstraintOK})
	}
	return out
}

func evidenceNameVariants(name string) []string {
	variants := []string{name, NormalizeTeamName(name, false), NormalizeTeamName(name, true), cleanTeamName(name), normalizeName(name)}
	seen := map[string]bool{}
	out := []string{}
	for _, v := range variants {
		v = strings.TrimSpace(v)
		if v != "" && !seen[strings.ToLower(v)] {
			seen[strings.ToLower(v)] = true
			out = append(out, v)
		}
	}
	return out
}

func isEvidenceInternational(name, country string) bool {
	text := strings.ToLower(name + " " + country)
	for _, kw := range []string{"international", "world", "uefa", "fifa", "afc", "conmebol", "concacaf", "caf", "ofc", "champions league", "europa league", "libertadores", "sudamericana", "nations league"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return strings.TrimSpace(country) == ""
}

func sameCountry(a, b string) bool {
	aa, bb := normalizeCountryText(a), normalizeCountryText(b)
	return aa != "" && bb != "" && aa == bb
}

func normalizeCountryText(s string) string {
	s = strings.ToLower(flattenDiacritics(strings.TrimSpace(s)))
	s = strings.ReplaceAll(s, "united states", "usa")
	s = strings.ReplaceAll(s, "u.s.a.", "usa")
	s = strings.ReplaceAll(s, "england", "united kingdom")
	return strings.Join(strings.Fields(s), " ")
}

type teamGuardProfile struct {
	gender, age string
	reserve     bool
}

var ageGuardRe = regexp.MustCompile(`(?i)\b(?:u|under\s*)(\d{2})\b`)

func extractTeamGuardProfile(s string) teamGuardProfile {
	lower := " " + strings.ToLower(flattenDiacritics(s)) + " "
	p := teamGuardProfile{}
	if strings.Contains(lower, " women") || strings.Contains(lower, " womens") || strings.Contains(lower, " ladies") || strings.Contains(lower, " female") || strings.Contains(lower, " (w)") || strings.Contains(lower, " w ") {
		p.gender = "women"
	}
	if strings.Contains(lower, " men") || strings.Contains(lower, " male") {
		p.gender = "men"
	}
	if m := ageGuardRe.FindStringSubmatch(lower); len(m) == 2 {
		p.age = "u" + m[1]
	}
	if strings.Contains(lower, " reserve") || strings.Contains(lower, " reserves") || strings.Contains(lower, " b team") {
		p.reserve = true
	}
	return p
}

func withinRecentWindow(ts int64, window time.Duration) bool {
	if ts <= 0 || window <= 0 {
		return false
	}
	return time.Since(time.Unix(ts, 0)) <= window
}

func evidenceTimeScore(diff int64) float64 {
	if diff < 0 {
		diff = -diff
	}
	const sigma = 6 * 3600.0
	return math.Exp(-float64(diff*diff) / (2 * sigma * sigma))
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func appendReason(reasons []string, reason string) []string {
	if reason == "" {
		return reasons
	}
	for _, r := range reasons {
		if r == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

func mergeReasons(groups ...[]string) []string {
	out := []string{}
	for _, group := range groups {
		for _, r := range group {
			out = appendReason(out, r)
		}
	}
	return out
}
