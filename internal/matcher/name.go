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
