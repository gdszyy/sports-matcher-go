// Package db — LSports 数据库适配器
// 连接 test-xp-lsports 库，读取 ls_sport_event / ls_tournament_en / ls_competitor_en
package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// LSAdapter 封装对 test-xp-lsports 的查询
type LSAdapter struct {
	db *sql.DB
}

// NewLSAdapter 创建 LSports 适配器
func NewLSAdapter(db *sql.DB) *LSAdapter {
	return &LSAdapter{db: db}
}

// lsSportName 将 LSports sport_id 映射为运动类型名称
func lsSportName(sportID string) string {
	switch sportID {
	case "6046":
		return "football"
	case "48242":
		return "basketball"
	case "54094":
		return "tennis"
	case "131506":
		return "ice_hockey"
	case "154914":
		return "baseball"
	default:
		return "unknown"
	}
}

// lsSportID 将运动类型名称映射为 LSports sport_id
func lsSportID(sport string) string {
	switch sport {
	case "football":
		return "6046"
	case "basketball":
		return "48242"
	case "tennis":
		return "54094"
	case "ice_hockey":
		return "131506"
	case "baseball":
		return "154914"
	default:
		return ""
	}
}

// GetTournament 查询 LSports 联赛信息
func (a *LSAdapter) GetTournament(tournamentID string) (*LSTournament, error) {
	query := `
		SELECT t.tournament_id, COALESCE(t.name,''), t.sport_id, t.category_id,
		       COALESCE(c.name,'') as category_name
		FROM ls_tournament_en t
		LEFT JOIN ls_category_en c ON t.category_id = c.category_id
		WHERE t.tournament_id = ?
		LIMIT 1`
	row := a.db.QueryRow(query, tournamentID)
	var t LSTournament
	if err := row.Scan(&t.ID, &t.Name, &t.SportID, &t.CategoryID, &t.CategoryName); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("GetTournament(LS): %w", err)
	}
	t.Sport = lsSportName(t.SportID)
	return &t, nil
}

// GetTournamentsBySport 查询某运动类型下的所有联赛
func (a *LSAdapter) GetTournamentsBySport(sport string) ([]LSTournament, error) {
	sportID := lsSportID(sport)
	if sportID == "" {
		return nil, fmt.Errorf("不支持的运动类型: %s", sport)
	}
	query := `
		SELECT t.tournament_id, COALESCE(t.name,''), t.sport_id, t.category_id,
		       COALESCE(c.name,'') as category_name
		FROM ls_tournament_en t
		LEFT JOIN ls_category_en c ON t.category_id = c.category_id
		WHERE t.sport_id = ?`
	rows, err := a.db.Query(query, sportID)
	if err != nil {
		return nil, fmt.Errorf("GetTournamentsBySport(LS): %w", err)
	}
	defer rows.Close()
	var result []LSTournament
	for rows.Next() {
		var t LSTournament
		if err := rows.Scan(&t.ID, &t.Name, &t.SportID, &t.CategoryID, &t.CategoryName); err != nil {
			continue
		}
		t.Sport = lsSportName(t.SportID)
		result = append(result, t)
	}
	return result, rows.Err()
}

// GetEvents 查询 LSports 联赛下的比赛（近两年）
// ls_sport_event 表：event_id, tournament_id, sport_id, category_id,
//                    home_competitor_id, away_competitor_id, scheduled, status_id
func (a *LSAdapter) GetEvents(tournamentID string) ([]LSEvent, error) {
	twoYearsAgo := time.Now().AddDate(-2, 0, 0).Format("2006-01-02T15:04:05")
	query := `
		SELECT
			e.event_id,
			e.tournament_id,
			COALESCE(e.scheduled,'') as scheduled,
			COALESCE(e.home_competitor_id,'') as home_id,
			COALESCE(h.name,'') as home_name,
			COALESCE(e.away_competitor_id,'') as away_id,
			COALESCE(a.name,'') as away_name,
			COALESCE(e.status, 0) as status_id
		FROM ls_sport_event e
		LEFT JOIN ls_competitor_en h ON e.home_competitor_id = h.competitor_id
		LEFT JOIN ls_competitor_en a ON e.away_competitor_id = a.competitor_id
		WHERE e.tournament_id = ?
		  AND e.scheduled >= ?
		ORDER BY e.scheduled
		LIMIT 3000`
	rows, err := a.db.Query(query, tournamentID, twoYearsAgo)
	if err != nil {
		return nil, fmt.Errorf("GetEvents(LS): %w", err)
	}
	defer rows.Close()
	var events []LSEvent
	seen := make(map[string]bool)
	for rows.Next() {
		var ev LSEvent
		if err := rows.Scan(
			&ev.ID, &ev.TournamentID, &ev.StartTime,
			&ev.HomeID, &ev.HomeName,
			&ev.AwayID, &ev.AwayName,
			&ev.StatusID,
		); err != nil {
			continue
		}
		// 去重（ls_sport_event 可能有重复 event_id）
		if seen[ev.ID] {
			continue
		}
		seen[ev.ID] = true
		ev.StartUnix = parseLSScheduled(ev.StartTime)
		events = append(events, ev)
	}
	return events, rows.Err()
}

// GetTeamNames 查询联赛下的球队名称映射 (competitor_id → name)
func (a *LSAdapter) GetTeamNames(tournamentID string) (map[string]string, error) {
	query := `
		SELECT DISTINCT c.competitor_id, COALESCE(c.name,'')
		FROM ls_sport_event e
		JOIN ls_competitor_en c ON (e.home_competitor_id = c.competitor_id OR e.away_competitor_id = c.competitor_id)
		WHERE e.tournament_id = ?
		LIMIT 200`
	rows, err := a.db.Query(query, tournamentID)
	if err != nil {
		return nil, fmt.Errorf("GetTeamNames(LS): %w", err)
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

// parseLSScheduled 解析 LSports scheduled 字段（支持多种格式）
// 格式示例：
//   - "2026-05-24T16:00:00"
//   - "2026-05-24T16:00:00Z"
//   - "2026-05-24T16:00:00+00:00"
func parseLSScheduled(s string) int64 {
	if s == "" {
		return 0
	}
	// 统一处理：去掉末尾 Z，替换 +00:00 为空
	s = strings.TrimSuffix(s, "Z")
	s = strings.TrimSuffix(s, "+00:00")

	formats := []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}
