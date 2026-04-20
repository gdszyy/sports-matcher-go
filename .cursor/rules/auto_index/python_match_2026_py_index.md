# python/match_2026.py 函数索引

> 自动生成于 2026-04-20 | 总行数: 1054 | 函数数: 24 | 语言: python
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

## 函数列表

| 函数名 | 类型 | 起始行 | 结束行 | 行数 | 签名 |
|--------|------|--------|--------|------|------|
| ts | function | L39 | L41 | 3 | `ts(msg)` |
| get_conn | function | L42 | L130 | 89 | `get_conn(db)` |
| is_virtual_sport | function | L131 | L212 | 82 | `is_virtual_sport(name: str)` |
| normalize_name | function | L213 | L226 | 14 | `normalize_name(s: str)` |
| name_similarity | function | L227 | L239 | 13 | `name_similarity(a: str, b: str)` |
| is_international | function | L240 | L245 | 6 | `is_international(name: str)` |
| extract_country | function | L246 | L258 | 13 | `extract_country(name: str)` |
| get_eff_country | function | L259 | L272 | 14 | `get_eff_country(comp: dict)` |
| match_league | function | L273 | L323 | 51 | `match_league(ls_name, ls_category, ts_comps, sport, ls_id)` |
| parse_ls_time | function | L324 | L336 | 13 | `parse_ls_time(s: str)` |
| team_sim | function | L337 | L341 | 5 | `team_sim(lh, la, th, ta)` |
| same_date | function | L342 | L347 | 6 | `same_date(t1, t2)` |
| match_events | function | L348 | L429 | 82 | `match_events(ls_events, ts_events)` |
| _no_match | function | L430 | L443 | 14 | `_no_match(ev)` |
| load_all_ts_data | function | L444 | L488 | 45 | `load_all_ts_data(sport)` |
| load_ls_tournaments_2026 | function | L489 | L512 | 24 | `load_ls_tournaments_2026(sport)` |
| load_ls_events_2026_bulk | function | L513 | L557 | 45 | `load_ls_events_2026_bulk(sport)` |
| load_ts_competitions | function | L558 | L572 | 15 | `load_ts_competitions(sport)` |
| run_sport_match | function | L573 | L767 | 195 | `run_sport_match(sport)` |
| fill | function | L768 | L770 | 3 | `fill(c)` |
| hdr_font | function | L771 | L773 | 3 | `hdr_font(bold=True)` |
| auto_width | function | L774 | L786 | 13 | `auto_width(ws, mn=8, mx=50)` |
| export_excel | function | L787 | L966 | 180 | `export_excel(fb_data, bb_data, path)` |
| _write_league_detail_rows | function | L967 | L1055 | 89 | `_write_league_detail_rows(ws, lr, ems, ev_fill, is_virtual=False)` |
