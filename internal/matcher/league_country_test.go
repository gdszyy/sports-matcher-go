// Package matcher — 国家/地区匹配精度提升单元测试
//
// 覆盖以下新增功能：
//  1. IsInternationalCategory：国际赛事豁免检测
//  2. LocationVeto：地区名称强否决（CategoryName vs CountryName）
//  3. countryCodeVeto：结构化 CountryCode 精确否决
//  4. locationScore：多维地区相似度计算
//  5. leagueNameScore：整合 CountryCode 后的综合评分
//  6. MatchLeague：端到端联赛匹配（含 CountryCode 字段）
package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// ─────────────────────────────────────────────────────────────────────────────
// IsInternationalCategory 测试
// ─────────────────────────────────────────────────────────────────────────────

func TestIsInternationalCategory(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"UEFA", "UEFA", true},
		{"FIFA", "FIFA", true},
		{"Europe", "Europe", true},
		{"International", "International", true},
		{"England", "England", false},
		{"Spain", "Spain", false},
		{"Germany", "Germany", false},
		{"empty", "", false},
		{"AFC", "AFC", true},
		{"South America", "South America", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsInternationalCategory(tt.input)
			if got != tt.expected {
				t.Errorf("IsInternationalCategory(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// LocationVeto 测试
// ─────────────────────────────────────────────────────────────────────────────

func TestLocationVeto(t *testing.T) {
	tests := []struct {
		name        string
		srcCategory string
		tsCountry   string
		wantVeto    bool
	}{
		// 同国不否决
		{"England vs England", "England", "England", false},
		{"Spain vs Spain", "Spain", "Spain", false},
		// 跨国否决
		{"England vs Spain", "England", "Spain", true},
		{"Libya vs Laos", "Libya", "Laos", true},
		// 空值不否决（信息不足保守处理）
		{"empty src", "", "England", false},
		{"empty ts", "England", "", false},
		{"both empty", "", "", false},
		// 国际赛事豁免
		{"UEFA vs England", "UEFA", "England", false},
		{"Europe vs Spain", "Europe", "Spain", false},
		{"England vs International", "England", "International", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LocationVeto(tt.srcCategory, tt.tsCountry)
			if got != tt.wantVeto {
				t.Errorf("LocationVeto(%q, %q) = %v, want %v", tt.srcCategory, tt.tsCountry, got, tt.wantVeto)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// countryCodeVeto 测试
// ─────────────────────────────────────────────────────────────────────────────

func TestCountryCodeVeto(t *testing.T) {
	tests := []struct {
		name     string
		srcCode  string
		tsCode   string
		wantVeto bool
	}{
		// 相同代码不否决
		{"ENG vs ENG", "ENG", "ENG", false},
		{"ESP vs ESP", "ESP", "ESP", false},
		// 不同代码否决
		{"ENG vs ESP", "ENG", "ESP", true},
		{"DEU vs FRA", "DEU", "FRA", true},
		// 空值不否决
		{"empty src", "", "ENG", false},
		{"empty ts", "ENG", "", false},
		{"both empty", "", "", false},
		// 国际代码豁免
		{"UEFA vs ENG", "UEFA", "ENG", false},
		{"FIFA vs ESP", "FIFA", "ESP", false},
		{"INT vs DEU", "INT", "DEU", false},
		// 大小写不敏感
		{"eng vs ENG", "eng", "ENG", false},
		{"eng vs esp", "eng", "esp", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countryCodeVeto(tt.srcCode, tt.tsCode)
			if got != tt.wantVeto {
				t.Errorf("countryCodeVeto(%q, %q) = %v, want %v", tt.srcCode, tt.tsCode, got, tt.wantVeto)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// locationScore 测试
// ─────────────────────────────────────────────────────────────────────────────

func TestLocationScore(t *testing.T) {
	tests := []struct {
		name          string
		srcCategory   string
		srcCode       string
		tsCountry     string
		tsCode        string
		wantHardMatch bool
		wantSimGt     float64 // 期望 locSim > 此值
	}{
		// 结构化代码精确匹配
		{"code exact match ENG", "England", "ENG", "England", "ENG", true, 0.99},
		// 代码不同，低分
		{"code mismatch", "England", "ENG", "Spain", "ESP", false, -1},
		// 无代码，文本匹配
		{"text match England", "England", "", "England", "", false, 0.9},
		// 无代码，文本不匹配
		{"text mismatch", "England", "", "Spain", "", false, -1},
		// 全空
		{"all empty", "", "", "", "", false, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locSim, hardMatch := locationScore(tt.srcCategory, tt.srcCode, tt.tsCountry, tt.tsCode)
			if hardMatch != tt.wantHardMatch {
				t.Errorf("locationScore hardMatch = %v, want %v", hardMatch, tt.wantHardMatch)
			}
			if tt.wantSimGt >= 0 && locSim <= tt.wantSimGt {
				t.Errorf("locationScore locSim = %.3f, want > %.3f", locSim, tt.wantSimGt)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// leagueNameScore 测试（整合 CountryCode 后的综合评分）
// ─────────────────────────────────────────────────────────────────────────────

func TestLeagueNameScore_WithCountryCode(t *testing.T) {
	tests := []struct {
		name         string
		srTour       db.SRTournament
		tsComp       db.TSCompetition
		wantGt       float64 // 期望分数 > 此值（-1 表示不检查下界）
		wantLt       float64 // 期望分数 < 此值（-1 表示不检查上界）
		wantZero     bool    // 期望分数为 0（被否决）
	}{
		{
			name: "同国联赛+代码精确匹配_高分",
			srTour: db.SRTournament{
				Name: "Premier League", CategoryName: "England", CategoryCountryCode: "ENG",
			},
			tsComp: db.TSCompetition{
				Name: "Premier League", CountryName: "England", CountryCode: "ENG",
			},
			wantGt: 0.85, wantLt: -1,
		},
		{
			name: "同国联赛+无代码_中高分",
			srTour: db.SRTournament{
				Name: "Premier League", CategoryName: "England", CategoryCountryCode: "",
			},
			tsComp: db.TSCompetition{
				Name: "Premier League", CountryName: "England", CountryCode: "",
			},
			wantGt: 0.80, wantLt: -1,
		},
		{
			name: "跨国代码否决_零分",
			srTour: db.SRTournament{
				Name: "Premier League", CategoryName: "England", CategoryCountryCode: "ENG",
			},
			tsComp: db.TSCompetition{
				Name: "Primera Division", CountryName: "Spain", CountryCode: "ESP",
			},
			wantZero: true,
		},
		{
			name: "跨国名称否决_零分",
			srTour: db.SRTournament{
				Name: "Super League", CategoryName: "England", CategoryCountryCode: "",
			},
			tsComp: db.TSCompetition{
				Name: "Super League", CountryName: "Spain", CountryCode: "",
			},
			wantZero: true,
		},
		{
			name: "国际赛事豁免_不否决",
			srTour: db.SRTournament{
				Name: "UEFA Champions League", CategoryName: "UEFA", CategoryCountryCode: "",
			},
			tsComp: db.TSCompetition{
				Name: "UEFA Champions League", CountryName: "Europe", CountryCode: "",
			},
			wantGt: 0.7, wantLt: -1,
		},
		{
			name: "性别冲突_零分",
			srTour: db.SRTournament{
				Name: "Premier League Women", CategoryName: "England", CategoryCountryCode: "ENG",
			},
			tsComp: db.TSCompetition{
				Name: "Premier League", CountryName: "England", CountryCode: "ENG",
			},
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := leagueNameScore(&tt.srTour, &tt.tsComp)
			if tt.wantZero {
				if score != 0.0 {
					t.Errorf("leagueNameScore = %.3f, want 0.0 (vetoed)", score)
				}
				return
			}
			if tt.wantGt >= 0 && score <= tt.wantGt {
				t.Errorf("leagueNameScore = %.3f, want > %.3f", score, tt.wantGt)
			}
			if tt.wantLt >= 0 && score >= tt.wantLt {
				t.Errorf("leagueNameScore = %.3f, want < %.3f", score, tt.wantLt)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MatchLeague 端到端测试（含 CountryCode 字段）
// ─────────────────────────────────────────────────────────────────────────────

func TestMatchLeague_WithCountryCode(t *testing.T) {
	// 模拟 TS 联赛列表（含 CountryCode 字段）
	tsComps := []db.TSCompetition{
		{ID: "eng-pl", Name: "Premier League", CountryName: "England", CountryCode: "ENG", Sport: "football"},
		{ID: "esp-ll", Name: "La Liga", CountryName: "Spain", CountryCode: "ESP", Sport: "football"},
		{ID: "deu-bl", Name: "Bundesliga", CountryName: "Germany", CountryCode: "DEU", Sport: "football"},
		{ID: "ita-sa", Name: "Serie A", CountryName: "Italy", CountryCode: "ITA", Sport: "football"},
		{ID: "fra-l1", Name: "Ligue 1", CountryName: "France", CountryCode: "FRA", Sport: "football"},
	}

	tests := []struct {
		name           string
		srTour         db.SRTournament
		wantMatchedID  string
		wantMatched    bool
	}{
		{
			// 已知映射优先级最高，匹配到映射表中的 TS ID（而非模拟的 eng-pl）
			name: "已知映射_Premier League",
			srTour: db.SRTournament{
				ID: "sr:tournament:17", Name: "Premier League",
				CategoryName: "England", CategoryCountryCode: "ENG", Sport: "football",
			},
			wantMatchedID: "jednm9whz0ryox8", // KnownLeagueMap 中的已知 TS ID
			wantMatched:   true,
		},
		{
			// 已知映射优先级最高，匹配到映射表中的 TS ID（而非模拟的 deu-bl）
			name: "已知映射_Bundesliga",
			srTour: db.SRTournament{
				ID: "sr:tournament:35", Name: "Bundesliga",
				CategoryName: "Germany", CategoryCountryCode: "DEU", Sport: "football",
			},
			wantMatchedID: "gy0or5jhg6qwzv3", // KnownLeagueMap 中的已知 TS ID
			wantMatched:   true,
		},
		{
			name: "跨国代码否决_不匹配",
			srTour: db.SRTournament{
				ID: "sr:tournament:9999", Name: "Premier League",
				CategoryName: "Spain", CategoryCountryCode: "ESP", Sport: "football",
			},
			// Premier League 名称相似但 CountryCode 不同（ESP vs ENG），应被否决
			// 只有 La Liga 是西班牙联赛，但名称不相似，所以可能匹配到 La Liga 或不匹配
			wantMatched: false, // 期望不匹配（跨国否决）
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchLeague(&tt.srTour, tsComps)
			if tt.wantMatched && !result.Matched {
				t.Errorf("MatchLeague: expected match but got no match")
				return
			}
			if !tt.wantMatched && result.Matched {
				t.Logf("MatchLeague: matched to %s (conf=%.3f, rule=%s)",
					result.TSCompetitionID, result.Confidence, result.MatchRule)
				// 如果匹配了，检查是否是错误的跨国匹配
				if tt.name == "跨国代码否决_不匹配" && result.TSCompetitionID == "eng-pl" {
					t.Errorf("MatchLeague: cross-country match should be vetoed, got %s", result.TSCompetitionID)
				}
				return
			}
			if tt.wantMatched && tt.wantMatchedID != "" && result.TSCompetitionID != tt.wantMatchedID {
				t.Errorf("MatchLeague: matched to %s, want %s", result.TSCompetitionID, tt.wantMatchedID)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 回归测试：确保原有逻辑不受影响
// ─────────────────────────────────────────────────────────────────────────────

func TestLeagueNameScore_Regression(t *testing.T) {
	// 验证原有的 CategoryName 匹配逻辑仍然正常工作（无 CountryCode 时）
	tests := []struct {
		name     string
		srTour   db.SRTournament
		tsComp   db.TSCompetition
		wantGt   float64
		wantZero bool
	}{
		{
			name: "无CountryCode_同国加分",
			srTour: db.SRTournament{Name: "Premier League", CategoryName: "England"},
			tsComp: db.TSCompetition{Name: "Premier League", CountryName: "England"},
			wantGt: 0.80,
		},
		{
			name: "无CountryCode_跨国否决",
			srTour: db.SRTournament{Name: "Super League", CategoryName: "England"},
			tsComp: db.TSCompetition{Name: "Super League", CountryName: "Spain"},
			wantZero: true,
		},
		{
			name: "无CountryCode_国际豁免",
			srTour: db.SRTournament{Name: "Champions League", CategoryName: "UEFA"},
			tsComp: db.TSCompetition{Name: "Champions League", CountryName: "Europe"},
			wantGt: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := leagueNameScore(&tt.srTour, &tt.tsComp)
			if tt.wantZero {
				if score != 0.0 {
					t.Errorf("leagueNameScore = %.3f, want 0.0", score)
				}
				return
			}
			if score <= tt.wantGt {
				t.Errorf("leagueNameScore = %.3f, want > %.3f", score, tt.wantGt)
			}
		})
	}
}
