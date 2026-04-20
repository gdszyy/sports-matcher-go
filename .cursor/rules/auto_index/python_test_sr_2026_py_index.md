# python/test_sr_2026.py 函数索引

> 自动生成于 2026-04-20 | 总行数: 751 | 函数数: 19 | 语言: python
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

## 函数列表

| 函数名 | 类型 | 起始行 | 结束行 | 行数 | 签名 |
|--------|------|--------|--------|------|------|
| normalize_name | function | L107 | L121 | 15 | `normalize_name(s: str)` |
| jaro_winkler | function | L122 | L164 | 43 | `jaro_winkler(s1: str, s2: str)` |
| team_name_similarity | function | L165 | L182 | 18 | `team_name_similarity(a: str, b: str)` |
| gaussian_time_factor | function | L183 | L206 | 24 | `gaussian_time_factor(time_diff_sec: float, sigma: float)` |
| is_placeholder | function | L207 | L213 | 7 | `is_placeholder(name: str)` |
| TeamAliasIndex | class | L214 | L217 | 4 | `TeamAliasIndex()` |
| __init__ | method | L218 | L239 | 22 | `__init__(self)` |
| lookup | method | L240 | L246 | 7 | `lookup(self, sr_team_id: str)` |
| match_event_pair | function | L247 | L297 | 51 | `match_event_pair(sr_ev, ts_ev, alias_idx: TeamAliasIndex, team_id_map: dict)` |
| match_events_for_league | function | L298 | L305 | 8 | `match_events_for_league(sr_events: list, ts_events: list)` |
| find_ts_candidates | method | L306 | L403 | 98 | `find_ts_candidates(sr_unix, window_sec=259200)` |
| evaluate | function | L404 | L419 | 16 | `evaluate(predictions: list, gt_index: dict)` |
| ts_now | function | L420 | L422 | 3 | `ts_now()` |
| parse_sr_time | function | L423 | L431 | 9 | `parse_sr_time(s)` |
| load_data | function | L432 | L505 | 74 | `load_data()` |
| run_test | function | L506 | L614 | 109 | `run_test(tier_filter=None, league_filter=None, output_xlsx=None)` |
| _empty_stat | function | L615 | L626 | 12 | `_empty_stat(tid, name, sport, tier, ts_comp_id, sr_count, ts_count, gt_count, note)` |
| print_summary | function | L627 | L681 | 55 | `print_summary(league_stats: list, gt_index: dict)` |
| save_xlsx | function | L682 | L752 | 71 | `save_xlsx(league_stats: list, path: str)` |
