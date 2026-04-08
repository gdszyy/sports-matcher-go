// Package matcher — 匹配结果数据结构
package matcher

// MatchRule 匹配规则枚举
type MatchRule string

const (
	RuleLeagueKnown   MatchRule = "LEAGUE_KNOWN"    // 已知映射
	RuleLeagueNameHi  MatchRule = "LEAGUE_NAME_HI"  // 名称高相似（≥0.85）
	RuleLeagueNameMed MatchRule = "LEAGUE_NAME_MED" // 名称中相似（≥0.70）
	RuleLeagueNameLow MatchRule = "LEAGUE_NAME_LOW" // 名称低相似（≥0.55）
	RuleLeagueNoMatch MatchRule = "LEAGUE_NO_MATCH" // 未匹配

	RuleEventL1      MatchRule = "EVENT_L1"        // 时差≤5min + 名称
	RuleEventL2      MatchRule = "EVENT_L2"        // 时差≤6h + 名称
	RuleEventL3      MatchRule = "EVENT_L3"        // 同日 + 名称
	RuleEventL4      MatchRule = "EVENT_L4"        // 超宽时间（≤72h）+ 别名强匹配（≥0.85），require_alias=true
	RuleEventL5      MatchRule = "EVENT_L5"        // 无时间约束唯一性匹配（名称≥0.90 且 TS 候选唯一，时差≤30天）
	RuleEventL4b     MatchRule = "EVENT_L4B"       // 球队 ID 精确对兜底（无时间限制）
	RuleEventNoMatch MatchRule = "EVENT_NO_MATCH"  // 未匹配

	RuleTeamDerived MatchRule = "TEAM_DERIVED" // 从比赛推导
	RuleTeamNoMatch MatchRule = "TEAM_NO_MATCH"

	RulePlayerNameHi  MatchRule = "PLAYER_NAME_HI"  // 名称高相似（≥0.85）
	RulePlayerNameMed MatchRule = "PLAYER_NAME_MED" // 名称中相似（≥0.70）
	RulePlayerDOB     MatchRule = "PLAYER_DOB"      // 名称+生日
	RulePlayerNoMatch MatchRule = "PLAYER_NO_MATCH"
)

// LeagueMatch 联赛匹配结果
type LeagueMatch struct {
	SRTournamentID  string    `json:"sr_tournament_id"`
	SRName          string    `json:"sr_name"`
	SRCategory      string    `json:"sr_category"`
	TSCompetitionID string    `json:"ts_competition_id"`
	TSName          string    `json:"ts_name"`
	TSCountry       string    `json:"ts_country"`
	Matched         bool      `json:"matched"`
	MatchRule       MatchRule `json:"match_rule"`
	Confidence      float64   `json:"confidence"`
}

// EventMatch 比赛匹配结果
type EventMatch struct {
	SREventID   string    `json:"sr_event_id"`
	SRStartTime string    `json:"sr_start_time"`
	SRStartUnix int64     `json:"sr_start_unix"`
	SRHomeName  string    `json:"sr_home_name"`
	SRHomeID    string    `json:"sr_home_id"`
	SRAwayName  string    `json:"sr_away_name"`
	SRAwayID    string    `json:"sr_away_id"`

	TSMatchID   string    `json:"ts_match_id,omitempty"`
	TSMatchTime int64     `json:"ts_match_time,omitempty"`
	TSHomeName  string    `json:"ts_home_name,omitempty"`
	TSHomeID    string    `json:"ts_home_id,omitempty"`
	TSAwayName  string    `json:"ts_away_name,omitempty"`
	TSAwayID    string    `json:"ts_away_id,omitempty"`

	Matched     bool      `json:"matched"`
	MatchRule   MatchRule `json:"match_rule"`
	Confidence  float64   `json:"confidence"`
	TimeDiffSec int64     `json:"time_diff_sec,omitempty"`

	// 自底向上校正后的置信度加成
	BottomUpBonus float64 `json:"bottom_up_bonus,omitempty"`
}

// TeamMapping 球队映射
type TeamMapping struct {
	SRTeamID   string    `json:"sr_team_id"`
	SRTeamName string    `json:"sr_team_name"`
	TSTeamID   string    `json:"ts_team_id"`
	TSTeamName string    `json:"ts_team_name"`
	MatchRule  MatchRule `json:"match_rule"`
	Confidence float64   `json:"confidence"`
	VoteCount  int       `json:"vote_count"` // 投票比赛数

	// 自底向上校正
	PlayerOverlapRate float64 `json:"player_overlap_rate,omitempty"`
	BottomUpBonus     float64 `json:"bottom_up_bonus,omitempty"`
}

// PlayerMatch 球员匹配结果
type PlayerMatch struct {
	SRPlayerID   string    `json:"sr_player_id"`
	SRName       string    `json:"sr_name"`
	SRDOB        string    `json:"sr_dob,omitempty"`
	SRTeamID     string    `json:"sr_team_id"`

	TSPlayerID   string    `json:"ts_player_id,omitempty"`
	TSName       string    `json:"ts_name,omitempty"`
	TSDOB        string    `json:"ts_dob,omitempty"`
	TSTeamID     string    `json:"ts_team_id,omitempty"`

	Matched     bool      `json:"matched"`
	MatchRule   MatchRule `json:"match_rule"`
	Confidence  float64   `json:"confidence"`
}

// MatchStats 匹配统计
type MatchStats struct {
	Sport   string `json:"sport"`
	Tier    string `json:"tier"`

	LeagueSRName    string    `json:"league_sr_name"`
	LeagueTSName    string    `json:"league_ts_name"`
	LeagueMatched   bool      `json:"league_matched"`
	LeagueRule      MatchRule `json:"league_rule"`
	LeagueConf      float64   `json:"league_confidence"`

	EventTotal        int     `json:"event_total"`
	EventMatched      int     `json:"event_matched"`
	EventMatchRate    float64 `json:"event_match_rate"`
	EventL1           int     `json:"event_l1"`
	EventL2           int     `json:"event_l2"`
	EventL3           int     `json:"event_l3"`
	EventL4           int     `json:"event_l4"`
	EventL5           int     `json:"event_l5"`
	EventL4b          int     `json:"event_l4b"`
	EventAvgConf      float64 `json:"event_avg_confidence"`

	TeamTotal      int     `json:"team_total"`
	TeamMatched    int     `json:"team_matched"`
	TeamMatchRate  float64 `json:"team_match_rate"`

	PlayerTotal      int     `json:"player_total"`
	PlayerMatched    int     `json:"player_matched"`
	PlayerMatchRate  float64 `json:"player_match_rate"`
	PlayerAvgConf    float64 `json:"player_avg_confidence"`

	ElapsedMs int64 `json:"elapsed_ms"`
}

// MatchResult 单联赛完整匹配结果
type MatchResult struct {
	League  *LeagueMatch  `json:"league"`
	Events  []EventMatch  `json:"events"`
	Teams   []TeamMapping `json:"teams"`
	Players []PlayerMatch `json:"players"`
	Stats   MatchStats    `json:"stats"`
}
