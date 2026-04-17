// Package matcher — Fellegi-Sunter 模型（无监督 EM 参数估计）
//
// TODO-016: 实现 Fellegi-Sunter 概率记录链接模型，通过无监督 EM 算法自动估计
// 各比较字段（球队名称、时间差、联赛层级等）在"真实匹配"和"非匹配"两个分布下
// 的参数，替代当前手动调参的固定权重方案。
//
// # 理论背景
//
// Fellegi-Sunter（FS）模型将记录对 (a, b) 的匹配判定建模为二分类问题：
//   - M 类（真实匹配）：两条记录描述同一实体
//   - U 类（非匹配）：两条记录描述不同实体
//
// 对于每个比较字段 f_i，定义：
//   - m_i = P(γ_i = agree | M)：真实匹配时字段一致的概率
//   - u_i = P(γ_i = agree | U)：非匹配时字段一致的概率
//
// 匹配权重（对数似然比）：
//
//	w_i = log2(m_i / u_i)   （一致时）
//	w_i = log2((1-m_i) / (1-u_i))  （不一致时）
//
// 综合匹配分数 = Σ w_i，通过阈值 μ 和 λ 划分为 Match/Possible/NonMatch 三区间。
//
// # EM 算法
//
// 由于真实标签未知，使用期望最大化（EM）算法无监督估计 m_i 和 u_i：
//
//  1. E 步：根据当前参数计算每对记录属于 M 类的后验概率 P(M | γ)
//  2. M 步：用加权计数更新 m_i 和 u_i
//  3. 重复直到收敛（Δ < tolerance）
//
// # 字段定义
//
// 本实现支持以下比较字段（FieldID）：
//   - FSFieldHomeName: 主队名称相似度（连续值 → 离散化为 agree/partial/disagree）
//   - FSFieldAwayName: 客队名称相似度
//   - FSFieldTimeDiff: 时间差（连续值 → 离散化为 exact/close/far/very_far）
//   - FSFieldLeagueTier: 联赛层级是否一致（二值）
//   - FSFieldSport: 运动类型是否一致（二值，强约束）
//
// # 与现有流程的集成
//
// FSModel 作为 MatchEvents 的**后置置信度校准器**：
//
//	原流程：综合分 = 0.30*S_time + 0.70*S_name（固定权重）
//	新流程：综合分 = FSModel.Score(γ)（EM 估计的动态权重）
//
// FSModel 可以从历史匹配结果中增量学习，也可以在每次联赛匹配任务开始时
// 用当前联赛的比赛对进行热启动（warm-start EM）。
package matcher

import (
	"math"
)

// ─────────────────────────────────────────────────────────────────────────────
// 字段 ID 与离散化级别
// ─────────────────────────────────────────────────────────────────────────────

// FSFieldID Fellegi-Sunter 比较字段 ID
type FSFieldID int

const (
	FSFieldHomeName   FSFieldID = iota // 主队名称相似度
	FSFieldAwayName                    // 客队名称相似度
	FSFieldTimeDiff                    // 时间差
	FSFieldLeagueTier                  // 联赛层级一致性
	FSFieldSport                       // 运动类型一致性
	fsFieldCount                       // 字段总数（内部使用）
)

// FSLevel 字段比较结果的离散化级别
type FSLevel int

const (
	FSLevelAgree    FSLevel = 2 // 完全一致（高相似度 / 时间精确）
	FSLevelPartial  FSLevel = 1 // 部分一致（中等相似度 / 时间接近）
	FSLevelDisagree FSLevel = 0 // 不一致（低相似度 / 时间偏差大）
)

// fsLevelCount 每个字段的离散化级别数（3 级：agree/partial/disagree）
const fsLevelCount = 3

// ─────────────────────────────────────────────────────────────────────────────
// 比较向量
// ─────────────────────────────────────────────────────────────────────────────

// FSComparison 一对记录的比较向量（每个字段的离散化级别）
type FSComparison struct {
	Fields [fsFieldCount]FSLevel
}

// CompareEventPair 将一对 SR/TS 比赛的原始特征转换为 FS 比较向量。
//
// 参数：
//   - homeNameSim: 主队名称相似度 ∈ [0, 1]
//   - awayNameSim: 客队名称相似度 ∈ [0, 1]
//   - timeDiffSec: 时间差（秒）
//   - leagueTierMatch: 联赛层级是否一致
//   - sportMatch: 运动类型是否一致
func CompareEventPair(
	homeNameSim, awayNameSim float64,
	timeDiffSec int64,
	leagueTierMatch, sportMatch bool,
) FSComparison {
	var c FSComparison

	// 主队名称离散化
	c.Fields[FSFieldHomeName] = discretizeNameSim(homeNameSim)

	// 客队名称离散化
	c.Fields[FSFieldAwayName] = discretizeNameSim(awayNameSim)

	// 时间差离散化
	c.Fields[FSFieldTimeDiff] = discretizeTimeDiff(timeDiffSec)

	// 联赛层级（二值）
	if leagueTierMatch {
		c.Fields[FSFieldLeagueTier] = FSLevelAgree
	} else {
		c.Fields[FSFieldLeagueTier] = FSLevelDisagree
	}

	// 运动类型（二值，强约束）
	if sportMatch {
		c.Fields[FSFieldSport] = FSLevelAgree
	} else {
		c.Fields[FSFieldSport] = FSLevelDisagree
	}

	return c
}

// discretizeNameSim 将名称相似度连续值离散化为 3 级
func discretizeNameSim(sim float64) FSLevel {
	switch {
	case sim >= 0.80:
		return FSLevelAgree
	case sim >= 0.50:
		return FSLevelPartial
	default:
		return FSLevelDisagree
	}
}

// discretizeTimeDiff 将时间差离散化为 3 级
func discretizeTimeDiff(diffSec int64) FSLevel {
	abs := diffSec
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs <= 1800: // ≤ 30 分钟：精确
		return FSLevelAgree
	case abs <= 21600: // ≤ 6 小时：接近
		return FSLevelPartial
	default: // > 6 小时：偏差大
		return FSLevelDisagree
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// FSModel — Fellegi-Sunter 模型
// ─────────────────────────────────────────────────────────────────────────────

// FSParams 单个字段的 FS 参数
type FSParams struct {
	// M[level] = P(γ=level | M)：真实匹配时各级别的概率
	M [fsLevelCount]float64
	// U[level] = P(γ=level | U)：非匹配时各级别的概率
	U [fsLevelCount]float64
}

// FSModel Fellegi-Sunter 概率记录链接模型。
//
// 使用方式：
//  1. 调用 NewFSModel() 创建模型（使用默认先验参数）
//  2. 调用 FitEM(comparisons) 用 EM 算法拟合参数
//  3. 调用 Score(comparison) 计算匹配分数
//  4. 调用 Classify(score) 判断匹配类型
type FSModel struct {
	// Params 各字段的 FS 参数（EM 估计后更新）
	Params [fsFieldCount]FSParams
	// Pi 先验匹配概率 P(M)
	Pi float64
	// MaxIter EM 最大迭代次数
	MaxIter int
	// Tolerance EM 收敛阈值（相邻两次迭代参数变化量）
	Tolerance float64
	// MatchThreshold 匹配分数阈值（高于此值判定为 Match）
	MatchThreshold float64
	// NonMatchThreshold 非匹配分数阈值（低于此值判定为 NonMatch）
	NonMatchThreshold float64
}

// FSMatchClass 匹配分类结果
type FSMatchClass int

const (
	FSClassMatch    FSMatchClass = 2 // 确定匹配
	FSClassPossible FSMatchClass = 1 // 可能匹配（需人工研判）
	FSClassNonMatch FSMatchClass = 0 // 确定非匹配
)

// NewFSModel 创建 Fellegi-Sunter 模型（使用经验先验参数）。
//
// 先验参数基于体育赛事数据的经验分布设置：
//   - 主/客队名称：真实匹配时高度一致（m_agree≈0.85），非匹配时低一致（u_agree≈0.05）
//   - 时间差：真实匹配时精确（m_agree≈0.70），非匹配时随机（u_agree≈0.10）
//   - 联赛层级：真实匹配时一致（m_agree≈0.95），非匹配时随机（u_agree≈0.30）
//   - 运动类型：强约束，真实匹配时必须一致（m_agree≈0.99），非匹配时低（u_agree≈0.20）
func NewFSModel() *FSModel {
	m := &FSModel{
		Pi:                0.05, // 假设 5% 的记录对是真实匹配（稀疏先验）
		MaxIter:           100,
		Tolerance:         1e-4,
		MatchThreshold:    6.0,  // 对数似然比阈值（经验值）
		NonMatchThreshold: -2.0, // 对数似然比阈值（经验值）
	}

	// 主队名称先验
	m.Params[FSFieldHomeName] = FSParams{
		M: [3]float64{0.85, 0.12, 0.03}, // agree/partial/disagree | M
		U: [3]float64{0.05, 0.20, 0.75}, // agree/partial/disagree | U
	}

	// 客队名称先验
	m.Params[FSFieldAwayName] = FSParams{
		M: [3]float64{0.85, 0.12, 0.03},
		U: [3]float64{0.05, 0.20, 0.75},
	}

	// 时间差先验
	m.Params[FSFieldTimeDiff] = FSParams{
		M: [3]float64{0.70, 0.25, 0.05},
		U: [3]float64{0.10, 0.30, 0.60},
	}

	// 联赛层级先验
	m.Params[FSFieldLeagueTier] = FSParams{
		M: [3]float64{0.95, 0.0, 0.05},
		U: [3]float64{0.30, 0.0, 0.70},
	}

	// 运动类型先验（强约束）
	m.Params[FSFieldSport] = FSParams{
		M: [3]float64{0.99, 0.0, 0.01},
		U: [3]float64{0.20, 0.0, 0.80},
	}

	return m
}

// Score 计算比较向量的 FS 匹配分数（对数似然比之和）。
//
// 分数越高表示越可能是真实匹配。
// 分数 = Σ_i log2(P(γ_i | M) / P(γ_i | U))
func (m *FSModel) Score(c FSComparison) float64 {
	score := 0.0
	for fieldID := FSFieldID(0); fieldID < fsFieldCount; fieldID++ {
		level := int(c.Fields[fieldID])
		mProb := m.Params[fieldID].M[level]
		uProb := m.Params[fieldID].U[level]

		// 避免除零和 log(0)
		if mProb < 1e-10 {
			mProb = 1e-10
		}
		if uProb < 1e-10 {
			uProb = 1e-10
		}
		score += math.Log2(mProb / uProb)
	}
	return score
}

// ScoreNormalized 将 FS 分数归一化到 [0, 1] 区间（用于与现有置信度体系对接）。
//
// 使用 sigmoid 函数：P = 1 / (1 + exp(-score/scale))
// scale=3.0 使得分数 ±6 对应约 0.12 和 0.88 的概率。
func (m *FSModel) ScoreNormalized(c FSComparison) float64 {
	score := m.Score(c)
	return 1.0 / (1.0 + math.Exp(-score/3.0))
}

// Classify 根据 FS 分数判断匹配类型
func (m *FSModel) Classify(score float64) FSMatchClass {
	switch {
	case score >= m.MatchThreshold:
		return FSClassMatch
	case score <= m.NonMatchThreshold:
		return FSClassNonMatch
	default:
		return FSClassPossible
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EM 算法
// ─────────────────────────────────────────────────────────────────────────────

// FitEM 使用 EM 算法无监督估计 FS 模型参数。
//
// 参数：
//   - comparisons: 比较向量列表（无标签）
//
// 返回：
//   - iterations: 实际迭代次数
//   - converged: 是否收敛
func (m *FSModel) FitEM(comparisons []FSComparison) (iterations int, converged bool) {
	if len(comparisons) == 0 {
		return 0, true
	}

	n := len(comparisons)
	posteriorM := make([]float64, n) // P(M | γ_i)

	for iter := 0; iter < m.MaxIter; iter++ {
		// ── E 步：计算后验概率 P(M | γ) ──────────────────────────────────────
		for i, c := range comparisons {
			logLR := m.Score(c)
			// P(M | γ) = P(M) * P(γ | M) / P(γ)
			// 使用对数空间避免数值下溢
			// log P(M | γ) ∝ log P(M) + Σ log P(γ_i | M)
			// log P(U | γ) ∝ log P(U) + Σ log P(γ_i | U)
			// logLR = Σ log(P(γ_i|M)/P(γ_i|U)) = log P(γ|M) - log P(γ|U)
			logPM := math.Log(m.Pi) + logLR*math.Log(2) // 转换为自然对数
			logPU := math.Log(1 - m.Pi)
			// P(M|γ) = exp(logPM) / (exp(logPM) + exp(logPU))
			// 使用 log-sum-exp 技巧
			maxLog := logPM
			if logPU > maxLog {
				maxLog = logPU
			}
			pM := math.Exp(logPM-maxLog) / (math.Exp(logPM-maxLog) + math.Exp(logPU-maxLog))
			posteriorM[i] = pM
		}

		// ── M 步：更新参数 ────────────────────────────────────────────────────
		newParams := [fsFieldCount]FSParams{}
		totalM := 0.0
		totalU := 0.0

		for _, p := range posteriorM {
			totalM += p
			totalU += 1 - p
		}

		// 更新 Pi（先验匹配概率）
		newPi := totalM / float64(n)
		if newPi < 1e-6 {
			newPi = 1e-6
		}
		if newPi > 1-1e-6 {
			newPi = 1 - 1e-6
		}

		// 更新各字段参数
		for fieldID := FSFieldID(0); fieldID < fsFieldCount; fieldID++ {
			levelCountM := [fsLevelCount]float64{}
			levelCountU := [fsLevelCount]float64{}

			for i, c := range comparisons {
				level := int(c.Fields[fieldID])
				levelCountM[level] += posteriorM[i]
				levelCountU[level] += 1 - posteriorM[i]
			}

			// 归一化（加 Laplace 平滑防止零概率）
			for l := 0; l < fsLevelCount; l++ {
				newParams[fieldID].M[l] = (levelCountM[l] + 0.01) / (totalM + 0.01*fsLevelCount)
				newParams[fieldID].U[l] = (levelCountU[l] + 0.01) / (totalU + 0.01*fsLevelCount)
			}
		}

		// ── 收敛检测 ──────────────────────────────────────────────────────────
		maxDelta := math.Abs(newPi - m.Pi)
		for fieldID := FSFieldID(0); fieldID < fsFieldCount; fieldID++ {
			for l := 0; l < fsLevelCount; l++ {
				d := math.Abs(newParams[fieldID].M[l] - m.Params[fieldID].M[l])
				if d > maxDelta {
					maxDelta = d
				}
				d = math.Abs(newParams[fieldID].U[l] - m.Params[fieldID].U[l])
				if d > maxDelta {
					maxDelta = d
				}
			}
		}

		// 更新参数
		m.Pi = newPi
		m.Params = newParams

		if maxDelta < m.Tolerance {
			return iter + 1, true
		}
	}

	return m.MaxIter, false
}

// ─────────────────────────────────────────────────────────────────────────────
// 辅助：从 EventMatch 列表构建比较向量（用于增量学习）
// ─────────────────────────────────────────────────────────────────────────────

// BuildComparisonsFromMatches 从已有的 EventMatch 结果构建 FS 比较向量列表。
//
// 用途：在每次联赛匹配任务完成后，将匹配结果转换为比较向量，
// 供 FSModel.FitEM 进行增量参数更新。
func BuildComparisonsFromMatches(
	matches []EventMatch,
	srTeamNames, tsTeamNames map[string]string,
) []FSComparison {
	comparisons := make([]FSComparison, 0, len(matches))

	for _, m := range matches {
		if !m.Matched {
			continue
		}

		homeNameSim := teamNameSimilarity(m.SRHomeName, tsTeamNames[m.TSHomeID])
		awayNameSim := teamNameSimilarity(m.SRAwayName, tsTeamNames[m.TSAwayID])

		c := CompareEventPair(
			homeNameSim,
			awayNameSim,
			m.TimeDiffSec,
			true,  // 同联赛内匹配，层级一致
			true,  // 同联赛内匹配，运动类型一致
		)
		comparisons = append(comparisons, c)
	}

	return comparisons
}

// BuildComparisonsFromLSMatches 从 LSEventMatch 结果构建 FS 比较向量列表
func BuildComparisonsFromLSMatches(
	matches []LSEventMatch,
	lsTeamNames, tsTeamNames map[string]string,
) []FSComparison {
	comparisons := make([]FSComparison, 0, len(matches))

	for _, m := range matches {
		if !m.Matched {
			continue
		}

		homeNameSim := teamNameSimilarity(m.LSHomeName, tsTeamNames[m.TSHomeID])
		awayNameSim := teamNameSimilarity(m.LSAwayName, tsTeamNames[m.TSAwayID])

		c := CompareEventPair(
			homeNameSim,
			awayNameSim,
			m.TimeDiffSec,
			true,
			true,
		)
		comparisons = append(comparisons, c)
	}

	return comparisons
}

// ─────────────────────────────────────────────────────────────────────────────
// FSModelStore — 联赛级 FS 模型缓存
// ─────────────────────────────────────────────────────────────────────────────

// FSModelStore 为每个联赛维护一个独立的 FS 模型实例，支持增量学习。
//
// key 格式: "{sourceSide}:{sport}:{tournamentID}"
type FSModelStore struct {
	models map[string]*FSModel
}

// NewFSModelStore 创建 FS 模型缓存
func NewFSModelStore() *FSModelStore {
	return &FSModelStore{models: make(map[string]*FSModel)}
}

// GetOrCreate 获取或创建指定联赛的 FS 模型
func (s *FSModelStore) GetOrCreate(key string) *FSModel {
	if m, ok := s.models[key]; ok {
		return m
	}
	m := NewFSModel()
	s.models[key] = m
	return m
}

// UpdateFromMatches 用本次匹配结果更新指定联赛的 FS 模型参数
func (s *FSModelStore) UpdateFromMatches(
	key string,
	matches []EventMatch,
	srTeamNames, tsTeamNames map[string]string,
) (iterations int, converged bool) {
	comparisons := BuildComparisonsFromMatches(matches, srTeamNames, tsTeamNames)
	if len(comparisons) < 5 {
		// 样本不足，跳过更新
		return 0, true
	}
	m := s.GetOrCreate(key)
	return m.FitEM(comparisons)
}

// UpdateFromLSMatches 用 LS 匹配结果更新指定联赛的 FS 模型参数
func (s *FSModelStore) UpdateFromLSMatches(
	key string,
	matches []LSEventMatch,
	lsTeamNames, tsTeamNames map[string]string,
) (iterations int, converged bool) {
	comparisons := BuildComparisonsFromLSMatches(matches, lsTeamNames, tsTeamNames)
	if len(comparisons) < 5 {
		return 0, true
	}
	m := s.GetOrCreate(key)
	return m.FitEM(comparisons)
}
