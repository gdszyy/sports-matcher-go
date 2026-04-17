// Package matcher — 稠密分块（DenseBlocking）
//
// TODO-015: 引入 Entity2Vec + HNSW 向量检索的稠密分块，替代传统基于字符串前缀/
// 倒排索引的稀疏分块（Blocking）方案。
//
// # 背景
//
// 传统 Blocking 方案（如 Q-gram 倒排索引）在球队/联赛名称高度缩写、多语言混合
// 的场景下召回率不足。例如：
//   - "Man Utd" vs "Manchester United"（缩写 vs 全称）
//   - "Bayer 04" vs "Bayer Leverkusen"（数字后缀 vs 城市名）
//   - "FC Bayern München" vs "Bayern Munich"（德语 vs 英语）
//
// 稠密分块通过将实体名称映射到低维稠密向量空间，利用向量近邻搜索（HNSW）在
// 候选生成阶段大幅提升召回率，同时通过 Top-K 截断控制候选集规模。
//
// # 架构设计
//
// 本实现采用**纯 Go 实现**的轻量级方案，不依赖外部向量数据库：
//
//  1. **Entity2Vec 编码器**：基于字符 n-gram（trigram）的 TF-IDF 加权向量，
//     维度固定为 256（哈希桶），通过 L2 归一化转换为单位向量。
//     这是一种轻量级的近似 Entity2Vec，无需预训练，适合冷启动场景。
//
//  2. **HNSW 近邻图**：基于 NSW（Navigable Small World）图的层次化近邻搜索，
//     支持 O(log N) 的近似最近邻查询。本实现为简化版（单层 NSW），
//     在实体数量 < 10000 的场景下性能足够。
//
//  3. **DenseBlocker**：对外暴露的主接口，封装编码器和 HNSW 图，
//     提供 `Build`（建立索引）和 `Query`（Top-K 候选检索）两个核心方法。
//
// # 与现有流程的集成
//
// DenseBlocker 作为 MatchEvents 的**前置候选过滤器**：
//
//	原流程：SR 比赛 × TS 比赛（全量笛卡尔积）
//	新流程：SR 比赛 → DenseBlocker.Query → Top-K TS 候选 → 精排（现有策略1/2/3/4）
//
// 当 TS 比赛数量 < denseBlockingMinCandidates 时，自动退化为全量笛卡尔积（兼容小联赛）。
package matcher

import (
	"hash/fnv"
	"math"
	"sort"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// 常量
// ─────────────────────────────────────────────────────────────────────────────

const (
	// vecDim Entity2Vec 向量维度（哈希桶数量）
	vecDim = 256

	// ngramSize n-gram 大小（trigram）
	ngramSize = 3

	// denseBlockingMinCandidates 触发稠密分块的最少 TS 候选数
	// 低于此值时退化为全量笛卡尔积
	denseBlockingMinCandidates = 20

	// defaultTopK 默认 Top-K 候选数
	defaultTopK = 10

	// hsnwM NSW 图每个节点的最大邻居数
	hsnwM = 16
)

// ─────────────────────────────────────────────────────────────────────────────
// 向量类型
// ─────────────────────────────────────────────────────────────────────────────

// Vec256 256 维单精度向量（L2 归一化后为单位向量）
type Vec256 [vecDim]float32

// dot 计算两个单位向量的点积（等价于余弦相似度）
func (a Vec256) dot(b Vec256) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// l2norm 对向量进行 L2 归一化（原地修改）
func (v *Vec256) l2norm() {
	var sum float32
	for _, x := range v {
		sum += x * x
	}
	if sum == 0 {
		return
	}
	inv := float32(1.0 / math.Sqrt(float64(sum)))
	for i := range v {
		v[i] *= inv
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Entity2Vec 编码器（基于 trigram TF-IDF 的轻量级实现）
// ─────────────────────────────────────────────────────────────────────────────

// encodeEntity 将实体名称编码为 256 维单位向量。
//
// 编码流程：
//  1. 名称归一化（小写 + 去除非字母数字字符）
//  2. 提取所有 trigram（3-gram）
//  3. 每个 trigram 通过 FNV-32a 哈希映射到 [0, vecDim) 的桶
//  4. 对桶内计数进行 TF 加权（词频 / 总 trigram 数）
//  5. L2 归一化为单位向量
func encodeEntity(name string) Vec256 {
	normalized := normalizeForVec(name)
	if len(normalized) == 0 {
		return Vec256{}
	}

	var vec Vec256
	runes := []rune(normalized)
	total := 0

	for i := 0; i <= len(runes)-ngramSize; i++ {
		gram := string(runes[i : i+ngramSize])
		h := fnvHash(gram) % vecDim
		vec[h]++
		total++
	}

	// 单字符或双字符名称（trigram 不足）：退化为 bigram
	if total == 0 {
		for i := 0; i < len(runes)-1; i++ {
			gram := string(runes[i : i+2])
			h := fnvHash(gram) % vecDim
			vec[h]++
			total++
		}
	}

	// TF 归一化
	if total > 0 {
		inv := float32(1.0 / float64(total))
		for i := range vec {
			vec[i] *= inv
		}
	}

	vec.l2norm()
	return vec
}

// normalizeForVec 将名称归一化为小写字母+数字序列（去除空格和特殊字符）
func normalizeForVec(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			sb.WriteRune(' ')
		}
	}
	return strings.TrimSpace(sb.String())
}

// fnvHash FNV-32a 哈希（非加密，速度快）
func fnvHash(s string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32())
}

// ─────────────────────────────────────────────────────────────────────────────
// NSW 近邻图（简化版单层 HNSW）
// ─────────────────────────────────────────────────────────────────────────────

// nswNode NSW 图中的一个节点
type nswNode struct {
	id        int     // 节点 ID（对应 entities 数组下标）
	vec       Vec256  // 实体向量
	neighbors []int   // 邻居节点 ID 列表（最多 hsnwM 个）
}

// nswGraph 简化版 NSW 图（单层）
type nswGraph struct {
	nodes    []*nswNode
	entryIDs []int // 入口节点 ID 列表（随机采样，提高搜索多样性）
}

// newNSWGraph 创建空的 NSW 图
func newNSWGraph() *nswGraph {
	return &nswGraph{}
}

// insert 向 NSW 图中插入一个新节点
func (g *nswGraph) insert(id int, vec Vec256) {
	node := &nswNode{id: id, vec: vec}

	if len(g.nodes) == 0 {
		g.nodes = append(g.nodes, node)
		g.entryIDs = append(g.entryIDs, id)
		return
	}

	// 找到当前图中最近的 hsnwM 个邻居
	neighbors := g.searchKNN(vec, hsnwM, g.entryIDs)
	for _, nb := range neighbors {
		node.neighbors = append(node.neighbors, nb.id)
		// 双向连接（保持图的连通性）
		nbNode := g.nodes[nb.id]
		if len(nbNode.neighbors) < hsnwM {
			nbNode.neighbors = append(nbNode.neighbors, id)
		}
	}

	g.nodes = append(g.nodes, node)

	// 每 hsnwM 个节点更新一次入口节点（保持入口多样性）
	if len(g.nodes)%hsnwM == 0 {
		g.entryIDs = append(g.entryIDs, id)
	}
}

// searchKNN 在 NSW 图中搜索 k 个最近邻（贪心搜索）
func (g *nswGraph) searchKNN(query Vec256, k int, entryIDs []int) []scoredID {
	if len(g.nodes) == 0 {
		return nil
	}

	visited := make(map[int]bool)
	candidates := make([]scoredID, 0, k*2)

	// 从所有入口节点开始
	for _, eid := range entryIDs {
		if eid >= len(g.nodes) {
			continue
		}
		sim := query.dot(g.nodes[eid].vec)
		candidates = append(candidates, scoredID{id: eid, score: float64(sim)})
		visited[eid] = true
	}

	// 贪心扩展：每次选取当前最优候选的邻居
	for iter := 0; iter < len(g.nodes) && iter < 200; iter++ {
		if len(candidates) == 0 {
			break
		}
		// 取当前最优候选
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].score > candidates[j].score
		})
		best := candidates[0]
		if best.id >= len(g.nodes) {
			break
		}
		bestNode := g.nodes[best.id]

		improved := false
		for _, nbID := range bestNode.neighbors {
			if visited[nbID] || nbID >= len(g.nodes) {
				continue
			}
			visited[nbID] = true
			sim := query.dot(g.nodes[nbID].vec)
			candidates = append(candidates, scoredID{id: nbID, score: float64(sim)})
			if float64(sim) > best.score {
				improved = true
			}
		}

		if !improved {
			break
		}
	}

	// 返回 Top-K
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > k {
		candidates = candidates[:k]
	}
	return candidates
}

// scoredID 带评分的 ID
type scoredID struct {
	id    int
	score float64
}

// ─────────────────────────────────────────────────────────────────────────────
// DenseBlocker — 稠密分块主接口
// ─────────────────────────────────────────────────────────────────────────────

// BlockCandidate 稠密分块候选结果
type BlockCandidate struct {
	// Index 候选实体在原始列表中的下标
	Index int
	// CosineSim 与查询实体的余弦相似度（越高越相似）
	CosineSim float64
}

// DenseBlocker 基于 Entity2Vec + NSW 图的稠密分块器。
//
// 典型用法：
//
//	blocker := NewDenseBlocker()
//	blocker.Build(tsTeamNames)  // 用 TS 侧实体建立索引
//	candidates := blocker.Query("Man Utd", 10)  // 查询 Top-10 候选
type DenseBlocker struct {
	graph    *nswGraph
	entities []string // 原始实体名称列表（与 graph.nodes 下标对应）
	vecs     []Vec256 // 预计算的实体向量
	topK     int
}

// NewDenseBlocker 创建稠密分块器（默认 Top-K=10）
func NewDenseBlocker() *DenseBlocker {
	return &DenseBlocker{
		graph: newNSWGraph(),
		topK:  defaultTopK,
	}
}

// NewDenseBlockerWithK 创建指定 Top-K 的稠密分块器
func NewDenseBlockerWithK(k int) *DenseBlocker {
	return &DenseBlocker{
		graph: newNSWGraph(),
		topK:  k,
	}
}

// Build 用实体名称列表建立 NSW 索引。
// entities: 实体名称列表（如 TS 侧球队名称）
func (b *DenseBlocker) Build(entities []string) {
	b.entities = make([]string, len(entities))
	b.vecs = make([]Vec256, len(entities))
	b.graph = newNSWGraph()

	for i, name := range entities {
		b.entities[i] = name
		vec := encodeEntity(name)
		b.vecs[i] = vec
		b.graph.insert(i, vec)
	}
}

// Query 查询与给定名称最相似的 Top-K 候选实体。
//
// 返回值按余弦相似度降序排列。
// 若索引为空或实体数量不足 denseBlockingMinCandidates，返回所有实体（全量模式）。
func (b *DenseBlocker) Query(name string, k int) []BlockCandidate {
	if len(b.entities) == 0 {
		return nil
	}

	// 实体数量不足时退化为全量线性扫描
	if len(b.entities) < denseBlockingMinCandidates {
		return b.linearScan(name, len(b.entities))
	}

	queryVec := encodeEntity(name)
	entryIDs := b.graph.entryIDs
	if len(entryIDs) == 0 && len(b.graph.nodes) > 0 {
		entryIDs = []int{0}
	}

	results := b.graph.searchKNN(queryVec, k, entryIDs)
	candidates := make([]BlockCandidate, 0, len(results))
	for _, r := range results {
		candidates = append(candidates, BlockCandidate{
			Index:     r.id,
			CosineSim: r.score,
		})
	}
	return candidates
}

// QueryDefault 使用默认 Top-K 查询候选
func (b *DenseBlocker) QueryDefault(name string) []BlockCandidate {
	return b.Query(name, b.topK)
}

// linearScan 全量线性扫描（退化模式）
func (b *DenseBlocker) linearScan(name string, k int) []BlockCandidate {
	queryVec := encodeEntity(name)
	results := make([]scoredID, len(b.entities))
	for i, vec := range b.vecs {
		results[i] = scoredID{id: i, score: float64(queryVec.dot(vec))}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	if len(results) > k {
		results = results[:k]
	}
	candidates := make([]BlockCandidate, len(results))
	for i, r := range results {
		candidates[i] = BlockCandidate{Index: r.id, CosineSim: r.score}
	}
	return candidates
}

// Len 返回索引中的实体数量
func (b *DenseBlocker) Len() int {
	return len(b.entities)
}

// EntityName 返回指定下标的实体名称
func (b *DenseBlocker) EntityName(idx int) string {
	if idx < 0 || idx >= len(b.entities) {
		return ""
	}
	return b.entities[idx]
}

// ─────────────────────────────────────────────────────────────────────────────
// 与 MatchEvents 的集成辅助函数
// ─────────────────────────────────────────────────────────────────────────────

// BuildTSEventBlocker 为 TS 比赛列表建立稠密分块索引。
//
// 索引键为 "{主队名} vs {客队名}"，用于快速过滤候选比赛。
// 返回的 DenseBlocker 可在 MatchEvents 的每个 SR 比赛查询时复用。
func BuildTSEventBlocker(tsTeamNames map[string]string, tsMatchIDs []string, tsHomeIDs, tsAwayIDs []string) *DenseBlocker {
	entities := make([]string, len(tsMatchIDs))
	for i := range tsMatchIDs {
		homeName := tsTeamNames[tsHomeIDs[i]]
		awayName := tsTeamNames[tsAwayIDs[i]]
		entities[i] = homeName + " vs " + awayName
	}
	blocker := NewDenseBlocker()
	blocker.Build(entities)
	return blocker
}

// QueryTSCandidates 查询与 SR 比赛最相似的 TS 候选比赛下标列表。
//
// srHomeName/srAwayName: SR 侧主客队名称
// topK: 返回的候选数量
func QueryTSCandidates(blocker *DenseBlocker, srHomeName, srAwayName string, topK int) []int {
	query := srHomeName + " vs " + srAwayName
	candidates := blocker.Query(query, topK)
	indices := make([]int, len(candidates))
	for i, c := range candidates {
		indices[i] = c.Index
	}
	return indices
}
