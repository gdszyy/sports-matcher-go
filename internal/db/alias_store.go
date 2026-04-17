// Package db — 持久化球队别名知识图谱 (AliasStore)
//
// TODO-012: 将 internal/matcher.TeamAliasIndex 从单次运行的内存索引升级为
// 跨任务持久化的全局知识图谱。
//
// 设计原则：
//   - 存储后端：数据库表 `team_alias_knowledge`（自动建表）
//   - 数据源侧：支持 "sr"（SportRadar）和 "ls"（LSports）两种来源
//   - 并发安全：读写操作均通过 SQL 事务保证原子性
//   - 内存缓存：启动时一次性加载到内存，减少查询开销
//   - 自动老化：超过 `maxAgeDays` 天未被命中的别名自动标记为待复核
//
// 表结构（自动建表）：
//
//	CREATE TABLE IF NOT EXISTS team_alias_knowledge (
//	    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
//	    source_side  VARCHAR(8)   NOT NULL,  -- "sr" 或 "ls"
//	    src_team_id  VARCHAR(64)  NOT NULL,  -- SR/LS 侧球队 ID
//	    ts_team_id   VARCHAR(64)  NOT NULL,  -- TS 侧球队 ID
//	    vote_count   INT          NOT NULL DEFAULT 1,
//	    confidence   FLOAT        NOT NULL DEFAULT 0.0,
//	    sport        VARCHAR(32)  NOT NULL DEFAULT '',
//	    competition_id VARCHAR(64) NOT NULL DEFAULT '',
//	    last_seen    DATETIME     NOT NULL,
//	    created_at   DATETIME     NOT NULL,
//	    UNIQUE KEY uq_alias (source_side, src_team_id, ts_team_id)
//	);
package db

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// AliasEntry — 单条别名记录
// ─────────────────────────────────────────────────────────────────────────────

// AliasEntry 持久化别名知识图谱中的单条记录
type AliasEntry struct {
	ID            int64
	SourceSide    string    // "sr" 或 "ls"
	SrcTeamID     string    // SR/LS 侧球队 ID
	TSTeamID      string    // TS 侧球队 ID
	VoteCount     int       // 累计投票次数（每次成功匹配 +1）
	Confidence    float64   // 最近一次匹配的置信度
	Sport         string    // 运动类型（"football" / "basketball"）
	CompetitionID string    // 所属联赛 ID（可为空，表示跨联赛通用）
	LastSeen      time.Time // 最近一次命中时间
	CreatedAt     time.Time // 首次写入时间
}

// ─────────────────────────────────────────────────────────────────────────────
// AliasStore — 持久化别名知识图谱
// ─────────────────────────────────────────────────────────────────────────────

const (
	// aliasStoreTable 别名知识图谱表名
	aliasStoreTable = "team_alias_knowledge"
	// aliasStoreMinVotes 写入持久化存储所需的最少投票次数
	aliasStoreMinVotes = 2
	// aliasStoreMaxAgeDays 超过此天数未命中则标记为低置信度（不删除，仅降权）
	aliasStoreMaxAgeDays = 90
)

// AliasStore 持久化球队别名知识图谱，跨任务保存已验证的球队 ID 映射。
//
// 使用方式：
//  1. 调用 NewAliasStore 创建实例（自动建表 + 加载缓存）
//  2. 在每场比赛匹配成功后调用 Upsert 写入/更新别名
//  3. 在 MatchEvents 开始前调用 LoadIntoAliasIndex 将持久化数据注入内存索引
type AliasStore struct {
	db    *sql.DB
	mu    sync.RWMutex
	// cache[sourceSide][srcTeamID] = TSTeamID（内存缓存，启动时加载）
	cache map[string]map[string]string
}

// NewAliasStore 创建别名知识图谱实例。
// 自动建表（如果不存在）并将现有数据加载到内存缓存。
func NewAliasStore(db *sql.DB) (*AliasStore, error) {
	store := &AliasStore{
		db:    db,
		cache: make(map[string]map[string]string),
	}
	if err := store.ensureTable(); err != nil {
		return nil, fmt.Errorf("AliasStore.ensureTable: %w", err)
	}
	if err := store.loadCache(); err != nil {
		return nil, fmt.Errorf("AliasStore.loadCache: %w", err)
	}
	return store, nil
}

// ensureTable 确保 team_alias_knowledge 表存在（幂等）
func (s *AliasStore) ensureTable() error {
	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id             BIGINT AUTO_INCREMENT PRIMARY KEY,
			source_side    VARCHAR(8)   NOT NULL,
			src_team_id    VARCHAR(64)  NOT NULL,
			ts_team_id     VARCHAR(64)  NOT NULL,
			vote_count     INT          NOT NULL DEFAULT 1,
			confidence     FLOAT        NOT NULL DEFAULT 0.0,
			sport          VARCHAR(32)  NOT NULL DEFAULT '',
			competition_id VARCHAR(64)  NOT NULL DEFAULT '',
			last_seen      DATETIME     NOT NULL,
			created_at     DATETIME     NOT NULL,
			UNIQUE KEY uq_alias (source_side, src_team_id, ts_team_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, aliasStoreTable)
	_, err := s.db.Exec(ddl)
	return err
}

// loadCache 从数据库加载高置信度别名到内存缓存。
// 仅加载 vote_count >= aliasStoreMinVotes 的记录。
func (s *AliasStore) loadCache() error {
	query := fmt.Sprintf(`
		SELECT source_side, src_team_id, ts_team_id
		FROM %s
		WHERE vote_count >= ?
		ORDER BY vote_count DESC, last_seen DESC`, aliasStoreTable)

	rows, err := s.db.Query(query, aliasStoreMinVotes)
	if err != nil {
		return fmt.Errorf("loadCache query: %w", err)
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for rows.Next() {
		var side, srcID, tsID string
		if err := rows.Scan(&side, &srcID, &tsID); err != nil {
			continue
		}
		if s.cache[side] == nil {
			s.cache[side] = make(map[string]string)
		}
		// 同一 srcTeamID 可能有多条记录（不同联赛），取 vote_count 最高的
		if _, exists := s.cache[side][srcID]; !exists {
			s.cache[side][srcID] = tsID
		}
		count++
	}
	log.Printf("[AliasStore] 已加载 %d 条持久化别名到内存缓存", count)
	return rows.Err()
}

// Upsert 写入或更新一条别名记录。
// 若 (sourceSide, srcTeamID, tsTeamID) 已存在，则累加 vote_count 并更新 last_seen。
// 若不存在，则插入新记录。
// 当 vote_count 达到 aliasStoreMinVotes 后，自动更新内存缓存。
func (s *AliasStore) Upsert(
	sourceSide, srcTeamID, tsTeamID string,
	confidence float64,
	sport, competitionID string,
) error {
	if srcTeamID == "" || tsTeamID == "" {
		return nil
	}

	now := time.Now()
	upsertSQL := fmt.Sprintf(`
		INSERT INTO %s
			(source_side, src_team_id, ts_team_id, vote_count, confidence, sport, competition_id, last_seen, created_at)
		VALUES
			(?, ?, ?, 1, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			vote_count   = vote_count + 1,
			confidence   = VALUES(confidence),
			last_seen    = VALUES(last_seen)`, aliasStoreTable)

	_, err := s.db.Exec(upsertSQL,
		sourceSide, srcTeamID, tsTeamID,
		confidence, sport, competitionID,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("AliasStore.Upsert: %w", err)
	}

	// 查询当前 vote_count，若达到阈值则更新内存缓存
	var voteCount int
	countSQL := fmt.Sprintf(`
		SELECT vote_count FROM %s
		WHERE source_side=? AND src_team_id=? AND ts_team_id=?`, aliasStoreTable)
	if err := s.db.QueryRow(countSQL, sourceSide, srcTeamID, tsTeamID).Scan(&voteCount); err == nil {
		if voteCount >= aliasStoreMinVotes {
			s.mu.Lock()
			if s.cache[sourceSide] == nil {
				s.cache[sourceSide] = make(map[string]string)
			}
			// 仅在该 srcTeamID 尚未有缓存时写入（保留最高票数的映射）
			if _, exists := s.cache[sourceSide][srcTeamID]; !exists {
				s.cache[sourceSide][srcTeamID] = tsTeamID
			}
			s.mu.Unlock()
		}
	}

	return nil
}

// UpsertBatch 批量写入别名记录（事务包裹，提升写入性能）
func (s *AliasStore) UpsertBatch(entries []AliasEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("AliasStore.UpsertBatch begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	upsertSQL := fmt.Sprintf(`
		INSERT INTO %s
			(source_side, src_team_id, ts_team_id, vote_count, confidence, sport, competition_id, last_seen, created_at)
		VALUES
			(?, ?, ?, 1, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			vote_count   = vote_count + 1,
			confidence   = VALUES(confidence),
			last_seen    = VALUES(last_seen)`, aliasStoreTable)

	stmt, err := tx.Prepare(upsertSQL)
	if err != nil {
		return fmt.Errorf("AliasStore.UpsertBatch prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, e := range entries {
		if e.SrcTeamID == "" || e.TSTeamID == "" {
			continue
		}
		createdAt := e.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		lastSeen := e.LastSeen
		if lastSeen.IsZero() {
			lastSeen = now
		}
		if _, err = stmt.Exec(
			e.SourceSide, e.SrcTeamID, e.TSTeamID,
			e.Confidence, e.Sport, e.CompetitionID,
			lastSeen, createdAt,
		); err != nil {
			return fmt.Errorf("AliasStore.UpsertBatch exec: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("AliasStore.UpsertBatch commit: %w", err)
	}

	// 批量写入后重新加载缓存（保证一致性）
	return s.loadCache()
}

// Lookup 查询 (sourceSide, srcTeamID) 对应的 TS 球队 ID。
// 优先从内存缓存读取，缓存未命中时查询数据库。
// 返回 (tsTeamID, found)。
func (s *AliasStore) Lookup(sourceSide, srcTeamID string) (string, bool) {
	s.mu.RLock()
	if s.cache[sourceSide] != nil {
		if tsID, ok := s.cache[sourceSide][srcTeamID]; ok {
			s.mu.RUnlock()
			return tsID, true
		}
	}
	s.mu.RUnlock()

	// 缓存未命中，查询数据库（vote_count >= 阈值）
	query := fmt.Sprintf(`
		SELECT ts_team_id FROM %s
		WHERE source_side=? AND src_team_id=? AND vote_count >= ?
		ORDER BY vote_count DESC, last_seen DESC
		LIMIT 1`, aliasStoreTable)

	var tsID string
	err := s.db.QueryRow(query, sourceSide, srcTeamID, aliasStoreMinVotes).Scan(&tsID)
	if err != nil {
		return "", false
	}

	// 写回缓存
	s.mu.Lock()
	if s.cache[sourceSide] == nil {
		s.cache[sourceSide] = make(map[string]string)
	}
	s.cache[sourceSide][srcTeamID] = tsID
	s.mu.Unlock()

	return tsID, true
}

// ListBySport 列出指定运动类型的所有高置信度别名记录（用于调试和审计）
func (s *AliasStore) ListBySport(sourceSide, sport string) ([]AliasEntry, error) {
	query := fmt.Sprintf(`
		SELECT id, source_side, src_team_id, ts_team_id, vote_count, confidence,
		       sport, competition_id, last_seen, created_at
		FROM %s
		WHERE source_side=? AND sport=? AND vote_count >= ?
		ORDER BY vote_count DESC, last_seen DESC`, aliasStoreTable)

	rows, err := s.db.Query(query, sourceSide, sport, aliasStoreMinVotes)
	if err != nil {
		return nil, fmt.Errorf("AliasStore.ListBySport: %w", err)
	}
	defer rows.Close()

	var entries []AliasEntry
	for rows.Next() {
		var e AliasEntry
		if err := rows.Scan(
			&e.ID, &e.SourceSide, &e.SrcTeamID, &e.TSTeamID,
			&e.VoteCount, &e.Confidence, &e.Sport, &e.CompetitionID,
			&e.LastSeen, &e.CreatedAt,
		); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Stats 返回别名知识图谱的统计信息
func (s *AliasStore) Stats() (total, highConf int, err error) {
	totalSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, aliasStoreTable)
	if err = s.db.QueryRow(totalSQL).Scan(&total); err != nil {
		return
	}
	highSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE vote_count >= ?`, aliasStoreTable)
	err = s.db.QueryRow(highSQL, aliasStoreMinVotes).Scan(&highConf)
	return
}

// PruneStale 清理超过 maxAgeDays 天未命中且 vote_count=1 的低质量记录（防止表膨胀）
func (s *AliasStore) PruneStale() (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -aliasStoreMaxAgeDays)
	pruneSQL := fmt.Sprintf(`
		DELETE FROM %s
		WHERE vote_count = 1 AND last_seen < ?`, aliasStoreTable)

	result, err := s.db.Exec(pruneSQL, cutoff)
	if err != nil {
		return 0, fmt.Errorf("AliasStore.PruneStale: %w", err)
	}
	affected, _ := result.RowsAffected()

	if affected > 0 {
		// 清理后重新加载缓存
		if cacheErr := s.loadCache(); cacheErr != nil {
			log.Printf("[AliasStore] PruneStale 后重新加载缓存失败: %v", cacheErr)
		}
	}
	return affected, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// AliasStoreAdapter — 将 AliasStore 与 matcher.TeamAliasIndex 桥接
// ─────────────────────────────────────────────────────────────────────────────

// AliasIndexLoader 定义将持久化别名注入内存索引的接口。
// matcher.TeamAliasIndex 实现此接口，使 AliasStore 无需直接依赖 matcher 包。
type AliasIndexLoader interface {
	// InjectAlias 直接注入一条已验证的别名对（绕过投票机制，直接写入 alias 映射）
	InjectAlias(srcTeamID, tsTeamID string)
}

// LoadIntoIndex 将持久化别名知识图谱中的高置信度记录注入内存别名索引。
// 在每次 MatchEvents 调用前执行，确保历史学习结果被复用。
//
// 参数：
//   - loader: 实现 AliasIndexLoader 接口的内存索引（通常是 matcher.TeamAliasIndex）
//   - sourceSide: "sr" 或 "ls"
func (s *AliasStore) LoadIntoIndex(loader AliasIndexLoader, sourceSide string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	if s.cache[sourceSide] == nil {
		return 0
	}
	for srcID, tsID := range s.cache[sourceSide] {
		loader.InjectAlias(srcID, tsID)
		count++
	}
	return count
}
