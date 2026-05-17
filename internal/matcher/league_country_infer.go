// Package matcher — league_country_infer.go: 联赛国别推断词典 (v1.30)
//
// 等价 Python evidence_first_strict_wide_baseline.py::_infer_country_from_tsname。
// 给 ts_competition.CountryName 为空（特别是 ts_bb_competition 表无 host_country）
// 的场景做 "国别字典" 补全。
//
// ⚠️ 性质：这是**算法字典**（对所有源生效的国别识别词典），不是 mapping。
// - 给定一个联赛名 → 返回推断 country（如 "English Premier League" → "England"）
// - 不针对特定 SR/LS ID，符合 v1.18 决议
// - 与 LeagueAliasGroup 性质一致（都是字典型算法辅助）
//
// 用法：leagueNameScore / lsLeagueNameScore 在 ts.CountryName=="" 时调用本函数补全，
// 避免 basketball pool（host_country 字段不存在）失去 country 约束能力。
package matcher

import "strings"

// countryInferKeywords 联赛名关键词 → 推断国别（v1.30 P0）
// 大小写不敏感（lower-cased keys）；按声明顺序匹配，首个命中返回。
// 注意顺序：形容词形式 > 国名前缀 > 国际组织 > 缩写。
var countryInferKeywords = []struct {
	keyword string
	country string
}{
	// 形容词形式
	{"spanish", "Spain"}, {"italian", "Italy"}, {"english", "England"},
	{"german", "Germany"}, {"french", "France"}, {"dutch", "Netherlands"},
	{"portuguese", "Portugal"}, {"belgian", "Belgium"}, {"turkish", "Turkey"},
	{"russian", "Russia"}, {"polish", "Poland"}, {"croatian", "Croatia"},
	{"scottish", "Scotland"}, {"swiss", "Switzerland"}, {"swedish", "Sweden"},
	{"norwegian", "Norway"}, {"austrian", "Austria"}, {"greek", "Greece"},
	{"brazilian", "Brazil"}, {"argentine", "Argentina"}, {"mexican", "Mexico"},
	{"chinese", "China"}, {"japanese", "Japan"}, {"korean", "Korea"},
	{"egyptian", "Egypt"}, {"danish", "Denmark"}, {"finnish", "Finland"},
	{"israeli", "Israel"}, {"ukrainian", "Ukraine"}, {"czech", "Czech Republic"},
	{"australian", "Australia"}, {"peruvian", "Peru"}, {"chilean", "Chile"},
	{"colombian", "Colombia"}, {"saudi", "Saudi Arabia"}, {"uruguay", "Uruguay"},
	// 国家名前缀
	{"england", "England"}, {"spain", "Spain"}, {"italy", "Italy"},
	{"france", "France"}, {"germany", "Germany"}, {"netherlands", "Netherlands"},
	{"portugal", "Portugal"}, {"belgium", "Belgium"}, {"turkey", "Turkey"},
	{"russia", "Russia"}, {"poland", "Poland"}, {"croatia", "Croatia"},
	{"scotland", "Scotland"}, {"sweden", "Sweden"}, {"norway", "Norway"},
	{"austria", "Austria"}, {"greece", "Greece"}, {"brazil", "Brazil"},
	{"argentina", "Argentina"}, {"mexico", "Mexico"}, {"china", "China"},
	{"japan", "Japan"}, {"korea", "Korea"}, {"united states", "USA"},
	{"usa", "USA"}, {"denmark", "Denmark"}, {"finland", "Finland"},
	{"ireland", "Ireland"}, {"australia", "Australia"},
	// 国际组织
	{"uefa", "International"}, {"conmebol", "International"}, {"fifa", "International"},
	{"afc ", "International"}, {"caf ", "International"},
	{"concacaf", "International"}, {"eurol", "International"},
	// 主流 ASCII 缩写
	{"mls", "USA"}, {"nba", "USA"}, {"nfl", "USA"}, {"nhl", "USA"},
	{"cba", "China"}, {"jpl", "Japan"},
	// 联赛名特征词
	{"jupiler", "Belgium"},
	{"eredivisie", "Netherlands"},
	{"allsvenskan", "Sweden"},
	{"superliga", "Denmark"},
	{"eliteserien", "Norway"},
	{"bundesliga", "Germany"},
	{"serie a", "Italy"},
	{"ligue", "France"},
	{"la liga", "Spain"},
	{"laliga", "Spain"},
}

// InferCountryFromName 从联赛名推断 country；未命中返回 ""。
// v1.30 P0: 给 ts_bb_competition 等无 host_country 字段的场景补 country 信号。
func InferCountryFromName(name string) string {
	if name == "" {
		return ""
	}
	lower := strings.ToLower(name)
	for _, kw := range countryInferKeywords {
		if strings.Contains(lower, kw.keyword) {
			return kw.country
		}
	}
	return ""
}

// EffectiveCountry 返回 ts.CountryName 或推断 country（v1.30 P0）。
// 优先用真实 CountryName；为空则用 name 推断。
//
// 注：v1.33 曾尝试加 alias group default_country fallback，让 BPL 也获得 England，
// 但导致同 group 多 ts_id 抢占时仍平手且 LS basketball Top-1 退化 -8.7 pp。回滚。
// DefaultCountry 字段保留作基础设施给未来更精细方案用（如 source-side 推断）。
func EffectiveCountry(countryName, leagueName string) string {
	if countryName != "" {
		return countryName
	}
	return InferCountryFromName(leagueName)
}
