// team_name_normalizer.go — 球队名称辅助归一化层
//
// 【重要说明】
// 本文件提供的归一化规则属于「辅助性预处理」，优先级低于主匹配引擎的以下机制：
//  1. 预设映射表（KnownLeagueMap / KnownTeamMap）            ← 最高优先级
//  2. TeamAliasIndex 动态别名学习（跨比赛队伍 ID 映射）      ← 次高优先级
//  3. 本文件：名称归一化后的 Jaccard 相似度                  ← 辅助/兜底
//
// 【关键字匹配规则】
// 所有俱乐部类型缩写（如 "FC"、"SC"、"CF" 等）的匹配必须满足严格词边界：
//  - 关键字左侧必须是字符串开头或空格（\b）
//  - 关键字右侧必须是字符串结尾或空格（\b）
//  - 不允许匹配某个单词的中间部分
//    例如："SC" 不能匹配 "Braunschweig" 中的 "sc"
//          "FC" 不能匹配 "AFC" 中的 "FC"（AFC 有自己的规则）
//
// 【归纳自真实数据的差异模式（v3 匹配结果，4545 对队名差异）】
//  1. 俱乐部类型缩写差异（占比最高，~60%）
//  2. 赞助商冠名差异（篮球联赛为主，~20%）
//  3. 城市名/地域名差异（~10%）
//  4. 本地语言 vs 英语翻译差异（~5%）
//  5. 特殊字符与变音符差异（~5%）
package matcher

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
	"unicode"
)

// ─────────────────────────────────────────────────────────────────────────────
// 俱乐部类型缩写规则（严格词边界）
// ─────────────────────────────────────────────────────────────────────────────

// clubTypeSuffix 描述一个俱乐部类型缩写规则
type clubTypeSuffix struct {
	abbr    string
	meaning string
	lang    string
	re      *regexp.Regexp
}

// clubTypeSuffixes 是所有已知的俱乐部类型缩写。
// 【辅助规则 - 低优先级】
// 较长的缩写排在前面，防止短缩写先匹配导致误剥离（如 "AFC" 必须在 "FC" 之前）。
// 所有正则均使用 (?i) 大小写不敏感 + \b 严格词边界。
var clubTypeSuffixes = func() []clubTypeSuffix {
	defs := []struct{ abbr, meaning, lang string }{
		// ── 足球 ──
		{"AFC", "Association Football Club", "英语"},
		{"RFC", "Royal/Rugby Football Club", "英语"},
		{"GNK", "Građanski Nogometni Klub", "克罗地亚语"},
		{"HNK", "Hrvatski Nogometni Klub", "克罗地亚语"},
		{"KRC", "Koninklijke Racing Club", "比利时/荷兰语"},
		{"PFC", "Professional Football Club", "斯拉夫语系"},
		{"SSC", "Società Sportiva Calcio", "意大利语"},
		{"ACF", "Associazione Calcio Fiorentina", "意大利语"},
		{"RCD", "Reial Club Deportiu", "加泰罗尼亚语"},
		{"OSC", "Olympique Sporting Club", "法语"},
		{"FSV", "Fußball- und Sportverein", "德语"},
		{"TSV", "Turn- und Sportverein", "德语"},
		{"VfL", "Verein für Leibesübungen", "德语"},
		{"VfB", "Verein für Bewegungsspiele", "德语"},
		{"IFK", "Idrottsföreningen Kamraterna", "瑞典语"},
		{"AIK", "Allmänna Idrottsklubben", "瑞典语"},
		{"BSV", "Ballspiel-Verein", "德语"},
		{"SL", "Sport Lisboa / Sporting Lisboa", "葡萄牙语"},
		{"AC", "Associazione Calcio / Athletic Club", "意大利语/英语"},
		{"FK", "Football Club / Fotballklubb", "斯拉夫/北欧语"},
		{"NK", "Nogometni Klub", "克罗地亚/斯洛文尼亚语"},
		{"SK", "Sports Club / Sportovní Klub", "斯堪的纳维亚/捷克语"},
		{"SV", "Sportverein", "德语"},
		{"FC", "Football Club", "英语/通用"},
		{"SC", "Sporting Club / Sports Club", "通用"},
		{"CF", "Club de Fútbol", "西班牙语"},
		{"UD", "Unión Deportiva", "西班牙语"},
		{"SD", "Sociedad Deportiva", "西班牙语"},
		{"CD", "Club Deportivo", "西班牙语"},
		{"RC", "Racing Club", "法语/西班牙语"},
		{"AS", "Association Sportive", "法语/意大利语"},
		{"US", "Unione Sportiva", "意大利语"},
		// ── 篮球 ──
		{"KK", "Košarkaški Klub", "巴尔干地区"},
		{"BC", "Basketball Club", "英语/通用"},
		{"BK", "Basketball Club / Boldspill Klub", "斯堪的纳维亚语"},
		{"CB", "Club de Baloncesto", "西班牙语"},
	}
	result := make([]clubTypeSuffix, 0, len(defs))
	for _, d := range defs {
		result = append(result, clubTypeSuffix{
			abbr:    d.abbr,
			meaning: d.meaning,
			lang:    d.lang,
			re:      regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(d.abbr) + `\b`),
		})
	}
	return result
}()

// ─────────────────────────────────────────────────────────────────────────────
// 赞助商冠名规则
// ─────────────────────────────────────────────────────────────────────────────

// sponsorWords 是已知的赞助商冠名词汇。
// 【辅助规则 - 低优先级】
// TS 库通常包含最新的赞助商冠名，而 SR 往往只保留原始队名。
// 数据来源：v3 匹配结果中 4545 对队名差异的赞助商冠名样本。
var sponsorPatterns = func() []*regexp.Regexp {
	words := []string{
		// CBA（中国篮球）
		"Guangsha", "Shougang", "Kentier", "Hi-Speed", "Jinqiang", "Xunxing", "Fenjiu",
		// 德篮甲
		"EnBW", "Telekom", "Ratiopharm", "EWE",
		// 意篮甲
		"Snaidero", "Guerino",
		// 欧冠篮/其他
		"Mozzart", "Beko", "Red Bull", "Dreamland", "CEZ", "DKV", "Unicaja",
	}
	result := make([]*regexp.Regexp, 0, len(words))
	for _, w := range words {
		result = append(result, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(w)+`\b`))
	}
	return result
}()

// ─────────────────────────────────────────────────────────────────────────────
// 语言别名规则
// ─────────────────────────────────────────────────────────────────────────────

// languageAlias 描述一个本地语言 → 英语的别名映射
type languageAlias struct {
	src string
	dst string
	re  *regexp.Regexp
}

// languageAliases 将本地语言拼写统一为英语标准写法。
// 【辅助规则 - 低优先级】
// 仅覆盖在 v3 匹配结果中实际出现的差异，不做大规模扩展。
var languageAliases = func() []languageAlias {
	defs := []struct{ src, dst string }{
		{"Milano", "Milan"},        // 意大利语→英语
		{"Praha", "Prague"},        // 捷克语→英语
		{"Wien", "Vienna"},         // 德语→英语
		{"Koln", "Cologne"},        // 德语→英语（去变音符后）
		{"Saloniki", "Thessaloniki"}, // 希腊语缩写→英语
		{"Beograd", "Belgrade"},    // 塞尔维亚语→英语
		{"Moskva", "Moscow"},       // 俄语→英语
	}
	result := make([]languageAlias, 0, len(defs))
	for _, d := range defs {
		result = append(result, languageAlias{
			src: d.src,
			dst: d.dst,
			re:  regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(d.src) + `\b`),
		})
	}
	return result
}()

// 预编译其他特殊正则
var (
	// 去开头数字序号（"1. FC Cologne" → "FC Cologne"）
	reLeadingNumber = regexp.MustCompile(`^\d+\.?\s*`)
	// 北欧地名 oe→o（词尾，如 Bodoe→Bodo）
	reNordicOE = regexp.MustCompile(`oe\b`)
	// 连字符 Saint-（"Paris Saint-Germain" → "Paris Saint Germain"）
	reSaintHyphen = regexp.MustCompile(`(?i)\bSaint-`)
	// 合并多余空格
	reMultiSpace = regexp.MustCompile(`\s+`)
)

// ─────────────────────────────────────────────────────────────────────────────
// flattenDiacritics 将变音符展平为基础 ASCII 字符
// ─────────────────────────────────────────────────────────────────────────────

func flattenDiacritics(s string) string {
	t := norm.NFD.String(s)
	var b strings.Builder
	for _, r := range t {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// stripClubTypeSuffix 剥离俱乐部类型缩写（严格词边界）
// ─────────────────────────────────────────────────────────────────────────────

// stripClubTypeSuffix 使用严格词边界正则剥离队名中的俱乐部类型缩写。
//
// 【辅助规则 - 低优先级】
// 示例：
//
//	"Chelsea FC"      → "Chelsea"
//	"AFC Bournemouth" → "Bournemouth"
//	"SSC Napoli"      → "Napoli"
//	"VfL Wolfsburg"   → "Wolfsburg"
//	"Rasta Vechta"    → "Rasta Vechta"  ← 不误匹配（"ta" 不是词边界）
//	"Schalke 04"      → "Schalke 04"   ← 不误匹配（"SC" 不在词边界）
func stripClubTypeSuffix(name string) string {
	result := name
	for _, s := range clubTypeSuffixes {
		result = s.re.ReplaceAllString(result, "")
	}
	result = strings.TrimSpace(reMultiSpace.ReplaceAllString(result, " "))
	if result == "" {
		return name
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// stripSponsor 剥离赞助商冠名词汇
// ─────────────────────────────────────────────────────────────────────────────

// stripSponsor 剥离队名中已知的赞助商冠名词汇。
//
// 【辅助规则 - 低优先级】
// 示例：
//
//	"Zhejiang Guangsha Lions"  → "Zhejiang Lions"
//	"Telekom Baskets Bonn"     → "Baskets Bonn"
//	"Red Bull Salzburg"        → "Salzburg"
func stripSponsor(name string) string {
	result := name
	for _, p := range sponsorPatterns {
		result = p.ReplaceAllString(result, "")
	}
	result = strings.TrimSpace(reMultiSpace.ReplaceAllString(result, " "))
	if result == "" {
		return name
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// applyLanguageAliases 应用语言别名替换
// ─────────────────────────────────────────────────────────────────────────────

func applyLanguageAliases(name string) string {
	result := name
	for _, a := range languageAliases {
		result = a.re.ReplaceAllString(result, a.dst)
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// NormalizeTeamName 主入口：辅助归一化流水线
// ─────────────────────────────────────────────────────────────────────────────

// NormalizeTeamName 对球队名称执行完整的辅助归一化流水线。
//
// 【辅助规则 - 低优先级】
// 本函数的输出仅用于辅助 Jaccard 相似度计算，不应单独决定匹配结果。
// 主匹配引擎的优先级顺序为：
//  1. 预设映射表（KnownLeagueMap / KnownTeamMap）
//  2. TeamAliasIndex 动态别名学习
//  3. 本函数归一化后的 Jaccard 相似度（辅助/兜底）
//
// 归一化步骤：
//  1. 变音符展平（é→e, ü→u）
//  2. 特殊符号统一（& → 空格, / → 空格, . → 去掉, 连字符Saint- → Saint 空格）
//  3. 去开头数字序号（"1. FC Cologne" → "FC Cologne"）
//  4. 语言别名替换（Milano → Milan）
//  5. 俱乐部类型缩写剥离（FC、SC、BC 等，严格词边界）
//  6. 北欧地名 oe→o（词尾，Bodoe → Bodo）
//  7. 转小写 + 合并空格
//
// withSponsorStrip 为 true 时额外执行赞助商剥离（默认建议 false，有误匹配风险）。
func NormalizeTeamName(name string, withSponsorStrip bool) string {
	if name == "" {
		return ""
	}

	// Step 1: 变音符展平
	result := flattenDiacritics(name)

	// Step 2: 特殊符号统一
	result = strings.ReplaceAll(result, "&", " ")  // Brighton & Hove → Brighton Hove
	result = strings.ReplaceAll(result, "/", " ")  // Bodoe/Glimt → Bodoe Glimt
	result = strings.ReplaceAll(result, ".", "")   // A.F.C. → AFC
	result = reSaintHyphen.ReplaceAllString(result, "Saint ") // Saint-Germain → Saint Germain

	// Step 3: 去开头数字序号
	result = reLeadingNumber.ReplaceAllString(result, "") // 1. FC Cologne → FC Cologne

	// Step 4: 语言别名替换
	result = applyLanguageAliases(result)

	// Step 5: 赞助商剥离（可选）
	if withSponsorStrip {
		result = stripSponsor(result)
	}

	// Step 6: 俱乐部类型缩写剥离（严格词边界）
	result = stripClubTypeSuffix(result)

	// Step 7: 北欧地名 oe→o（词尾）
	result = reNordicOE.ReplaceAllString(result, "o") // Bodoe → Bodo

	// Step 8: 转小写 + 合并空格
	result = strings.ToLower(result)
	result = strings.TrimSpace(reMultiSpace.ReplaceAllString(result, " "))

	if result == "" {
		return strings.ToLower(name)
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// normalizedTeamSimilarity 归一化后的 Jaccard 相似度（辅助分数）
// ─────────────────────────────────────────────────────────────────────────────

// normalizedTeamSimilarity 对两个球队名称分别执行 NormalizeTeamName，
// 然后计算 Jaccard 相似度。
//
// 【辅助规则 - 低优先级】
// 此函数的结果应作为辅助分数，与原始 Jaccard 分数取最大值，
// 而非直接替换主匹配引擎的得分。
func normalizedTeamSimilarity(nameA, nameB string) float64 {
	na := NormalizeTeamName(nameA, false)
	nb := NormalizeTeamName(nameB, false)
	return jaccardSimilarity(na, nb)
}
