"""scripts/audit_known_ls_league_map.py — KnownLSLeagueMap GT 错配审计 (v1.39)

扫描所有 KnownLSLeagueMap entry：
1. 排除已知 placeholder ((closest)/(alt id))
2. 排除 GT_DRIFT (ts_id 不在生产真实池)
3. 对每条 entry：用算法在真实池里搜 LS src_name 的 Top-5 候选，
   若 GT ts_id 不在 Top-5 → 标记为 SUSPECT，给出 Top-1 建议替换。

输出：docs/known_ls_league_map_audit.md
让 Ops 决策修脏数据。
"""
from __future__ import annotations
import json
import os
import re
import sys

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(SCRIPT_DIR)
sys.path.insert(0, os.path.join(REPO_ROOT, 'python'))

from evidence_first_strict_baseline import (  # noqa: E402
    match_league_topk, name_similarity, alias_lookup, alias_default_country,
)
from evidence_first_strict_wide_baseline import (  # noqa: E402
    extract_known_map_with_comments, parse_comment_for_ls,
    _infer_country_from_tsname,
)


def main():
    ls_go = os.path.join(REPO_ROOT, 'internal/matcher/ls_engine.go')
    entries = extract_known_map_with_comments(ls_go, 'KnownLSLeagueMap')
    pool = json.load(open(os.path.join(REPO_ROOT, 'python/data/ts_pool_real.json')))
    # 填 country 推断
    for t in pool:
        if not t.get('country'):
            t['country'] = _infer_country_from_tsname(t.get('name', ''))
    pool_by_id = {t['id']: t for t in pool}

    placeholder_re = re.compile(r'\(closest\)|\(alt id\)', re.I)

    rows_drift = []
    rows_placeholder = []
    rows_suspect = []
    rows_ok = []
    for e in entries:
        src_key = e['src_key']
        comment = e['comment']
        ts_id = e['ts_id']
        parts = src_key.split(':', 1)
        if len(parts) != 2:
            continue
        sport, src_id = parts
        if placeholder_re.search(comment):
            rows_placeholder.append({'src_key': src_key, 'ts_id': ts_id, 'comment': comment})
            continue
        # parse LS name
        src_name_fallback, ts_name_in_comment, ts_country_in_comment = parse_comment_for_ls(comment)
        src_name = src_name_fallback
        src_cat = ts_country_in_comment
        gt = pool_by_id.get(ts_id)
        if not gt:
            rows_drift.append({
                'src_key': src_key, 'src_name': src_name, 'src_cat': src_cat,
                'gt_ts_id': ts_id, 'comment': comment,
            })
            continue
        # 用算法找 Top-5
        topk = match_league_topk(src_name, src_cat, sport, pool, k=5)
        gt_in_topk = any(c['ts_id'] == ts_id for c in topk)
        gt_pos = next((i + 1 for i, c in enumerate(topk) if c['ts_id'] == ts_id), None)
        if gt_in_topk and gt_pos == 1:
            rows_ok.append({'src_key': src_key, 'gt_ts_id': ts_id, 'gt_name': gt['name']})
        else:
            row = {
                'src_key': src_key, 'src_name': src_name, 'src_cat': src_cat,
                'sport': sport,
                'gt_ts_id': ts_id, 'gt_name': gt['name'], 'gt_country': gt.get('country', ''),
                'gt_pos': gt_pos,
                'comment': comment,
                'algo_top1_id': topk[0]['ts_id'] if topk else '',
                'algo_top1_name': topk[0]['ts_name'] if topk else '',
                'algo_top1_score': topk[0]['score'] if topk else 0.0,
                'algo_top1_country': topk[0].get('ts_country', '') if topk else '',
                'topk_summary': [(c['ts_name'], c['ts_id'], c['score']) for c in topk[:5]],
            }
            rows_suspect.append(row)

    # 输出 markdown
    out_path = os.path.join(REPO_ROOT, 'docs/known_ls_league_map_audit.md')
    with open(out_path, 'w', encoding='utf-8') as f:
        f.write('# KnownLSLeagueMap GT 错配审计报告 (v1.39)\n\n')
        f.write('> 2026-05-16 | 配套 PI-006 v1.38\n')
        f.write('> 脚本：`scripts/audit_known_ls_league_map.py`\n\n')
        f.write('## 总览\n\n')
        f.write(f'- KnownLSLeagueMap 总条目：{len(entries)}\n')
        f.write(f'- 🟢 算法验证 OK (GT 是算法 Top-1)：{len(rows_ok)}\n')
        f.write(f'- 🔵 GT_PLACEHOLDER (注释含 closest/alt id)：{len(rows_placeholder)}\n')
        f.write(f'- 🟡 GT_DRIFT (ts_id 不在生产真实池)：{len(rows_drift)}\n')
        f.write(f'- 🔴 SUSPECT (GT 不是算法 Top-1)：{len(rows_suspect)}\n\n')
        f.write('## 🔴 SUSPECT — 建议 Ops review\n\n')
        f.write('算法在生产真实池找到的 Top-1 跟当前 GT ts_id 不同。可能：\n')
        f.write('- (a) GT ts_id 标错 → 应改为算法 Top-1\n')
        f.write('- (b) 算法仍有 bug → 保持 GT，记录给 v1.40+ 算法改进\n\n')
        for r in rows_suspect:
            f.write(f"### {r['src_key']}\n\n")
            f.write(f"- **LS 注释**: `{r['comment']}`\n")
            f.write(f"- **LS src**: name={r['src_name']!r}, category={r['src_cat']!r}, sport={r['sport']}\n")
            f.write(f"- **当前 GT ts_id**: `{r['gt_ts_id']}` → 真实 TS name={r['gt_name']!r}, country={r['gt_country']!r}\n")
            f.write(f"  - GT 在算法 Top-5 位置：{'第 ' + str(r['gt_pos']) + ' 位' if r['gt_pos'] else '不在 Top-5'}\n")
            f.write(f"- **算法 Top-1 建议**: `{r['algo_top1_id']}` name={r['algo_top1_name']!r}, country={r['algo_top1_country']!r}, score={r['algo_top1_score']:.3f}\n")
            f.write(f"- **算法 Top-5**:\n")
            for n, i, s in r['topk_summary']:
                marker = ' ← GT' if i == r['gt_ts_id'] else ''
                f.write(f"  - {s:.3f} | `{i}` {n!r}{marker}\n")
            f.write('\n')
        f.write('## 🟡 GT_DRIFT — ts_id 不在生产真实池\n\n')
        f.write('这些 KnownLSLeagueMap 注释的 ts_id 在生产 `ts_fb_competition`/`ts_bb_competition` 表里找不到，可能是过期或拼错。\n\n')
        for r in rows_drift:
            f.write(f"- **{r['src_key']}** GT `{r['gt_ts_id']}` 注释={r['comment']!r}\n")
        f.write('\n## 🔵 GT_PLACEHOLDER — `(closest)` / `(alt id)` 占位 (v1.34 已识别)\n\n')
        f.write(f'共 {len(rows_placeholder)} 条。详见 PI-006 v1.34。\n\n')
        for r in rows_placeholder:
            f.write(f"- **{r['src_key']}** GT `{r['ts_id']}` 注释={r['comment']!r}\n")
    print(f'写入 {out_path}')
    print(f'OK: {len(rows_ok)} | PLACEHOLDER: {len(rows_placeholder)} | DRIFT: {len(rows_drift)} | SUSPECT: {len(rows_suspect)}')


if __name__ == '__main__':
    main()
