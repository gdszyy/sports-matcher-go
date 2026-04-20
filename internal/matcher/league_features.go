// Package matcher — 联赛名称结构化特征提取
//
// 本文件实现 TODO-001（P0 阶段）：将联赛名称拆解为
// {国家} + {赛事体系} + {层级} + {性别} + {年龄段} + {区域分区} + {赛制类型}
// 的结构化特征向量，供强约束一票否决机制使用。
//
// 关联文档：
//   - docs/league_guard_keywords.json  （关键词词典真相源）
//   - docs/universal_matching_algorithm_design.md §3.1.3
//   - .cursor/rules/process_insights/PI-001_universal_matching_algorithm_design.md §坑1
package matcher

import (
	"regexp"
	"strconv"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// 数据结构
// ─────────────────────────────────────────────────────────────────────────────

// LeagueGender 联赛性别维度
type LeagueGender int

const (
	GenderUnknown LeagueGender = iota
	GenderMen
	GenderWomen
)

// LeagueFeatures 联赛名称结构化特征向量（六维强约束 + 辅助信息）
type LeagueFeatures struct {
	// 强约束维度（任意一维冲突即一票否决）
	Gender          LeagueGender // 性别：Unknown / Men / Women
	AgeGroup        string       // 年龄段：空字符串 / "u23" / "u21" / "u19" / "youth" 等
	Region          string       // 区域分区：空字符串 / "north" / "south" / "east" / "west" / "central" 等
	CompetitionType string       // 赛制类型：空字符串 / "cup" / "league" / "short_format" / "playoff" / "friendly"
	TierNumber      int          // 层级数字：0 表示未检测到；1=顶级，2=次级，3=三级，4=四级…
	TierRoman       string       // 层级罗马数字原始值（用于调试）

	// 辅助信息（不参与强约束，但可用于置信度计算）
	NormalizedName string // 归一化后的联赛名称（去除已提取的特征词后的剩余核心名称）
}

// ─────────────────────────────────────────────────────────────────────────────
// 关键词词典（与 docs/league_guard_keywords.json 保持同步）
// ─────────────────────────────────────────────────────────────────────────────

var womenKeywords = []string{
	"women", "woman", "ladies", "feminine", "female", "girls",
	"femenino", "feminin", "donne", "frauen", "vrouwen", "mulheres",
	"naiset", "damer",
}

var menKeywords = []string{
	"men", "man", "male", "boys",
	"masculino", "masculin", "uomini", "männer", "mannen",
	"homens", "miehet", "herrar",
}

// ageGroupKeywords key=规范化年龄段标签，value=匹配词列表（优先级从高到低）
var ageGroupKeywords = map[string][]string{
	"u23":   {"u23", "under 23", "under-23", "under23", "u-23"},
	"u22":   {"u22", "under 22", "under-22", "under22", "u-22"},
	"u21":   {"u21", "under 21", "under-21", "under21", "u-21"},
	"u20":   {"u20", "under 20", "under-20", "under20", "u-20"},
	"u19":   {"u19", "under 19", "under-19", "under19", "u-19"},
	"u18":   {"u18", "under 18", "under-18", "under18", "u-18"},
	"u17":   {"u17", "under 17", "under-17", "under17", "u-17"},
	"u16":   {"u16", "under 16", "under-16", "under16", "u-16"},
	"u15":   {"u15", "under 15", "under-15", "under15", "u-15"},
	"u14":   {"u14", "under 14", "under-14", "under14", "u-14"},
	"youth": {"youth", "junior", "juniors", "reserve", "reserves", "academy", "b team", "b-team", "ii team"},
}

// ageGroupOrder 用于按数字从大到小检测（避免 u23 被 u2 误匹配）
var ageGroupOrder = []string{"u23", "u22", "u21", "u20", "u19", "u18", "u17", "u16", "u15", "u14", "youth"}

// regionKeywords key=规范化区域标签
var regionKeywords = map[string][]string{
	"north":     {"north", "northern"},
	"south":     {"south", "southern"},
	"east":      {"east", "eastern"},
	"west":      {"west", "western"},
	"central":   {"central", "center", "centre"},
	"northeast": {"north east", "north-east", "northeast"},
	"northwest": {"north west", "north-west", "northwest"},
	"southeast": {"south east", "south-east", "southeast"},
	"southwest": {"south west", "south-west", "southwest"},
	"midlands":  {"midlands", "midland"},
	"islands":   {"islands", "island"},
}

// regionOrder 多词区域优先（避免 northeast 被 north 提前匹配）
var regionOrder = []string{
	"northeast", "northwest", "southeast", "southwest",
	"north", "south", "east", "west", "central", "midlands", "islands",
}

// competitionTypeKeywords key=规范化赛制标签
// 扩充（优化建议 §3.4）：
//   - reserve: 预备队/二队识别（Reserve, B Team, II, Reserves）
//   - regional_league: 州联赛/地区性联赛识别（Campeonato, Paulista, Carioca 等）
var competitionTypeKeywords = map[string][]string{
	"short_format":   {"5x5", "6x6", "7x7", "futsal", "indoor", "amateur", "lfl", "mini", "street", "beach", "sala", "futsala"},
	"cup":            {"cup", "copa", "coppa", "cupe", "pokal", "trophy", "coupe", "taca", "kupa", "puchar", "cupen", "cupp", "pokalen", "beker"},
	"super_cup":      {"super cup", "supercup", "super copa", "supercoppa", "super coupe", "charity shield"},
	"playoff":        {"playoff", "play-offs", "play offs", "final stage", "championship round", "relegation round"},
	"friendly":       {"friendly", "friendlies", "test match", "exhibition", "international friendly"},
	// reserve: 预备队/二队识别（优化建议 §3.4）
	// 注意："reserve"/"b team" 已在 ageGroupKeywords["youth"] 中处理，此处展开为赛制类型层面的强约束
	"reserve":        {"reserve", "reserves", "b team", "b-team", "ii team", "segunda equipa", "equipe b"},
	// regional_league: 州联赛/地区性联赛（优化建议 §3.4）
	// 针对巴西等国家的州级赛事，防止与全国性 Serie A/B 误匹配
	"regional_league": {"paulista", "carioca", "gaucho", "mineiro", "baiano", "pernambucano", "paraense", "cearense", "paranaense", "alagoano", "sergipano", "capixaba", "potiguar"},
	// draft: 选秀赛事（修复 CBA→CBA Draft 误配）
	// 注意："draft" 需在 "league" 之前检测，防止被泛化关键词覆盖
	"draft":           {"draft"},
	// all_star: 全明星/明星赛（修复 Bundesliga→German All Star 误配）
	"all_star":        {"all star", "all-star", "allstar", "asg", "all stars"},
	"league":          {"league", "liga", "ligue", "serie", "division", "championship", "ekstraklasa", "superliga", "premiership"},
}

// competitionTypeOrder short_format 和 super_cup 优先于 cup/league（避免误匹配）
// reserve 和 regional_league 在 league 之前检测，防止被泛化的 "league" 关键词先行匹配
// draft 和 all_star 在 league 之前检测，防止被 "league" 关键词先行匹配（修复 CBA→CBA Draft 误配）
var competitionTypeOrder = []string{"short_format", "super_cup", "cup", "playoff", "friendly", "draft", "all_star", "reserve", "regional_league", "league"}

// ─────────────────────────────────────────────────────────────────────────────
// 层级数字提取（TODO-004 的核心逻辑，在本文件中实现）
// ─────────────────────────────────────────────────────────────────────────────

// romanToArabic 罗马数字 → 阿拉伯数字（仅支持 I-X）
var romanToArabic = map[string]int{
	"i": 1, "ii": 2, "iii": 3, "iv": 4, "v": 5,
	"vi": 6, "vii": 7, "viii": 8, "ix": 9, "x": 10,
}

// seriesLetterToTier 赛事字母 → 层级（Serie A=1, B=2, C=3, D=4, E=5）
var seriesLetterToTier = map[string]int{
	"a": 1, "b": 2, "c": 3, "d": 4, "e": 5,
}

// tierPatterns 层级数字提取的正则列表（按优先级排序）
// 每个 pattern 的第一个捕获组为层级数字或字母
var tierPatterns = []*regexp.Regexp{
	// "2.Bundesliga" / "2. Bundesliga" / "2 Bundesliga"
	regexp.MustCompile(`(?i)\b(\d+)\s*\.?\s*bundesliga\b`),
	// "Bundesliga 2" （较少见，但需支持）
	regexp.MustCompile(`(?i)\bbundesliga\s+(\d+)\b`),
	// "Liga 3" / "Liga3"
	regexp.MustCompile(`(?i)\bliga\s*(\d+)\b`),
	// "3 Liga" / "3. Liga" / "3.Liga"
	regexp.MustCompile(`(?i)\b(\d+)\s*\.?\s*liga\b`),
	// "Division 2" / "Division2"
	regexp.MustCompile(`(?i)\bdivision\s*(\d+)\b`),
	// "2 Division" / "2. Division"
	regexp.MustCompile(`(?i)\b(\d+)\s*\.?\s*division\b`),
	// "Serie A" / "Serie B" / "Serie C"
	regexp.MustCompile(`(?i)\bserie\s+([a-e])\b`),
	// "J1 League" / "J2 League" / "J3 League"（日本足球）
	regexp.MustCompile(`(?i)\bj(\d+)\s*league\b`),
	// "K League 1" / "K League 2"（韩国足球）
	regexp.MustCompile(`(?i)\bk\s*league\s*(\d+)\b`),
	// "K1 League" / "K2 League"（韩国足球缩写格式）
	regexp.MustCompile(`(?i)\bk(\d+)\s*league\b`),
	// "Ligue 1" / "Ligue 2"（法国足球）
	regexp.MustCompile(`(?i)\bligue\s*(\d+)\b`),
	// "Super League 1" / "Super League 2"（希腊等）
	regexp.MustCompile(`(?i)\bsuper\s+league\s*(\d+)\b`),
	// "Liga 1" / "Liga 2" 已有，但补充 "1. Liga" / "2. Liga" 等格式
	// "First Division" / "Second Division" 等文字层级
	// 注意：这些已在 tier_keywords 中处理，此处不再重复
}

// wordTierMap 文字层级词 → 数字层级（与 tier_keywords 对应）
// 扩充（优化建议 §3.3）：
//   - 英语序数词：1st/2nd/3rd/4th/5th division
//   - 葡萄牙语/西班牙语：primera/segunda/tercera/terceira division
//   - 德语：erste/zweite/dritte liga
var wordTierMap = map[string]int{
	// 英语文字层级
	"first division": 1, "division 1": 1, "liga 1": 1, "1 liga": 1, "1. liga": 1,
	"second division": 2, "division 2": 2, "liga 2": 2, "2 liga": 2, "2. liga": 2,
	"third division": 3, "division 3": 3, "liga 3": 3, "3 liga": 3, "3. liga": 3,
	"fourth division": 4, "division 4": 4, "liga 4": 4, "4 liga": 4, "4. liga": 4,
	"fifth division": 5, "division 5": 5, "liga 5": 5, "5 liga": 5, "5. liga": 5,
	// 英语序数词（优化建议 §3.3）
	"1st division": 1, "2nd division": 2, "3rd division": 3, "4th division": 4, "5th division": 5,
	// 葡萄牙语/西班牙语层级词（优化建议 §3.3）
	"primera division": 1, "segunda division": 2, "tercera division": 3, "cuarta division": 4,
	"primera liga": 1, "segunda liga": 2, "terceira liga": 3, "tercera liga": 3,
	// 德语层级词（优化建议 §3.3）
	"erste liga": 1, "zweite liga": 2, "dritte liga": 3, "vierte liga": 4,
	// 法语层级词
	"ligue 1": 1, "ligue 2": 2, "ligue 3": 3,
	// 日本 J 联赛
	"j1 league": 1, "j2 league": 2, "j3 league": 3,
	// 韩国 K 联赛
	"k league 1": 1, "k league 2": 2, "k1 league": 1, "k2 league": 2,
	// 希腊超级联赛
	"super league 1": 1, "super league 2": 2,
}

// extractTierNumber 从归一化联赛名称中提取层级数字
// 返回 (tierNumber, romanStr)，tierNumber=0 表示未检测到
func extractTierNumber(normalizedName string) (int, string) {
	lower := strings.ToLower(normalizedName)

	// 1. 正则模式匹配
	for _, re := range tierPatterns {
		m := re.FindStringSubmatch(lower)
		if len(m) >= 2 {
			captured := strings.ToLower(strings.TrimSpace(m[1]))
			// 尝试阿拉伯数字
			if n, err := strconv.Atoi(captured); err == nil && n >= 1 && n <= 10 {
				return n, ""
			}
			// 尝试赛事字母（Serie A/B/C）
			if tier, ok := seriesLetterToTier[captured]; ok {
				return tier, ""
			}
			// 尝试罗马数字
			if tier, ok := romanToArabic[captured]; ok {
				return tier, captured
			}
		}
	}

	// 2. 文字层级词匹配（如 "first division"）
	for phrase, tier := range wordTierMap {
		if strings.Contains(lower, phrase) {
			return tier, ""
		}
	}

	// 3. 独立罗马数字匹配（作为兜底，避免误匹配）
	// 仅在名称末尾或括号内的独立罗马数字才认为是层级标识
	romanRe := regexp.MustCompile(`(?i)(?:^|\s|\()(ii|iii|iv|vi|vii|viii|ix|x)(?:\s|$|\))`)
	if m := romanRe.FindStringSubmatch(lower); len(m) >= 2 {
		roman := strings.ToLower(m[1])
		if tier, ok := romanToArabic[roman]; ok {
			return tier, roman
		}
	}

	return 0, ""
}

// ─────────────────────────────────────────────────────────────────────────────
// 主提取函数
// ─────────────────────────────────────────────────────────────────────────────

// ExtractLeagueFeatures 从联赛名称（及可选的 category/country 字段）中提取结构化特征向量
//
// leagueName: 联赛名称（如 "Premier League Women U19"）
// categoryOrCountry: 可选的地区/国家字段（如 "England"），仅用于辅助信息，不参与本函数的强约束提取
func ExtractLeagueFeatures(leagueName string) LeagueFeatures {
	f := LeagueFeatures{}
	norm := normalizeName(leagueName)

	// ── 1. 性别检测 ──────────────────────────────────────────────────────────
	f.Gender = detectGender(norm)

	// ── 2. 年龄段检测 ────────────────────────────────────────────────────────
	f.AgeGroup = detectAgeGroup(norm)

	// ── 3. 区域分区检测 ──────────────────────────────────────────────────────
	f.Region = detectRegion(norm)

	// ── 4. 赛制类型检测 ──────────────────────────────────────────────────────
	f.CompetitionType = detectCompetitionType(norm)

	// ── 5. 层级数字提取 ──────────────────────────────────────────────────────
	f.TierNumber, f.TierRoman = extractTierNumber(norm)

	// ── 6. 归一化核心名称（去除已提取的特征词后的剩余部分，供相似度计算使用）
	f.NormalizedName = norm

	return f
}

// ─────────────────────────────────────────────────────────────────────────────
// 各维度检测辅助函数
// ─────────────────────────────────────────────────────────────────────────────

// detectGender 从归一化名称中检测性别标注
func detectGender(norm string) LeagueGender {
	// 短词 "w" 和 "m" 只在独立 token 时才匹配，避免误匹配 "women" 中的 "w"
	tokens := strings.Fields(norm)

	for _, kw := range womenKeywords {
		if kw == "w" {
			for _, t := range tokens {
				if t == "w" {
					return GenderWomen
				}
			}
			continue
		}
		if strings.Contains(norm, kw) {
			return GenderWomen
		}
	}

	for _, kw := range menKeywords {
		if kw == "m" {
			for _, t := range tokens {
				if t == "m" {
					return GenderMen
				}
			}
			continue
		}
		// "men" 需要独立 token 匹配，避免被 "women" 误匹配
		if kw == "men" || kw == "man" {
			for _, t := range tokens {
				if t == kw {
					return GenderMen
				}
			}
			continue
		}
		if strings.Contains(norm, kw) {
			return GenderMen
		}
	}

	return GenderUnknown
}

// detectAgeGroup 从归一化名称中检测年龄段标注
func detectAgeGroup(norm string) string {
	for _, group := range ageGroupOrder {
		keywords := ageGroupKeywords[group]
		for _, kw := range keywords {
			if strings.Contains(norm, kw) {
				return group
			}
		}
	}
	return ""
}

// detectRegion 从归一化名称中检测区域分区标注
// 注意：多词区域（northeast/northwest 等）优先检测
func detectRegion(norm string) string {
	for _, region := range regionOrder {
		keywords := regionKeywords[region]
		for _, kw := range keywords {
			// 区域词需要作为独立词出现（避免 "eastern" 被 "east" 误匹配）
			if containsWholeWord(norm, kw) {
				return region
			}
		}
	}
	return ""
}

// detectCompetitionType 从归一化名称中检测赛制类型
func detectCompetitionType(norm string) string {
	for _, ct := range competitionTypeOrder {
		keywords := competitionTypeKeywords[ct]
		for _, kw := range keywords {
			if strings.Contains(norm, kw) {
				return ct
			}
		}
	}
	return ""
}

// containsWholeWord 检查 s 中是否包含完整的单词 word（前后为空格或字符串边界）
func containsWholeWord(s, word string) bool {
	if !strings.Contains(s, word) {
		return false
	}
	idx := strings.Index(s, word)
	end := idx + len(word)
	// 检查前边界
	if idx > 0 && s[idx-1] != ' ' {
		return false
	}
	// 检查后边界
	if end < len(s) && s[end] != ' ' {
		return false
	}
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// 强约束一票否决（六维）
// ─────────────────────────────────────────────────────────────────────────────

// VetoReason 否决原因
type VetoReason string

const (
	VetoNone            VetoReason = ""
	VetoGender          VetoReason = "gender_conflict"
	VetoAge             VetoReason = "age_conflict"
	VetoRegion          VetoReason = "region_conflict"
	VetoCompetitionType VetoReason = "competition_type_conflict"
	VetoTierNumber      VetoReason = "tier_number_conflict"
	VetoShortFormat     VetoReason = "short_format_conflict"
)

// LeagueVetoResult 强约束校验结果
type LeagueVetoResult struct {
	Vetoed bool
	Reason VetoReason
	Detail string
}

// CheckLeagueVeto 对两个联赛特征向量执行六维强约束一票否决校验
//
// confidenceLevel: 当前匹配置信度等级（"hi" / "med" / "low"），
//
//	层级冲突仅在 med/low 时否决，hi 时放行（已知映射白名单场景）
func CheckLeagueVeto(a, b LeagueFeatures, confidenceLevel string) LeagueVetoResult {
	// @section:gender_veto - 性别强约束一票否决
	// ── 1. 性别强约束 ────────────────────────────────────────────────────────
	if a.Gender != GenderUnknown && b.Gender != GenderUnknown && a.Gender != b.Gender {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoGender,
			Detail: genderStr(a.Gender) + " vs " + genderStr(b.Gender),
		}
	}
	// 一侧显式女性，另一侧未知（默认男性赛事）→ 否决
	if a.Gender == GenderWomen && b.Gender == GenderUnknown {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoGender,
			Detail: "women vs unknown(assumed men)",
		}
	}
	if b.Gender == GenderWomen && a.Gender == GenderUnknown {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoGender,
			Detail: "unknown(assumed men) vs women",
		}
	}

	// @section:age_group_veto - 年龄段强约束一票否决
	// ── 2. 年龄段强约束 ──────────────────────────────────────────────────────
	if a.AgeGroup != "" && b.AgeGroup != "" && a.AgeGroup != b.AgeGroup {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoAge,
			Detail: a.AgeGroup + " vs " + b.AgeGroup,
		}
	}
	// 一侧有年龄标注，另一侧无 → 否决
	if (a.AgeGroup != "" && b.AgeGroup == "") || (a.AgeGroup == "" && b.AgeGroup != "") {
		ag := a.AgeGroup
		if ag == "" {
			ag = b.AgeGroup
		}
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoAge,
			Detail: "age group present on one side: " + ag,
		}
	}

	// @section:region_veto - 区域分区强约束一票否决
	// ── 3. 区域分区强约束 ────────────────────────────────────────────────────
	if a.Region != "" && b.Region != "" && a.Region != b.Region {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoRegion,
			Detail: a.Region + " vs " + b.Region,
		}
	}
	// 一侧有区域分区，另一侧无 → 否决
	if (a.Region != "" && b.Region == "") || (a.Region == "" && b.Region != "") {
		reg := a.Region
		if reg == "" {
			reg = b.Region
		}
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoRegion,
			Detail: "region present on one side: " + reg,
		}
	}

	// @section:competition_type_veto - 赛制类型强约束一票否决
	// ── 4. 赛制类型强约束 ────────────────────────────────────────────────────
	// short_format 与任何非 short_format 类型冲突
	if a.CompetitionType == "short_format" && b.CompetitionType != "short_format" && b.CompetitionType != "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoShortFormat,
			Detail: "short_format vs " + b.CompetitionType,
		}
	}
	if b.CompetitionType == "short_format" && a.CompetitionType != "short_format" && a.CompetitionType != "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoShortFormat,
			Detail: a.CompetitionType + " vs short_format",
		}
	}
	// 一侧为 short_format，另一侧未知 → 否决
	if a.CompetitionType == "short_format" && b.CompetitionType == "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoShortFormat,
			Detail: "short_format vs unknown",
		}
	}
	if b.CompetitionType == "short_format" && a.CompetitionType == "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoShortFormat,
			Detail: "unknown vs short_format",
		}
	}
	// cup 与 league 不得互相映射
	if isCupType(a.CompetitionType) && isLeagueType(b.CompetitionType) {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: a.CompetitionType + " vs " + b.CompetitionType,
		}
	}
	if isLeagueType(a.CompetitionType) && isCupType(b.CompetitionType) {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: a.CompetitionType + " vs " + b.CompetitionType,
		}
	}
	// reserve 与主赛事（league/cup）不得互相映射（优化建议 §3.4）
	if a.CompetitionType == "reserve" && isMainCompetition(b.CompetitionType) {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: "reserve vs " + b.CompetitionType,
		}
	}
	if b.CompetitionType == "reserve" && isMainCompetition(a.CompetitionType) {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: a.CompetitionType + " vs reserve",
		}
	}
	// regional_league 与全国性 league 不得互相映射（优化建议 §3.4）
	if a.CompetitionType == "regional_league" && isLeagueType(b.CompetitionType) {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: "regional_league vs " + b.CompetitionType,
		}
	}
	if b.CompetitionType == "regional_league" && isLeagueType(a.CompetitionType) {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: a.CompetitionType + " vs regional_league",
		}
	}

	// draft 与非 draft 赛事不得互相映射（修复 CBA→CBA Draft 误配）
	if a.CompetitionType == "draft" && b.CompetitionType != "draft" && b.CompetitionType != "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: "draft vs " + b.CompetitionType,
		}
	}
	if b.CompetitionType == "draft" && a.CompetitionType != "draft" && a.CompetitionType != "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: a.CompetitionType + " vs draft",
		}
	}
	// draft 与未知类型（正赛）不得互相映射
	if a.CompetitionType == "draft" && b.CompetitionType == "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: "draft vs unknown(assumed main competition)",
		}
	}
	if b.CompetitionType == "draft" && a.CompetitionType == "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: "unknown(assumed main competition) vs draft",
		}
	}
	// all_star 与非 all_star 赛事不得互相映射（修复 Bundesliga→German All Star 误配）
	if a.CompetitionType == "all_star" && b.CompetitionType != "all_star" && b.CompetitionType != "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: "all_star vs " + b.CompetitionType,
		}
	}
	if b.CompetitionType == "all_star" && a.CompetitionType != "all_star" && a.CompetitionType != "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: a.CompetitionType + " vs all_star",
		}
	}
	// all_star 与未知类型（正赛）不得互相映射
	if a.CompetitionType == "all_star" && b.CompetitionType == "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: "all_star vs unknown(assumed main competition)",
		}
	}
	if b.CompetitionType == "all_star" && a.CompetitionType == "" {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoCompetitionType,
			Detail: "unknown(assumed main competition) vs all_star",
		}
	}

	// @section:tier_number_veto - 层级数字强约束一票否决
	// ── 5. 层级数字强约束（在所有置信度级别下均生效）─────────────────────
	// 层级数字冲突属于强约束，不应因名称相似度高而豪免。
	// 典型场景："Liga 2 Peru" vs "Liga 1 Peru" 名称相似度高（~0.97）但层级不同，应否决。
	if a.TierNumber > 0 && b.TierNumber > 0 && a.TierNumber != b.TierNumber {
		return LeagueVetoResult{
			Vetoed: true,
			Reason: VetoTierNumber,
			Detail: "tier " + strconv.Itoa(a.TierNumber) + " vs tier " + strconv.Itoa(b.TierNumber),
		}
	}

	return LeagueVetoResult{Vetoed: false, Reason: VetoNone}
}

// ─────────────────────────────────────────────────────────────────────────────
// 负向特征惩罚（优化建议 §3.6）
// ─────────────────────────────────────────────────────────────────────────────

// CalcFeaturePenalty 计算负向特征惩罚系数（返回 0.0~1.0，1.0 表示无惩罚）。
//
// 与 CheckLeagueVeto 的区别：
//   - CheckLeagueVeto 是二元否决（否决/放行）
//   - CalcFeaturePenalty 是连续惩罚，对“未触发 Veto 但特征存在明显差异”的情况施加惩罚
//
// 典型惩罚场景：
//   - 一侧包含 U19，另一侧包含 U21 → 应在相似度上大幅扣分（而非就否决）
//   - 一侧包含 Women，另一侧包含 Men → 相似度直接降至 0
func CalcFeaturePenalty(a, b LeagueFeatures) float64 {
	penalty := 1.0

	// 性别差异惩罚：一侧 Women 另一侧 Men → 得分直接降至 0
	if (a.Gender == GenderWomen && b.Gender == GenderMen) ||
		(a.Gender == GenderMen && b.Gender == GenderWomen) {
		return 0.0
	}

	// 年龄段差异惩罚：两侧均有年龄段但不一致，大幅扣分
	// 如 U19 vs U21：不应就否决，但应大幅降低匹配得分
	if a.AgeGroup != "" && b.AgeGroup != "" && a.AgeGroup != b.AgeGroup {
		// 同为年龄组但数字不同（如 u19 vs u21）→ 惩罚 0.3
		penalty *= 0.3
	}

	// 赛制类型差异惩罚：一侧为 reserve，另一侧未知（未触发 Veto）
	if (a.CompetitionType == "reserve" && b.CompetitionType == "") ||
		(b.CompetitionType == "reserve" && a.CompetitionType == "") {
		penalty *= 0.5
	}

	// regional_league 与未知类型的轻度惩罚
	if (a.CompetitionType == "regional_league" && b.CompetitionType == "") ||
		(b.CompetitionType == "regional_league" && a.CompetitionType == "") {
		penalty *= 0.7
	}

	return penalty
}

// isCupType 判断赛制类型是否属于杯赛
func isCupType(ct string) bool {
	return ct == "cup" || ct == "super_cup"
}

// isLeagueType 判断赛制类型是否属于联赛
func isLeagueType(ct string) bool {
	return ct == "league"
}

// isMainCompetition 判断赛制类型是否属于主赛事（联赛或杯赛，非预备队）
// 用于 reserve vs 主赛事的强约束否决（优化建议 §3.4）
func isMainCompetition(ct string) bool {
	return ct == "league" || ct == "cup" || ct == "super_cup" || ct == "regional_league"
}

// genderStr 性别枚举 → 字符串（用于日志输出）
func genderStr(g LeagueGender) string {
	switch g {
	case GenderMen:
		return "men"
	case GenderWomen:
		return "women"
	default:
		return "unknown"
	}
}
