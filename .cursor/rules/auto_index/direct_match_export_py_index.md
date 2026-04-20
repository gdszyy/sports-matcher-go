# direct_match_export.py 函数索引

> 自动生成于 2026-04-20 | 总行数: 706 | 函数数: 13 | 语言: python
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

**巨型函数警告**: 本文件包含 1 个超过 200 行的函数，建议优先通过 `@section` 标记进行内部导航。

## 函数列表

| 函数名 | 类型 | 起始行 | 结束行 | 行数 | 签名 |
|--------|------|--------|--------|------|------|
| get_conn | function | L29 | L281 | **253** | `get_conn(port, db)` |
| normalize_name | function | L282 | L293 | 12 | `normalize_name(s: str)` |
| seq_similarity | function | L294 | L302 | 9 | `seq_similarity(a: str, b: str)` |
| jaccard_similarity | function | L303 | L313 | 11 | `jaccard_similarity(a: str, b: str)` |
| name_similarity | function | L314 | L330 | 17 | `name_similarity(a: str, b: str)` |
| is_international_category | function | L331 | L341 | 11 | `is_international_category(name: str)` |
| extract_country_from_name | function | L342 | L375 | 34 | `extract_country_from_name(league_name: str)` |
| get_effective_ts_country | function | L376 | L392 | 17 | `get_effective_ts_country(comp: dict)` |
| location_veto | function | L393 | L513 | 121 | `location_veto(ls_category: str, effective_ts_country: str)` |
| load_ls_tournaments | function | L514 | L533 | 20 | `load_ls_tournaments(sport_id: int = 6046)` |
| load_ts_competitions | function | L534 | L557 | 24 | `load_ts_competitions(sport: str = 'football')` |
| run_batch_match | function | L558 | L627 | 70 | `run_batch_match(sport: str = 'football', output_path: str = None)` |
| export_excel | function | L628 | L707 | 80 | `export_excel(results: list, output_path: str, sport: str)` |
