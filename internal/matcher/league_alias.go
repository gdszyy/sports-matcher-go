// Package matcher — 联赛别名索引
//
// 本文件实现 PI-002 中提出的联赛别名索引机制，解决官方名称与常用名称差异较大
// 导致名称相似度计算失效的问题。
//
// 典型案例：
//   - SR: "EFL League One" / TS: "League One" / 常用: "Football League One"
//   - SR: "EFL Championship" / TS: "Championship" / 常用: "English Championship"
//   - SR: "Carabao Cup" / TS: "EFL Cup" / 常用: "League Cup"
//   - SR: "FA Premier League" / TS: "Premier League" / 常用: "EPL"
//
// 设计原则：
//   - 静态别名词典：内置高频官方名/常用名对照表，覆盖英格兰、德国、西班牙等主要联赛
//   - 运行时扩展：支持从数据库 `league_alias_knowledge` 表加载动态别名
//   - 别名展开：在相似度计算前，将输入名称展开为规范名称（canonical name）
//   - 双向索引：支持 "官方名 → 规范名" 和 "常用名 → 规范名" 的双向查找
//   - 与现有强约束一票否决机制完全兼容（别名展开在强约束校验前执行）
//
// 集成方式：
//   - 修改 leagueNameScore（league.go）和 lsLeagueNameScore（ls_engine.go）
//   - 在调用 nameSimilarityMax 前，先对两侧名称分别做 ExpandLeagueName
//   - 相似度计算同时考虑原名和展开名，取最大值
package matcher

import (
	"strings"
	"sync"
)

// ─────────────────────────────────────────────────────────────────────────────
// 静态别名词典
// ─────────────────────────────────────────────────────────────────────────────

// LeagueAliasGroup 一组等价的联赛名称（官方名 + 常用名 + 缩写）
// CanonicalName 是该组的规范名称（用于相似度计算的基准）
type LeagueAliasGroup struct {
	CanonicalName string   // 规范名称（通常为最完整的官方名）
	Aliases       []string // 所有等价名称（含规范名本身）
}

// staticLeagueAliasGroups 内置静态别名词典
// 覆盖官方名与常用名差异最大的联赛，按国家/赛事体系分组
var staticLeagueAliasGroups = []LeagueAliasGroup{
	// ── 英格兰足球联赛体系 ──────────────────────────────────────────────────
	{
		CanonicalName: "Premier League",
		Aliases: []string{
			"Premier League",
			"English Premier League",
			"FA Premier League",
			"EPL",
			"Barclays Premier League",
			"BPL",
		},
	},
	{
		CanonicalName: "EFL Championship",
		Aliases: []string{
			"EFL Championship",
			"Championship",
			"English Football League Championship",
			"Football League Championship",
			"The Championship",
			"Championship England",
			"English Championship",
			"Second Division England",
		},
	},
	{
		CanonicalName: "EFL League One",
		Aliases: []string{
			"EFL League One",
			"League One",
			"English Football League One",
			"Football League One",
			"League One England",
			"Third Division England",
			"League 1 England",
			"League 1",
		},
	},
	{
		CanonicalName: "EFL League Two",
		Aliases: []string{
			"EFL League Two",
			"League Two",
			"English Football League Two",
			"Football League Two",
			"League Two England",
			"Fourth Division England",
			"League 2 England",
			"League 2",
		},
	},
	{
		CanonicalName: "National League",
		Aliases: []string{
			"National League",
			"National League England",
			"Conference National",
			"Football Conference",
			"Vanarama National League",
		},
	},
	{
		CanonicalName: "FA Cup",
		Aliases: []string{
			"FA Cup",
			"FA Challenge Cup",
			"Football Association Challenge Cup",
			"The FA Cup",
		},
	},
	{
		CanonicalName: "EFL Cup",
		Aliases: []string{
			"EFL Cup",
			"League Cup",
			"Carabao Cup",
			"Capital One Cup",
			"Carling Cup",
			"Football League Cup",
			"English League Cup",
		},
	},
	{
		CanonicalName: "EFL Trophy",
		Aliases: []string{
			"EFL Trophy",
			"Football League Trophy",
			"Johnstone's Paint Trophy",
			"Papa John's Trophy",
			"LDV Vans Trophy",
		},
	},
	// ── 德国足球联赛体系 ──────────────────────────────────────────────────
	{
		CanonicalName: "Bundesliga",
		Aliases: []string{
			"Bundesliga",
			"1. Bundesliga",
			"German Bundesliga",
			"Fussball-Bundesliga",
			"German First Division",
		},
	},
	{
		CanonicalName: "2. Bundesliga",
		Aliases: []string{
			"2. Bundesliga",
			"2.Bundesliga",
			"Bundesliga 2",
			"German Second Division",
			"2nd Bundesliga",
		},
	},
	{
		CanonicalName: "3. Liga",
		Aliases: []string{
			"3. Liga",
			"3.Liga",
			"Liga 3",
			"German Third Division",
			"3rd Liga",
		},
	},
	// ── 西班牙足球联赛体系 ────────────────────────────────────────────────
	{
		CanonicalName: "LaLiga",
		Aliases: []string{
			"LaLiga",
			"La Liga",
			"Primera Division",
			"Primera División",
			"Spanish La Liga",
			"Liga BBVA",
			"Santander La Liga",
			"LaLiga Santander",
		},
	},
	{
		CanonicalName: "LaLiga2",
		Aliases: []string{
			"LaLiga2",
			"La Liga 2",
			"Segunda Division",
			"Segunda División",
			"Spanish Segunda Division",
			"LaLiga SmartBank",
			"Liga Adelante",
		},
	},
	// ── 意大利足球联赛体系 ────────────────────────────────────────────────
	{
		CanonicalName: "Serie A",
		Aliases: []string{
			"Serie A",
			"Italian Serie A",
			"Serie A TIM",
			"Lega Serie A",
		},
	},
	{
		CanonicalName: "Serie B",
		Aliases: []string{
			"Serie B",
			"Italian Serie B",
			"Serie BKT",
		},
	},
	// ── 法国足球联赛体系 ──────────────────────────────────────────────────
	{
		CanonicalName: "Ligue 1",
		Aliases: []string{
			"Ligue 1",
			"French Ligue 1",
			"Ligue 1 Uber Eats",
			"Division 1 France",
		},
	},
	{
		CanonicalName: "Ligue 2",
		Aliases: []string{
			"Ligue 2",
			"French Ligue 2",
			"Ligue 2 BKT",
			"Division 2 France",
		},
	},
	// ── UEFA 赛事 ────────────────────────────────────────────────────────
	{
		CanonicalName: "UEFA Champions League",
		Aliases: []string{
			"UEFA Champions League",
			"Champions League",
			"UCL",
			"European Champions Cup",
			"European Cup",
		},
	},
	{
		CanonicalName: "UEFA Europa League",
		Aliases: []string{
			"UEFA Europa League",
			"Europa League",
			"UEL",
			"UEFA Cup",
		},
	},
	{
		CanonicalName: "UEFA Europa Conference League",
		Aliases: []string{
			"UEFA Europa Conference League",
			"Conference League",
			"UECL",
			"Europa Conference League",
		},
	},
	// ── 荷兰足球联赛体系 ──────────────────────────────────────────────────
	{
		CanonicalName: "Eredivisie",
		Aliases: []string{
			"Eredivisie",
			"Netherlands Eredivisie",
			"Dutch Eredivisie",
			"Dutch First Division",
		},
	},
	// ── 葡萄牙足球联赛体系 ────────────────────────────────────────────────
	{
		CanonicalName: "Primeira Liga",
		Aliases: []string{
			"Primeira Liga",
			"Liga Portugal",
			"Liga NOS",
			"Ligabwin",
			"Portuguese Primeira Liga",
			"Liga Portuguesa",
		},
	},
	// ── 土耳其足球联赛体系 ────────────────────────────────────────────────
	{
		CanonicalName: "Süper Lig",
		Aliases: []string{
			"Süper Lig",
			"Super Lig",
			"Turkish Super League",
			"Turkish Süper Lig",
		},
	},
	// ── 俄罗斯足球联赛体系 ────────────────────────────────────────────────
	{
		CanonicalName: "Russian Premier League",
		Aliases: []string{
			"Russian Premier League",
			"RPL",
			"RFPL",
			"Russian Football National League",
		},
	},
	// ── 美国足球联赛体系 ──────────────────────────────────────────────────
	{
		CanonicalName: "MLS",
		Aliases: []string{
			"MLS",
			"Major League Soccer",
			"United States Major League Soccer",
			"American MLS",
		},
	},
	// ── 巴西足球联赛体系 ──────────────────────────────────────────────────
	{
		CanonicalName: "Campeonato Brasileiro Série A",
		Aliases: []string{
			"Campeonato Brasileiro Série A",
			"Brasileiro Serie A",
			"Brazilian Serie A",
			"Brasileirao",
			"Serie A Brazil",
		},
	},
	// ── 日本足球联赛体系 ──────────────────────────────────────────────────
	{
		CanonicalName: "J1 League",
		Aliases: []string{
			"J1 League",
			"J.League",
			"J-League",
			"Japanese J1 League",
			"Meiji Yasuda J1 League",
		},
	},
	// ── 中国足球联赛体系 ──────────────────────────────────────────────────
	{
		CanonicalName: "Chinese Super League",
		Aliases: []string{
			"Chinese Super League",
			"CSL",
			"China Super League",
			"Chinese Football Super League",
		},
	},
	// ── 篮球联赛体系 ────────────────────────────────────────────────────
	{
		CanonicalName: "NBA",
		Aliases: []string{
			"NBA",
			"National Basketball Association",
			"American NBA",
		},
	},
	{
		CanonicalName: "EuroLeague",
		Aliases: []string{
			"EuroLeague",
			"EuroLeague Basketball",
			"Turkish Airlines EuroLeague",
		},
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// LeagueAliasIndex — 内存别名索引
// ─────────────────────────────────────────────────────────────────────────────

// LeagueAliasIndex 联赛别名索引，支持从任意别名快速查找规范名称
// 线程安全，支持运行时动态扩展
type LeagueAliasIndex struct {
	mu sync.RWMutex
	// normalizedToCanonical: 归一化别名 → 规范名称
	normalizedToCanonical map[string]string
	// canonicalToAliases: 规范名称 → 所有别名（含规范名本身）
	canonicalToAliases map[string][]string
}

// globalLeagueAliasIndex 全局单例别名索引（启动时从静态词典初始化）
var globalLeagueAliasIndex *LeagueAliasIndex
var globalLeagueAliasOnce sync.Once

// GetLeagueAliasIndex 获取全局联赛别名索引（懒加载单例）
func GetLeagueAliasIndex() *LeagueAliasIndex {
	globalLeagueAliasOnce.Do(func() {
		idx := &LeagueAliasIndex{
			normalizedToCanonical: make(map[string]string),
			canonicalToAliases:    make(map[string][]string),
		}
		for _, group := range staticLeagueAliasGroups {
			idx.addGroup(group.CanonicalName, group.Aliases)
		}
		globalLeagueAliasIndex = idx
	})
	return globalLeagueAliasIndex
}

// addGroup 内部方法：将一组别名写入索引
func (idx *LeagueAliasIndex) addGroup(canonicalName string, aliases []string) {
	normCanonical := normalizeName(canonicalName)
	for _, alias := range aliases {
		normAlias := normalizeName(alias)
		if normAlias == "" {
			continue
		}
		idx.normalizedToCanonical[normAlias] = normCanonical
	}
	// 规范名本身也写入
	idx.normalizedToCanonical[normCanonical] = normCanonical
	idx.canonicalToAliases[normCanonical] = aliases
}

// RegisterAlias 动态注册一条别名（运行时扩展，线程安全）
// 若 canonicalName 已存在，则追加 alias；否则创建新组
func (idx *LeagueAliasIndex) RegisterAlias(canonicalName, alias string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	normCanonical := normalizeName(canonicalName)
	normAlias := normalizeName(alias)
	if normAlias == "" || normCanonical == "" {
		return
	}
	idx.normalizedToCanonical[normAlias] = normCanonical
	idx.normalizedToCanonical[normCanonical] = normCanonical
	idx.canonicalToAliases[normCanonical] = append(idx.canonicalToAliases[normCanonical], alias)
}

// RegisterGroup 动态注册一组别名（运行时扩展，线程安全）
func (idx *LeagueAliasIndex) RegisterGroup(canonicalName string, aliases []string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.addGroup(canonicalName, aliases)
}

// Lookup 查找归一化名称对应的规范名称
// 返回 (canonicalName, found)
// 若未找到，返回 ("", false)
func (idx *LeagueAliasIndex) Lookup(name string) (string, bool) {
	normName := normalizeName(name)
	idx.mu.RLock()
	canonical, ok := idx.normalizedToCanonical[normName]
	idx.mu.RUnlock()
	return canonical, ok
}

// ExpandName 将联赛名称展开为规范名称（若存在别名映射）
// 若未找到别名，返回原始名称（不做修改）
func (idx *LeagueAliasIndex) ExpandName(name string) string {
	canonical, ok := idx.Lookup(name)
	if !ok {
		return name
	}
	// 返回规范名称（已归一化，但保留原始大小写格式的规范名）
	// 从 canonicalToAliases 中找到第一个别名（即规范名本身）
	idx.mu.RLock()
	aliases := idx.canonicalToAliases[canonical]
	idx.mu.RUnlock()
	if len(aliases) > 0 {
		return aliases[0] // 第一个别名即为原始格式的规范名
	}
	return name
}

// GetAllAliases 获取指定联赛名称的所有别名（含规范名）
// 若未找到，返回 nil
func (idx *LeagueAliasIndex) GetAllAliases(name string) []string {
	canonical, ok := idx.Lookup(name)
	if !ok {
		return nil
	}
	idx.mu.RLock()
	aliases := idx.canonicalToAliases[canonical]
	idx.mu.RUnlock()
	return aliases
}

// Size 返回索引中的别名条目总数
func (idx *LeagueAliasIndex) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.normalizedToCanonical)
}

// ─────────────────────────────────────────────────────────────────────────────
// 别名感知的联赛名称相似度
// ─────────────────────────────────────────────────────────────────────────────

// leagueNameSimilarityWithAlias 计算两个联赛名称的相似度，支持别名展开
//
// 算法：
//  1. 直接计算原始名称相似度（基线）
//  2. 将两侧名称分别展开为规范名称（若存在别名映射）
//  3. 计算展开后名称的相似度
//  4. 检查两侧是否映射到同一规范名称（直接命中，返回高置信度）
//  5. 取所有相似度的最大值
//
// 返回值范围 [0.0, 1.0]，1.0 表示完全匹配（同一规范名）
func leagueNameSimilarityWithAlias(nameA, nameB string) float64 {
	idx := GetLeagueAliasIndex()

	// 1. 基线：原始名称相似度
	baseSim := nameSimilarityMax(nameA, nameB)

	// 2. 别名展开
	canonicalA, foundA := idx.Lookup(nameA)
	canonicalB, foundB := idx.Lookup(nameB)

	// 3. 两侧都命中别名索引，且映射到同一规范名 → 直接返回高置信度
	if foundA && foundB && canonicalA == canonicalB {
		return 0.98 // 同一规范名，高置信度（非 1.0 以区别于精确字符串匹配）
	}

	// 4. 计算展开后名称的相似度
	expandedA := idx.ExpandName(nameA)
	expandedB := idx.ExpandName(nameB)

	expandedSim := 0.0
	if expandedA != nameA || expandedB != nameB {
		// 至少一侧发生了展开
		expandedSim = nameSimilarityMax(expandedA, expandedB)
	}

	// 5. 若一侧命中别名，尝试用规范名与另一侧比较
	crossSim := 0.0
	if foundA {
		// 用 A 的所有别名与 B 比较，取最大值
		for _, alias := range idx.GetAllAliases(nameA) {
			s := nameSimilarityMax(alias, nameB)
			if s > crossSim {
				crossSim = s
			}
		}
	}
	if foundB {
		// 用 B 的所有别名与 A 比较，取最大值
		for _, alias := range idx.GetAllAliases(nameB) {
			s := nameSimilarityMax(nameA, alias)
			if s > crossSim {
				crossSim = s
			}
		}
	}

	// 6. 取所有相似度的最大值
	best := baseSim
	if expandedSim > best {
		best = expandedSim
	}
	if crossSim > best {
		best = crossSim
	}
	return best
}

// ─────────────────────────────────────────────────────────────────────────────
// 别名索引加载器接口（供 db.LeagueAliasStore 使用）
// ─────────────────────────────────────────────────────────────────────────────

// LeagueAliasLoader 定义将持久化联赛别名注入内存索引的接口
// db.LeagueAliasStore 实现此接口，使存储层无需直接依赖 matcher 包
type LeagueAliasLoader interface {
	// RegisterAlias 注册一条联赛别名
	RegisterAlias(canonicalName, alias string)
	// RegisterGroup 注册一组联赛别名
	RegisterGroup(canonicalName string, aliases []string)
}

// ─────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ─────────────────────────────────────────────────────────────────────────────

// normalizeLeagueName 联赛名称归一化（专用版本，比通用 normalizeName 更激进）
// 去除赞助商前缀、年份、括号内容等干扰词
func normalizeLeagueName(name string) string {
	// 先做通用归一化
	norm := normalizeName(name)

	// 去除括号内容（如 "(England)"、"(2023/24)"）
	bracketRe := strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ")
	norm = bracketRe.Replace(norm)

	// 去除年份（如 "2023"、"2023/24"、"23/24"）
	// 通过简单的 token 过滤实现
	tokens := strings.Fields(norm)
	filtered := tokens[:0]
	for _, t := range tokens {
		// 跳过纯数字 token（年份）
		if isYearToken(t) {
			continue
		}
		filtered = append(filtered, t)
	}

	return strings.Join(filtered, " ")
}

// isYearToken 判断 token 是否为年份格式（如 "2023"、"2023/24"、"23/24"）
func isYearToken(t string) bool {
	if len(t) == 4 {
		// 纯四位数字
		allDigit := true
		for _, c := range t {
			if c < '0' || c > '9' {
				allDigit = false
				break
			}
		}
		if allDigit {
			return true
		}
	}
	// "2023/24" 或 "23/24" 格式
	if strings.Contains(t, "/") {
		parts := strings.Split(t, "/")
		if len(parts) == 2 {
			allDigit := true
			for _, p := range parts {
				for _, c := range p {
					if c < '0' || c > '9' {
						allDigit = false
						break
					}
				}
			}
			if allDigit {
				return true
			}
		}
	}
	return false
}
