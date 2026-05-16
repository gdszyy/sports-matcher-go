"""
evidence_first_strict_baseline.py — Strict 无 mapping 模式基线评估（v1.18）

目的：杜绝 KnownLeagueMap 自证算法（v1.18 决议）后，评估"算法只靠联赛名 + 国别
相似度从全量 TS 候选池里找正确联赛"的真实水平。

评估集来源：sr_ts_ground_truth.json 反推（带真实 ts_competition_name）。
"""

from __future__ import annotations
import argparse
import difflib
import json
import os
import re
import sys
import unicodedata
from collections import Counter, defaultdict
from datetime import datetime, timezone
from typing import Any, Dict, List, Tuple

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.path.join(SCRIPT_DIR, 'data')
REPO_ROOT = os.path.dirname(SCRIPT_DIR)

sys.path.insert(0, SCRIPT_DIR)
from evidence_first_baseline_eval import LS_2026_LEAGUES  # noqa: E402

# ─────────────────────────────────────────────────────────────────────────────
# 名称归一化 + 相似度
# ─────────────────────────────────────────────────────────────────────────────

_NORM_KEEP = re.compile(r'[a-z0-9 ]')

def normalize(s):
    if not s:
        return ''
    s = unicodedata.normalize('NFKD', s)
    s = ''.join(c for c in s if not unicodedata.combining(c))
    s = s.lower()
    out = []
    for c in s:
        if c in '-_/.,':
            out.append(' ')
        elif _NORM_KEEP.match(c):
            out.append(c)
    return re.sub(r'\s+', ' ', ''.join(out)).strip()


def jaro_winkler(s1, s2):
    if s1 == s2:
        return 1.0
    if not s1 or not s2:
        return 0.0
    l1, l2 = len(s1), len(s2)
    match_dist = max(l1, l2) // 2 - 1
    s1m = [False] * l1
    s2m = [False] * l2
    matches = 0
    for i in range(l1):
        st = max(0, i - match_dist)
        en = min(i + match_dist + 1, l2)
        for j in range(st, en):
            if s2m[j] or s1[i] != s2[j]:
                continue
            s1m[i] = True
            s2m[j] = True
            matches += 1
            break
    if matches == 0:
        return 0.0
    k = trans = 0
    for i in range(l1):
        if not s1m[i]:
            continue
        while not s2m[k]:
            k += 1
        if s1[i] != s2[k]:
            trans += 1
        k += 1
    jaro = (matches / l1 + matches / l2 + (matches - trans / 2) / matches) / 3
    prefix = 0
    for i in range(min(4, l1, l2)):
        if s1[i] == s2[i]:
            prefix += 1
        else:
            break
    return jaro + prefix * 0.1 * (1 - jaro)


def name_similarity(a, b):
    na, nb = normalize(a), normalize(b)
    if not na or not nb:
        return 0.0
    if na == nb:
        return 1.0
    seq = difflib.SequenceMatcher(None, na, nb).ratio()
    jw = jaro_winkler(na, nb)
    return max(seq, jw) * 0.6 + min(seq, jw) * 0.4


def is_international(s):
    if not s:
        return False
    return bool(re.search(r'international|world|continental|cup', s.lower()))


# ─────────────────────────────────────────────────────────────────────────────
# 联赛匹配（strict mode）
# ─────────────────────────────────────────────────────────────────────────────

def match_league_topk(src_name, src_category, src_sport, ts_pool, k=5):
    scored = []
    for ts in ts_pool:
        if ts.get('sport') != src_sport:
            continue
        ts_name = ts.get('name', '')
        ts_country = ts.get('country', '') or ''
        base = name_similarity(src_name, ts_name)
        if src_category and ts_country:
            loc = name_similarity(src_category, ts_country)
            if not is_international(src_category) and not is_international(ts_country):
                if loc < 0.4:
                    continue
            if loc >= 0.6:
                base = base * 0.75 + 0.25 * loc
            elif loc >= 0.4:
                base = base * (0.70 + 0.30 * (loc - 0.4) / 0.2)
        if base < 0.30:
            continue
        scored.append((base, ts))
    scored.sort(key=lambda x: -x[0])
    return [{'ts_id': ts['id'], 'ts_name': ts['name'], 'ts_country': ts.get('country', ''),
             'score': round(s, 4)} for s, ts in scored[:k]]


def eval_side(side, src_leagues, gt_map, ts_pool):
    rows = []
    top1_hits = top5_hits = no_gt = gt_not_in_pool = 0
    misclass = []
    for sl in src_leagues:
        if side == 'SR':
            src_id = sl['tournament_id']
            src_name = sl['name']
            src_cat = sl.get('category_name', '')
            src_sport = sl.get('sport', 'football')
        else:
            src_id = sl['id']
            src_name = sl['name']
            src_cat = sl.get('category', '')
            src_sport = sl.get('sport', 'football')
        gt_key = f"{src_sport}:{src_id}"
        gt_ts = gt_map.get(gt_key, '')
        topk = match_league_topk(src_name, src_cat, src_sport, ts_pool, k=5)
        topk_ids = [c['ts_id'] for c in topk]
        if not gt_ts:
            no_gt += 1
            row_status = 'NO_GT'
        else:
            in_pool = any(t['id'] == gt_ts for t in ts_pool)
            if not in_pool:
                gt_not_in_pool += 1
                row_status = 'GT_NOT_IN_POOL'
            else:
                t1 = bool(topk and topk[0]['ts_id'] == gt_ts)
                tk = gt_ts in topk_ids
                if t1:
                    top1_hits += 1
                if tk:
                    top5_hits += 1
                row_status = 'TOP1_HIT' if t1 else ('TOPN_HIT' if tk else 'MISS')
                if not t1:
                    misclass.append({
                        'src_id': src_id, 'src_name': src_name, 'src_cat': src_cat,
                        'gt_ts_id': gt_ts,
                        'top1_ts_id': topk[0]['ts_id'] if topk else '',
                        'top1_ts_name': topk[0]['ts_name'] if topk else '',
                        'top1_score': topk[0]['score'] if topk else 0.0,
                        'topk_ids': topk_ids, 'status': row_status,
                    })
        rows.append({
            'src_id': src_id, 'src_name': src_name, 'src_category': src_cat, 'sport': src_sport,
            'gt_ts_id': gt_ts,
            'top1_ts_id': topk[0]['ts_id'] if topk else '',
            'top1_ts_name': topk[0]['ts_name'] if topk else '',
            'top1_score': topk[0]['score'] if topk else 0.0,
            'top5': topk, 'status': row_status,
        })
    total = len(src_leagues)
    evaluable = total - no_gt - gt_not_in_pool
    return {
        'side': side, 'total_leagues': total,
        'no_gt': no_gt, 'gt_not_in_pool': gt_not_in_pool, 'evaluable': evaluable,
        'top1_hits': top1_hits, 'top5_hits': top5_hits,
        'top1_accuracy': round(top1_hits / evaluable, 4) if evaluable else 0.0,
        'top5_recall': round(top5_hits / evaluable, 4) if evaluable else 0.0,
        'misclassified': misclass, 'per_league': rows,
    }


def _infer_country_from_tsname(tsname):
    t = tsname.lower()
    for kw, cn in [
        ('spanish', 'Spain'), ('italian', 'Italy'), ('english', 'England'),
        ('german', 'Germany'), ('french', 'France'), ('dutch', 'Netherlands'),
        ('portuguese', 'Portugal'), ('belgian', 'Belgium'), ('turkish', 'Turkey'),
        ('russian', 'Russia'), ('polish', 'Poland'), ('croatian', 'Croatia'),
        ('scottish', 'Scotland'), ('swiss', 'Switzerland'), ('swedish', 'Sweden'),
        ('norwegian', 'Norway'), ('austrian', 'Austria'), ('greek', 'Greece'),
        ('brazilian', 'Brazil'), ('argentine', 'Argentina'), ('mexican', 'Mexico'),
        ('uefa', 'International'), ('conmebol', 'International'), ('fifa', 'International'),
        ('mls', 'USA'), ('nba', 'USA'), ('cba', 'China'), ('chinese', 'China'),
        ('japanese', 'Japan'), ('korean', 'Korea'),
    ]:
        if kw in t:
            return cn
    return ''


def build_eval_set_from_gt(gt_records, sr_leagues_idx):
    """从 sr_ts_ground_truth.json 反推 SR 评估集 + GT + 补充 TS pool。
    sr_leagues_idx: sr_tournament_id → category_name（从 sr_leagues_2026.json）"""
    seen_sr = {}
    extra_ts = {}
    for r in gt_records:
        srid = r.get('sr_tournament_id', '')
        srname = r.get('sr_tournament_name', '')
        tsid = r.get('ts_competition_id', '')
        tsname = r.get('ts_competition_name', '')
        sport = r.get('sport', 'football')
        if not srid or not tsid:
            continue
        if srid not in seen_sr:
            seen_sr[srid] = {
                'tournament_id': srid, 'name': srname,
                'category_name': sr_leagues_idx.get(srid, ''),
                'sport': sport, 'gt_ts_id': tsid, 'gt_ts_name': tsname,
            }
        if tsid not in extra_ts:
            extra_ts[tsid] = {
                'id': tsid, 'name': tsname,
                'country': _infer_country_from_tsname(tsname),
                'sport': sport,
            }
    return (list(seen_sr.values()),
            {f"{lg['sport']}:{lg['tournament_id']}": lg['gt_ts_id'] for lg in seen_sr.values()},
            list(extra_ts.values()))


def load_json(p):
    with open(p, 'r', encoding='utf-8') as f:
        return json.load(f)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--output-json', default='docs/tests/evidence_first_strict_baseline.json')
    args = ap.parse_args()

    ts_pool_base = load_json(os.path.join(DATA_DIR, 'ts_leagues_2026.json'))
    gt_records = load_json(os.path.join(DATA_DIR, 'sr_ts_ground_truth.json'))
    ls_leagues = load_json(os.path.join(DATA_DIR, 'ls_leagues_2026.json'))

    sr_leagues_raw = load_json(os.path.join(DATA_DIR, 'sr_leagues_2026.json'))
    sr_leagues_idx = {s['tournament_id']: s.get('category_name', '') for s in sr_leagues_raw}
    sr_eval_set, sr_gt, sr_extra_ts = build_eval_set_from_gt(gt_records, sr_leagues_idx)
    ts_pool_ids = {t['id'] for t in ts_pool_base}
    ts_pool = list(ts_pool_base) + [t for t in sr_extra_ts if t['id'] not in ts_pool_ids]

    ls_gt = {f"{sport}:{lid}": ts_id for lid, sport, _, ts_id, _ in LS_2026_LEAGUES if ts_id}

    sr_result = eval_side('SR', sr_eval_set, sr_gt, ts_pool)
    ls_result = eval_side('LS', ls_leagues, ls_gt, ts_pool)

    out = {
        'generated_at': datetime.now(timezone.utc).isoformat(),
        'mode': 'strict-no-mapping (v1.18)',
        'description': '只靠 league 名+国别相似度，不使用任何 KnownLeagueMap',
        'ts_pool_size': len(ts_pool),
        'eval_set_source': 'sr_ts_ground_truth.json (反推真实匹配集)',
        'sr': sr_result, 'ls': ls_result,
        'summary': {
            'sr_top1_accuracy': sr_result['top1_accuracy'],
            'sr_top5_recall': sr_result['top5_recall'],
            'ls_top1_accuracy': ls_result['top1_accuracy'],
            'ls_top5_recall': ls_result['top5_recall'],
        },
    }
    out_path = os.path.join(REPO_ROOT, args.output_json)
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    with open(out_path, 'w', encoding='utf-8') as f:
        json.dump(out, f, ensure_ascii=False, indent=2)

    print(f"\n=== Strict-No-Mapping 基线（v1.18）===")
    print(f"TS 候选池: {len(ts_pool)} 联赛 (base={len(ts_pool_base)}, +GT 反推 {len(ts_pool) - len(ts_pool_base)})")
    for sr in (sr_result, ls_result):
        print(f"\n[{sr['side']}]")
        print(f"  总联赛数: {sr['total_leagues']}")
        print(f"  评估有效: {sr['evaluable']} (no_gt={sr['no_gt']}, gt_not_in_pool={sr['gt_not_in_pool']})")
        print(f"  Top-1 准确率: {sr['top1_accuracy']:.1%} ({sr['top1_hits']}/{sr['evaluable']})")
        print(f"  Top-5 召回: {sr['top5_recall']:.1%} ({sr['top5_hits']}/{sr['evaluable']})")
        print(f"  Top-1 误匹配数: {len(sr['misclassified'])}")
        for m in sr['misclassified'][:8]:
            print(f"    ✗ {m['src_id']} {m['src_name']!r} ({m['src_cat']}) → Top1={m['top1_ts_name']!r} (score={m['top1_score']:.3f}) GT={m['gt_ts_id']}")
    print(f"\n→ JSON 输出: {out_path}")


if __name__ == '__main__':
    main()
