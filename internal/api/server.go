// Package api — HTTP API 服务（Gin）
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
	cfg          *config.Config
	tunnel       *db.Tunnel
	engine       *matcher.Engine        // SR ↔ TS 旧版引擎（向后兼容）
	uniEngine    *matcher.UniversalEngine // SR ↔ TS 最新通用引擎（UniversalEngine）
	srAdapter    *db.SRAdapter          // SR 数据库适配器（供 UniversalEngine 使用）
	lsEngine     *matcher.LSEngine      // LS ↔ TS 引擎
	router       *gin.Engine
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

	// 创建 LS 球员适配器（共享 LS 数据库连接）
	// 用于支持 med 置信度强制触发球员匹配反向验证
	lsPlayerAdapter := db.LSPlayerAdapterFromLSAdapter(lsAdapter, db.DefaultLSPlayerConfig)

	// 旧版 SR 引擎（向后兼容）
	eng := matcher.NewEngine(srAdapter, tsAdapter, cfg.RunPlayers)

	// 最新通用引擎（SR 2026 热门+常规匹配使用）
	// 包含：高斯时间衰减窗口 + FS 模型 + DTW + 六维强约束 + 持久化别名知识图谱
	uniEng := matcher.NewUniversalEngine(tsAdapter, cfg.RunPlayers)

	lsEng := matcher.NewLSEngineWithPlayers(lsAdapter, tsAdapter, lsPlayerAdapter)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	s := &Server{
		cfg:       cfg,
		tunnel:    tunnel,
		engine:    eng,
		uniEngine: uniEng,
		srAdapter: srAdapter,
		lsEngine:  lsEng,
		router:    router,
	}
	s.registerRoutes()
	return s, nil
}

// registerRoutes 注册所有路由
func (s *Server) registerRoutes() {
	s.router.GET("/health", s.handleHealth)

	// SR ↔ TS 路由（旧版 Engine，向后兼容）
	s.router.GET("/api/v1/match/league", s.handleMatchLeague)
	s.router.POST("/api/v1/match/batch", s.handleMatchBatch)

	// SR ↔ TS 路由（最新 UniversalEngine，SR 2026 热门+常规）
	// 使用高斯时间衰减 + FS 模型 + DTW + 六维强约束 + 持久化别名知识图谱
	s.router.GET("/api/v2/match/league", s.handleMatchLeagueV2)
	s.router.POST("/api/v2/match/batch", s.handleMatchBatchV2)

	// LS ↔ TS 路由
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

// ─── SR ↔ TS 处理器（旧版 Engine，v1）────────────────────────────────────

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

// handleMatchLeague 单联赛匹配（SR ↔ TS，旧版 Engine）
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

// handleMatchBatch 批量联赛匹配（SR ↔ TS，旧版 Engine）
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

// ─── SR ↔ TS 处理器（最新 UniversalEngine，v2）────────────────────────────

// handleMatchLeagueV2 单联赛匹配（SR ↔ TS，最新 UniversalEngine）
//
// GET /api/v2/match/league?tournament_id=sr:tournament:17&sport=football&tier=hot
//
// 使用最新算法：
//   - 高斯时间衰减连续模糊时间窗口（替代硬性分级）
//   - Fellegi-Sunter 无监督 EM 参数估计
//   - EventDTW 动态时间规整兜底
//   - 六维强约束一票否决（性别/年龄/赛制/层级/区域/国家）
//   - 持久化球队别名知识图谱（AliasStore）
//   - 已知映射反向确认率自动验证（KnownLeagueMapValidator）
func (s *Server) handleMatchLeagueV2(c *gin.Context) {
	var req MatchLeagueRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Tier == "" {
		req.Tier = "unknown"
	}

	srcAdapter := matcher.NewSRSourceAdapter(s.srAdapter, s.uniEngine.RunPlayers)
	if req.RunPlayers != nil {
		srcAdapter = matcher.NewSRSourceAdapter(s.srAdapter, *req.RunPlayers)
	}

	result, err := s.uniEngine.RunLeague(srcAdapter, req.TournamentID, req.Sport, req.Tier, req.TSCompetitionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// handleMatchBatchV2 批量联赛匹配（SR ↔ TS，最新 UniversalEngine）
//
// POST /api/v2/match/batch
//
// 默认匹配 SR 2026 热门+常规联赛（共 22 个），也可通过 Body 自定义联赛列表。
// 若 Body 为空或 leagues 为空，则使用内置 SR 2026 联赛配置。
func (s *Server) handleMatchBatchV2(c *gin.Context) {
	type v2BatchRequest struct {
		Leagues []BatchLeagueItem `json:"leagues"`
	}

	var req v2BatchRequest
	// 允许 Body 为空（使用内置 SR 2026 配置）
	_ = c.ShouldBindJSON(&req)

	// 内置 SR 2026 热门+常规联赛配置（与 cmd/server/main.go 中的 sr2026Leagues 保持一致）
	defaultSR2026 := []BatchLeagueItem{
		// 足球热门
		{"sr:tournament:17", "football", "hot", "jednm9whz0ryox8"},
		{"sr:tournament:8", "football", "hot", "vl7oqdehlyr510j"},
		{"sr:tournament:35", "football", "hot", "gy0or5jhg6qwzv3"},
		{"sr:tournament:23", "football", "hot", "4zp5rzghp5q82w1"},
		{"sr:tournament:34", "football", "hot", "yl5ergphnzr8k0o"},
		{"sr:tournament:7", "football", "hot", "z8yomo4h7wq0j6l"},
		{"sr:tournament:679", "football", "hot", "56ypq3nh0xmd7oj"},
		// 足球常规
		{"sr:tournament:18", "football", "regular", "l965mkyh32r1ge4"},
		{"sr:tournament:242", "football", "regular", "kn54qllhg2qvy9d"},
		{"sr:tournament:203", "football", "regular", "8y39mp1hwxmojxg"},
		{"sr:tournament:325", "football", "regular", "4zp5rzgh9zq82w1"},
		{"sr:tournament:37", "football", "regular", "vl7oqdeheyr510j"},
		{"sr:tournament:52", "football", "regular", "8y39mp1h6jmojxg"},
		{"sr:tournament:238", "football", "regular", "gx7lm7phpnm2wdk"},
		{"sr:tournament:11", "football", "regular", "9vjxm8gh22r6odg"},
		{"sr:tournament:955", "football", "regular", "z318q66hl1qo9jd"},
		// 篮球热门
		{"sr:tournament:132", "basketball", "hot", "49vjxm8xt4q6odg"},
		{"sr:tournament:138", "basketball", "hot", "jednm9ktd5ryox8"},
		// 篮球常规
		{"sr:tournament:176", "basketball", "regular", "v2y8m4ptx1ml074"},
		{"sr:tournament:131", "basketball", "regular", "v2y8m4ptdeml074"},
		{"sr:tournament:53", "basketball", "regular", "x4zp5rzkt1r82w1"},
		{"sr:tournament:54", "basketball", "regular", "0l965mk8tom1ge4"},
		{"sr:tournament:390", "basketball", "regular", "kjw2r02t6xqz84o"},
	}

	leagues := req.Leagues
	if len(leagues) == 0 {
		leagues = defaultSR2026
	}

	type leagueResult struct {
		TournamentID string                      `json:"tournament_id"`
		Stats        matcher.UniversalMatchStats `json:"stats"`
		Error        string                      `json:"error,omitempty"`
	}

	results := make([]leagueResult, 0, len(leagues))
	for _, item := range leagues {
		tier := item.Tier
		if tier == "" {
			tier = "unknown"
		}
		srcAdapter := matcher.NewSRSourceAdapter(s.srAdapter, s.uniEngine.RunPlayers)
		res, err := s.uniEngine.RunLeague(srcAdapter, item.TournamentID, item.Sport, tier, item.TSCompetitionID)
		lr := leagueResult{TournamentID: item.TournamentID}
		if err != nil {
			lr.Error = err.Error()
		} else {
			lr.Stats = res.Stats
		}
		results = append(results, lr)
	}

	c.JSON(http.StatusOK, gin.H{
		"engine":  "UniversalEngine/v2",
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
