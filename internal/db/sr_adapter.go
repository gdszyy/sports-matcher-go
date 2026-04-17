// Package db — SR 数据库查询适配器
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SRAdapter 封装对 xp-bet-test 的查询
type SRAdapter struct {
	db *sql.DB
}

// NewSRAdapter 创建 SR 适配器
func NewSRAdapter(db *sql.DB) *SRAdapter {
	return &SRAdapter{db: db}
}

// GetTournament 查询联赛信息
// 实际字段: tournament_id, name, category_id, sport_id
// 尝试查询 category 的 country_code 字段（若数据库不支持则回落为空字符串）
func (a *SRAdapter) GetTournament(tournamentID string) (*SRTournament, error) {
	// 先尝试包含 country_code 的查询
	query := `
		SELECT t.tournament_id, t.name, t.sport_id, t.category_id,
		       COALESCE(c.name, '') as category_name,
		       COALESCE(c.country_code, '') as category_country_code
		FROM sr_tournament_en t
		LEFT JOIN sr_category_en c ON t.category_id = c.category_id
		WHERE t.tournament_id = ?
		LIMIT 1`
	row := a.db.QueryRow(query, tournamentID)
	var t SRTournament
	if err := row.Scan(&t.ID, &t.Name, &t.SportID, &t.CategoryID, &t.CategoryName, &t.CategoryCountryCode); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		// 若 country_code 字段不存在，回落到不含 country_code 的查询
		fallbackQuery := `
			SELECT t.tournament_id, t.name, t.sport_id, t.category_id,
			       COALESCE(c.name, '') as category_name
			FROM sr_tournament_en t
			LEFT JOIN sr_category_en c ON t.category_id = c.category_id
			WHERE t.tournament_id = ?
			LIMIT 1`
		row2 := a.db.QueryRow(fallbackQuery, tournamentID)
		var t2 SRTournament
		if err2 := row2.Scan(&t2.ID, &t2.Name, &t2.SportID, &t2.CategoryID, &t2.CategoryName); err2 != nil {
			if err2 == sql.ErrNoRows {
				return nil, nil
			}
			return nil, fmt.Errorf("GetTournament: %w", err2)
		}
		t2.Sport = sportFromID(t2.SportID)
		return &t2, nil
	}
	// 从 sport_id 推导运动类型
	t.Sport = sportFromID(t.SportID)
	return &t, nil
}

// GetEvents 查询联赛下的所有比赛
// 实际字段: sport_event_id, scheduled, home_competitor_id, away_competitor_id, status_code
func (a *SRAdapter) GetEvents(tournamentID string) ([]SREvent, error) {
	query := `
		SELECT 
			e.sport_event_id,
			e.tournament_id,
			COALESCE(e.scheduled, '') as scheduled,
			COALESCE(e.home_competitor_id, '') as home_id,
			COALESCE(h.name, '') as home_name,
			COALESCE(e.away_competitor_id, '') as away_id,
			COALESCE(aw.name, '') as away_name,
			COALESCE(e.status_code, 0) as status_code
		FROM sr_sport_event e
		LEFT JOIN sr_competitor_en h ON e.home_competitor_id = h.competitor_id
		LEFT JOIN sr_competitor_en aw ON e.away_competitor_id = aw.competitor_id
		WHERE e.tournament_id = ?
		ORDER BY e.scheduled`
	rows, err := a.db.Query(query, tournamentID)
	if err != nil {
		return nil, fmt.Errorf("GetEvents: %w", err)
	}
	defer rows.Close()

	var events []SREvent
	for rows.Next() {
		var ev SREvent
		if err := rows.Scan(
			&ev.ID, &ev.TournamentID, &ev.StartTime,
			&ev.HomeID, &ev.HomeName,
			&ev.AwayID, &ev.AwayName,
			&ev.StatusCode,
		); err != nil {
			continue
		}
		ev.StartUnix = parseISO8601Unix(ev.StartTime)
		events = append(events, ev)
	}
	return events, rows.Err()
}

// GetTeamNames 查询联赛下的球队名称映射 (competitor_id → name)
func (a *SRAdapter) GetTeamNames(tournamentID string) (map[string]string, error) {
	query := `
		SELECT DISTINCT 
			COALESCE(e.home_competitor_id, '') as team_id,
			COALESCE(c.name, '') as team_name
		FROM sr_sport_event e
		JOIN sr_competitor_en c ON e.home_competitor_id = c.competitor_id
		WHERE e.tournament_id = ?
		UNION
		SELECT DISTINCT 
			COALESCE(e.away_competitor_id, '') as team_id,
			COALESCE(c.name, '') as team_name
		FROM sr_sport_event e
		JOIN sr_competitor_en c ON e.away_competitor_id = c.competitor_id
		WHERE e.tournament_id = ?`
	rows, err := a.db.Query(query, tournamentID, tournamentID)
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

// GetPlayersByTeam 查询球队的球员列表
// player_ids 存储在 sr_competitor_en.player_ids（JSON 数组，存储 player_id 字符串）
func (a *SRAdapter) GetPlayersByTeam(competitorID string) ([]SRPlayer, error) {
	// 先从 sr_competitor_en.player_ids 获取球员 ID 列表
	var playerIDsJSON sql.NullString
	err := a.db.QueryRow(
		`SELECT player_ids FROM sr_competitor_en WHERE competitor_id = ? LIMIT 1`, competitorID,
	).Scan(&playerIDsJSON)
	if err != nil || !playerIDsJSON.Valid || playerIDsJSON.String == "" || playerIDsJSON.String == "null" {
		return nil, nil
	}

	var playerIDs []string
	if err := json.Unmarshal([]byte(playerIDsJSON.String), &playerIDs); err != nil {
		return nil, nil
	}
	if len(playerIDs) == 0 {
		return nil, nil
	}

	// 批量查询球员信息（player_id 字段）
	placeholders := strings.Repeat("?,", len(playerIDs))
	placeholders = placeholders[:len(placeholders)-1]
	query := fmt.Sprintf(`
		SELECT player_id, COALESCE(name,''), COALESCE(full_name,''), 
		       COALESCE(date_of_birth,''), COALESCE(nationality,'')
		FROM sr_player_en
		WHERE player_id IN (%s)`, placeholders)

	args := make([]interface{}, len(playerIDs))
	for i, id := range playerIDs {
		args[i] = id
	}
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("GetPlayersByTeam: %w", err)
	}
	defer rows.Close()

	var players []SRPlayer
	for rows.Next() {
		var p SRPlayer
		var fullName string
		p.TeamID = competitorID
		if err := rows.Scan(&p.ID, &p.Name, &fullName, &p.DateOfBirth, &p.Nationality); err != nil {
			continue
		}
		// 优先使用 full_name（更完整）
		if fullName != "" && fullName != p.Name {
			p.FullName = fullName
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

// sportFromID 从 sport_id 推导运动类型名称
func sportFromID(sportID string) string {
	switch sportID {
	case "sr:sport:1":
		return "football"
	case "sr:sport:2":
		return "basketball"
	case "sr:sport:5":
		return "tennis"
	case "sr:sport:4":
		return "ice_hockey"
	case "sr:sport:3":
		return "baseball"
	default:
		return "unknown"
	}
}

// parseISO8601Unix 将 "2026-03-22T14:15:00+00:00" 解析为 Unix 时间戳
func parseISO8601Unix(s string) int64 {
	formats := []string{
		"2006-01-02T15:04:05+00:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}
