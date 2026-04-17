// Package matcher — 已知映射表反向确认率自动验证（KnownLeagueMapValidator）
//
// TODO-014: 为 KnownLSLeagueMap / KnownLeagueMap 引入比赛反向确认率（RCR）
// 自动验证，防止手动映射表中的跨级别错误（如 LS tournament_id=66 被误映射到德甲而非德乙）。
//
// 设计原则：
//   - 非侵入式：验证器不修改 KnownLSLeagueMap 本身，仅在运行时标记可疑映射
//   - 持久化：验证结果写入数据库表 `known_map_validation_log`（自动建表）
//   - 阈值可配：RCR 低于 `ReviewThreshold`（默认 0.30）时标记为待复核
//   - 可覆盖：人工确认的映射可通过 `MarkManualOverride` 豁免自动验证
//   - 零阻断：验证失败不阻断匹配流程，仅记录日志和降低联赛置信度
//
// 验证流程：
//  1. 联赛匹配命中 KnownLSLeagueMap（`RuleLeagueKnown`）
//  2. 完成比赛匹配后，计算 RCR
//  3. 调用 `Validate` 写入验证日志
//  4. 若 RCR < ReviewThreshold，返回 `ValidationStatusSuspect`
//  5. 调用方可选择降低联赛置信度或输出告警
package matcher

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// 常量与枚举
// ─────────────────────────────────────────────────────────────────────────────

const (
	// validationLogTable 验证日志表名
	validationLogTable = "known_map_validation_log"

	// DefaultReviewThreshold RCR 低于此值时标记为待复核
	DefaultReviewThreshold = 0.30

	// DefaultSuspectPenalty 待复核映射的联赛置信度惩罚量
	DefaultSuspectPenalty = 0.20

	// validationLogMaxRows 每个 mapKey 保留的最大日志条数（防止表膨胀）
	validationLogMaxRows = 100
)

// ValidationStatus 验证状态
type ValidationStatus string

const (
	// ValidationStatusOK RCR 达标，映射可信
	ValidationStatusOK ValidationStatus = "OK"
	// ValidationStatusSuspect RCR 低于阈值，映射待复核
	ValidationStatusSuspect ValidationStatus = "SUSPECT"
	// ValidationStatusOverride 人工确认豁免，跳过自动验证
	ValidationStatusOverride ValidationStatus = "OVERRIDE"
	// ValidationStatusInsufficient 比赛数量不足，无法有效验证（< minEventsForValidation）
	ValidationStatusInsufficient ValidationStatus = "INSUFFICIENT"
)

// minEventsForValidation 触发有效验证所需的最少比赛数
const minEventsForValidation = 5

// ─────────────────────────────────────────────────────────────────────────────
// ValidationRecord — 单次验证记录
// ─────────────────────────────────────────────────────────────────────────────

// ValidationRecord 单次已知映射验证记录
type ValidationRecord struct {
	ID              int64
	MapKey          string           // 格式: "ls:football:8363" 或 "sr:football:sr:tournament:xxx"
	SrcTournamentID string           // 源侧联赛 ID
	TSCompetitionID string           // TS 侧联赛 ID
	Sport           string           // 运动类型
	SourceSide      string           // "ls" 或 "sr"
	EventTotal      int              // 源侧比赛总数
	EventMatched    int              // 成功匹配比赛数
	RCR             float64          // 反向确认率
	Status          ValidationStatus // 验证状态
	Note            string           // 备注（如 "RCR=0.12 < threshold=0.30"）
	CreatedAt       time.Time
}

// ─────────────────────────────────────────────────────────────────────────────
// KnownLeagueMapValidator — 已知映射表验证器
// ─────────────────────────────────────────────────────────────────────────────

// KnownLeagueMapValidator 已知映射表反向确认率自动验证器。
//
// 使用方式：
//  1. 调用 NewKnownLeagueMapValidator 创建实例（自动建表 + 加载豁免列表）
//  2. 在每次联赛匹配命中 KnownLSLeagueMap 后，完成比赛匹配，然后调用 Validate
//  3. 根据返回的 ValidationStatus 决定是否降低置信度或输出告警
//  4. 人工确认某映射正确后，调用 MarkManualOverride 豁免该映射
type KnownLeagueMapValidator struct {
	db              *sql.DB
	mu              sync.RWMutex
	reviewThreshold float64
	suspectPenalty  float64
	// overrides[mapKey] = true 表示该映射已人工确认，跳过自动验证
	overrides map[string]bool
}

// NewKnownLeagueMapValidator 创建验证器实例
func NewKnownLeagueMapValidator(db *sql.DB) (*KnownLeagueMapValidator, error) {
	v := &KnownLeagueMapValidator{
		db:              db,
		reviewThreshold: DefaultReviewThreshold,
		suspectPenalty:  DefaultSuspectPenalty,
		overrides:       make(map[string]bool),
	}
	if err := v.ensureTable(); err != nil {
		return nil, fmt.Errorf("KnownLeagueMapValidator.ensureTable: %w", err)
	}
	if err := v.loadOverrides(); err != nil {
		return nil, fmt.Errorf("KnownLeagueMapValidator.loadOverrides: %w", err)
	}
	return v, nil
}

// SetThreshold 设置 RCR 审核阈值（默认 0.30）
func (v *KnownLeagueMapValidator) SetThreshold(t float64) {
	v.mu.Lock()
	v.reviewThreshold = t
	v.mu.Unlock()
}

// SetPenalty 设置待复核映射的置信度惩罚量（默认 0.20）
func (v *KnownLeagueMapValidator) SetPenalty(p float64) {
	v.mu.Lock()
	v.suspectPenalty = p
	v.mu.Unlock()
}

// ensureTable 确保验证日志表存在（幂等）
func (v *KnownLeagueMapValidator) ensureTable() error {
	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id                BIGINT AUTO_INCREMENT PRIMARY KEY,
			map_key           VARCHAR(128) NOT NULL,
			src_tournament_id VARCHAR(64)  NOT NULL,
			ts_competition_id VARCHAR(64)  NOT NULL,
			sport             VARCHAR(32)  NOT NULL DEFAULT '',
			source_side       VARCHAR(8)   NOT NULL DEFAULT 'ls',
			event_total       INT          NOT NULL DEFAULT 0,
			event_matched     INT          NOT NULL DEFAULT 0,
			rcr               FLOAT        NOT NULL DEFAULT 0.0,
			status            VARCHAR(16)  NOT NULL DEFAULT 'OK',
			note              VARCHAR(256) NOT NULL DEFAULT '',
			is_override       TINYINT(1)   NOT NULL DEFAULT 0,
			created_at        DATETIME     NOT NULL,
			INDEX idx_map_key (map_key),
			INDEX idx_status  (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, validationLogTable)
	_, err := v.db.Exec(ddl)
	return err
}

// loadOverrides 从数据库加载所有人工豁免的映射 key
func (v *KnownLeagueMapValidator) loadOverrides() error {
	query := fmt.Sprintf(`SELECT DISTINCT map_key FROM %s WHERE is_override=1`, validationLogTable)
	rows, err := v.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	v.mu.Lock()
	defer v.mu.Unlock()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err == nil {
			v.overrides[key] = true
		}
	}
	return rows.Err()
}

// mapKey 生成验证记录的唯一 key
// 格式: "{sourceSide}:{sport}:{tournamentID}"
func mapKey(sourceSide, sport, tournamentID string) string {
	return fmt.Sprintf("%s:%s:%s", sourceSide, sport, tournamentID)
}

// ─────────────────────────────────────────────────────────────────────────────
// 核心验证方法
// ─────────────────────────────────────────────────────────────────────────────

// ValidateLS 验证 LS 已知映射（KnownLSLeagueMap）
//
// 参数：
//   - tournamentID: LS tournament_id
//   - tsCompetitionID: TS competition_id（来自 KnownLSLeagueMap）
//   - sport: "football" / "basketball"
//   - events: 本次比赛匹配结果（用于计算 RCR）
//
// 返回值：
//   - status: 验证状态
//   - adjustedConf: 调整后的联赛置信度（原始为 1.0，SUSPECT 时扣减 suspectPenalty）
//   - rcr: 本次计算的反向确认率
func (v *KnownLeagueMapValidator) ValidateLS(
	tournamentID, tsCompetitionID, sport string,
	events []LSEventMatch,
) (status ValidationStatus, adjustedConf, rcr float64) {
	return v.validate("ls", tournamentID, tsCompetitionID, sport, len(events), countLSMatched(events), computeRCRFromLS(events))
}

// ValidateSR 验证 SR 已知映射（KnownLeagueMap）
//
// 参数：
//   - tournamentID: SR tournament_id
//   - tsCompetitionID: TS competition_id（来自 KnownLeagueMap）
//   - sport: "football" / "basketball"
//   - events: 本次比赛匹配结果（用于计算 RCR）
func (v *KnownLeagueMapValidator) ValidateSR(
	tournamentID, tsCompetitionID, sport string,
	events []EventMatch,
) (status ValidationStatus, adjustedConf, rcr float64) {
	return v.validate("sr", tournamentID, tsCompetitionID, sport, len(events), countSRMatched(events), ComputeReverseConfirmRateSR(events))
}

// validate 内部通用验证逻辑
func (v *KnownLeagueMapValidator) validate(
	sourceSide, tournamentID, tsCompetitionID, sport string,
	eventTotal, eventMatched int,
	rcr float64,
) (status ValidationStatus, adjustedConf float64, outRCR float64) {
	key := mapKey(sourceSide, sport, tournamentID)
	outRCR = rcr

	// 检查人工豁免
	v.mu.RLock()
	isOverride := v.overrides[key]
	threshold := v.reviewThreshold
	penalty := v.suspectPenalty
	v.mu.RUnlock()

	if isOverride {
		_ = v.writeLog(key, sourceSide, tournamentID, tsCompetitionID, sport,
			eventTotal, eventMatched, rcr, ValidationStatusOverride, "人工豁免，跳过自动验证", false)
		return ValidationStatusOverride, 1.0, rcr
	}

	// 比赛数量不足，无法有效验证
	if eventTotal < minEventsForValidation {
		note := fmt.Sprintf("比赛数量不足（%d < %d），跳过验证", eventTotal, minEventsForValidation)
		_ = v.writeLog(key, sourceSide, tournamentID, tsCompetitionID, sport,
			eventTotal, eventMatched, rcr, ValidationStatusInsufficient, note, false)
		return ValidationStatusInsufficient, 1.0, rcr
	}

	// 判断 RCR 是否达标
	if rcr < threshold {
		note := fmt.Sprintf("RCR=%.3f < threshold=%.3f，映射待复核（%s:%s → %s）",
			rcr, threshold, sourceSide, tournamentID, tsCompetitionID)
		log.Printf("[KnownMapValidator] ⚠️  %s", note)
		_ = v.writeLog(key, sourceSide, tournamentID, tsCompetitionID, sport,
			eventTotal, eventMatched, rcr, ValidationStatusSuspect, note, false)

		// 降低联赛置信度
		adjustedConf = math.Round((1.0-penalty)*1000) / 1000
		return ValidationStatusSuspect, adjustedConf, rcr
	}

	note := fmt.Sprintf("RCR=%.3f >= threshold=%.3f，映射验证通过", rcr, threshold)
	_ = v.writeLog(key, sourceSide, tournamentID, tsCompetitionID, sport,
		eventTotal, eventMatched, rcr, ValidationStatusOK, note, false)
	return ValidationStatusOK, 1.0, rcr
}

// writeLog 写入验证日志（INSERT，不更新旧记录，保留历史趋势）
func (v *KnownLeagueMapValidator) writeLog(
	key, sourceSide, tournamentID, tsCompetitionID, sport string,
	eventTotal, eventMatched int,
	rcr float64,
	status ValidationStatus,
	note string,
	isOverride bool,
) error {
	overrideInt := 0
	if isOverride {
		overrideInt = 1
	}
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s
			(map_key, src_tournament_id, ts_competition_id, sport, source_side,
			 event_total, event_matched, rcr, status, note, is_override, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, validationLogTable)

	_, err := v.db.Exec(insertSQL,
		key, tournamentID, tsCompetitionID, sport, sourceSide,
		eventTotal, eventMatched, rcr, string(status), note, overrideInt,
		time.Now(),
	)
	if err != nil {
		log.Printf("[KnownMapValidator] 写入验证日志失败: %v", err)
	}

	// 异步清理超出上限的旧记录（保留最新 validationLogMaxRows 条）
	go v.pruneOldLogs(key)
	return err
}

// pruneOldLogs 清理同一 mapKey 超出上限的旧记录
func (v *KnownLeagueMapValidator) pruneOldLogs(key string) {
	pruneSQL := fmt.Sprintf(`
		DELETE FROM %s
		WHERE map_key = ? AND id NOT IN (
			SELECT id FROM (
				SELECT id FROM %s
				WHERE map_key = ?
				ORDER BY created_at DESC
				LIMIT %d
			) AS t
		)`, validationLogTable, validationLogTable, validationLogMaxRows)
	_, _ = v.db.Exec(pruneSQL, key, key)
}

// ─────────────────────────────────────────────────────────────────────────────
// 人工干预
// ─────────────────────────────────────────────────────────────────────────────

// MarkManualOverride 将某个已知映射标记为人工确认豁免。
// 调用后，该映射的 RCR 验证将被永久跳过（直到调用 ClearOverride）。
func (v *KnownLeagueMapValidator) MarkManualOverride(sourceSide, sport, tournamentID, tsCompetitionID, reason string) error {
	key := mapKey(sourceSide, sport, tournamentID)
	note := fmt.Sprintf("人工确认豁免: %s", reason)
	if err := v.writeLog(key, sourceSide, tournamentID, tsCompetitionID, sport,
		0, 0, 0, ValidationStatusOverride, note, true); err != nil {
		return err
	}
	v.mu.Lock()
	v.overrides[key] = true
	v.mu.Unlock()
	log.Printf("[KnownMapValidator] ✅ 已标记人工豁免: %s (%s)", key, reason)
	return nil
}

// ClearOverride 清除某个映射的人工豁免标记，恢复自动验证
func (v *KnownLeagueMapValidator) ClearOverride(sourceSide, sport, tournamentID string) error {
	key := mapKey(sourceSide, sport, tournamentID)
	clearSQL := fmt.Sprintf(`UPDATE %s SET is_override=0 WHERE map_key=?`, validationLogTable)
	if _, err := v.db.Exec(clearSQL, key); err != nil {
		return fmt.Errorf("ClearOverride: %w", err)
	}
	v.mu.Lock()
	delete(v.overrides, key)
	v.mu.Unlock()
	log.Printf("[KnownMapValidator] 已清除人工豁免: %s", key)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 查询与统计
// ─────────────────────────────────────────────────────────────────────────────

// GetRecentRCR 查询某个映射最近 N 次验证的平均 RCR
func (v *KnownLeagueMapValidator) GetRecentRCR(sourceSide, sport, tournamentID string, n int) (avgRCR float64, count int, err error) {
	key := mapKey(sourceSide, sport, tournamentID)
	query := fmt.Sprintf(`
		SELECT AVG(rcr), COUNT(*)
		FROM (
			SELECT rcr FROM %s
			WHERE map_key=? AND status NOT IN ('OVERRIDE', 'INSUFFICIENT')
			ORDER BY created_at DESC
			LIMIT ?
		) AS t`, validationLogTable)
	err = v.db.QueryRow(query, key, n).Scan(&avgRCR, &count)
	return
}

// ListSuspectMappings 列出近期 RCR 低于阈值的待复核映射
func (v *KnownLeagueMapValidator) ListSuspectMappings() ([]ValidationRecord, error) {
	query := fmt.Sprintf(`
		SELECT id, map_key, src_tournament_id, ts_competition_id, sport, source_side,
		       event_total, event_matched, rcr, status, note, created_at
		FROM %s
		WHERE status='SUSPECT'
		GROUP BY map_key
		ORDER BY rcr ASC, created_at DESC`, validationLogTable)

	rows, err := v.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("ListSuspectMappings: %w", err)
	}
	defer rows.Close()

	var records []ValidationRecord
	for rows.Next() {
		var r ValidationRecord
		if err := rows.Scan(
			&r.ID, &r.MapKey, &r.SrcTournamentID, &r.TSCompetitionID,
			&r.Sport, &r.SourceSide, &r.EventTotal, &r.EventMatched,
			&r.RCR, &r.Status, &r.Note, &r.CreatedAt,
		); err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// 内部辅助函数
// ─────────────────────────────────────────────────────────────────────────────

// computeRCRFromLS 从 LSEventMatch 列表计算 RCR（复用 ComputeReverseConfirmRate）
func computeRCRFromLS(events []LSEventMatch) float64 {
	return ComputeReverseConfirmRate(events)
}

// countLSMatched 统计 LSEventMatch 中成功匹配的比赛数
func countLSMatched(events []LSEventMatch) int {
	count := 0
	for _, ev := range events {
		if ev.Matched {
			count++
		}
	}
	return count
}

// countSRMatched 统计 EventMatch 中成功匹配的比赛数
func countSRMatched(events []EventMatch) int {
	count := 0
	for _, ev := range events {
		if ev.Matched {
			count++
		}
	}
	return count
}
