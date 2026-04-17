// Package db — 数据路由层 (DataRouter)
//
// 职责：
//   - 根据 source 标识（"sr" / "ls"）动态路由到对应的 Adapter 和 Normalizer
//   - 对调用方（UniversalEngine）提供统一的数据获取接口，返回 Canonical 实体
//   - 支持运行时动态注册新数据源，实现开闭原则
//
// 使用示例：
//
//	router := db.NewDataRouter()
//	router.RegisterSource("sr", srAdapter, db.NewSRNormalizer())
//	router.RegisterSource("ls", lsAdapter, db.NewLSNormalizer())
//
//	// 获取标准化比赛数据（不关心来源是 SR 还是 LS）
//	events, err := router.GetCanonicalEvents("ls", "8363")
package db

import (
	"fmt"
)

// ─────────────────────────────────────────────────────────────────────────────
// SourceFetcher — 原始数据获取接口（Adapter 侧）
// ─────────────────────────────────────────────────────────────────────────────

// SourceFetcher 原始数据获取接口，由各 Adapter 实现
// 与 matcher.SourceAdapter 不同，此接口专注于原始数据获取，不涉及匹配逻辑
type SourceFetcher interface {
	// FetchTournament 获取原始联赛数据
	FetchTournament(tournamentID string) (interface{}, error)

	// FetchEvents 获取原始比赛列表
	FetchEvents(tournamentID string) (interface{}, error)

	// FetchTeamNames 获取球队名称映射
	FetchTeamNames(tournamentID string) (map[string]string, error)

	// FetchPlayers 获取球员列表（按球队 ID）
	FetchPlayers(teamID, sport string) (interface{}, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// SRFetcher — SR 数据获取器
// ─────────────────────────────────────────────────────────────────────────────

// SRFetcher 封装 SRAdapter，实现 SourceFetcher 接口
type SRFetcher struct {
	Adapter *SRAdapter
}

// NewSRFetcher 创建 SR 数据获取器
func NewSRFetcher(adapter *SRAdapter) *SRFetcher {
	return &SRFetcher{Adapter: adapter}
}

func (f *SRFetcher) FetchTournament(tournamentID string) (interface{}, error) {
	return f.Adapter.GetTournament(tournamentID)
}

func (f *SRFetcher) FetchEvents(tournamentID string) (interface{}, error) {
	events, err := f.Adapter.GetEvents(tournamentID)
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (f *SRFetcher) FetchTeamNames(tournamentID string) (map[string]string, error) {
	return f.Adapter.GetTeamNames(tournamentID)
}

func (f *SRFetcher) FetchPlayers(teamID, sport string) (interface{}, error) {
	return f.Adapter.GetPlayersByTeam(teamID)
}

// ─────────────────────────────────────────────────────────────────────────────
// LSFetcher — LS 数据获取器
// ─────────────────────────────────────────────────────────────────────────────

// LSFetcher 封装 LSAdapter，实现 SourceFetcher 接口
type LSFetcher struct {
	Adapter *LSAdapter
}

// NewLSFetcher 创建 LS 数据获取器
func NewLSFetcher(adapter *LSAdapter) *LSFetcher {
	return &LSFetcher{Adapter: adapter}
}

func (f *LSFetcher) FetchTournament(tournamentID string) (interface{}, error) {
	return f.Adapter.GetTournament(tournamentID)
}

func (f *LSFetcher) FetchEvents(tournamentID string) (interface{}, error) {
	events, err := f.Adapter.GetEvents(tournamentID)
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (f *LSFetcher) FetchTeamNames(tournamentID string) (map[string]string, error) {
	return f.Adapter.GetTeamNames(tournamentID)
}

func (f *LSFetcher) FetchPlayers(teamID, sport string) (interface{}, error) {
	// LS 球员通过 LSPlayerAdapter 获取，此处返回空切片作为占位
	// 实际球员匹配由 LSSourceAdapter.RunPlayerMatch 处理（批量获取）
	return []LSPlayer{}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// DataRouter — 数据路由层
// ─────────────────────────────────────────────────────────────────────────────

// sourceEntry 注册的数据源条目
type sourceEntry struct {
	fetcher    SourceFetcher
	normalizer DataNormalizer
}

// DataRouter 数据路由层，根据 source 标识动态路由到对应的 Fetcher 和 Normalizer
type DataRouter struct {
	sources map[string]*sourceEntry
}

// NewDataRouter 创建数据路由器
func NewDataRouter() *DataRouter {
	return &DataRouter{
		sources: make(map[string]*sourceEntry),
	}
}

// RegisterSource 注册数据源
// source: "sr" / "ls" / 自定义标识
// fetcher: 原始数据获取器（实现 SourceFetcher 接口）
// normalizer: 数据标准化器（实现 DataNormalizer 接口）
func (r *DataRouter) RegisterSource(source string, fetcher SourceFetcher, normalizer DataNormalizer) {
	r.sources[source] = &sourceEntry{
		fetcher:    fetcher,
		normalizer: normalizer,
	}
}

// HasSource 检查是否已注册指定数据源
func (r *DataRouter) HasSource(source string) bool {
	_, ok := r.sources[source]
	return ok
}

// GetCanonicalTournament 获取规范化联赛数据
// 路由逻辑：source → Fetcher.FetchTournament → Normalizer.NormalizeTournament → CanonicalTournament
func (r *DataRouter) GetCanonicalTournament(source, tournamentID string) (*CanonicalTournament, error) {
	entry, err := r.getEntry(source)
	if err != nil {
		return nil, err
	}
	raw, err := entry.fetcher.FetchTournament(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("DataRouter[%s].FetchTournament(%s): %w", source, tournamentID, err)
	}
	if raw == nil {
		return nil, nil
	}
	return entry.normalizer.NormalizeTournament(raw)
}

// GetCanonicalEvents 获取规范化比赛列表
// 路由逻辑：source → Fetcher.FetchEvents → Normalizer.NormalizeEvents → []CanonicalEvent
func (r *DataRouter) GetCanonicalEvents(source, tournamentID string) ([]CanonicalEvent, error) {
	entry, err := r.getEntry(source)
	if err != nil {
		return nil, err
	}
	raw, err := entry.fetcher.FetchEvents(tournamentID)
	if err != nil {
		return nil, fmt.Errorf("DataRouter[%s].FetchEvents(%s): %w", source, tournamentID, err)
	}
	return entry.normalizer.NormalizeEvents(raw)
}

// GetTeamNames 获取球队名称映射（无需标准化，直接透传）
// 返回 map[sourceTeamID]teamName
func (r *DataRouter) GetTeamNames(source, tournamentID string) (map[string]string, error) {
	entry, err := r.getEntry(source)
	if err != nil {
		return nil, err
	}
	return entry.fetcher.FetchTeamNames(tournamentID)
}

// GetCanonicalPlayers 获取规范化球员列表
// 路由逻辑：source → Fetcher.FetchPlayers → Normalizer.NormalizePlayers → []CanonicalPlayer
func (r *DataRouter) GetCanonicalPlayers(source, teamID, sport string) ([]CanonicalPlayer, error) {
	entry, err := r.getEntry(source)
	if err != nil {
		return nil, err
	}
	raw, err := entry.fetcher.FetchPlayers(teamID, sport)
	if err != nil {
		return nil, fmt.Errorf("DataRouter[%s].FetchPlayers(%s): %w", source, teamID, err)
	}
	return entry.normalizer.NormalizePlayers(raw)
}

// ToSREvents 将 []CanonicalEvent 转换为 []SREvent（供现有算法层消费）
// 这是一个过渡性辅助函数，在算法层完全迁移到 Canonical 实体之前使用
func ToSREvents(events []CanonicalEvent) []SREvent {
	result := make([]SREvent, 0, len(events))
	for _, ev := range events {
		result = append(result, SREvent{
			ID:           ev.ID,
			TournamentID: ev.TournamentID,
			StartUnix:    ev.StartUnix,
			HomeID:       ev.HomeID,
			HomeName:     ev.HomeName,
			AwayID:       ev.AwayID,
			AwayName:     ev.AwayName,
			StatusCode:   ev.StatusCode,
		})
	}
	return result
}

// getEntry 获取注册的数据源条目（内部辅助方法）
func (r *DataRouter) getEntry(source string) (*sourceEntry, error) {
	entry, ok := r.sources[source]
	if !ok {
		return nil, fmt.Errorf("DataRouter: 未注册的数据源 '%s'，已注册: %v", source, r.registeredSources())
	}
	return entry, nil
}

// registeredSources 返回已注册的数据源列表（用于错误信息）
func (r *DataRouter) registeredSources() []string {
	keys := make([]string, 0, len(r.sources))
	for k := range r.sources {
		keys = append(keys, k)
	}
	return keys
}
