"""evidence_first_strict_wide_baseline.py — 扩 strict eval 评估集 (v1.21)

v1.20 之前只有 14 个 GT SR 联赛 evaluable，太小不显微。
v1.21 把评估集扩到 KnownLeagueMap 36 个 SR + KnownLSLeagueMap 64 个 LS = 100 个 GT 联赛。

数据流：
  GT = KnownLeagueMap / KnownLSLeagueMap（仅作 GT 对照，不进算法路径，符合 v1.18 决议）
  src_name + category = leagues_2026.json
  ts pool = ts_leagues_2026.json + GT 反推 + KnownMap 注释解析的 TS name stubs
"""
from __future__ import annotations
import argparse
import json
import os
import re
import sys
from datetime import datetime, timezone
from typing import Any, Dict, List

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.path.join(SCRIPT_DIR, 'data')
REPO_ROOT = os.path.dirname(SCRIPT_DIR)

sys.path.insert(0, SCRIPT_DIR)
from evidence_first_strict_baseline import (  # noqa: E402
    K_LIST, eval_side, _aggregate_topk,
    match_league_topk, league_name_similarity_with_alias, _load_alias_index,
)




def _infer_country_from_tsname(tsname):
    """从 TS 联赛名提取国别词作为 stub 的 country 字段（v1.22 P0-B 配套）。"""
    t = (tsname or '').lower()
    for kw, cn in [
        ('spanish', 'Spain'), ('italian', 'Italy'), ('english', 'England'),
        ('german', 'Germany'), ('french', 'France'), ('dutch', 'Netherlands'),
        ('portuguese', 'Portugal'), ('belgian', 'Belgium'), ('turkish', 'Turkey'),
        ('russian', 'Russia'), ('polish', 'Poland'), ('croatian', 'Croatia'),
        ('scottish', 'Scotland'), ('swiss', 'Switzerland'), ('swedish', 'Sweden'),
        ('norwegian', 'Norway'), ('austrian', 'Austria'), ('greek', 'Greece'),
        ('brazilian', 'Brazil'), ('argentine', 'Argentina'), ('mexican', 'Mexico'),
        ('chinese', 'China'), ('japanese', 'Japan'), ('korean', 'Korea'),
        ('uefa', 'International'), ('conmebol', 'International'), ('fifa', 'International'),
        ('egyptian', 'Egypt'),
    ]:
        if kw in t:
            return cn
    return ''


def extract_known_map_with_comments(go_filepath, var_name):
    with open(go_filepath) as f:
        src = f.read()
    m = re.search(rf'var {var_name} = map\[string\]string\{{(.*?)\n\}}', src, re.DOTALL)
    if not m:
        return []
    out = []
    for line in m.group(1).split('\n'):
        mm = re.match(r'\s*"([^"]+)":\s*"([^"]+)",?\s*(?://\s*(.*?))?\s*$', line)
        if mm:
            key, val, comment = mm.group(1), mm.group(2), mm.group(3) or ''
            out.append({'src_key': key, 'ts_id': val, 'comment': comment.strip()})
    return out


def parse_comment_for_sr(comment):
    """SR comment 格式：'Premier League (English Premier League)'"""
    m = re.match(r'^([^(]+?)\s*\(([^)]+)\)\s*$', comment)
    if m:
        return m.group(1).strip(), m.group(2).strip(), ''
    return comment, '', ''


def parse_comment_for_ls(comment):
    """LS comment 格式：'Premier League (England) → English Premier League'"""
    m = re.match(r'^([^(]+?)\s*\(([^)]+)\)\s*[→\-]+>?\s*(.+)$', comment)
    if m:
        return m.group(1).strip(), m.group(3).strip(), m.group(2).strip()
    m = re.match(r'^(.+?)\s*[→\-]+>?\s*(.+)$', comment)
    if m:
        return m.group(1).strip(), m.group(2).strip(), ''
    return comment, '', ''


def build_eval_set(side, entries, leagues_2026_json):
    """构造评估集。
    side='SR': leagues_idx key = tournament_id, value = {name, category_name}
    side='LS': leagues_idx key = id, value = {name, category}
    """
    leagues_idx = {}
    if side == 'SR':
        for l in leagues_2026_json:
            leagues_idx[l['tournament_id']] = l
    else:
        for l in leagues_2026_json:
            leagues_idx[l['id']] = l
    eval_set = []
    gt_map = {}
    ts_stubs = {}
    for e in entries:
        # src_key format: "football:xxx" or "basketball:xxx"
        parts = e['src_key'].split(':', 1)
        if len(parts) != 2:
            continue
        sport, src_id = parts
        comment = e['comment']
        if side == 'SR':
            src_name_fallback, ts_name, ts_country = parse_comment_for_sr(comment)
            l = leagues_idx.get(src_id)
            if l:
                src_name = l.get('name', src_name_fallback)
                src_cat = l.get('category_name', '')
            else:
                src_name = src_name_fallback
                src_cat = ''
            eval_set.append({
                'tournament_id': src_id,
                'name': src_name,
                'category_name': src_cat,
                'sport': sport,
            })
        else:
            src_name_fallback, ts_name, ts_country = parse_comment_for_ls(comment)
            l = leagues_idx.get(src_id)
            if l:
                src_name = l.get('name', src_name_fallback)
                src_cat = l.get('category', ts_country)
            else:
                src_name = src_name_fallback
                src_cat = ts_country
            eval_set.append({
                'id': src_id,
                'name': src_name,
                'category': src_cat,
                'sport': sport,
            })
        gt_key = f"{sport}:{src_id}"
        gt_map[gt_key] = e['ts_id']
        # v1.24 fallback: LS 注释格式不统一（有的有 → 有的没），ts_name 解析空时
        # 用 src_name_fallback（隐含约定：注释只写一个名时 SR 名 ≈ TS 名）
        effective_ts_name = ts_name or src_name_fallback
        if e['ts_id'] not in ts_stubs:
            ts_stubs[e['ts_id']] = {
                'id': e['ts_id'],
                'name': effective_ts_name,
                'country': ts_country or _infer_country_from_tsname(effective_ts_name),
                'sport': sport,
            }
    return eval_set, gt_map, list(ts_stubs.values())


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--output-json',
                    default='docs/tests/evidence_first_strict_wide_baseline.json')
    ap.add_argument('--use-real-pool', action='store_true',
                    help='用 python/data/ts_pool_real.json (生产 DB 真实 3988 TS 联赛) 替代 stub 池')
    args = ap.parse_args()

    sr_entries = extract_known_map_with_comments(
        os.path.join(REPO_ROOT, 'internal/matcher/league.go'),
        'KnownLeagueMap')
    ls_entries = extract_known_map_with_comments(
        os.path.join(REPO_ROOT, 'internal/matcher/ls_engine.go'),
        'KnownLSLeagueMap')
    print(f'SR KnownLeagueMap entries: {len(sr_entries)}')
    print(f'LS KnownLSLeagueMap entries: {len(ls_entries)}')

    if args.use_real_pool:
        ts_pool_base = json.load(open(os.path.join(DATA_DIR, 'ts_pool_real.json')))
        print(f'[wide_baseline] real pool: {len(ts_pool_base)} TS leagues (production DB)')
    else:
        ts_pool_base = json.load(open(os.path.join(DATA_DIR, 'ts_leagues_2026.json')))
    gt_records = json.load(open(os.path.join(DATA_DIR, 'sr_ts_ground_truth.json')))
    sr_leagues = json.load(open(os.path.join(DATA_DIR, 'sr_leagues_2026.json')))
    ls_leagues = json.load(open(os.path.join(DATA_DIR, 'ls_leagues_2026.json')))

    # 候选池：ts_leagues_2026 + GT 反推
    ts_ids = {t['id'] for t in ts_pool_base}
    extra_from_gt = {}
    for r in gt_records:
        tid = r.get('ts_competition_id', '')
        tname = r.get('ts_competition_name', '')
        sport = r.get('sport', 'football')
        if tid and tid not in ts_ids and tid not in extra_from_gt:
            extra_from_gt[tid] = {'id': tid, 'name': tname, 'country': _infer_country_from_tsname(tname), 'sport': sport}
    ts_pool = list(ts_pool_base) + list(extra_from_gt.values())
    ts_ids = {t['id'] for t in ts_pool}

    # SR/LS eval set 构造（含 TS stub 补充候选池）
    sr_eval_set, sr_gt, sr_ts_stubs = build_eval_set('SR', sr_entries, sr_leagues)
    ls_eval_set, ls_gt, ls_ts_stubs = build_eval_set('LS', ls_entries, ls_leagues)

    # 把 stub 加入候选池（仅当 ts_id 不在池里）
    for s in sr_ts_stubs + ls_ts_stubs:
        if s['id'] not in ts_ids:
            ts_pool.append(s)
            ts_ids.add(s['id'])

    print(f'TS 候选池: {len(ts_pool)} (base={len(ts_pool_base)} +GT={len(extra_from_gt)} '
          f'+SR_stub={sum(1 for s in sr_ts_stubs if s["id"] not in {t["id"] for t in ts_pool_base} - set(extra_from_gt.keys()))} '
          f'+LS_stub=...)')

    sr_result = eval_side('SR', sr_eval_set, sr_gt, ts_pool, topk_k=7)
    ls_result = eval_side('LS', ls_eval_set, ls_gt, ts_pool, topk_k=7)

    out = {
        'generated_at': datetime.now(timezone.utc).isoformat(),
        'mode': 'strict-no-mapping wide (v1.21)',
        'description': 'Strict eval 扩到 KnownLeagueMap + KnownLSLeagueMap 全 100 个 GT 联赛',
        'ts_pool_size': len(ts_pool),
        'eval_set_source': 'KnownLeagueMap / KnownLSLeagueMap (v1.18 决议允许作 GT，不进算法)',
        'sr': sr_result, 'ls': ls_result,
        'summary': {
            'sr_overall': sr_result['overall'],
            'sr_by_sport': sr_result['by_sport'],
            'ls_overall': ls_result['overall'],
            'ls_by_sport': ls_result['by_sport'],
        },
    }
    out_path = os.path.join(REPO_ROOT, args.output_json)
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    with open(out_path, 'w', encoding='utf-8') as f:
        json.dump(out, f, ensure_ascii=False, indent=2)

    print(f"\n=== Strict-No-Mapping Wide 基线（v1.21）===")
    print(f"TS 候选池: {len(ts_pool)} 联赛\n")
    for res in (sr_result, ls_result):
        side = res['side']
        print(f"[{side}] 总联赛 {res['total_leagues']}, 评估有效 {res['evaluable']} (no_gt={res['no_gt']}, gt_not_in_pool={res['gt_not_in_pool']})")
        print(f"  整体 Top-K:")
        print('  K=' + '  '.join(str(k) for k in K_LIST))
        print('  ' + '  '.join(f"{res['overall'][f'top{k}_accuracy']:.1%}" for k in K_LIST))
        for sp, agg in sorted(res['by_sport'].items()):
            print(f"  [sport={sp}] 总{agg['total_leagues']} 有效{agg['evaluable']}")
            print('    K=' + '  '.join(str(k) for k in K_LIST))
            print('    ' + '  '.join(f"{agg[f'top{k}_accuracy']:.1%}" for k in K_LIST))
        if res['misclassified']:
            print(f"  Top-1 误匹配 ({len(res['misclassified'])} 条) 前 10 条:")
            for m in res['misclassified'][:10]:
                print(f"    ✗ [{m['sport']}] {m['src_id']} {m['src_name']!r} ({m['src_cat']}) → Top1={m['top1_ts_name']!r} score={m['top1_score']:.3f} GT={m['gt_ts_id']}")
        print()
    print(f"→ JSON: {out_path}")


if __name__ == '__main__':
    main()
