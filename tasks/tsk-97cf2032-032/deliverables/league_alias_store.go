// Package db — 持久化联赛别名知识图谱 (LeagueAliasStore)
//
// 本文件实现 PI-002 中提出的联赛别名持久化存储，解决官方名称与常用名称差异较大
// 导致联赛匹配失败的问题（典型案例：English Football League One）。
//
// 设计原则：
//   - 存储后端：数据库表 `league_alias_knowledge`（自动建表）
//   - 数据源侧：支持 "sr"（SportRadar）、"ls"（LSports）和 "manual"（人工录入）
//   - 并发安全：读写操作均通过 SQL 事务保证原子性
//   - 内存缓存：启动时一次性加载到内存，减少查询开销
//   - 优先级：manual > sr/ls（人工录入的别名不被自动学习覆盖）
//
// 表结构（自动建表）：
//
//	CREATE TABLE IF NOT EXISTS league_alias_knowledge (
//	    id             BIGINT AUTO_INCREMENT PRIMARY KEY,
//	    source_side    VARCHAR(16)  NOT NULL,  -- "sr" / "ls" / "manual"
//	    canonical_name VARCHAR(256) NOT NULL,  -- 规范名称（真相源）
//	    alias_name     VARCHAR(256) NOT NULL,  -- 别名（官方名或常用名）
//	    sport          VARCHAR(32)  NOT NULL DEFAULT '',
//	    vote_count     INT          NOT NULL DEFAULT 1,
//	    confidence     FLOAT        NOT NULL DEFAULT 1.0,
//	    last_seen      DATETIME     NOT NULL,
//	    created_at     DATETIME     NOT NULL,
//	    UNIQUE KEY uq_league_alias (source_side, canonical_name, alias_name)
//	);
package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// LeagueAliasEntry — 单条联赛别名记录
// ─────────────────────────────────────────────────────────────────────────────

// LeagueAliasEntry 持久化联赛别名知识图谱中的单条记录
type LeagueAliasEntry struct {
	ID            int64
	SourceSide    string    // "sr" / "ls" / "manual"
	CanonicalName string    // 规范名称（真相源）
	AliasName     string    // 别名（官方名或常用名）
	Sport         string    // 运动类型（"football" / "basketball"）
	VoteCount     int       // 累计投票次数（每次成功匹配 +1）
	Confidence    float64   // 置信度
	LastSeen      time.Time // 最近一次命中时间
	CreatedAt     time.Time // 首次写入时间
}

// ─────────────────────────────────────────────────────────────────────────────
// LeagueAliasStore — 持久化联赛别名知识图谱
// ─────────────────────────────────────────────────────────────────────────────

const (
	// leagueAliasTable 联赛别名知识图谱表名
	leagueAliasTable = "league_alias_knowledge"
	// leagueAliasMinVotes 写入持久化存储所需的最少投票次数（自动学习）
	leagueAliasMinVotes = 2
	// leagueAliasManualSource 人工录入的来源标识
	leagueAliasManualSource = "manual"
)

// LeagueAliasStore 持久化联赛别名知识图谱，跨任务保存已验证的联赛名称映射。
//
// 使用方式：
//  1. 调用 NewLeagueAliasStore 创建实例（自动建表 + 加载缓存）
//  2. 在联赛匹配成功后调用 UpsertAlias 写入/更新别名
//  3. 在匹配开始前调用 LoadIntoIndex 将持久化数据注入内存别名索引
type LeagueAliasStore struct {
	db    *sql.DB
	mu    sync.RWMutex
	// cache[normalizedAlias] = canonicalName（内存缓存，启动时加载）
	cache map[string]string
}

// NewLeagueAliasStore 创建联赛别名知识图谱实例。
// 自动建表（如果不存在）并将现有数据加载到内存缓存。
func NewLeagueAliasStore(db *sql.DB) (*LeagueAliasStore, error) {
	store := &LeagueAliasStore{
		db:    db,
		cache: make(map[string]string),
	}
	if err := store.ensureTable(); err != nil {
		return nil, fmt.Errorf("LeagueAliasStore.ensureTable: %w", err)
	}
	if err := store.loadCache(); err != nil {
		return nil, fmt.Errorf("LeagueAliasStore.loadCache: %w", err)
	}
	return store, nil
}

// ensureTable 确保 league_alias_knowledge 表存在（幂等）
func (s *LeagueAliasStore) ensureTable() error {
	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id             BIGINT AUTO_INCREMENT PRIMARY KEY,
			source_side    VARCHAR(16)  NOT NULL,
			canonical_name VARCHAR(256) NOT NULL,
			alias_name     VARCHAR(256) NOT NULL,
			sport          VARCHAR(32)  NOT NULL DEFAULT '',
			vote_count     INT          NOT NULL DEFAULT 1,
			confidence     FLOAT        NOT NULL DEFAULT 1.0,
			last_seen      DATETIME     NOT NULL,
			created_at     DATETIME     NOT NULL,
			UNIQUE KEY uq_league_alias (source_side, canonical_name, alias_name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, leagueAliasTable)
	_, err := s.db.Exec(ddl)
	return err
}

// loadCache 从数据库加载别名到内存缓存。
// 优先加载 manual 来源，再加载自动学习的高置信度别名。
func (s *LeagueAliasStore) loadCache() error {
	// 加载所有 manual 来源（无最低投票次数限制）
	manualQuery := fmt.Sprintf(`
		SELECT canonical_name, alias_name
		FROM %s
		WHERE source_side = ?
		ORDER BY vote_count DESC, last_seen DESC`, leagueAliasTable)

	rows, err := s.db.Query(manualQuery, leagueAliasManualSource)
	if err != nil {
		return fmt.Errorf("loadCache manual query: %w", err)
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for rows.Next() {
		var canonical, alias string
		if err := rows.Scan(&canonical, &alias); err != nil {
			continue
		}
		normAlias := normalizeForCache(alias)
		if _, exists := s.cache[normAlias]; !exists {
			s.cache[normAlias] = normalizeForCache(canonical)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// 加载自动学习的高置信度别名（vote_count >= 阈值）
	autoQuery := fmt.Sprintf(`
		SELECT canonical_name, alias_name
		FROM %s
		WHERE source_side != ? AND vote_count >= ?
		ORDER BY vote_count DESC, last_seen DESC`, leagueAliasTable)

	rows2, err := s.db.Query(autoQuery, leagueAliasManualSource, leagueAliasMinVotes)
	if err != nil {
		return fmt.Errorf("loadCache auto query: %w", err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var canonical, alias string
		if err := rows2.Scan(&canonical, &alias); err != nil {
			continue
		}
		normAlias := normalizeForCache(alias)
		// manual 来源优先，不覆盖
		if _, exists := s.cache[normAlias]; !exists {
			s.cache[normAlias] = normalizeForCache(canonical)
		}
		count++
	}

	log.Printf("[LeagueAliasStore] 已加载 %d 条持久化联赛别名到内存缓存", count)
	return rows2.Err()
}

// normalizeForCache 用于缓存 key/value 的简单归一化（小写 + 去多余空格）
func normalizeForCache(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

// UpsertAlias 写入或更新一条联赛别名记录。
// 若 (sourceSide, canonicalName, aliasName) 已存在，则累加 vote_count 并更新 last_seen。
// 若不存在，则插入新记录。
func (s *LeagueAliasStore) UpsertAlias(
	sourceSide, canonicalName, aliasName, sport string,
	confidence float64,
) error {
	if canonicalName == "" || aliasName == "" {
		return nil
	}

	now := time.Now()
	upsertSQL := fmt.Sprintf(`
		INSERT INTO %s
			(source_side, canonical_name, alias_name, sport, vote_count, confidence, last_seen, created_at)
		VALUES
			(?, ?, ?, ?, 1, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			vote_count = vote_count + 1,
			confidence = VALUES(confidence),
			last_seen  = VALUES(last_seen)`, leagueAliasTable)

	_, err := s.db.Exec(upsertSQL,
		sourceSide, canonicalName, aliasName, sport,
		confidence, now, now,
	)
	if err != nil {
		return fmt.Errorf("LeagueAliasStore.UpsertAlias: %w", err)
	}

	// 查询当前 vote_count，若达到阈值则更新内存缓存
	var voteCount int
	countSQL := fmt.Sprintf(`
		SELECT vote_count FROM %s
		WHERE source_side=? AND canonical_name=? AND alias_name=?`, leagueAliasTable)
	if err := s.db.QueryRow(countSQL, sourceSide, canonicalName, aliasName).Scan(&voteCount); err == nil {
		if voteCount >= leagueAliasMinVotes || sourceSide == leagueAliasManualSource {
			s.mu.Lock()
			normAlias := normalizeForCache(aliasName)
			if _, exists := s.cache[normAlias]; !exists || sourceSide == leagueAliasManualSource {
				s.cache[normAlias] = normalizeForCache(canonicalName)
			}
			s.mu.Unlock()
		}
	}

	return nil
}

// UpsertManual 人工录入一条联赛别名（最高优先级，直接写入缓存）
func (s *LeagueAliasStore) UpsertManual(canonicalName, aliasName, sport string) error {
	return s.UpsertAlias(leagueAliasManualSource, canonicalName, aliasName, sport, 1.0)
}

// LookupCanonical 查询别名对应的规范名称。
// 优先从内存缓存读取，缓存未命中时查询数据库。
// 返回 (canonicalName, found)。
func (s *LeagueAliasStore) LookupCanonical(aliasName string) (string, bool) {
	normAlias := normalizeForCache(aliasName)

	s.mu.RLock()
	if canonical, ok := s.cache[normAlias]; ok {
		s.mu.RUnlock()
		return canonical, true
	}
	s.mu.RUnlock()

	// 缓存未命中，查询数据库
	query := fmt.Sprintf(`
		SELECT canonical_name FROM %s
		WHERE alias_name = ?
		ORDER BY
			CASE source_side WHEN 'manual' THEN 0 ELSE 1 END,
			vote_count DESC, last_seen DESC
		LIMIT 1`, leagueAliasTable)

	var canonical string
	err := s.db.QueryRow(query, aliasName).Scan(&canonical)
	if err != nil {
		return "", false
	}

	// 写回缓存
	s.mu.Lock()
	s.cache[normAlias] = normalizeForCache(canonical)
	s.mu.Unlock()

	return canonical, true
}

// ListBySport 列出指定运动类型的所有别名记录（用于调试和审计）
func (s *LeagueAliasStore) ListBySport(sport string) ([]LeagueAliasEntry, error) {
	query := fmt.Sprintf(`
		SELECT id, source_side, canonical_name, alias_name, sport, vote_count, confidence, last_seen, created_at
		FROM %s
		WHERE sport = ?
		ORDER BY canonical_name, vote_count DESC`, leagueAliasTable)

	rows, err := s.db.Query(query, sport)
	if err != nil {
		return nil, fmt.Errorf("LeagueAliasStore.ListBySport: %w", err)
	}
	defer rows.Close()

	var entries []LeagueAliasEntry
	for rows.Next() {
		var e LeagueAliasEntry
		if err := rows.Scan(
			&e.ID, &e.SourceSide, &e.CanonicalName, &e.AliasName, &e.Sport,
			&e.VoteCount, &e.Confidence, &e.LastSeen, &e.CreatedAt,
		); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Stats 返回联赛别名知识图谱的统计信息
func (s *LeagueAliasStore) Stats() (total, manualCount, autoCount int, err error) {
	totalSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, leagueAliasTable)
	if err = s.db.QueryRow(totalSQL).Scan(&total); err != nil {
		return
	}
	manualSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE source_side = ?`, leagueAliasTable)
	if err = s.db.QueryRow(manualSQL, leagueAliasManualSource).Scan(&manualCount); err != nil {
		return
	}
	autoCount = total - manualCount
	return
}

// LoadIntoIndex 将持久化联赛别名知识图谱中的记录注入内存别名索引。
// 在每次联赛匹配前执行，确保历史学习结果被复用。
//
// 参数：
//   - loader: 实现 LeagueAliasLoader 接口的内存索引（通常是 matcher.LeagueAliasIndex）
func (s *LeagueAliasStore) LoadIntoIndex(loader interface {
	RegisterAlias(canonicalName, alias string)
}) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for normAlias, normCanonical := range s.cache {
		loader.RegisterAlias(normCanonical, normAlias)
		count++
	}
	log.Printf("[LeagueAliasStore] 已注入 %d 条联赛别名到内存索引", count)
	return count
}
