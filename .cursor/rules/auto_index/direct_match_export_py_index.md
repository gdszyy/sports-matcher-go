# direct_match_export.py 函数索引

> 自动生成于 2026-05-16 | 总行数: 706 | 函数数: 13 | 语言: python
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

**巨型函数警告**: 本文件包含 1 个超过 200 行的函数，建议优先通过 `@section` 标记进行内部导航。

## 函数列表

> 定位方式：在源文件中 `grep -n "函数名"` 即可跳转，行号不在此列出（行号随代码变化而失效）。

| 函数名 | 类型 | 签名 | 备注 |
|--------|------|------|------|
| get_conn | function | `get_conn(port, db)` | ⚠️ 巨型函数，见 @section 导航 |
| normalize_name | function | `normalize_name(s: str)` |  |
| seq_similarity | function | `seq_similarity(a: str, b: str)` |  |
| jaccard_similarity | function | `jaccard_similarity(a: str, b: str)` |  |
| name_similarity | function | `name_similarity(a: str, b: str)` |  |
| is_international_category | function | `is_international_category(name: str)` |  |
| extract_country_from_name | function | `extract_country_from_name(league_name: str)` |  |
| get_effective_ts_country | function | `get_effective_ts_country(comp: dict)` |  |
| location_veto | function | `location_veto(ls_category: str, effective_ts_country: str)` |  |
| load_ls_tournaments | function | `load_ls_tournaments(sport_id: int = 6046)` |  |
| load_ts_competitions | function | `load_ts_competitions(sport: str = 'football')` |  |
| run_batch_match | function | `run_batch_match(sport: str = 'football', output_path: str = None)` |  |
| export_excel | function | `export_excel(results: list, output_path: str, sport: str)` |  |
