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
