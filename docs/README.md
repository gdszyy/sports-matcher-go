# Sports Matcher Optimization Project

本项目旨在优化 `sports-matcher-go` 的匹配算法，提升在高歧义场景下的匹配精度。

## 2026-04-17 优化成果汇总

| 任务 ID | 优化项 | 核心变更 | 状态 |
| :--- | :--- | :--- | :--- |
| `tsk-97cf2032-032` | **联赛别名索引** | 引入 `league_alias.go`，支持 145+ 静态别名，优化英格兰联赛体系匹配。 | ✅ 已验收 |
| `tsk-c93a8ede-fc7` | **多维特征融合** | 整合 `CountryCode/Region` 结构化字段，实现四层评分架构，显著提升国家匹配精度。 | ✅ 已验收 |
| `tsk-f64e9bdf-ebf` | **自底向上校验** | 实现 med 置信度场景下强制触发球员匹配反向验证机制，增强结果可靠性。 | ✅ 已验收 |

## 核心文档
- [PI-002: 匹配算法优化与歧义处理](.cursor/rules/process_insights/PI-002_matching_algorithm_optimization.md)
- [2026-04-17 测试报告](docs/tests/2026-04-17_matching_test/REPORT.md)

## 任务历史
详情请参阅 `tasks/` 目录下的各任务记录。


## Evidence-First P5：实验入口、审核与安全回写

Evidence-First 现已接入为显式实验入口，默认不影响旧生产流程。端到端入口 `UniversalEngine.RunLeagueEvidenceFirst` 串联源侧加载、TS competition 候选池、P3 比赛级证据匹配、P4 联赛证据聚合、KnownMap RCR 验证、审核输出与可选安全回写。

| 使用方式 | 命令或接口 | 默认是否写回 |
|----------|------------|--------------|
| 单联赛 CLI | `./sports-matcher match-evidence <tournament_id> --sport football --review-out review.json --json` | 否 |
| 批量 CLI | `./sports-matcher batch-evidence --config leagues.json --review-dir reviews --json` | 否 |
| 单联赛 API | `GET /api/v2/match/evidence?tournament_id=...&sport=football` | 否 |
| 批量 API | `POST /api/v2/match/evidence/batch` | 否 |

写回安全门槛包括：`AUTO_CONFIRMED`、高置信比赛数达标、双队锚点比例达标、无 hard veto、候选分差达标。自动流程只允许写入 `TeamAliasStore`，不会静默覆盖 `KnownLeagueMap`；KnownMap RCR `<0.30` 时标记 suspect 并输出审核证据。验证脚本：`scripts/evidence_first_smoke.sh`。
