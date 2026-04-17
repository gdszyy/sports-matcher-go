// Package matcher — 名称归一化与相似度计算
package matcher

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// normalizeName 将名称归一化：小写、去变音符号、去标点、合并空格
func normalizeName(s string) string {
	// Unicode NFC 规范化，然后去掉组合字符（变音符号）
	t := norm.NFD.String(s)
	var b strings.Builder
	for _, r := range t {
		if unicode.Is(unicode.Mn, r) {
			continue // 跳过组合字符（变音符号）
		}
		b.WriteRune(r)
	}
	s = b.String()

	// 转小写
	s = strings.ToLower(s)

	// 替换常见分隔符为空格
	replacer := strings.NewReplacer(
		"·", " ", ".", " ", "-", " ", "_", " ", ",", " ", "'", "", "\"", "",
	)
	s = replacer.Replace(s)

	// 合并多余空格
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// tokenSet 将名称拆分为 token 集合
func tokenSet(s string) map[string]bool {
	tokens := strings.Fields(normalizeName(s))
	set := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		if len(t) > 0 {
			set[t] = true
		}
	}
	return set
}

// jaccardSimilarity 计算两个字符串的 Jaccard 相似度（基于 token）
func jaccardSimilarity(a, b string) float64 {
	setA := tokenSet(a)
	setB := tokenSet(b)
	if len(setA) == 0 && len(setB) == 0 {
		return 1.0
	}
	if len(setA) == 0 || len(setB) == 0 {
		return 0.0
	}

	intersection := 0
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	return float64(intersection) / float64(union)
}

// nameCandidates 生成名称的多种候选形式（用于球员名称多格式匹配）
// 处理：先姓后名/先名后姓/去中间名/token 排序
func nameCandidates(name string) []string {
	norm := normalizeName(name)
	tokens := strings.Fields(norm)

	candidates := []string{norm}

	if len(tokens) >= 2 {
		// token 排序版（消除姓名顺序差异）
		sorted := sortedTokens(tokens)
		candidates = append(candidates, strings.Join(sorted, " "))

		// 只取首尾两个 token（去中间名）
		if len(tokens) > 2 {
			firstLast := tokens[0] + " " + tokens[len(tokens)-1]
			candidates = append(candidates, firstLast)
			// 排序版的首尾
			candidates = append(candidates, sorted[0]+" "+sorted[len(sorted)-1])
		}

		// 姓名反转（last first → first last）
		reversed := reverseTokens(tokens)
		candidates = append(candidates, strings.Join(reversed, " "))
	}

	return dedupStrings(candidates)
}

// sortedTokens 返回排序后的 token 列表（字典序）
func sortedTokens(tokens []string) []string {
	cp := make([]string, len(tokens))
	copy(cp, tokens)
	// 简单冒泡排序（token 数量少，不需要 sort 包）
	for i := 0; i < len(cp); i++ {
		for j := i + 1; j < len(cp); j++ {
			if cp[i] > cp[j] {
				cp[i], cp[j] = cp[j], cp[i]
			}
		}
	}
	return cp
}

// reverseTokens 反转 token 列表
func reverseTokens(tokens []string) []string {
	cp := make([]string, len(tokens))
	for i, t := range tokens {
		cp[len(tokens)-1-i] = t
	}
	return cp
}

// dedupStrings 去重字符串切片
func dedupStrings(ss []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// playerNameSimilarity 计算两个球员名称的最大相似度（多候选组合）
func playerNameSimilarity(srName, tsName string) float64 {
	srCandidates := nameCandidates(srName)
	tsCandidates := nameCandidates(tsName)

	maxSim := 0.0
	for _, a := range srCandidates {
		for _, b := range tsCandidates {
			sim := jaccardSimilarity(a, b)
			if sim > maxSim {
				maxSim = sim
			}
		}
	}
	return maxSim
}

// teamNameSimilarity 计算球队名称相似度，取原始 Jaccard 和辅助归一化后 Jaccard 的最大値。
//
// 【辅助归一化优先级说明】
// normalizedTeamSimilarity 作为辅助分数，与原始 Jaccard 取最大値。
// 它不单独决定匹配结果，主匹配引擎的优先级顺序为：
//  1. 预设映射表（KnownLeagueMap / KnownTeamMap）
//  2. TeamAliasIndex 动态别名学习
//  3. 本函数归一化后的 Jaccard 相似度（辅助/尼底）
func teamNameSimilarity(a, b string) float64 {
	// 直接 Jaccard
	direct := jaccardSimilarity(a, b)

	// 【辅助规则 - 低优先级】归一化后 Jaccard（去俘乐部类型缩写、变音符、特殊符号等）
	normalized := normalizedTeamSimilarity(a, b)

	if normalized > direct {
		return normalized
	}
	return direct
}

// cleanTeamName 去掉球队名称中的常见后缀/前缀（已被 NormalizeTeamName 替代，保留为兼容）
func cleanTeamName(name string) string {
	return NormalizeTeamName(name, false)
}

// ─────────────────────────────────────────────────────────────────────────────
// Jaro-Winkler 相似度（TODO-003，P0 阶段）
// 对短名称和前缀匹配比 Jaccard 更敏感，与 Jaccard 取最大值作为最终相似度
// ─────────────────────────────────────────────────────────────────────────────

// jaroSimilarity 计算两个字符串的 Jaro 相似度
// 公式：Jaro(s1,s2) = (m/|s1| + m/|s2| + (m-t)/m) / 3
// 其中 m 为匹配字符数，t 为转置数（匹配字符顺序不同的对数/2）
func jaroSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	r1 := []rune(s1)
	r2 := []rune(s2)
	l1, l2 := len(r1), len(r2)
	if l1 == 0 || l2 == 0 {
		return 0.0
	}

	// 匹配窗口大小：max(l1,l2)/2 - 1
	maxL := l1
	if l2 > maxL {
		maxL = l2
	}
	window := maxL/2 - 1
	if window < 0 {
		window = 0
	}

	matched1 := make([]bool, l1)
	matched2 := make([]bool, l2)

	matches := 0
	for i := 0; i < l1; i++ {
		start := i - window
		if start < 0 {
			start = 0
		}
		end := i + window + 1
		if end > l2 {
			end = l2
		}
		for j := start; j < end; j++ {
			if !matched2[j] && r1[i] == r2[j] {
				matched1[i] = true
				matched2[j] = true
				matches++
				break
			}
		}
	}

	if matches == 0 {
		return 0.0
	}

	// 计算转置数
	transpositions := 0
	k := 0
	for i := 0; i < l1; i++ {
		if !matched1[i] {
			continue
		}
		for !matched2[k] {
			k++
		}
		if r1[i] != r2[k] {
			transpositions++
		}
		k++
	}

	m := float64(matches)
	jaro := (m/float64(l1) + m/float64(l2) + (m-float64(transpositions)/2)/m) / 3.0
	return jaro
}

// jaroWinklerSimilarity 计算两个字符串的 Jaro-Winkler 相似度
// 在 Jaro 基础上对公共前缀给予额外加分（p=0.1，最多 4 个字符前缀）
// 公式：JW(s1,s2) = Jaro + l * p * (1 - Jaro)
func jaroWinklerSimilarity(s1, s2 string) float64 {
	jaro := jaroSimilarity(s1, s2)

	// 计算公共前缀长度（最多 4 个字符）
	r1 := []rune(s1)
	r2 := []rune(s2)
	prefixLen := 0
	maxPrefix := 4
	for i := 0; i < len(r1) && i < len(r2) && prefixLen < maxPrefix; i++ {
		if r1[i] == r2[i] {
			prefixLen++
		} else {
			break
		}
	}

	const p = 0.1
	return jaro + float64(prefixLen)*p*(1.0-jaro)
}

// nameSimilarityMax 计算两个名称的最大相似度：取 Jaccard 和 Jaro-Winkler 的最大值
// 适用于联赛名称、球队名称等短文本场景
func nameSimilarityMax(a, b string) float64 {
	normA := normalizeName(a)
	normB := normalizeName(b)

	jaccard := jaccardSimilarity(normA, normB)
	jw := jaroWinklerSimilarity(normA, normB)

	if jw > jaccard {
		return jw
	}
	return jaccard
}
