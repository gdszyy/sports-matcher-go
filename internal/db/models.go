// Package db — 数据模型定义
package db

// SRTournament SR 联赛
type SRTournament struct {
	ID           string
	Name         string
	SportID      string // 原始 sport_id，如 sr:sport:1
	CategoryID   string
	CategoryName string
	Sport        string // 推导后的运动类型名称
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
