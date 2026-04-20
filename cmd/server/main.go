// sports-matcher — 体育数据跨库匹配服务
//
// 用法:
//
//	sports-matcher serve               启动 HTTP API 服务
//	sports-matcher match <tournament>  命令行单联赛匹配（旧版 Engine）
//	sports-matcher match2 <tournament> 命令行单联赛匹配（最新 UniversalEngine，SR 数据源）
//	sports-matcher ls-match <id>       命令行单联赛匹配（最新 UniversalEngine，LS 数据源）
//	sports-matcher batch               批量匹配（旧版 Engine，读取内置联赛配置）
//	sports-matcher batch2              批量匹配（最新 UniversalEngine，SR 2026 热门+常规）
//	sports-matcher ls-batch            批量匹配（最新 UniversalEngine，LS 2026 热门+常规）
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/gdszyy/sports-matcher/internal/api"
	"github.com/gdszyy/sports-matcher/internal/config"
	"github.com/gdszyy/sports-matcher/internal/db"
	"github.com/gdszyy/sports-matcher/internal/matcher"
)

var cfg = config.Default()

func main() {
	root := &cobra.Command{
		Use:   "sports-matcher",
		Short: "体育数据跨库匹配服务（SR → TS）",
	}

	root.AddCommand(serveCmd(), matchCmd(), matchUniversalCmd(), lsMatchCmd(), batchCmd(), batchUniversalCmd(), lsBatchCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// ── serve ─────────────────────────────────────────────────────────────────

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动 HTTP API 服务",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Printf("启动 HTTP 服务 %s:%d", cfg.ServerHost, cfg.ServerPort)
			srv, err := api.NewServer(cfg)
			if err != nil {
				return err
			}
			defer srv.Close()
			return srv.Run()
		},
	}
	cmd.Flags().IntVar(&cfg.ServerPort, "port", cfg.ServerPort, "HTTP 监听端口")
	return cmd
}

// ── match（旧版 Engine）────────────────────────────────────────────────────

func matchCmd() *cobra.Command {
	var sport, tier, tsCompID string
	var noPlayers bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "match <tournament_id>",
		Short: "对单个联赛执行匹配（旧版 Engine）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tournamentID := args[0]
			cfg.RunPlayers = !noPlayers

			tunnel, err := db.NewTunnel(cfg)
			if err != nil {
				return err
			}
			defer tunnel.Close()

			eng := matcher.NewEngine(
				db.NewSRAdapter(tunnel.SRDb),
				db.NewTSAdapter(tunnel.TSDb),
				cfg.RunPlayers,
			)

			log.Printf("开始匹配联赛: %s  sport=%s  tier=%s", tournamentID, sport, tier)
			result, err := eng.RunLeague(tournamentID, sport, tier, tsCompID)
			if err != nil {
				return err
			}

			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			printStats(result.Stats)
			return nil
		},
	}

	cmd.Flags().StringVar(&sport, "sport", "football", "运动类型: football / basketball")
	cmd.Flags().StringVar(&tier, "tier", "hot", "联赛热度: hot / regular / cold")
	cmd.Flags().StringVar(&tsCompID, "ts-id", "", "TS competition_id（可选，跳过联赛匹配）")
	cmd.Flags().BoolVar(&noPlayers, "no-players", false, "跳过球员匹配（加速）")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "输出完整 JSON 结果")
	return cmd
}

// ── match2（最新 UniversalEngine）─────────────────────────────────────────

func matchUniversalCmd() *cobra.Command {
	var sport, tier, tsCompID string
	var noPlayers bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "match2 <tournament_id>",
		Short: "对单个联赛执行匹配（最新 UniversalEngine，含高斯时间衰减+FS模型+DTW）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tournamentID := args[0]
			cfg.RunPlayers = !noPlayers

			tunnel, err := db.NewTunnel(cfg)
			if err != nil {
				return err
			}
			defer tunnel.Close()

			srAdapter := db.NewSRAdapter(tunnel.SRDb)
			tsAdapter := db.NewTSAdapter(tunnel.TSDb)

			eng := matcher.NewUniversalEngine(tsAdapter, cfg.RunPlayers)
			srcAdapter := matcher.NewSRSourceAdapter(srAdapter, cfg.RunPlayers)

			log.Printf("[UniversalEngine] 开始匹配联赛: %s  sport=%s  tier=%s", tournamentID, sport, tier)
			result, err := eng.RunLeague(srcAdapter, tournamentID, sport, tier, tsCompID)
			if err != nil {
				return err
			}

			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			printUniversalStats(result.Stats)
			return nil
		},
	}

	cmd.Flags().StringVar(&sport, "sport", "football", "运动类型: football / basketball")
	cmd.Flags().StringVar(&tier, "tier", "hot", "联赛热度: hot / regular / cold")
	cmd.Flags().StringVar(&tsCompID, "ts-id", "", "TS competition_id（可选，跳过联赛匹配）")
	cmd.Flags().BoolVar(&noPlayers, "no-players", false, "跳过球员匹配（加速）")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "输出完整 JSON 结果")
	return cmd
}

// ── batch（旧版 Engine）────────────────────────────────────────────────────

// LeagueConfig 批量匹配配置
type LeagueConfig struct {
	TournamentID    string `json:"tournament_id"`
	Sport           string `json:"sport"`
	Tier            string `json:"tier"`
	TSCompetitionID string `json:"ts_competition_id"`
}

// defaultLeagues 内置联赛配置（足球热门+常规+冷门，篮球热门+常规+冷门）
// TS competition_id 均经过 ground truth 数据库验证（来自 ts_sr_match_mapping_3）
var defaultLeagues = []LeagueConfig{
	// ── 足球热门 ──
	{"sr:tournament:17", "football", "hot", "jednm9whz0ryox8"},  // Premier League (English Premier League)
	{"sr:tournament:8", "football", "hot", "vl7oqdehlyr510j"},   // LaLiga (Spanish La Liga)
	{"sr:tournament:35", "football", "hot", "gy0or5jhg6qwzv3"},  // Bundesliga
	{"sr:tournament:23", "football", "hot", "4zp5rzghp5q82w1"},  // Serie A (Italian Serie A)
	{"sr:tournament:34", "football", "hot", "yl5ergphnzr8k0o"},  // Ligue 1 (French Ligue 1)
	{"sr:tournament:7", "football", "hot", "z8yomo4h7wq0j6l"},   // UEFA Champions League
	{"sr:tournament:679", "football", "hot", "56ypq3nh0xmd7oj"}, // UEFA Europa League

	// ── 足球常规 ──
	{"sr:tournament:18", "football", "regular", "l965mkyh32r1ge4"},  // EFL Championship
	{"sr:tournament:37", "football", "regular", "vl7oqdeheyr510j"},  // Eredivisie
	{"sr:tournament:238", "football", "regular", "gx7lm7phpnm2wdk"}, // Liga Portugal
	{"sr:tournament:52", "football", "regular", "8y39mp1h6jmojxg"},  // Super Lig
	{"sr:tournament:203", "football", "regular", "8y39mp1hwxmojxg"}, // Russian Premier League
	{"sr:tournament:11", "football", "regular", "9vjxm8gh22r6odg"},  // Belgian Pro League
	{"sr:tournament:242", "football", "regular", "kn54qllhg2qvy9d"}, // MLS
	{"sr:tournament:325", "football", "regular", "4zp5rzgh9zq82w1"}, // Brasileiro Serie A
	{"sr:tournament:955", "football", "regular", "z318q66hl1qo9jd"}, // J1 League

	// ── 足球冷门 ──
	{"sr:tournament:551", "football", "cold", "e4wyrn4hoeq86pv"}, // Greek Super League
	{"sr:tournament:44", "football", "cold", "l965mkyhg0r1ge4"},  // Allsvenskan
	{"sr:tournament:48", "football", "cold", "gy0or5jhj6qwzv3"},  // Eliteserien

	// ── 篮球热门 ──
	{"sr:tournament:132", "basketball", "hot", "49vjxm8xt4q6odg"}, // NBA
	{"sr:tournament:138", "basketball", "hot", "jednm9ktd5ryox8"}, // EuroLeague

	// ── 篮球常规 ──
	{"sr:tournament:176", "basketball", "regular", "v2y8m4ptx1ml074"}, // VTB United League
	{"sr:tournament:53", "basketball", "regular", "x4zp5rzkt1r82w1"},  // Lega Basket Serie A
	{"sr:tournament:54", "basketball", "regular", "0l965mk8tom1ge4"},  // Basketball Bundesliga

	// ── 篮球冷门 ──
	{"sr:tournament:955", "basketball", "cold", "ngy0or5gteqwzv3"}, // CBA
}

func batchCmd() *cobra.Command {
	var noPlayers bool
	var configFile string

	cmd := &cobra.Command{
		Use:   "batch",
		Short: "批量匹配所有内置联赛（旧版 Engine）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RunPlayers = !noPlayers

			leagues := defaultLeagues
			if configFile != "" {
				f, err := os.Open(configFile)
				if err != nil {
					return err
				}
				defer f.Close()
				if err := json.NewDecoder(f).Decode(&leagues); err != nil {
					return err
				}
			}

			tunnel, err := db.NewTunnel(cfg)
			if err != nil {
				return err
			}
			defer tunnel.Close()

			eng := matcher.NewEngine(
				db.NewSRAdapter(tunnel.SRDb),
				db.NewTSAdapter(tunnel.TSDb),
				cfg.RunPlayers,
			)

			var allStats []matcher.MatchStats
			for _, lc := range leagues {
				log.Printf("\n══ 匹配联赛: %s [%s/%s] ══", lc.TournamentID, lc.Sport, lc.Tier)
				result, err := eng.RunLeague(lc.TournamentID, lc.Sport, lc.Tier, lc.TSCompetitionID)
				if err != nil {
					log.Printf("  ✗ 错误: %v", err)
					continue
				}
				allStats = append(allStats, result.Stats)
			}

			fmt.Println()
			printBatchTable(allStats)
			return nil
		},
	}

	cmd.Flags().BoolVar(&noPlayers, "no-players", false, "跳过球员匹配（加速）")
	cmd.Flags().StringVar(&configFile, "config", "", "自定义联赛配置文件（JSON）")
	return cmd
}

// ── batch2（最新 UniversalEngine，SR 2026 热门+常规）──────────────────────

// sr2026Leagues SR 2026 年热门 + 常规联赛配置
// TS competition_id 均来自 python/data/sr_ts_ground_truth.json（ground truth 验证）
// 覆盖 sr_leagues_2026.json 中 has_ts_mapping=true 的全部联赛，以及 KnownLeagueMap 中的标准联赛
var sr2026Leagues = []LeagueConfig{
	// ── 足球热门（来自 ground truth + KnownLeagueMap）──
	{"sr:tournament:17", "football", "hot", "jednm9whz0ryox8"},  // Premier League → English Premier League
	{"sr:tournament:8", "football", "hot", "vl7oqdehlyr510j"},   // LaLiga → Spanish La Liga
	{"sr:tournament:35", "football", "hot", "gy0or5jhg6qwzv3"},  // Bundesliga → Bundesliga
	{"sr:tournament:23", "football", "hot", "4zp5rzghp5q82w1"},  // Serie A → Italian Serie A
	{"sr:tournament:34", "football", "hot", "yl5ergphnzr8k0o"},  // Ligue 1 → French Ligue 1
	{"sr:tournament:7", "football", "hot", "z8yomo4h7wq0j6l"},   // UEFA Champions League
	{"sr:tournament:679", "football", "hot", "56ypq3nh0xmd7oj"}, // UEFA Europa League

	// ── 足球常规（来自 ground truth + sr_leagues_2026.json has_ts_mapping=true）──
	{"sr:tournament:18", "football", "regular", "l965mkyh32r1ge4"},  // Championship → EFL Championship
	{"sr:tournament:242", "football", "regular", "kn54qllhg2qvy9d"}, // MLS → United States Major League Soccer
	{"sr:tournament:203", "football", "regular", "8y39mp1hwxmojxg"}, // Russian PL → Russian Premier League
	{"sr:tournament:325", "football", "regular", "4zp5rzgh9zq82w1"}, // Brasileiro Serie A → Brazilian Serie A
	{"sr:tournament:37", "football", "regular", "vl7oqdeheyr510j"},  // Eredivisie → Netherlands Eredivisie
	{"sr:tournament:52", "football", "regular", "8y39mp1h6jmojxg"},  // Super Lig → Turkish Super League
	{"sr:tournament:238", "football", "regular", "gx7lm7phpnm2wdk"}, // Liga Portugal
	{"sr:tournament:11", "football", "regular", "9vjxm8gh22r6odg"},  // Belgian Pro League
	{"sr:tournament:955", "football", "regular", "z318q66hl1qo9jd"}, // J1 League

	// ── 篮球热门（来自 ground truth）──
	{"sr:tournament:132", "basketball", "hot", "49vjxm8xt4q6odg"}, // NBA → National Basketball Association
	{"sr:tournament:138", "basketball", "hot", "jednm9ktd5ryox8"}, // EuroLeague

	// ── 篮球常规（来自 KnownLeagueMap）──
	{"sr:tournament:176", "basketball", "regular", "v2y8m4ptx1ml074"}, // VTB United League
	{"sr:tournament:131", "basketball", "regular", "v2y8m4ptdeml074"}, // Liga ACB (Spain)
	{"sr:tournament:53", "basketball", "regular", "x4zp5rzkt1r82w1"},  // Lega Basket Serie A
	{"sr:tournament:54", "basketball", "regular", "0l965mk8tom1ge4"},  // Basketball Bundesliga
	{"sr:tournament:390", "basketball", "regular", "kjw2r02t6xqz84o"}, // FIBA Basketball Champions League
}

// ── ls-match（最新 UniversalEngine，LS 数据源）─────────────────────────────────

func lsMatchCmd() *cobra.Command {
	var sport, tier, tsCompID string
	var noPlayers bool
	var outputJSON bool
	var noKnownMap bool

	cmd := &cobra.Command{
		Use:   "ls-match <tournament_id>",
		Short: "对单个 LS 联赛执行匹配（最新 UniversalEngine，含高斯时间衰减+FS模型+DTW）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tournamentID := args[0]
			cfg.RunPlayers = !noPlayers

			tunnel, err := db.NewTunnel(cfg)
			if err != nil {
				return err
			}
			defer tunnel.Close()

			lsAdapter := db.NewLSAdapter(tunnel.LSDb)
			lsPlayerAdapter := db.NewLSPlayerAdapter(tunnel.LSDb, db.DefaultLSPlayerConfig)
			tsAdapter := db.NewTSAdapter(tunnel.TSDb)

			eng := matcher.NewUniversalEngine(tsAdapter, cfg.RunPlayers)
			var srcAdapter *matcher.LSSourceAdapter
			if noKnownMap {
				srcAdapter = matcher.NewLSSourceAdapterNoKnown(lsAdapter, lsPlayerAdapter, cfg.RunPlayers)
				tsCompID = "" // 纯算法模式：不预设 TS ID，让引擎拉全量 TS 联赛列表
			} else {
				srcAdapter = matcher.NewLSSourceAdapter(lsAdapter, lsPlayerAdapter, cfg.RunPlayers)
			}
			log.Printf("[UniversalEngine/LS] 开始匹配联赛: %s  sport=%s  tier=%s  no-known-map=%v", tournamentID, sport, tier, noKnownMap)
			result, err := eng.RunLeague(srcAdapter, tournamentID, sport, tier, tsCompID)
			if err != nil {
				return err
			}

			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			printUniversalStats(result.Stats)
			return nil
		},
	}

	cmd.Flags().StringVar(&sport, "sport", "football", "运动类型: football / basketball")
	cmd.Flags().StringVar(&tier, "tier", "hot", "联赛热度: hot / regular / cold")
	cmd.Flags().StringVar(&tsCompID, "ts-id", "", "TS competition_id（可选，跳过联赛匹配）")
	cmd.Flags().BoolVar(&noPlayers, "no-players", false, "跳过球员匹配（加速）")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "输出完整 JSON 结果")
	cmd.Flags().BoolVar(&noKnownMap, "no-known-map", false, "跳过 KnownLSLeagueMap，使用纯算法名称相似度匹配（验证算法效果）")
	return cmd
}
// ── ls-batch（最新 UniversalEngine，LS 2026 热门+常规）─────────────────────────

// ls2026Leagues LS 2026 年热门 + 常规联赛配置
// LS tournament_id 来自 test-xp-lsports.ls_tournament_en（已通过数据库查询验证）
// TS competition_id 来自 test-thesports-db（已通过 KnownLSLeagueMap 验证）
// 覆盖足球：五大联赛+UEFA+英格兰次级+主要欧洲+全球热门；篮球：NBA+EuroLeague+主要国内联赛
var ls2026Leagues = []LeagueConfig{
	// ── 足球热门 ──
	{"67", "football", "hot", "jednm9whz0ryox8"},  // Premier League (England) → English Premier League
	{"8363", "football", "hot", "vl7oqdehlyr510j"},  // LaLiga (Spain) → Spanish La Liga
	{"65", "football", "hot", "gy0or5jhg6qwzv3"},   // Bundesliga (Germany) → Bundesliga
	{"4", "football", "hot", "4zp5rzghp5q82w1"},    // Serie A (Italy) → Italian Serie A
	{"61", "football", "hot", "yl5ergphnzr8k0o"},   // Ligue 1 (France) → French Ligue 1
	{"32644", "football", "hot", "z8yomo4h7wq0j6l"}, // UEFA Champions League
	{"30444", "football", "hot", "56ypq3nh0xmd7oj"}, // UEFA Europa League
	{"27738", "football", "hot", "v2y8m4zhe6ql074"}, // CONMEBOL Copa Libertadores
	{"36297", "football", "hot", "56ypq3nhpkmd7oj"}, // Copa Sudamericana
	// ── 足球常规 ──
	{"58", "football", "regular", "l965mkyh32r1ge4"},    // The Championship (England)
	{"68", "football", "regular", "8y39mp1hjzmojxg"},    // League One (England)
	{"70", "football", "regular", "9k82rekhygrepzj"},    // League Two (England)
	{"8203", "football", "regular", "z318q66hv8qo9jd"},  // National League (England)
	{"66", "football", "regular", "kn54qllhjzqvy9d"},   // 2. Bundesliga (Germany)
	{"8", "football", "regular", "j1l4rjnhx9m7vx5"},    // Serie B (Italy)
	{"60", "football", "regular", "kjw2r09hw8rz84o"},   // Ligue 2 (France)
	{"22263", "football", "regular", "kdj2ryohnkq1zpg"}, // LaLiga2 (Spain)
	{"59", "football", "regular", "9vjxm8gh22r6odg"},   // Jupiler League (Belgium)
	{"2944", "football", "regular", "vl7oqdeheyr510j"},  // Eredivisie (Netherlands)
	{"6603", "football", "regular", "9vjxm8ghx2r6odg"},  // Primeira Liga (Portugal)
	{"63", "football", "regular", "8y39mp1h6jmojxg"},   // Super Lig (Turkey)
	{"3799", "football", "regular", "8y39mp1hwxmojxg"},  // FNL (Russia)
	{"32521", "football", "regular", "vl7oqdeh3lr510j"}, // Ekstraklasa (Poland)
	{"61243", "football", "regular", "gx7lm7pho13m2wd"}, // HNL (Croatia)
	{"30058", "football", "regular", "p4jwq2gh1gm0veo"}, // Scotland Premiership
	{"72", "football", "regular", "e4wyrn4hoeq86pv"},   // Super League (Greece)
	{"3", "football", "regular", "l965mkyhg0r1ge4"},    // Allsvenskan (Sweden)
	{"38", "football", "regular", "8y39mp1hk8mojxg"},   // Superettan (Sweden)
	{"24289", "football", "regular", "gy0or5jhj6qwzv3"}, // Eliteserien (Norway)
	{"156", "football", "regular", "kn54qllhg2qvy9d"},  // MLS (USA)
	{"5299", "football", "regular", "9k82rekhp6repzj"},  // Liga MX (Mexico)
	{"20913", "football", "regular", "4zp5rzgh9zq82w1"}, // Serie A (Brazil)
	{"41558", "football", "regular", "p3glrw7hevqdyjv"}, // Liga Profesional (Argentina)
	{"1543", "football", "regular", "9k82rekh52repzj"},  // China Super League
	{"35637", "football", "regular", "z318q66hl1qo9jd"}, // J1 League (Japan)
	{"24585", "football", "regular", "gy0or5jhlxgqwzv"}, // K League Classic (South Korea)
	{"28898", "football", "regular", "kn54qllh25dqvy9"}, // K2 League (South Korea)
	{"2018", "football", "regular", "56ypq3nh01nmd7o"},  // Premier League (Egypt)
	// ── 篮球热门 ──
	{"64", "basketball", "hot", "49vjxm8xt4q6odg"},   // NBA (United States)
	{"33249", "basketball", "hot", "jednm9ktd5ryox8"}, // Euroleague (International)
	// ── 篮球常规 ──
	{"4871", "basketball", "regular", "ngy0or5gteqwzv3"},  // CBA (China)
	{"3111", "basketball", "regular", "v2y8m4ptdeml074"},  // Liga ACB Endesa (Spain)
	{"621", "basketball", "regular", "0l965mk8tom1ge4"},   // Bundesliga (Germany)
	{"62013", "basketball", "regular", "v2y8m4ptx1ml074"}, // VTB United League (Russia)
	{"293", "basketball", "regular", "x4zp5rzkt1r82w1"},   // Serie A (Italy)
	{"48524", "basketball", "regular", "l965mk8tzpkm1ge"}, // BNXT League
	{"34184", "basketball", "regular", "ngy0or5gteqwzv3"}, // B.League - B1 (Japan)
	{"25357", "basketball", "regular", "v2y8m4ptx1ml074"}, // Orlen Basket Liga (Poland)
	{"1834", "basketball", "regular", "x4zp5rzkt1r82w1"},  // NBL (Australia)
}

func lsBatchCmd() *cobra.Command {
	var noPlayers bool
	var configFile string
	var tierFilter string
	var noKnownMap bool

	cmd := &cobra.Command{
		Use:   "ls-batch",
		Short: "批量匹配 LS 2026 热门+常规联赛（最新 UniversalEngine，含高斯时间衰减+FS模型+DTW）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RunPlayers = !noPlayers

			leagues := ls2026Leagues
			if configFile != "" {
				f, err := os.Open(configFile)
				if err != nil {
					return err
				}
				defer f.Close()
				if err := json.NewDecoder(f).Decode(&leagues); err != nil {
					return err
				}
			}

			// 按 tier 过滤
			if tierFilter != "" {
				var filtered []LeagueConfig
				for _, lc := range leagues {
					if lc.Tier == tierFilter {
						filtered = append(filtered, lc)
					}
				}
				leagues = filtered
			}

			tunnel, err := db.NewTunnel(cfg)
			if err != nil {
				return err
			}
			defer tunnel.Close()

			lsAdapter := db.NewLSAdapter(tunnel.LSDb)
			lsPlayerAdapter := db.NewLSPlayerAdapter(tunnel.LSDb, db.DefaultLSPlayerConfig)
			tsAdapter := db.NewTSAdapter(tunnel.TSDb)
			eng := matcher.NewUniversalEngine(tsAdapter, cfg.RunPlayers)

			log.Printf("[UniversalEngine/LS] 开始批量匹配 LS 2026 联赛，共 %d 个", len(leagues))

			var allStats []matcher.UniversalMatchStats
			for _, lc := range leagues {
				log.Printf("\n══ [UniversalEngine/LS] 匹配联赛: %s [%s/%s] ══", lc.TournamentID, lc.Sport, lc.Tier)
				var srcAdapter *matcher.LSSourceAdapter
				tsCompID := lc.TSCompetitionID
				if noKnownMap {
					srcAdapter = matcher.NewLSSourceAdapterNoKnown(lsAdapter, lsPlayerAdapter, cfg.RunPlayers)
					tsCompID = "" // 纯算法模式：不预设 TS ID，让引擎拉全量 TS 联赛列表
				} else {
					srcAdapter = matcher.NewLSSourceAdapter(lsAdapter, lsPlayerAdapter, cfg.RunPlayers)
				}
				result, err := eng.RunLeague(srcAdapter, lc.TournamentID, lc.Sport, lc.Tier, tsCompID)
				if err != nil {
					log.Printf("  ✗ 错误: %v", err)
					continue
				}
				allStats = append(allStats, result.Stats)
			}

			fmt.Println()
			printLSBatchTable(allStats)
			return nil
		},
	}

	cmd.Flags().BoolVar(&noPlayers, "no-players", false, "跳过球员匹配（加速）")
	cmd.Flags().StringVar(&configFile, "config", "", "自定义联赛配置文件（JSON）")
	cmd.Flags().StringVar(&tierFilter, "tier", "", "仅匹配指定热度的联赛: hot / regular / cold（空=全部）")
	cmd.Flags().BoolVar(&noKnownMap, "no-known-map", false, "跳过 KnownLSLeagueMap，使用纯算法名称相似度匹配（验证算法效果）")
	return cmd
}
func batchUniversalCmd() *cobra.Command {
	var noPlayers bool
	var configFile string
	var tierFilter string

	cmd := &cobra.Command{
		Use:   "batch2",
		Short: "批量匹配 SR 2026 热门+常规联赛（最新 UniversalEngine，含高斯时间衰减+FS模型+DTW）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RunPlayers = !noPlayers

			leagues := sr2026Leagues
			if configFile != "" {
				f, err := os.Open(configFile)
				if err != nil {
					return err
				}
				defer f.Close()
				if err := json.NewDecoder(f).Decode(&leagues); err != nil {
					return err
				}
			}

			// 按 tier 过滤
			if tierFilter != "" {
				var filtered []LeagueConfig
				for _, lc := range leagues {
					if lc.Tier == tierFilter {
						filtered = append(filtered, lc)
					}
				}
				leagues = filtered
			}

			tunnel, err := db.NewTunnel(cfg)
			if err != nil {
				return err
			}
			defer tunnel.Close()

			srAdapter := db.NewSRAdapter(tunnel.SRDb)
			tsAdapter := db.NewTSAdapter(tunnel.TSDb)
			eng := matcher.NewUniversalEngine(tsAdapter, cfg.RunPlayers)

			log.Printf("[UniversalEngine] 开始批量匹配 SR 2026 联赛，共 %d 个", len(leagues))

			var allStats []matcher.UniversalMatchStats
			for _, lc := range leagues {
				log.Printf("\n══ [UniversalEngine] 匹配联赛: %s [%s/%s] ══", lc.TournamentID, lc.Sport, lc.Tier)
				srcAdapter := matcher.NewSRSourceAdapter(srAdapter, cfg.RunPlayers)
				result, err := eng.RunLeague(srcAdapter, lc.TournamentID, lc.Sport, lc.Tier, lc.TSCompetitionID)
				if err != nil {
					log.Printf("  ✗ 错误: %v", err)
					continue
				}
				allStats = append(allStats, result.Stats)
			}

			fmt.Println()
			printUniversalBatchTable(allStats)
			return nil
		},
	}

	cmd.Flags().BoolVar(&noPlayers, "no-players", false, "跳过球员匹配（加速）")
	cmd.Flags().StringVar(&configFile, "config", "", "自定义联赛配置文件（JSON）")
	cmd.Flags().StringVar(&tierFilter, "tier", "", "仅匹配指定热度的联赛: hot / regular / cold（空=全部）")
	return cmd
}

// ── 输出格式化 ────────────────────────────────────────────────────────────

func printStats(s matcher.MatchStats) {
	fmt.Printf("\n─── 匹配结果: %s (%s/%s) ───\n", s.LeagueSRName, s.Sport, s.Tier)
	fmt.Printf("  联赛: %s → %s  rule=%-20s  conf=%.3f\n",
		s.LeagueSRName, s.LeagueTSName, s.LeagueRule, s.LeagueConf)
	fmt.Printf("  比赛: %d/%d (%.1f%%)  [L1=%d L2=%d L3=%d L4=%d L5=%d L4b=%d L6=%d]  avg_conf=%.3f\n",
		s.EventMatched, s.EventTotal, s.EventMatchRate*100,
		s.EventL1, s.EventL2, s.EventL3, s.EventL4, s.EventL5, s.EventL4b, s.EventL6, s.EventAvgConf)
	fmt.Printf("  球队: %d/%d (%.1f%%)\n",
		s.TeamMatched, s.TeamTotal, s.TeamMatchRate*100)
	fmt.Printf("  球员: %d/%d (%.1f%%)  avg_conf=%.3f\n",
		s.PlayerMatched, s.PlayerTotal, s.PlayerMatchRate*100, s.PlayerAvgConf)
	fmt.Printf("  耗时: %dms\n", s.ElapsedMs)
}

func printUniversalStats(s matcher.UniversalMatchStats) {
	fmt.Printf("\n─── [UniversalEngine] 匹配结果: %s (%s/%s) ───\n", s.SrcLeagueName, s.Sport, s.Tier)
	fmt.Printf("  联赛: %s → %s  rule=%-20s  conf=%.3f\n",
		s.SrcLeagueName, s.TSLeagueName, s.LeagueRule, s.LeagueConf)
	fmt.Printf("  比赛: %d/%d (%.1f%%)  [L1=%d L2=%d L3=%d L4=%d L5=%d L4b=%d]  avg_conf=%.3f\n",
		s.EventMatched, s.EventTotal, s.EventMatchRate*100,
		s.EventL1, s.EventL2, s.EventL3, s.EventL4, s.EventL5, s.EventL4b, s.EventAvgConf)
	fmt.Printf("  球队: %d/%d (%.1f%%)\n",
		s.TeamMatched, s.TeamTotal, s.TeamMatchRate*100)
	fmt.Printf("  球员: %d/%d (%.1f%%)  avg_conf=%.3f\n",
		s.PlayerMatched, s.PlayerTotal, s.PlayerMatchRate*100, s.PlayerAvgConf)
	fmt.Printf("  耗时: %dms\n", s.ElapsedMs)
}

func printBatchTable(stats []matcher.MatchStats) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "运动\t热度\t联赛(SR)\t联赛(TS)\t联赛规则\t联赛置信\t比赛总数\t已匹配\t匹配率\tL1\tL2\tL3\tL4\tL5\tL4b\tL6\t比赛置信\t球队匹配\t球员匹配\t耗时(ms)")
	fmt.Fprintln(w, "────\t────\t────────\t────────\t────────\t────────\t────────\t────────\t────────\t──\t──\t──\t──\t──\t───\t──\t────────\t────────\t────────\t────────")

	for _, s := range stats {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%.3f\t%d\t%d\t%.1f%%\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%.3f\t%d/%d\t%d/%d\t%d\n",
			s.Sport, s.Tier,
			truncate(s.LeagueSRName, 20), truncate(s.LeagueTSName, 20),
			s.LeagueRule, s.LeagueConf,
			s.EventTotal, s.EventMatched, s.EventMatchRate*100,
			s.EventL1, s.EventL2, s.EventL3, s.EventL4, s.EventL5, s.EventL4b, s.EventL6, s.EventAvgConf,
			s.TeamMatched, s.TeamTotal,
			s.PlayerMatched, s.PlayerTotal,
			s.ElapsedMs,
		)
	}
	w.Flush()
}

func printUniversalBatchTable(stats []matcher.UniversalMatchStats) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "[UniversalEngine] SR 2026 批量匹配结果")
	fmt.Fprintln(w, "运动\t热度\t联赛(SR)\t联赛(TS)\t联赛规则\t联赛置信\t比赛总数\t已匹配\t匹配率\tL1\tL2\tL3\tL4\tL5\tL4b\t比赛置信\t球队匹配\t球员匹配\t耗时(ms)")
	fmt.Fprintln(w, "────\t────\t────────\t────────\t────────\t────────\t────────\t────────\t────────\t──\t──\t──\t──\t──\t───\t────────\t────────\t────────\t────────")

	totalEvents, totalMatched := 0, 0
	for _, s := range stats {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%.3f\t%d\t%d\t%.1f%%\t%d\t%d\t%d\t%d\t%d\t%d\t%.3f\t%d/%d\t%d/%d\t%d\n",
			s.Sport, s.Tier,
			truncate(s.SrcLeagueName, 20), truncate(s.TSLeagueName, 20),
			s.LeagueRule, s.LeagueConf,
			s.EventTotal, s.EventMatched, s.EventMatchRate*100,
			s.EventL1, s.EventL2, s.EventL3, s.EventL4, s.EventL5, s.EventL4b, s.EventAvgConf,
			s.TeamMatched, s.TeamTotal,
			s.PlayerMatched, s.PlayerTotal,
			s.ElapsedMs,
		)
		totalEvents += s.EventTotal
		totalMatched += s.EventMatched
	}
	w.Flush()

	if totalEvents > 0 {
		fmt.Printf("\n汇总: %d 个联赛，%d/%d 场比赛匹配 (%.1f%%)\n",
			len(stats), totalMatched, totalEvents,
			float64(totalMatched)/float64(totalEvents)*100)
	}
}

func printLSBatchTable(stats []matcher.UniversalMatchStats) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "[UniversalEngine/LS] LS 2026 批量匹配结果")
	fmt.Fprintln(w, "运动\t热度\t联赛(LS)\t联赛(TS)\t联配规则\t联配置信\t比赛总数\t已匹配\t匹配率\tL1\tL2\tL3\tL4\tL5\tL4b\t比赛置信\t球队匹配\t球员匹配\t耗时(ms)")
	fmt.Fprintln(w, "────\t────\t────────\t────────\t────────\t────────\t────────\t────────\t────────\t──\t──\t──\t──\t──\t───\t────────\t────────\t────────\t────────")

	totalEvents, totalMatched := 0, 0
	for _, s := range stats {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%.3f\t%d\t%d\t%.1f%%\t%d\t%d\t%d\t%d\t%d\t%d\t%.3f\t%d/%d\t%d/%d\t%d\n",
			s.Sport, s.Tier,
			truncate(s.SrcLeagueName, 20), truncate(s.TSLeagueName, 20),
			s.LeagueRule, s.LeagueConf,
			s.EventTotal, s.EventMatched, s.EventMatchRate*100,
			s.EventL1, s.EventL2, s.EventL3, s.EventL4, s.EventL5, s.EventL4b, s.EventAvgConf,
			s.TeamMatched, s.TeamTotal,
			s.PlayerMatched, s.PlayerTotal,
			s.ElapsedMs,
		)
		totalEvents += s.EventTotal
		totalMatched += s.EventMatched
	}
	w.Flush()
	if totalEvents > 0 {
		fmt.Printf("\n汇总: %d 个联赛，%d/%d 场比赛匹配 (%.1f%%)\n",
			len(stats), totalMatched, totalEvents,
			float64(totalMatched)/float64(totalEvents)*100)
	}
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}
