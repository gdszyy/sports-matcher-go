// Package db — TS 数据库查询适配器
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// TSAdapter 封装对 test-thesports-db 的查询
type TSAdapter struct {
	db *sql.DB
}

// NewTSAdapter 创建 TS 适配器
func NewTSAdapter(db *sql.DB) *TSAdapter {
	return &TSAdapter{db: db}
}

// GetCompetitionsByFootball 查询足球联赛列表
func (a *TSAdapter) GetCompetitionsByFootball() ([]TSCompetition, error) {
	return a.getCompetitions("football")
}

// GetCompetitionsByBasketball 查询篮球联赛列表
func (a *TSAdapter) GetCompetitionsByBasketball() ([]TSCompetition, error) {
	return a.getCompetitions("basketball")
}

func (a *TSAdapter) getCompetitions(sport string) ([]TSCompetition, error) {
	var table, countryField string
	switch sport {
	case "football":
		table = "ts_fb_competition"
		countryField = "COALESCE(host_country,'')"
	case "basketball":
		table = "ts_bb_competition"
		countryField = "''" // ts_bb_competition 没有 host_country 字段
	default:
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}

	query := fmt.Sprintf(`
		SELECT competition_id, COALESCE(name,''), %s
		FROM %s
		LIMIT 2000`, countryField, table)
	rows, err := a.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("getCompetitions(%s): %w", sport, err)
	}
	defer rows.Close()

	var comps []TSCompetition
	for rows.Next() {
		var c TSCompetition
		c.Sport = sport
		if err := rows.Scan(&c.ID, &c.Name, &c.CountryName); err != nil {
			continue
		}
		comps = append(comps, c)
	}
	return comps, rows.Err()
}

// GetCompetition 查询单个联赛
func (a *TSAdapter) GetCompetition(competitionID, sport string) (*TSCompetition, error) {
	var table, countryField string
	switch sport {
	case "football":
		table = "ts_fb_competition"
		countryField = "COALESCE(host_country,'')"
	case "basketball":
		table = "ts_bb_competition"
		countryField = "''"
	default:
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}

	query := fmt.Sprintf(`
		SELECT competition_id, COALESCE(name,''), %s
		FROM %s WHERE competition_id = ? LIMIT 1`, countryField, table)
	row := a.db.QueryRow(query, competitionID)
	var c TSCompetition
	c.Sport = sport
	if err := row.Scan(&c.ID, &c.Name, &c.CountryName); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("GetCompetition: %w", err)
	}
	return &c, nil
}

// GetEvents 查询联赛下的比赛
// 只取近两年内的比赛（避免历史数据占满 LIMIT ）
// TS 比赛表主键是 match_id（字符串），时间字段是 match_time（Unix 时间戳整数）
func (a *TSAdapter) GetEvents(competitionID, sport string) ([]TSEvent, error) {
	var table string
	switch sport {
	case "football":
		table = "ts_fb_match"
	case "basketball":
		table = "ts_bb_match"
	default:
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}

	// 用 Go 计算时间戳，避免 MySQL 函数在部分驱动中失效
	twoYearsAgo := time.Now().AddDate(-2, 0, 0).Unix()
	query := fmt.Sprintf(`
		SELECT 
			match_id,
			COALESCE(match_time, 0) as match_time,
			COALESCE(home_team_id, '') as home_id,
			COALESCE(away_team_id, '') as away_id,
			COALESCE(status_id, 0) as status_id
		FROM %s
		WHERE competition_id = ?
		  AND match_time >= ?
		ORDER BY match_time
		LIMIT 3000`, table)

	rows, err := a.db.Query(query, competitionID, twoYearsAgo)
	if err != nil {
		return nil, fmt.Errorf("GetEvents(%s): %w", sport, err)
	}
	defer rows.Close()

	var events []TSEvent
	for rows.Next() {
		var ev TSEvent
		if err := rows.Scan(&ev.ID, &ev.MatchTime, &ev.HomeID, &ev.AwayID, &ev.StatusID); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// GetTeamNames 查询联赛下的球队名称映射 (team_id → name)
func (a *TSAdapter) GetTeamNames(competitionID, sport string) (map[string]string, error) {
	var matchTable, teamTable string
	switch sport {
	case "football":
		matchTable = "ts_fb_match"
		teamTable = "ts_fb_team"
	case "basketball":
		matchTable = "ts_bb_match"
		teamTable = "ts_bb_team"
	default:
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}

	// TS 球队表主键是 team_id（字符串）
	query := fmt.Sprintf(`
		SELECT DISTINCT t.team_id, COALESCE(t.name,'')
		FROM %s m
		JOIN %s t ON (m.home_team_id = t.team_id OR m.away_team_id = t.team_id)
		WHERE m.competition_id = ?
		LIMIT 200`, matchTable, teamTable)

	rows, err := a.db.Query(query, competitionID)
	if err != nil {
		return nil, fmt.Errorf("GetTeamNames: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		if id != "" {
			result[id] = name
		}
	}
	return result, rows.Err()
}

// GetPlayersByTeam 查询球队球员列表
// TS 球员表主键是 player_id（字符串），生日字段是 birthday（整数 Unix 时间戳）
func (a *TSAdapter) GetPlayersByTeam(teamID, sport string) ([]TSPlayer, error) {
	var table string
	switch sport {
	case "football":
		table = "ts_fb_player"
	case "basketball":
		table = "ts_bb_player"
	default:
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT
			player_id,
			COALESCE(name,''),
			COALESCE(birthday, 0),
			COALESCE(nationality,'')
		FROM %s
		WHERE team_id = ?
		LIMIT 100`, table)

	rows, err := a.db.Query(query, teamID)
	if err != nil {
		return nil, fmt.Errorf("GetPlayersByTeam: %w", err)
	}
	defer rows.Close()

	var players []TSPlayer
	seen := make(map[string]bool)
	for rows.Next() {
		var p TSPlayer
		var birthdayUnix int64
		p.TeamID = teamID
		if err := rows.Scan(&p.ID, &p.Name, &birthdayUnix, &p.Nationality); err != nil {
			continue
		}
		// birthday 是 Unix 时间戳，转为 YYYY-MM-DD
		if birthdayUnix > 0 {
			p.Birthday = fmt.Sprintf("%d", birthdayUnix) // 保留原始值用于比较
		}
		// 去重（TS 库有重复记录）
		key := p.Name + "|" + p.Birthday
		if !seen[key] {
			seen[key] = true
			players = append(players, p)
		}
	}
	return players, rows.Err()
}

// FindEventsByTeamPair 通过主客队 ID 对查找比赛（L4 兜底用）
func (a *TSAdapter) FindEventsByTeamPair(competitionID, homeID, awayID, sport string) ([]TSEvent, error) {
	var table string
	switch sport {
	case "football":
		table = "ts_fb_match"
	case "basketball":
		table = "ts_bb_match"
	default:
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}

	query := fmt.Sprintf(`
		SELECT match_id, COALESCE(match_time,0), home_team_id, away_team_id, COALESCE(status_id,0)
		FROM %s
		WHERE competition_id = ? AND home_team_id = ? AND away_team_id = ?
		LIMIT 5`, table)

	rows, err := a.db.Query(query, competitionID, homeID, awayID)
	if err != nil {
		return nil, fmt.Errorf("FindEventsByTeamPair: %w", err)
	}
	defer rows.Close()

	var events []TSEvent
	for rows.Next() {
		var ev TSEvent
		if err := rows.Scan(&ev.ID, &ev.MatchTime, &ev.HomeID, &ev.AwayID, &ev.StatusID); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// ─── PI-006 v1.9: Batch query 优化 ──────────────────────────────────────────
// 单次 SQL IN(...) 拉取多个 competition 的事件 / 球队名，避免 P1 候选池构建
// 时的 N 次 round-trip。

// GetEventsBatch 批量查询多个 competition 的事件，单次 SQL 查询。
// 返回 map[competition_id][]TSEvent。空切片 / 不存在的 competition_id 不会出现在结果中。
func (a *TSAdapter) GetEventsBatch(competitionIDs []string, sport string) (map[string][]TSEvent, error) {
	if len(competitionIDs) == 0 {
		return map[string][]TSEvent{}, nil
	}
	var table string
	switch sport {
	case "football":
		table = "ts_fb_match"
	case "basketball":
		table = "ts_bb_match"
	default:
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}

	twoYearsAgo := time.Now().AddDate(-2, 0, 0).Unix()

	// 构造 placeholders + args
	placeholders := ""
	args := make([]interface{}, 0, len(competitionIDs)+1)
	for i, id := range competitionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	args = append(args, twoYearsAgo)

	query := fmt.Sprintf(`
		SELECT
			competition_id,
			match_id,
			COALESCE(match_time, 0) as match_time,
			COALESCE(home_team_id, '') as home_id,
			COALESCE(away_team_id, '') as away_id,
			COALESCE(status_id, 0) as status_id
		FROM %s
		WHERE competition_id IN (%s)
		  AND match_time >= ?
		ORDER BY competition_id, match_time
		LIMIT 15000`, table, placeholders)

	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("GetEventsBatch(%s): %w", sport, err)
	}
	defer rows.Close()

	result := make(map[string][]TSEvent, len(competitionIDs))
	for rows.Next() {
		var compID string
		var ev TSEvent
		if err := rows.Scan(&compID, &ev.ID, &ev.MatchTime, &ev.HomeID, &ev.AwayID, &ev.StatusID); err != nil {
			continue
		}
		result[compID] = append(result[compID], ev)
	}
	return result, rows.Err()
}

// GetTeamNamesBatch 批量查询多个 competition 的球队名映射，单次 SQL 查询。
// 返回 map[competition_id]map[team_id]name。
func (a *TSAdapter) GetTeamNamesBatch(competitionIDs []string, sport string) (map[string]map[string]string, error) {
	if len(competitionIDs) == 0 {
		return map[string]map[string]string{}, nil
	}
	var matchTable, teamTable string
	switch sport {
	case "football":
		matchTable = "ts_fb_match"
		teamTable = "ts_fb_team"
	case "basketball":
		matchTable = "ts_bb_match"
		teamTable = "ts_bb_team"
	default:
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}

	placeholders := ""
	args := make([]interface{}, 0, len(competitionIDs))
	for i, id := range competitionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT
			m.competition_id,
			t.team_id,
			COALESCE(t.name,'')
		FROM %s m
		JOIN %s t ON (m.home_team_id = t.team_id OR m.away_team_id = t.team_id)
		WHERE m.competition_id IN (%s)
		LIMIT 1000`, matchTable, teamTable, placeholders)

	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("GetTeamNamesBatch: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]string, len(competitionIDs))
	for rows.Next() {
		var compID, tid, name string
		if err := rows.Scan(&compID, &tid, &name); err != nil {
			continue
		}
		if tid == "" {
			continue
		}
		if _, ok := result[compID]; !ok {
			result[compID] = make(map[string]string)
		}
		result[compID][tid] = name
	}
	return result, rows.Err()
}

// GetEventsBatchInRange 是 GetEventsBatch 的时间窗版（PI-006 v1.13）。
// 当 timeMinUnix > 0 时使用其作为 match_time 下界（取代默认的 twoYearsAgo），
// timeMaxUnix > 0 时使用其作为上界。
// EF 路径通过 SR events 的时间分布派生 ±30 天窗口，把 P1 fetch 数据量从
// "2 年 LIMIT 3000" 缩到典型 "60 天 LIMIT 200"，单步 4-8x 加速。
func (a *TSAdapter) GetEventsBatchInRange(competitionIDs []string, sport string, timeMinUnix, timeMaxUnix int64) (map[string][]TSEvent, error) {
	if len(competitionIDs) == 0 {
		return map[string][]TSEvent{}, nil
	}
	var table string
	switch sport {
	case "football":
		table = "ts_fb_match"
	case "basketball":
		table = "ts_bb_match"
	default:
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}

	if timeMinUnix <= 0 {
		timeMinUnix = time.Now().AddDate(-2, 0, 0).Unix()
	}

	placeholders := ""
	args := make([]interface{}, 0, len(competitionIDs)+2)
	for i, id := range competitionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	args = append(args, timeMinUnix)

	timeMaxClause := ""
	if timeMaxUnix > 0 {
		timeMaxClause = " AND match_time <= ?"
		args = append(args, timeMaxUnix)
	}

	query := fmt.Sprintf(`
		SELECT
			competition_id,
			match_id,
			COALESCE(match_time, 0) as match_time,
			COALESCE(home_team_id, '') as home_id,
			COALESCE(away_team_id, '') as away_id,
			COALESCE(status_id, 0) as status_id
		FROM %s
		WHERE competition_id IN (%s)
		  AND match_time >= ?%s
		ORDER BY competition_id, match_time
		LIMIT 15000`, table, placeholders, timeMaxClause)

	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("GetEventsBatchInRange(%s): %w", sport, err)
	}
	defer rows.Close()

	result := make(map[string][]TSEvent, len(competitionIDs))
	for rows.Next() {
		var compID string
		var ev TSEvent
		if err := rows.Scan(&compID, &ev.ID, &ev.MatchTime, &ev.HomeID, &ev.AwayID, &ev.StatusID); err != nil {
			continue
		}
		result[compID] = append(result[compID], ev)
	}
	return result, rows.Err()
}
