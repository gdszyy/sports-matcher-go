// Package matcher — EventDTW 动态时间规整（Dynamic Time Warping）
//
// TODO-017: 实现基于事件流的动态时间规整（EventDTW），用于处理赛程延期 > 72h
// 或时区偏移极端场景下，现有 L1/L2/L3/L4 策略全部失效的漏匹配问题。
//
// # 背景
//
// 标准 DTW 算法用于时间序列对齐，允许序列在时间轴上非线性拉伸/压缩。
// EventDTW 将其适配到体育赛事流场景：
//   - 将一个联赛的所有比赛按开始时间排序，形成"事件序列"
//   - 两条序列（SR 侧 vs TS 侧）可能因赛程延期、补赛等原因产生时间偏移
//   - DTW 通过寻找最优对齐路径，找到全局时间偏移量（anchor offset）
//   - 将 anchor offset 应用到 L4/L5 策略的时间容差中，扩大匹配窗口
//
// # 算法设计
//
// EventDTW 分为两阶段：
//
//  1. **锚点事件提取**（AnchorExtraction）：
//     从 SR 和 TS 序列中提取"高置信度锚点事件"（名称相似度 ≥ 0.85 的候选对）。
//     锚点事件是时间偏移估计的基础，避免全量 DTW 的 O(N²) 复杂度。
//
//  2. **偏移量估计**（OffsetEstimation）：
//     对锚点对的时间差进行中位数估计，得到全局时间偏移量 δt。
//     中位数比均值更鲁棒，能抵抗少量错误锚点的干扰。
//
// # 与现有流程的集成
//
// EventDTW 作为 MatchEvents 的**兜底修正层**，在 L1/L2/L3/L4 全部失败后激活：
//
//	原流程：L1 → L2 → L3 → L4 → L5 → L4b → NoMatch
//	新流程：L1 → L2 → L3 → L4 → L5 → L4b → EventDTW 修正 → 重试 L1/L2 → NoMatch
//
// EventDTW 修正后，将估计的时间偏移量 δt 应用到未匹配的 SR 比赛时间戳上，
// 然后重新尝试 L1/L2 策略。
//
// # 完整 DTW 路径对齐（可选）
//
// 除偏移量估计外，本实现还提供完整的 DTW 路径对齐功能（DTWAlign），
// 用于生成 SR↔TS 比赛的全局最优对齐方案。这在联赛层面的质量评估中有用，
// 但计算复杂度为 O(N*M)，不适合在热路径中使用。
package matcher

import (
	"math"
	"sort"
)

// ─────────────────────────────────────────────────────────────────────────────
// 常量
// ─────────────────────────────────────────────────────────────────────────────

const (
	// dtwAnchorNameThreshold 锚点事件的名称相似度阈值
	dtwAnchorNameThreshold = 0.85

	// dtwMinAnchors 偏移量估计所需的最少锚点数
	dtwMinAnchors = 3

	// dtwMaxTimeDiffSec DTW 锚点对的最大时间差（超过此值视为无效锚点）
	// 设为 7 天（604800s），覆盖极端赛程延期场景
	dtwMaxTimeDiffSec = 604800

	// dtwWindowRatio DTW 路径对齐的 Sakoe-Chiba 窗口比例
	// 窗口大小 = max(N, M) * dtwWindowRatio
	dtwWindowRatio = 0.3

	// dtwInfinity DTW 代价矩阵的无穷大值
	dtwInfinity = 1e18
)

// ─────────────────────────────────────────────────────────────────────────────
// 事件序列元素
// ─────────────────────────────────────────────────────────────────────────────

// DTWEvent DTW 算法中的事件序列元素
type DTWEvent struct {
	// ID 事件 ID（SR event_id 或 TS match_id）
	ID string
	// StartUnix 开始时间（Unix 时间戳，秒）
	StartUnix int64
	// HomeName 主队名称
	HomeName string
	// AwayName 客队名称
	AwayName string
	// HomeID 主队 ID
	HomeID string
	// AwayID 客队 ID
	AwayID string
}

// ─────────────────────────────────────────────────────────────────────────────
// 锚点提取
// ─────────────────────────────────────────────────────────────────────────────

// AnchorPair 一对高置信度锚点事件
type AnchorPair struct {
	// SRIdx SR 序列中的下标
	SRIdx int
	// TSIdx TS 序列中的下标
	TSIdx int
	// TimeDiff TS.StartUnix - SR.StartUnix（秒，可为负）
	TimeDiff int64
	// NameSim 名称相似度（主客队平均）
	NameSim float64
}

// ExtractAnchors 从 SR 和 TS 事件序列中提取高置信度锚点对。
//
// 算法：
//  1. 对每个 SR 事件，在 TS 序列中寻找名称相似度 ≥ dtwAnchorNameThreshold 的候选
//  2. 若候选唯一（无歧义），则记录为锚点对
//  3. 过滤时间差 > dtwMaxTimeDiffSec 的异常锚点
func ExtractAnchors(srEvents, tsEvents []DTWEvent) []AnchorPair {
	anchors := make([]AnchorPair, 0)

	for srIdx, sr := range srEvents {
		var bestTS int = -1
		var bestSim float64 = -1
		var ambiguous bool

		for tsIdx, ts := range tsEvents {
			homeSim := teamNameSimilarity(sr.HomeName, ts.HomeName)
			awaySim := teamNameSimilarity(sr.AwayName, ts.AwayName)
			// 也检查主客场互换的情况
			homeSimRev := teamNameSimilarity(sr.HomeName, ts.AwayName)
			awaySimRev := teamNameSimilarity(sr.AwayName, ts.HomeName)

			fwdSim := (homeSim + awaySim) / 2.0
			revSim := (homeSimRev + awaySimRev) / 2.0
			sim := fwdSim
			if revSim > fwdSim {
				sim = revSim
			}

			if sim >= dtwAnchorNameThreshold {
				if bestTS == -1 {
					bestTS = tsIdx
					bestSim = sim
				} else {
					// 存在多个高相似度候选，标记为歧义
					ambiguous = true
					break
				}
			}
		}

		if bestTS == -1 || ambiguous {
			continue
		}

		timeDiff := tsEvents[bestTS].StartUnix - sr.StartUnix
		if timeDiff < 0 {
			timeDiff = -timeDiff
		}
		if timeDiff > dtwMaxTimeDiffSec {
			continue
		}

		anchors = append(anchors, AnchorPair{
			SRIdx:    srIdx,
			TSIdx:    bestTS,
			TimeDiff: tsEvents[bestTS].StartUnix - sr.StartUnix, // 有符号
			NameSim:  bestSim,
		})
	}

	return anchors
}

// ─────────────────────────────────────────────────────────────────────────────
// 偏移量估计
// ─────────────────────────────────────────────────────────────────────────────

// DTWOffsetResult 时间偏移量估计结果
type DTWOffsetResult struct {
	// OffsetSec 估计的全局时间偏移量（秒，TS 时间 - SR 时间）
	// 正值表示 TS 时间比 SR 时间晚，负值表示 TS 时间比 SR 时间早
	OffsetSec int64
	// AnchorCount 用于估计的锚点数量
	AnchorCount int
	// MAD 绝对偏差中位数（Median Absolute Deviation），反映偏移量的稳定性
	// MAD 越小表示偏移量越一致（越可信）
	MAD int64
	// Valid 是否有效（锚点数量 ≥ dtwMinAnchors）
	Valid bool
}

// EstimateOffset 使用锚点对估计全局时间偏移量。
//
// 使用中位数估计（鲁棒于异常值）。
func EstimateOffset(anchors []AnchorPair) DTWOffsetResult {
	if len(anchors) < dtwMinAnchors {
		return DTWOffsetResult{Valid: false, AnchorCount: len(anchors)}
	}

	// 提取所有时间差
	diffs := make([]int64, len(anchors))
	for i, a := range anchors {
		diffs[i] = a.TimeDiff
	}

	// 计算中位数
	sort.Slice(diffs, func(i, j int) bool { return diffs[i] < diffs[j] })
	n := len(diffs)
	var median int64
	if n%2 == 0 {
		median = (diffs[n/2-1] + diffs[n/2]) / 2
	} else {
		median = diffs[n/2]
	}

	// 计算 MAD（绝对偏差中位数）
	absDevs := make([]int64, len(diffs))
	for i, d := range diffs {
		dev := d - median
		if dev < 0 {
			dev = -dev
		}
		absDevs[i] = dev
	}
	sort.Slice(absDevs, func(i, j int) bool { return absDevs[i] < absDevs[j] })
	var mad int64
	if len(absDevs)%2 == 0 {
		mad = (absDevs[len(absDevs)/2-1] + absDevs[len(absDevs)/2]) / 2
	} else {
		mad = absDevs[len(absDevs)/2]
	}

	return DTWOffsetResult{
		OffsetSec:   median,
		AnchorCount: len(anchors),
		MAD:         mad,
		Valid:       true,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 完整 DTW 路径对齐
// ─────────────────────────────────────────────────────────────────────────────

// DTWAlignPair DTW 对齐路径中的一对
type DTWAlignPair struct {
	SRIdx int
	TSIdx int
	Cost  float64 // 对齐代价（越小越好）
}

// DTWAlign 对 SR 和 TS 事件序列执行完整的 DTW 路径对齐。
//
// 使用 Sakoe-Chiba 带宽约束（窗口大小 = max(N,M) * dtwWindowRatio）
// 将复杂度从 O(N*M) 降低到 O(N*W)。
//
// 代价函数：1 - (名称相似度 + 时间相似度) / 2
// 时间相似度使用高斯衰减：S_time = exp(-Δt² / (2 * 86400²))（σ=1天）
func DTWAlign(srEvents, tsEvents []DTWEvent) []DTWAlignPair {
	n, m := len(srEvents), len(tsEvents)
	if n == 0 || m == 0 {
		return nil
	}

	// 计算 Sakoe-Chiba 窗口大小
	maxNM := n
	if m > maxNM {
		maxNM = m
	}
	window := int(math.Ceil(float64(maxNM) * dtwWindowRatio))
	if window < 1 {
		window = 1
	}

	// 初始化代价矩阵
	dp := make([][]float64, n)
	for i := range dp {
		dp[i] = make([]float64, m)
		for j := range dp[i] {
			dp[i][j] = dtwInfinity
		}
	}

	// 填充代价矩阵（带 Sakoe-Chiba 窗口约束）
	for i := 0; i < n; i++ {
		jMin := i - window
		if jMin < 0 {
			jMin = 0
		}
		jMax := i + window
		if jMax >= m {
			jMax = m - 1
		}

		for j := jMin; j <= jMax; j++ {
			cost := dtwCost(srEvents[i], tsEvents[j])

			prev := dtwInfinity
			if i == 0 && j == 0 {
				prev = 0
			} else if i == 0 {
				if dp[i][j-1] < dtwInfinity {
					prev = dp[i][j-1]
				}
			} else if j == 0 {
				if dp[i-1][j] < dtwInfinity {
					prev = dp[i-1][j]
				}
			} else {
				if dp[i-1][j] < prev {
					prev = dp[i-1][j]
				}
				if dp[i][j-1] < prev {
					prev = dp[i][j-1]
				}
				if dp[i-1][j-1] < prev {
					prev = dp[i-1][j-1]
				}
			}

			if prev < dtwInfinity {
				dp[i][j] = cost + prev
			}
		}
	}

	// 回溯最优路径
	pairs := make([]DTWAlignPair, 0, maxNM)
	i, j := n-1, m-1

	for i > 0 || j > 0 {
		pairs = append(pairs, DTWAlignPair{
			SRIdx: i,
			TSIdx: j,
			Cost:  dtwCost(srEvents[i], tsEvents[j]),
		})

		if i == 0 {
			j--
		} else if j == 0 {
			i--
		} else {
			// 选择代价最小的前驱
			diagCost := dp[i-1][j-1]
			upCost := dp[i-1][j]
			leftCost := dp[i][j-1]

			if diagCost <= upCost && diagCost <= leftCost {
				i--
				j--
			} else if upCost <= leftCost {
				i--
			} else {
				j--
			}
		}
	}
	pairs = append(pairs, DTWAlignPair{SRIdx: 0, TSIdx: 0, Cost: dtwCost(srEvents[0], tsEvents[0])})

	// 反转路径（从起点到终点）
	for left, right := 0, len(pairs)-1; left < right; left, right = left+1, right-1 {
		pairs[left], pairs[right] = pairs[right], pairs[left]
	}

	return pairs
}

// dtwCost 计算两个事件之间的 DTW 代价。
//
// 代价 = 1 - (名称相似度 + 时间相似度) / 2
// 代价范围 [0, 1]，越小表示越相似。
func dtwCost(sr, ts DTWEvent) float64 {
	// 名称相似度（主客队平均，考虑主客场互换）
	fwdSim := (teamNameSimilarity(sr.HomeName, ts.HomeName) +
		teamNameSimilarity(sr.AwayName, ts.AwayName)) / 2.0
	revSim := (teamNameSimilarity(sr.HomeName, ts.AwayName) +
		teamNameSimilarity(sr.AwayName, ts.HomeName)) / 2.0
	nameSim := fwdSim
	if revSim > fwdSim {
		nameSim = revSim
	}

	// 时间相似度（高斯衰减，σ=1天=86400s）
	dt := float64(sr.StartUnix - ts.StartUnix)
	if dt < 0 {
		dt = -dt
	}
	const sigma = 86400.0
	timeSim := math.Exp(-(dt * dt) / (2.0 * sigma * sigma))

	return 1.0 - (nameSim+timeSim)/2.0
}

// ─────────────────────────────────────────────────────────────────────────────
// 与 MatchEvents 的集成辅助函数
// ─────────────────────────────────────────────────────────────────────────────

// ApplyDTWOffset 将 DTW 估计的时间偏移量应用到未匹配的 SR 事件时间戳上。
//
// 返回修正后的 SR 事件列表（原列表不修改）。
// 调用方可将修正后的列表重新传入 MatchEvents 进行第二轮匹配。
func ApplyDTWOffset(srEvents []DTWEvent, offsetSec int64) []DTWEvent {
	corrected := make([]DTWEvent, len(srEvents))
	for i, ev := range srEvents {
		corrected[i] = ev
		corrected[i].StartUnix = ev.StartUnix + offsetSec
	}
	return corrected
}

// SREventsToDTW 将 db.SREvent 列表转换为 DTWEvent 列表（按开始时间排序）
func SREventsToDTW(srEvents []SREventForDTW) []DTWEvent {
	dtwEvents := make([]DTWEvent, len(srEvents))
	for i, ev := range srEvents {
		dtwEvents[i] = DTWEvent{
			ID:        ev.ID,
			StartUnix: ev.StartUnix,
			HomeName:  ev.HomeName,
			AwayName:  ev.AwayName,
			HomeID:    ev.HomeID,
			AwayID:    ev.AwayID,
		}
	}
	sort.Slice(dtwEvents, func(i, j int) bool {
		return dtwEvents[i].StartUnix < dtwEvents[j].StartUnix
	})
	return dtwEvents
}

// TSEventsToDTW 将 TS 比赛列表转换为 DTWEvent 列表（按开始时间排序）
func TSEventsToDTW(tsEvents []TSEventForDTW) []DTWEvent {
	dtwEvents := make([]DTWEvent, len(tsEvents))
	for i, ev := range tsEvents {
		dtwEvents[i] = DTWEvent{
			ID:        ev.ID,
			StartUnix: ev.StartUnix,
			HomeName:  ev.HomeName,
			AwayName:  ev.AwayName,
			HomeID:    ev.HomeID,
			AwayID:    ev.AwayID,
		}
	}
	sort.Slice(dtwEvents, func(i, j int) bool {
		return dtwEvents[i].StartUnix < dtwEvents[j].StartUnix
	})
	return dtwEvents
}

// SREventForDTW DTW 转换所需的 SR 事件最小接口
type SREventForDTW struct {
	ID        string
	StartUnix int64
	HomeName  string
	AwayName  string
	HomeID    string
	AwayID    string
}

// TSEventForDTW DTW 转换所需的 TS 事件最小接口
type TSEventForDTW struct {
	ID        string
	StartUnix int64
	HomeName  string
	AwayName  string
	HomeID    string
	AwayID    string
}

// ─────────────────────────────────────────────────────────────────────────────
// EventDTWMatcher — 完整的 DTW 兜底匹配器
// ─────────────────────────────────────────────────────────────────────────────

// EventDTWMatcher 封装 DTW 兜底匹配的完整流程：
//  1. 提取锚点
//  2. 估计时间偏移量
//  3. 修正 SR 时间戳
//  4. 返回修正后的 SR 事件列表供重新匹配
type EventDTWMatcher struct {
	// MinAnchors 最少锚点数（低于此值则认为 DTW 不可靠）
	MinAnchors int
	// MaxMADSec MAD 上限（超过此值则认为偏移量不稳定）
	MaxMADSec int64
}

// NewEventDTWMatcher 创建 EventDTW 兜底匹配器
func NewEventDTWMatcher() *EventDTWMatcher {
	return &EventDTWMatcher{
		MinAnchors: dtwMinAnchors,
		MaxMADSec:  3600 * 6, // MAD ≤ 6 小时才认为偏移量可信
	}
}

// TryCorrect 尝试对 SR 事件序列进行 DTW 时间修正。
//
// 返回：
//   - corrected: 修正后的 SR 事件列表（若 DTW 不可靠则返回原列表）
//   - offset: 估计的时间偏移量结果
//   - applied: 是否实际应用了修正
func (m *EventDTWMatcher) TryCorrect(
	srEvents, tsEvents []DTWEvent,
) (corrected []DTWEvent, offset DTWOffsetResult, applied bool) {
	anchors := ExtractAnchors(srEvents, tsEvents)
	offset = EstimateOffset(anchors)

	if !offset.Valid {
		return srEvents, offset, false
	}

	if offset.AnchorCount < m.MinAnchors {
		return srEvents, offset, false
	}

	if offset.MAD > m.MaxMADSec {
		return srEvents, offset, false
	}

	// 偏移量过小（< 5 分钟），不需要修正
	absOffset := offset.OffsetSec
	if absOffset < 0 {
		absOffset = -absOffset
	}
	if absOffset < 300 {
		return srEvents, offset, false
	}

	corrected = ApplyDTWOffset(srEvents, offset.OffsetSec)
	return corrected, offset, true
}
