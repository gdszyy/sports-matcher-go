// sports-matcher — 体育数据跨库匹配服务
//
// 用法:
//
//	sports-matcher serve               启动 HTTP API 服务
//	sports-matcher match <tournament>  命令行单联赛匹配
//	sports-matcher batch               批量匹配（读取内置联赛配置）
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

	root.AddCommand(serveCmd(), matchCmd(), batchCmd())

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

// ── match ─────────────────────────────────────────────────────────────────

func matchCmd() *cobra.Command {
	var sport, tier, tsCompID string
	var noPlayers bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "match <tournament_id>",
		Short: "对单个联赛执行匹配",
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

// ── batch ─────────────────────────────────────────────────────────────────

// LeagueConfig 批量匹配配置
type LeagueConfig struct {
	TournamentID    string `json:"tournament_id"`
	Sport           string `json:"sport"`
	Tier            string `json:"tier"`
	TSCompetitionID string `json:"ts_competition_id"`
}

// defaultLeagues 内置联赛配置（足球热门+常规+冷门，篮球热门+常规+冷门）
var defaultLeagues = []LeagueConfig{
	// ── 足球热门 ──
	{"sr:tournament:17", "football", "hot", "jednm9whz0ryox8"},  // Premier League
	{"sr:tournament:8", "football", "hot", "l965mkyhjpxr1ge"},   // LaLiga
	{"sr:tournament:35", "football", "hot", "l965mkyhjp4r1ge"},  // Bundesliga
	{"sr:tournament:23", "football", "hot", "l965mkyhjpyr1ge"},  // Serie A
	{"sr:tournament:34", "football", "hot", "l965mkyhjpxr1g8"},  // Ligue 1
	{"sr:tournament:7", "football", "hot", "l965mkyhjpxr1g3"},   // UCL
	{"sr:tournament:679", "football", "hot", "l965mkyhjpxr1g7"}, // UEL

	// ── 足球常规 ──
	{"sr:tournament:18", "football", "regular", "l965mkyhjp4r1g8"},  // Championship
	{"sr:tournament:37", "football", "regular", "l965mkyhjpxr1g6"},  // Eredivisie
	{"sr:tournament:238", "football", "regular", "l965mkyhjpxr1g5"}, // Liga Portugal
	{"sr:tournament:52", "football", "regular", "l965mkyhjpxr1g4"},  // Super Lig
	{"sr:tournament:203", "football", "regular", "l965mkyhjp4r1g7"}, // Russian PL
	{"sr:tournament:11", "football", "regular", "l965mkyhjp4r1g6"},  // Belgian PL
	{"sr:tournament:242", "football", "regular", "l965mkyhjp4r1g5"}, // MLS
	{"sr:tournament:325", "football", "regular", "l965mkyhjp4r1g4"}, // Brasileiro
	{"sr:tournament:955", "football", "regular", "l965mkyhjp4r1g3"}, // J1 League

	// ── 足球冷门 ──
	{"sr:tournament:551", "football", "cold", "l965mkyhjp4r1g1"}, // Greek SL
	{"sr:tournament:44", "football", "cold", "l965mkyhjp4r1g0"},  // Allsvenskan
	{"sr:tournament:48", "football", "cold", "l965mkyhjp4r1gz"},  // Eliteserien

	// ── 篮球热门 ──
	{"sr:tournament:132", "basketball", "hot", "l965mkyhjpxr1gz"}, // NBA
	{"sr:tournament:23", "basketball", "hot", "l965mkyhjpxr1gy"},  // EuroLeague

	// ── 篮球常规 ──
	{"sr:tournament:176", "basketball", "regular", "l965mkyhjpxr1gw"}, // VTB
	{"sr:tournament:53", "basketball", "regular", "l965mkyhjpxr1gu"},  // Lega Basket
	{"sr:tournament:54", "basketball", "regular", "l965mkyhjpxr1gt"},  // BBL

	// ── 篮球冷门 ──
	{"sr:tournament:955", "basketball", "cold", "l965mkyhjpxr1gs"}, // CBA
}

func batchCmd() *cobra.Command {
	var noPlayers bool
	var configFile string

	cmd := &cobra.Command{
		Use:   "batch",
		Short: "批量匹配所有内置联赛",
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

// ── 输出格式化 ────────────────────────────────────────────────────────────

func printStats(s matcher.MatchStats) {
	fmt.Printf("\n─── 匹配结果: %s (%s/%s) ───\n", s.LeagueSRName, s.Sport, s.Tier)
	fmt.Printf("  联赛: %s → %s  rule=%-20s  conf=%.3f\n",
		s.LeagueSRName, s.LeagueTSName, s.LeagueRule, s.LeagueConf)
	fmt.Printf("  比赛: %d/%d (%.1f%%)  [L1=%d L2=%d L3=%d L4=%d]  avg_conf=%.3f\n",
		s.EventMatched, s.EventTotal, s.EventMatchRate*100,
		s.EventL1, s.EventL2, s.EventL3, s.EventL4, s.EventAvgConf)
	fmt.Printf("  球队: %d/%d (%.1f%%)\n",
		s.TeamMatched, s.TeamTotal, s.TeamMatchRate*100)
	fmt.Printf("  球员: %d/%d (%.1f%%)  avg_conf=%.3f\n",
		s.PlayerMatched, s.PlayerTotal, s.PlayerMatchRate*100, s.PlayerAvgConf)
	fmt.Printf("  耗时: %dms\n", s.ElapsedMs)
}

func printBatchTable(stats []matcher.MatchStats) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "运动\t热度\t联赛(SR)\t联赛(TS)\t联赛规则\t联赛置信\t比赛总数\t已匹配\t匹配率\tL1\tL2\tL3\tL4\t比赛置信\t球队匹配\t球员匹配\t耗时(ms)")
	fmt.Fprintln(w, "────\t────\t────────\t────────\t────────\t────────\t────────\t────────\t────────\t──\t──\t──\t──\t────────\t────────\t────────\t────────")

	for _, s := range stats {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%.3f\t%d\t%d\t%.1f%%\t%d\t%d\t%d\t%d\t%.3f\t%d/%d\t%d/%d\t%d\n",
			s.Sport, s.Tier,
			truncate(s.LeagueSRName, 20), truncate(s.LeagueTSName, 20),
			s.LeagueRule, s.LeagueConf,
			s.EventTotal, s.EventMatched, s.EventMatchRate*100,
			s.EventL1, s.EventL2, s.EventL3, s.EventL4, s.EventAvgConf,
			s.TeamMatched, s.TeamTotal,
			s.PlayerMatched, s.PlayerTotal,
			s.ElapsedMs,
		)
	}
	w.Flush()
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}
