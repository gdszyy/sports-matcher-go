# python/build_sr_ts_ground_truth.py 函数索引

> 自动生成于 2026-05-16 | 总行数: 416 | 函数数: 10 | 语言: python
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

## 函数列表

> 定位方式：在源文件中 `grep -n "函数名"` 即可跳转，行号不在此列出（行号随代码变化而失效）。

| 函数名 | 类型 | 签名 | 备注 |
|--------|------|------|------|
| ts_log | function | `ts_log(msg)` |  |
| get_conn | function | `get_conn(db)` |  |
| save_json | function | `save_json(data, filename)` |  |
| sport_name | function | `sport_name(sport_id)` |  |
| load_raw_mappings | function | `load_raw_mappings()` |  |
| load_sr_events | function | `load_sr_events(sr_event_ids)` |  |
| load_ts_events | function | `load_ts_events(ts_match_ids, sport_map)` |  |
| query_batch | method | `query_batch(ids, table, team_table)` |  |
| build_ground_truth | function | `build_ground_truth(raw_maps, sr_events, ts_events)` |  |
| group_by_league | function | `group_by_league(ground_truth)` |  |
