// Package db — 数据模型定义
package db

// SRTournament SR 联赛
type SRTournament struct {
	ID              string
	Name            string
	SportID         string // 原始 sport_id，如 sr:sport:1
	CategoryID      string
	CategoryName    string
	CategoryCountryCode string // 分类国家/地区代码（如 "ENG"、"ESP"），可为空
	Sport           string // 推导后的运动类型名称
}

// SREvent SR 比赛
type SREvent struct {
	ID           string
	TournamentID string
	StartTime    string // ISO8601
	StartUnix    int64  // Unix 时间戳（秒）
	HomeID       string
	HomeName     string
	AwayID       string
	AwayName     string
	StatusCode   int
	VenueID      string
	VenueName    string
}

// SRTeam SR 球队
type SRTeam struct {
	ID        string
	Name      string
	PlayerIDs []string
}

// SRPlayer SR 球员
type SRPlayer struct {
	ID          string
	Name        string
	FullName    string // full_name 字段（更完整）
	DateOfBirth string
	Nationality string
	TeamID      string
}

// TSCompetition TS 联赛
type TSCompetition struct {
	ID          string
	Name        string
	CountryName string
	CountryCode string // 国家/地区代码（如 "ENG"、"ESP"），可为空
	Sport       string // football / basketball
}

// TSEvent TS 比赛
type TSEvent struct {
	ID         string
	MatchID    string
	MatchTime  int64 // Unix 时间戳（秒）
	HomeID     string
	HomeName   string
	AwayID     string
	AwayName   string
	StatusID   int
	VenueID    string
	VenueName  string
}

// TSTeam TS 球队
type TSTeam struct {
	ID            string
	Name          string
	CompetitionID string
}

// TSPlayer TS 球员
type TSPlayer struct {
	ID          string
	Name        string
	Birthday    string
	Nationality string
	TeamID      string
}

// ─── LSports 模型 ─────────────────────────────────────────────────────────────

// LSTournament LSports 联赛
type LSTournament struct {
	ID           string // tournament_id（整数字符串）
	Name         string
	SportID      string // 如 "6046"（足球）
	CategoryID   string
	CategoryName string
	Sport        string // 推导后的运动类型名称：football / basketball / tennis 等
}

// LSTeam LSports 球队（从 Snapshot Participants 中提取）
type LSTeam struct {
	ID   string // competitor_id
	Name string
}

// LSPlayer LSports 球员（从 Snapshot Fixture.Participants[].Players 中提取）
// 注意：LSports Snapshot 提供的球员数据字段较少，仅含 ID 和名称，无生日/国籍
type LSPlayer struct {
	ID     string // LSports Player ID（整数字符串）
	Name   string // 英文名称
	TeamID string // 所属球队 competitor_id
}

// LSEvent LSports 赛事
type LSEvent struct {
	ID           string // event_id
	TournamentID string
	StartTime    string // scheduled，ISO8601 字符串
	StartUnix    int64  // Unix 时间戳（秒）
	HomeID       string // home_competitor_id
	HomeName     string
	AwayID       string // away_competitor_id
	AwayName     string
	StatusID     int
}
