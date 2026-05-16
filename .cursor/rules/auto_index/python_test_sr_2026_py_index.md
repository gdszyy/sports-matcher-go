# python/test_sr_2026.py 函数索引

> 自动生成于 2026-05-16 | 总行数: 751 | 函数数: 19 | 语言: python
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

## 函数列表

> 定位方式：在源文件中 `grep -n "函数名"` 即可跳转，行号不在此列出（行号随代码变化而失效）。

| 函数名 | 类型 | 签名 | 备注 |
|--------|------|------|------|
| normalize_name | function | `normalize_name(s: str)` |  |
| jaro_winkler | function | `jaro_winkler(s1: str, s2: str)` |  |
| team_name_similarity | function | `team_name_similarity(a: str, b: str)` |  |
| gaussian_time_factor | function | `gaussian_time_factor(time_diff_sec: float, sigma: float)` |  |
| is_placeholder | function | `is_placeholder(name: str)` |  |
| TeamAliasIndex | class | `TeamAliasIndex()` |  |
| __init__ | method | `__init__(self)` |  |
| lookup | method | `lookup(self, sr_team_id: str)` |  |
| match_event_pair | function | `match_event_pair(sr_ev, ts_ev, alias_idx: TeamAliasIndex, team_id_map: dict)` |  |
| match_events_for_league | function | `match_events_for_league(sr_events: list, ts_events: list)` |  |
| find_ts_candidates | method | `find_ts_candidates(sr_unix, window_sec=259200)` |  |
| evaluate | function | `evaluate(predictions: list, gt_index: dict)` |  |
| ts_now | function | `ts_now()` |  |
| parse_sr_time | function | `parse_sr_time(s)` |  |
| load_data | function | `load_data()` |  |
| run_test | function | `run_test(tier_filter=None, league_filter=None, output_xlsx=None)` |  |
| _empty_stat | function | `_empty_stat(tid, name, sport, tier, ts_comp_id, sr_count, ts_count, gt_count, note)` |  |
| print_summary | function | `print_summary(league_stats: list, gt_index: dict)` |  |
| save_xlsx | function | `save_xlsx(league_stats: list, path: str)` |  |
