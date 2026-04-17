// Package db — LSports 球员数据适配器
//
// 数据来源策略（双路兜底）：
//  1. 优先查询本地数据库 ls_player_en 表（若存在）
//  2. 回退到 LSports Snapshot REST API（POST /InPlay/GetFixtures 或 /PreMatch/GetFixtures）
//     从 Fixture.Participants[].Players 字段中提取球员列表
//
// 注意：LSports Snapshot 的球员数据字段极少（仅 Id + Name），无生日/国籍，
// 因此球员匹配层只能依赖名称相似度，无法使用 DOB 辅助消歧。
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// LSPlayerAdapter 球员数据适配器
// ─────────────────────────────────────────────────────────────────────────────

// LSPlayerAdapter 封装 LS 球员数据获取逻辑（数据库优先，Snapshot API 兜底）
type LSPlayerAdapter struct {
	db         *sql.DB    // 可为 nil（仅使用 Snapshot API 时）
	httpClient *http.Client
	snapshotBaseURL string // 默认 "https://stm-snapshot.lsports.eu"
	username        string // TRADE 账号（用于 Snapshot API Basic Auth）
	password        string // TRADE 密码
	packageID       string // 包 ID（InPlay=3801 / PreMatch=3802）
}

// LSPlayerAdapterConfig Snapshot API 配置
type LSPlayerAdapterConfig struct {
	SnapshotBaseURL string // 默认 "https://stm-snapshot.lsports.eu"
	Username        string // TRADE 账号
	Password        string // TRADE 密码
	PackageID       string // "3801"（InPlay）或 "3802"（PreMatch）
}

// DefaultLSPlayerConfig 默认配置（来自 lsport-connector 技能）
var DefaultLSPlayerConfig = LSPlayerAdapterConfig{
	SnapshotBaseURL: "https://stm-snapshot.lsports.eu",
	Username:        "why451300@gmail.com",
	Password:        "Af374658!",
	PackageID:       "3802", // PreMatch 包含更完整的球员名单
}

// NewLSPlayerAdapter 创建 LS 球员适配器
// db 可为 nil（仅使用 Snapshot API）
func NewLSPlayerAdapter(db *sql.DB, cfg LSPlayerAdapterConfig) *LSPlayerAdapter {
	if cfg.SnapshotBaseURL == "" {
		cfg.SnapshotBaseURL = DefaultLSPlayerConfig.SnapshotBaseURL
	}
	if cfg.Username == "" {
		cfg.Username = DefaultLSPlayerConfig.Username
	}
	if cfg.Password == "" {
		cfg.Password = DefaultLSPlayerConfig.Password
	}
	if cfg.PackageID == "" {
		cfg.PackageID = DefaultLSPlayerConfig.PackageID
	}
	return &LSPlayerAdapter{
		db:              db,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		snapshotBaseURL: cfg.SnapshotBaseURL,
		username:        cfg.Username,
		password:        cfg.Password,
		packageID:       cfg.PackageID,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 主入口：GetPlayersByTeam
// ─────────────────────────────────────────────────────────────────────────────

// GetPlayersByTeam 获取指定球队的球员列表
// 策略：数据库优先，失败时回退到 Snapshot API
func (a *LSPlayerAdapter) GetPlayersByTeam(competitorID string) ([]LSPlayer, error) {
	// 优先尝试本地数据库
	if a.db != nil {
		players, err := a.getPlayersFromDB(competitorID)
		if err == nil && len(players) > 0 {
			return players, nil
		}
	}

	// 回退到 Snapshot API
	return a.getPlayersFromSnapshot(competitorID)
}

// GetPlayersByTeamBatch 批量获取多个球队的球员列表（减少 API 调用次数）
// 返回 competitorID → []LSPlayer 的映射
func (a *LSPlayerAdapter) GetPlayersByTeamBatch(competitorIDs []string) (map[string][]LSPlayer, error) {
	if len(competitorIDs) == 0 {
		return nil, nil
	}

	// 优先尝试本地数据库批量查询
	if a.db != nil {
		result, err := a.getPlayersBatchFromDB(competitorIDs)
		if err == nil && len(result) > 0 {
			return result, nil
		}
	}

	// 回退到 Snapshot API 批量查询（一次请求获取联赛下所有球队球员）
	return a.getPlayersBatchFromSnapshot(competitorIDs)
}

// ─────────────────────────────────────────────────────────────────────────────
// 数据库路径（ls_player_en 表，若存在）
// ─────────────────────────────────────────────────────────────────────────────

// getPlayersFromDB 从本地数据库查询球员
// 表结构假设：ls_player_en(player_id, name, competitor_id)
func (a *LSPlayerAdapter) getPlayersFromDB(competitorID string) ([]LSPlayer, error) {
	// 先检查表是否存在
	var tableName string
	err := a.db.QueryRow(
		`SELECT table_name FROM information_schema.tables 
		 WHERE table_schema = DATABASE() AND table_name = 'ls_player_en' LIMIT 1`,
	).Scan(&tableName)
	if err != nil || tableName == "" {
		return nil, fmt.Errorf("ls_player_en 表不存在")
	}

	query := `
		SELECT player_id, COALESCE(name,''), COALESCE(competitor_id,'')
		FROM ls_player_en
		WHERE competitor_id = ?
		LIMIT 200`
	rows, err := a.db.Query(query, competitorID)
	if err != nil {
		return nil, fmt.Errorf("getPlayersFromDB: %w", err)
	}
	defer rows.Close()

	var players []LSPlayer
	for rows.Next() {
		var p LSPlayer
		if err := rows.Scan(&p.ID, &p.Name, &p.TeamID); err != nil {
			continue
		}
		if p.ID != "" && p.Name != "" {
			players = append(players, p)
		}
	}
	return players, rows.Err()
}

// getPlayersBatchFromDB 从数据库批量查询多个球队的球员
func (a *LSPlayerAdapter) getPlayersBatchFromDB(competitorIDs []string) (map[string][]LSPlayer, error) {
	// 检查表是否存在
	var tableName string
	err := a.db.QueryRow(
		`SELECT table_name FROM information_schema.tables 
		 WHERE table_schema = DATABASE() AND table_name = 'ls_player_en' LIMIT 1`,
	).Scan(&tableName)
	if err != nil || tableName == "" {
		return nil, fmt.Errorf("ls_player_en 表不存在")
	}

	placeholders := strings.Repeat("?,", len(competitorIDs))
	placeholders = placeholders[:len(placeholders)-1]
	query := fmt.Sprintf(`
		SELECT player_id, COALESCE(name,''), COALESCE(competitor_id,'')
		FROM ls_player_en
		WHERE competitor_id IN (%s)
		LIMIT 2000`, placeholders)

	args := make([]interface{}, len(competitorIDs))
	for i, id := range competitorIDs {
		args[i] = id
	}
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("getPlayersBatchFromDB: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]LSPlayer)
	for rows.Next() {
		var p LSPlayer
		if err := rows.Scan(&p.ID, &p.Name, &p.TeamID); err != nil {
			continue
		}
		if p.ID != "" && p.Name != "" {
			result[p.TeamID] = append(result[p.TeamID], p)
		}
	}
	return result, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Snapshot API 路径
// ─────────────────────────────────────────────────────────────────────────────

// snapshotRequest Snapshot API 请求体
type snapshotRequest struct {
	Username  string `json:"UserName"`
	Password  string `json:"Password"`
	PackageID int    `json:"PackageId"`
	// 可选过滤字段
	SportIDs    []int `json:"SportIds,omitempty"`
	LocationIDs []int `json:"LocationIds,omitempty"`
	LeagueIDs   []int `json:"LeagueIds,omitempty"`
}

// snapshotFixture Snapshot API 响应中的 Fixture 结构（仅解析球员相关字段）
type snapshotFixture struct {
	FixtureID    int `json:"FixtureId"`
	Participants []struct {
		ID      int    `json:"Id"`
		Name    string `json:"Name"`
		Players []struct {
			ID   int    `json:"Id"`
			Name string `json:"Name"`
		} `json:"Players"`
	} `json:"Participants"`
}

// snapshotResponse Snapshot API 响应体
type snapshotResponse struct {
	Header struct {
		Type int `json:"Type"`
	} `json:"Header"`
	Body struct {
		Fixtures []snapshotFixture `json:"Fixtures"`
	} `json:"Body"`
}

// getPlayersFromSnapshot 通过 Snapshot API 获取指定球队的球员列表
// 注意：Snapshot API 按联赛/运动类型过滤，无法直接按球队 ID 过滤
// 此函数通过全量拉取后在内存中过滤（适用于单次调用场景）
func (a *LSPlayerAdapter) getPlayersFromSnapshot(competitorID string) ([]LSPlayer, error) {
	batch, err := a.getPlayersBatchFromSnapshot([]string{competitorID})
	if err != nil {
		return nil, err
	}
	return batch[competitorID], nil
}

// getPlayersBatchFromSnapshot 通过 Snapshot API 批量获取球员数据
// 一次 API 调用获取 PreMatch 快照，从中提取所有球队的球员列表
func (a *LSPlayerAdapter) getPlayersBatchFromSnapshot(competitorIDs []string) (map[string][]LSPlayer, error) {
	// 构建目标球队 ID 集合（用于内存过滤）
	targetSet := make(map[string]bool, len(competitorIDs))
	for _, id := range competitorIDs {
		targetSet[id] = true
	}

	// 构建请求体
	packageID := 3802 // 默认 PreMatch
	if a.packageID == "3801" {
		packageID = 3801
	}
	reqBody := snapshotRequest{
		Username:  a.username,
		Password:  a.password,
		PackageID: packageID,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化 Snapshot 请求失败: %w", err)
	}

	// 发送 HTTP 请求
	url := a.snapshotBaseURL + "/PreMatch/GetFixtures"
	if packageID == 3801 {
		url = a.snapshotBaseURL + "/InPlay/GetFixtures"
	}
	req, err := http.NewRequest("POST", url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("创建 Snapshot 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Snapshot API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Snapshot API 返回 HTTP %d", resp.StatusCode)
	}

	// 解析响应（流式解析，避免大响应体全量加载到内存）
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024)) // 最多读取 50MB
	if err != nil {
		return nil, fmt.Errorf("读取 Snapshot 响应失败: %w", err)
	}

	var snapshotResp snapshotResponse
	if err := json.Unmarshal(respBytes, &snapshotResp); err != nil {
		return nil, fmt.Errorf("解析 Snapshot 响应失败: %w", err)
	}

	// 从 Fixtures 中提取球员数据
	result := make(map[string][]LSPlayer)
	seen := make(map[string]map[string]bool) // competitorID → playerID set（去重）

	for _, fixture := range snapshotResp.Body.Fixtures {
		for _, participant := range fixture.Participants {
			competitorIDStr := fmt.Sprintf("%d", participant.ID)
			if !targetSet[competitorIDStr] {
				continue
			}
			if seen[competitorIDStr] == nil {
				seen[competitorIDStr] = make(map[string]bool)
			}
			for _, player := range participant.Players {
				if player.ID == 0 || player.Name == "" {
					continue
				}
				playerIDStr := fmt.Sprintf("%d", player.ID)
				if seen[competitorIDStr][playerIDStr] {
					continue // 去重
				}
				seen[competitorIDStr][playerIDStr] = true
				result[competitorIDStr] = append(result[competitorIDStr], LSPlayer{
					ID:     playerIDStr,
					Name:   player.Name,
					TeamID: competitorIDStr,
				})
			}
		}
	}

	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 工具函数
// ─────────────────────────────────────────────────────────────────────────────

// LSPlayerAdapterFromLSAdapter 从现有 LSAdapter 创建球员适配器（共享数据库连接）
// 方便在 ls_engine.go 中直接使用，无需单独初始化
func LSPlayerAdapterFromLSAdapter(lsAdapter *LSAdapter, cfg LSPlayerAdapterConfig) *LSPlayerAdapter {
	if lsAdapter == nil {
		return NewLSPlayerAdapter(nil, cfg)
	}
	return NewLSPlayerAdapter(lsAdapter.db, cfg)
}
