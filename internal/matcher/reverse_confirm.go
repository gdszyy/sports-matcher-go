// Package matcher — 比赛反向确认率（TODO-010，P1 阶段）
//
// 反向确认率（ReverseConfirmRate）定义：
//   RCR = 成功匹配的比赛数 / min(源A比赛总数, 源B比赛总数)
//
// 语义：在两侧比赛数量的"公共上限"中，有多少比例被成功匹配。
// 用途：
//  1. 作为联赛匹配置信度的辅助信号（高 RCR 表明联赛匹配正确）
//  2. 用于自动验证 KnownLSLeagueMap / KnownLeagueMap 中的已知映射
//  3. 在 LSMatchStats 中作为质量指标输出
//
// 与比赛匹配率（EventMatchRate）的区别：
//   - EventMatchRate = 已匹配比赛数 / 源A比赛总数（单侧视角）
//   - ReverseConfirmRate = 已匹配比赛数 / min(源A, 源B)（双侧视角，更严格）
package matcher

import "math"

// ─────────────────────────────────────────────────────────────────────────────
// LS 链路反向确认率
// ─────────────────────────────────────────────────────────────────────────────

// ComputeReverseConfirmRate 计算 LS↔TS 比赛匹配的反向确认率
//
// 参数：
//   - events: LSEventMatch 列表（包含 LS 侧和 TS 侧的比赛数据）
//
// 返回值：
//   - RCR ∈ [0.0, 1.0]，保留 3 位小数
//   - 若 events 为空则返回 0.0
func ComputeReverseConfirmRate(events []LSEventMatch) float64 {
	if len(events) == 0 {
		return 0.0
	}

	// 统计 LS 侧比赛总数（即 events 长度）
	lsTotal := len(events)

	// 统计 TS 侧出现的唯一比赛数（避免一对多映射导致高估）
	tsMatchIDs := make(map[string]bool)
	matchedCount := 0
	for _, ev := range events {
		if ev.Matched && ev.TSMatchID != "" {
			tsMatchIDs[ev.TSMatchID] = true
			matchedCount++
		}
	}
	tsTotal := len(tsMatchIDs)

	if lsTotal == 0 || tsTotal == 0 {
		return 0.0
	}

	// min(lsTotal, tsTotal) 作为分母
	minTotal := lsTotal
	if tsTotal < minTotal {
		minTotal = tsTotal
	}

	rcr := float64(matchedCount) / float64(minTotal)
	if rcr > 1.0 {
		rcr = 1.0
	}
	return math.Round(rcr*1000) / 1000
}

// ─────────────────────────────────────────────────────────────────────────────
// SR 链路反向确认率
// ─────────────────────────────────────────────────────────────────────────────

// ComputeReverseConfirmRateSR 计算 SR↔TS 比赛匹配的反向确认率
//
// 参数：
//   - events: EventMatch 列表（SR 链路）
//
// 返回值：
//   - RCR ∈ [0.0, 1.0]，保留 3 位小数
func ComputeReverseConfirmRateSR(events []EventMatch) float64 {
	if len(events) == 0 {
		return 0.0
	}

	srTotal := len(events)

	tsMatchIDs := make(map[string]bool)
	matchedCount := 0
	for _, ev := range events {
		if ev.Matched && ev.TSMatchID != "" {
			tsMatchIDs[ev.TSMatchID] = true
			matchedCount++
		}
	}
	tsTotal := len(tsMatchIDs)

	if srTotal == 0 || tsTotal == 0 {
		return 0.0
	}

	minTotal := srTotal
	if tsTotal < minTotal {
		minTotal = tsTotal
	}

	rcr := float64(matchedCount) / float64(minTotal)
	if rcr > 1.0 {
		rcr = 1.0
	}
	return math.Round(rcr*1000) / 1000
}

// ─────────────────────────────────────────────────────────────────────────────
// 反向确认率等级判断
// ─────────────────────────────────────────────────────────────────────────────

// RCRLevel 反向确认率等级
type RCRLevel string

const (
	RCRLevelHigh   RCRLevel = "HIGH"   // ≥ 0.80：联赛匹配高度可信
	RCRLevelMedium RCRLevel = "MEDIUM" // ≥ 0.50：联赛匹配基本可信
	RCRLevelLow    RCRLevel = "LOW"    // ≥ 0.20：联赛匹配存疑
	RCRLevelFail   RCRLevel = "FAIL"   // < 0.20：联赛匹配可能错误
)

// ClassifyRCR 将反向确认率分级
func ClassifyRCR(rcr float64) RCRLevel {
	switch {
	case rcr >= 0.80:
		return RCRLevelHigh
	case rcr >= 0.50:
		return RCRLevelMedium
	case rcr >= 0.20:
		return RCRLevelLow
	default:
		return RCRLevelFail
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 联赛置信度回灌（反向确认率 → 联赛置信度加成）
// ─────────────────────────────────────────────────────────────────────────────

// RCRLeagueBonus 根据反向确认率计算联赛置信度加成
//
// 设计原则：
//   - 高 RCR（≥0.80）：强烈支持联赛匹配正确，给予最大加成 +0.10
//   - 中 RCR（≥0.50）：支持联赛匹配，给予中等加成 +0.05
//   - 低 RCR（≥0.20）：弱支持，给予小幅加成 +0.02
//   - 极低 RCR（<0.20）：不支持，不加成（可能是联赛错误匹配）
//
// 注意：此函数仅在联赛匹配置信度 < 1.0 时才有意义（已知映射不需要加成）
func RCRLeagueBonus(rcr float64) float64 {
	switch {
	case rcr >= 0.80:
		return 0.10
	case rcr >= 0.50:
		return 0.05
	case rcr >= 0.20:
		return 0.02
	default:
		return 0.0
	}
}

// ApplyRCRToLeague 将反向确认率加成应用到联赛匹配置信度
//
// 参数：
//   - leagueConf: 原始联赛置信度
//   - rcr: 比赛反向确认率
//
// 返回值：
//   - 调整后的联赛置信度（上限 1.0，保留 3 位小数）
func ApplyRCRToLeague(leagueConf, rcr float64) float64 {
	bonus := RCRLeagueBonus(rcr)
	newConf := leagueConf + bonus
	if newConf > 1.0 {
		newConf = 1.0
	}
	return math.Round(newConf*1000) / 1000
}
