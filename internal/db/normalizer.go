// Package db — 数据标准化层 (Normalizer)
//
// 职责：
//   - 将各数据源 Adapter 返回的原始实体转换为统一的 Canonical 实体
//   - 集中管理所有数据清洗逻辑（时间格式、运动类型映射、字段归一化等）
//   - 对算法层屏蔽数据源差异，实现真正的泛化
//
// 架构位置：
//   Adapter (原始数据) → Normalizer (清洗/标准化) → Canonical 实体 → Algorithm Layer
//
// 扩展方式：
//   接入新数据源时，只需实现 DataNormalizer 接口并注册到 DataRouter，
//   核心算法层（UniversalEngine）零修改。
package db

import "fmt"

// ─────────────────────────────────────────────────────────────────────────────
// DataNormalizer 接口
// ─────────────────────────────────────────────────────────────────────────────

// DataNormalizer 数据标准化接口，负责将源侧原始数据转换为 Canonical 实体
type DataNormalizer interface {
	// SourceSide 返回数据源标识（"sr" / "ls"）
	SourceSide() string

	// NormalizeTournament 将源侧联赛转换为规范化实体
	NormalizeTournament(raw interface{}) (*CanonicalTournament, error)

	// NormalizeEvents 将源侧比赛列表转换为规范化实体列表
	NormalizeEvents(raw interface{}) ([]CanonicalEvent, error)

	// NormalizePlayers 将源侧球员列表转换为规范化实体列表
	NormalizePlayers(raw interface{}) ([]CanonicalPlayer, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// SRNormalizer — SportRadar 数据标准化器
// ─────────────────────────────────────────────────────────────────────────────

// SRNormalizer 将 SR 原始数据标准化为 Canonical 实体
type SRNormalizer struct{}

// NewSRNormalizer 创建 SR 标准化器
func NewSRNormalizer() *SRNormalizer {
	return &SRNormalizer{}
}

func (n *SRNormalizer) SourceSide() string { return "sr" }

// NormalizeTournament 将 SRTournament 转换为 CanonicalTournament
// 清洗逻辑：sportFromID("sr:sport:1") → "football"
func (n *SRNormalizer) NormalizeTournament(raw interface{}) (*CanonicalTournament, error) {
	t, ok := raw.(*SRTournament)
	if !ok {
		return nil, fmt.Errorf("SRNormalizer: expected *SRTournament, got %T", raw)
	}
	return &CanonicalTournament{
		ID:           t.ID,
		Name:         t.Name,
		Sport:        t.Sport, // 已由 SRAdapter.GetTournament 通过 sportFromID 推导
		CategoryName: t.CategoryName,
		Source:       "sr",
	}, nil
}

// NormalizeEvents 将 []SREvent 转换为 []CanonicalEvent
// 清洗逻辑：
//   - StartUnix 已由 SRAdapter.GetEvents 通过 parseISO8601Unix 解析
//   - StatusCode 直接映射
func (n *SRNormalizer) NormalizeEvents(raw interface{}) ([]CanonicalEvent, error) {
	events, ok := raw.([]SREvent)
	if !ok {
		return nil, fmt.Errorf("SRNormalizer: expected []SREvent, got %T", raw)
	}
	canonical := make([]CanonicalEvent, 0, len(events))
	for _, ev := range events {
		canonical = append(canonical, CanonicalEvent{
			ID:           ev.ID,
			TournamentID: ev.TournamentID,
			StartUnix:    ev.StartUnix,
			HomeID:       ev.HomeID,
			HomeName:     ev.HomeName,
			AwayID:       ev.AwayID,
			AwayName:     ev.AwayName,
			StatusCode:   ev.StatusCode,
			Source:       "sr",
		})
	}
	return canonical, nil
}

// NormalizePlayers 将 []SRPlayer 转换为 []CanonicalPlayer
// 清洗逻辑：
//   - 优先使用 FullName（更完整），FullName 为空时退回 Name
//   - DateOfBirth 直接映射为 Birthday
func (n *SRNormalizer) NormalizePlayers(raw interface{}) ([]CanonicalPlayer, error) {
	players, ok := raw.([]SRPlayer)
	if !ok {
		return nil, fmt.Errorf("SRNormalizer: expected []SRPlayer, got %T", raw)
	}
	canonical := make([]CanonicalPlayer, 0, len(players))
	for _, p := range players {
		name := p.Name
		if p.FullName != "" {
			name = p.FullName
		}
		canonical = append(canonical, CanonicalPlayer{
			ID:          p.ID,
			Name:        name,
			FullName:    p.FullName,
			Birthday:    p.DateOfBirth,
			Nationality: p.Nationality,
			TeamID:      p.TeamID,
			Source:      "sr",
		})
	}
	return canonical, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// LSNormalizer — LSports 数据标准化器
// ─────────────────────────────────────────────────────────────────────────────

// LSNormalizer 将 LS 原始数据标准化为 Canonical 实体
type LSNormalizer struct{}

// NewLSNormalizer 创建 LS 标准化器
func NewLSNormalizer() *LSNormalizer {
	return &LSNormalizer{}
}

func (n *LSNormalizer) SourceSide() string { return "ls" }

// NormalizeTournament 将 LSTournament 转换为 CanonicalTournament
// 清洗逻辑：lsSportName("6046") → "football"（已由 LSAdapter 推导）
func (n *LSNormalizer) NormalizeTournament(raw interface{}) (*CanonicalTournament, error) {
	t, ok := raw.(*LSTournament)
	if !ok {
		return nil, fmt.Errorf("LSNormalizer: expected *LSTournament, got %T", raw)
	}
	return &CanonicalTournament{
		ID:           t.ID,
		Name:         t.Name,
		Sport:        t.Sport, // 已由 LSAdapter.GetTournament 通过 lsSportName 推导
		CategoryName: t.CategoryName,
		Source:       "ls",
	}, nil
}

// NormalizeEvents 将 []LSEvent 转换为 []CanonicalEvent
// 清洗逻辑（LS 特有，集中在此处）：
//   - StartUnix 已由 LSAdapter.GetEvents 通过 parseLSScheduled 解析（支持多种 ISO8601 变体）
//   - StatusID (int) → StatusCode (int)，字段重命名
//   - 去重逻辑已在 LSAdapter.GetEvents 中完成（seen map），此处不重复处理
func (n *LSNormalizer) NormalizeEvents(raw interface{}) ([]CanonicalEvent, error) {
	events, ok := raw.([]LSEvent)
	if !ok {
		return nil, fmt.Errorf("LSNormalizer: expected []LSEvent, got %T", raw)
	}
	canonical := make([]CanonicalEvent, 0, len(events))
	for _, ev := range events {
		canonical = append(canonical, CanonicalEvent{
			ID:           ev.ID,
			TournamentID: ev.TournamentID,
			StartUnix:    ev.StartUnix, // parseLSScheduled 已处理多格式时间
			HomeID:       ev.HomeID,
			HomeName:     ev.HomeName,
			AwayID:       ev.AwayID,
			AwayName:     ev.AwayName,
			StatusCode:   ev.StatusID, // LS 字段名为 StatusID，统一为 StatusCode
			Source:       "ls",
		})
	}
	return canonical, nil
}

// NormalizePlayers 将 []LSPlayer 转换为 []CanonicalPlayer
// 清洗逻辑（LS 特有）：
//   - LSPlayer 无 Birthday / Nationality / FullName（Snapshot API 限制）
//   - 字段置空，由算法层按 LS 专用阈值处理（详见 team_player.go MatchPlayersForLSTeam）
func (n *LSNormalizer) NormalizePlayers(raw interface{}) ([]CanonicalPlayer, error) {
	players, ok := raw.([]LSPlayer)
	if !ok {
		return nil, fmt.Errorf("LSNormalizer: expected []LSPlayer, got %T", raw)
	}
	canonical := make([]CanonicalPlayer, 0, len(players))
	for _, p := range players {
		canonical = append(canonical, CanonicalPlayer{
			ID:          p.ID,
			Name:        p.Name,
			FullName:    "",  // LS Snapshot API 不提供
			Birthday:    "",  // LS Snapshot API 不提供
			Nationality: "",  // LS Snapshot API 不提供
			TeamID:      p.TeamID,
			Source:      "ls",
		})
	}
	return canonical, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// TSNormalizer — TheSports 数据标准化器（目标侧）
// ─────────────────────────────────────────────────────────────────────────────

// TSNormalizer 将 TS 原始数据标准化（目标侧，主要用于文档完整性）
// TS 作为匹配目标侧，其数据已在 TSAdapter 中按 sport 路由，字段相对统一
type TSNormalizer struct{}

// NewTSNormalizer 创建 TS 标准化器
func NewTSNormalizer() *TSNormalizer {
	return &TSNormalizer{}
}

func (n *TSNormalizer) SourceSide() string { return "ts" }

// NormalizeTournament TS 联赛直接映射（TSCompetition 已足够规范）
func (n *TSNormalizer) NormalizeTournament(raw interface{}) (*CanonicalTournament, error) {
	t, ok := raw.(*TSCompetition)
	if !ok {
		return nil, fmt.Errorf("TSNormalizer: expected *TSCompetition, got %T", raw)
	}
	return &CanonicalTournament{
		ID:           t.ID,
		Name:         t.Name,
		Sport:        t.Sport,
		CategoryName: t.CountryName,
		Source:       "ts",
	}, nil
}

// NormalizeEvents TS 比赛直接映射
// 清洗逻辑：TS match_time 已是 Unix 时间戳，无需转换
func (n *TSNormalizer) NormalizeEvents(raw interface{}) ([]CanonicalEvent, error) {
	events, ok := raw.([]TSEvent)
	if !ok {
		return nil, fmt.Errorf("TSNormalizer: expected []TSEvent, got %T", raw)
	}
	canonical := make([]CanonicalEvent, 0, len(events))
	for _, ev := range events {
		canonical = append(canonical, CanonicalEvent{
			ID:           ev.ID,
			TournamentID: "", // TSEvent 无 TournamentID 字段
			StartUnix:    ev.MatchTime,
			HomeID:       ev.HomeID,
			HomeName:     ev.HomeName,
			AwayID:       ev.AwayID,
			AwayName:     ev.AwayName,
			StatusCode:   ev.StatusID,
			Source:       "ts",
		})
	}
	return canonical, nil
}

// NormalizePlayers TS 球员直接映射
// 清洗逻辑：birthday 在 TSAdapter 中已保留为字符串（原始 Unix 值），此处直接传递
func (n *TSNormalizer) NormalizePlayers(raw interface{}) ([]CanonicalPlayer, error) {
	players, ok := raw.([]TSPlayer)
	if !ok {
		return nil, fmt.Errorf("TSNormalizer: expected []TSPlayer, got %T", raw)
	}
	canonical := make([]CanonicalPlayer, 0, len(players))
	for _, p := range players {
		canonical = append(canonical, CanonicalPlayer{
			ID:          p.ID,
			Name:        p.Name,
			FullName:    "",
			Birthday:    p.Birthday,
			Nationality: p.Nationality,
			TeamID:      p.TeamID,
			Source:      "ts",
		})
	}
	return canonical, nil
}
