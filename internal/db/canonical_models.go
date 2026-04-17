// Package db — 规范化（Canonical）数据模型
//
// 设计原则：
//   - 消除数据源前缀（SR/LS/TS），提供统一的实体形状
//   - 所有时间字段统一为 Unix 秒时间戳（StartUnix / MatchTime）
//   - 所有运动类型统一为小写英文字符串（"football", "basketball" 等）
//   - Source 字段记录数据来源，供路由层追踪
//
// 使用方：
//   - UniversalEngine 及后续所有算法层应直接消费 Canonical 实体
//   - 各 DataNormalizer 实现负责将 Adapter 原始数据转换为 Canonical 实体
package db

// CanonicalTournament 规范化联赛实体
// 统一了 SRTournament、LSTournament 的字段差异
type CanonicalTournament struct {
	ID           string // 源侧联赛 ID（如 SR tournament_id 或 LS tournament_id）
	Name         string // 联赛英文名称
	Sport        string // 运动类型：football / basketball / tennis / ice_hockey / baseball
	CategoryName string // 地区/分类名称（如 "England"、"Spain"）
	Source       string // 数据来源标识：sr / ls
}

// CanonicalEvent 规范化比赛实体
// 统一了 SREvent 和 LSEvent 的字段差异：
//   - SR: StartTime(ISO8601) → StartUnix(Unix)
//   - LS: StartTime(多格式ISO8601) → StartUnix(Unix)，StatusID → StatusCode
type CanonicalEvent struct {
	ID           string // 源侧比赛 ID
	TournamentID string // 源侧联赛 ID
	StartUnix    int64  // 比赛开始时间（Unix 秒），已统一时区处理
	HomeID       string // 主队 ID
	HomeName     string // 主队名称
	AwayID       string // 客队 ID
	AwayName     string // 客队名称
	StatusCode   int    // 比赛状态码（各源含义可能不同，仅作参考）
	Source       string // 数据来源标识：sr / ls
}

// CanonicalTeam 规范化球队实体
type CanonicalTeam struct {
	ID     string // 源侧球队 ID
	Name   string // 球队名称
	Source string // 数据来源标识：sr / ls
}

// CanonicalPlayer 规范化球员实体
// 统一了 SRPlayer 和 LSPlayer 的字段差异：
//   - SR: DateOfBirth(YYYY-MM-DD), Nationality, FullName
//   - LS: 无生日/国籍字段（从 Snapshot API 获取），FullName 为空
type CanonicalPlayer struct {
	ID          string // 源侧球员 ID
	Name        string // 球员名称（优先 FullName）
	FullName    string // 完整名称（SR 专有，LS 为空）
	Birthday    string // 生日字符串（YYYY-MM-DD 或原始 Unix 字符串，LS 为空）
	Nationality string // 国籍（LS 为空）
	TeamID      string // 所属球队 ID
	Source      string // 数据来源标识：sr / ls
}

// CanonicalTSCompetition 规范化 TS 联赛实体（目标侧，不变）
// 与 TSCompetition 保持一致，仅作类型别名用于文档清晰性
type CanonicalTSCompetition = TSCompetition

// CanonicalTSEvent 规范化 TS 比赛实体（目标侧，不变）
// 与 TSEvent 保持一致，仅作类型别名用于文档清晰性
type CanonicalTSEvent = TSEvent
