// Package api — HTTP API 服务层
package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gdszyy/sports-matcher/internal/config"
	"github.com/gdszyy/sports-matcher/internal/db"
	"github.com/gdszyy/sports-matcher/internal/matcher"
)

// Server HTTP 服务
type Server struct {
	cfg      *config.Config
	tunnel   *db.Tunnel
	engine   *matcher.Engine   // SR ↔ TS 引擎
	lsEngine *matcher.LSEngine // LS ↔ TS 引擎
	router   *gin.Engine
}

// NewServer 创建 HTTP 服务
func NewServer(cfg *config.Config) (*Server, error) {
	tunnel, err := db.NewTunnel(cfg)
	if err != nil {
		return nil, fmt.Errorf("建立数据库隧道失败: %w", err)
	}

	srAdapter := db.NewSRAdapter(tunnel.SRDb)
	tsAdapter := db.NewTSAdapter(tunnel.TSDb)
	lsAdapter := db.NewLSAdapter(tunnel.LSDb)

	eng := matcher.NewEngine(srAdapter, tsAdapter, cfg.RunPlayers)
	lsEng := matcher.NewLSEngine(lsAdapter, tsAdapter)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	s := &Server{
		cfg:      cfg,
		tunnel:   tunnel,
		engine:   eng,
		lsEngine: lsEng,
		router:   router,
	}
	s.registerRoutes()
	return s, nil
}

// registerRoutes 注册所有路由
func (s *Server) registerRoutes() {
	s.router.GET("/health", s.handleHealth)

	// SR ↔ TS 路由（原有）
	s.router.GET("/api/v1/match/league", s.handleMatchLeague)
	s.router.POST("/api/v1/match/batch", s.handleMatchBatch)

	// LS ↔ TS 路由（新增）
	s.router.GET("/api/v1/ls/match/league", s.handleLSMatchLeague)
	s.router.POST("/api/v1/ls/match/batch", s.handleLSMatchBatch)
}

// Run 启动 HTTP 服务
func (s *Server) Run() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.ServerHost, s.cfg.ServerPort)
	return s.router.Run(addr)
}

// Close 关闭服务
func (s *Server) Close() {
	s.tunnel.Close()
}

// ─── SR ↔ TS 处理器 ────────────────────────────────────────────────────────

// handleHealth 健康检查
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// MatchLeagueRequest 单联赛匹配请求
type MatchLeagueRequest struct {
	TournamentID    string `form:"tournament_id" binding:"required"` // SR tournament_id
	Sport           string `form:"sport" binding:"required"`          // football / basketball
	Tier            string `form:"tier"`                              // hot / regular / cold
	TSCompetitionID string `form:"ts_competition_id"`                 // 可选：预设 TS ID
	RunPlayers      *bool  `form:"run_players"`                       // 可选：是否匹配球员
}

// handleMatchLeague 单联赛匹配（SR ↔ TS）
//
// GET /api/v1/match/league?tournament_id=sr:tournament:17&sport=football&tier=hot
func (s *Server) handleMatchLeague(c *gin.Context) {
	var req MatchLeagueRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Tier == "" {
		req.Tier = "unknown"
	}

	// 临时覆盖 RunPlayers
	origRunPlayers := s.engine.RunPlayers
	if req.RunPlayers != nil {
		s.engine.RunPlayers = *req.RunPlayers
	}
	defer func() { s.engine.RunPlayers = origRunPlayers }()

	result, err := s.engine.RunLeague(req.TournamentID, req.Sport, req.Tier, req.TSCompetitionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// BatchMatchRequest 批量匹配请求
type BatchMatchRequest struct {
	Leagues []BatchLeagueItem `json:"leagues" binding:"required"`
}

// BatchLeagueItem 批量匹配中的单个联赛
type BatchLeagueItem struct {
	TournamentID    string `json:"tournament_id"`
	Sport           string `json:"sport"`
	Tier            string `json:"tier"`
	TSCompetitionID string `json:"ts_competition_id"`
}

// handleMatchBatch 批量联赛匹配（SR ↔ TS）
//
// POST /api/v1/match/batch
func (s *Server) handleMatchBatch(c *gin.Context) {
	var req BatchMatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	type leagueResult struct {
		TournamentID string             `json:"tournament_id"`
		Stats        matcher.MatchStats `json:"stats"`
		Error        string             `json:"error,omitempty"`
	}

	results := make([]leagueResult, 0, len(req.Leagues))
	for _, item := range req.Leagues {
		tier := item.Tier
		if tier == "" {
			tier = "unknown"
		}
		res, err := s.engine.RunLeague(item.TournamentID, item.Sport, tier, item.TSCompetitionID)
		lr := leagueResult{TournamentID: item.TournamentID}
		if err != nil {
			lr.Error = err.Error()
		} else {
			lr.Stats = res.Stats
		}
		results = append(results, lr)
	}

	c.JSON(http.StatusOK, gin.H{
		"total":   len(results),
		"results": results,
	})
}

// ─── LS ↔ TS 处理器 ────────────────────────────────────────────────────────

// LSMatchLeagueRequest LS 单联赛匹配请求
type LSMatchLeagueRequest struct {
	TournamentID    string `form:"tournament_id" binding:"required"` // LS tournament_id（整数字符串）
	Sport           string `form:"sport" binding:"required"`          // football / basketball
	Tier            string `form:"tier"`                              // hot / regular / cold
	TSCompetitionID string `form:"ts_competition_id"`                 // 可选：预设 TS competition_id
}

// handleLSMatchLeague LS ↔ TS 单联赛匹配
//
// GET /api/v1/ls/match/league?tournament_id=8363&sport=football&tier=hot
// GET /api/v1/ls/match/league?tournament_id=8363&sport=football&ts_competition_id=vl7oqdehlyr510j
func (s *Server) handleLSMatchLeague(c *gin.Context) {
	var req LSMatchLeagueRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Tier == "" {
		req.Tier = "unknown"
	}

	result, err := s.lsEngine.RunLeague(req.TournamentID, req.Sport, req.Tier, req.TSCompetitionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// LSBatchMatchRequest LS 批量匹配请求
type LSBatchMatchRequest struct {
	Leagues []LSBatchLeagueItem `json:"leagues" binding:"required"`
}

// LSBatchLeagueItem LS 批量匹配中的单个联赛
type LSBatchLeagueItem struct {
	TournamentID    string `json:"tournament_id"`
	Sport           string `json:"sport"`
	Tier            string `json:"tier"`
	TSCompetitionID string `json:"ts_competition_id"`
}

// handleLSMatchBatch LS ↔ TS 批量联赛匹配
//
// POST /api/v1/ls/match/batch
// Body: {"leagues": [{"tournament_id": "8363", "sport": "football", "tier": "hot"}]}
func (s *Server) handleLSMatchBatch(c *gin.Context) {
	var req LSBatchMatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	type leagueResult struct {
		TournamentID string               `json:"tournament_id"`
		Stats        matcher.LSMatchStats `json:"stats"`
		Error        string               `json:"error,omitempty"`
	}

	results := make([]leagueResult, 0, len(req.Leagues))
	for _, item := range req.Leagues {
		tier := item.Tier
		if tier == "" {
			tier = "unknown"
		}
		res, err := s.lsEngine.RunLeague(item.TournamentID, item.Sport, tier, item.TSCompetitionID)
		lr := leagueResult{TournamentID: item.TournamentID}
		if err != nil {
			lr.Error = err.Error()
		} else {
			lr.Stats = res.Stats
		}
		results = append(results, lr)
	}

	c.JSON(http.StatusOK, gin.H{
		"total":   len(results),
		"results": results,
	})
}
