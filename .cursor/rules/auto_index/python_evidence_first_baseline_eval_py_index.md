# python/evidence_first_baseline_eval.py 函数索引

> 自动生成于 2026-05-16 | 总行数: 522 | 函数数: 13 | 语言: python
> **本文件由 code-indexer 脚本自动生成，严禁手动编辑。**

## 函数列表

> 定位方式：在源文件中 `grep -n "函数名"` 即可跳转，行号不在此列出（行号随代码变化而失效）。

| 函数名 | 类型 | 签名 | 备注 |
|--------|------|------|------|
| load_json | function | `load_json(name: str)` |  |
| parse_ls_time | function | `parse_ls_time(value: str)` |  |
| compute_rcr | function | `compute_rcr(matched: int, source_total: int, unique_ts_total: int)` |  |
| classify_rcr | function | `classify_rcr(rcr: float)` |  |
| derive_team_metrics | function | `derive_team_metrics(results: List[Dict[str, Any]], source_events: List[Dict[str, Any]], ts_events: L)` |  |
| compact_stat | function | `compact_stat(row: Dict[str, Any])` |  |
| load_common_indexes | function | `load_common_indexes()` |  |
| run_sr | function | `run_sr()` |  |
| run_ls | function | `run_ls()` |  |
| summarize | function | `summarize(rows: List[Dict[str, Any]])` |  |
| aggregate | method | `aggregate(scope_rows: List[Dict[str, Any]])` |  |
| write_outputs | function | `write_outputs(rows: List[Dict[str, Any]], output_json: str, output_csv: str | None)` |  |
| main | function | `main()` |  |
