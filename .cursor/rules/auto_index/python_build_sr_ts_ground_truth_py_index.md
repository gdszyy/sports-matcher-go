# python/build_sr_ts_ground_truth.py 函数索引

> 自动生成于 2026-04-20 | 总行数: 416 | 函数数: 10 | 语言: python
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

## 函数列表

| 函数名 | 类型 | 起始行 | 结束行 | 行数 | 签名 |
|--------|------|--------|--------|------|------|
| ts_log | function | L54 | L56 | 3 | `ts_log(msg)` |
| get_conn | function | L57 | L62 | 6 | `get_conn(db)` |
| save_json | function | L63 | L73 | 11 | `save_json(data, filename)` |
| sport_name | function | L74 | L82 | 9 | `sport_name(sport_id)` |
| load_raw_mappings | function | L83 | L143 | 61 | `load_raw_mappings()` |
| load_sr_events | function | L144 | L194 | 51 | `load_sr_events(sr_event_ids)` |
| load_ts_events | function | L195 | L210 | 16 | `load_ts_events(ts_match_ids, sport_map)` |
| query_batch | method | L211 | L260 | 50 | `query_batch(ids, table, team_table)` |
| build_ground_truth | function | L261 | L323 | 63 | `build_ground_truth(raw_maps, sr_events, ts_events)` |
| group_by_league | function | L324 | L417 | 94 | `group_by_league(ground_truth)` |
