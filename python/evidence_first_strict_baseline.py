"""
evidence_first_strict_baseline.py — Strict 无 mapping 模式基线评估（v1.18）

目的：杜绝 KnownLeagueMap 自证算法（v1.18 决议）后，评估"算法只靠联赛名 + 国别
相似度从全量 TS 候选池里找正确联赛"的真实水平。

评估集来源：sr_ts_ground_truth.json 反推（带真实 ts_competition_name）。

v1.20 (2026-05-16): 集成 LeagueAliasIndex（移植自 internal/matcher/league_alias.go），
让 Python eval 算法等价于 Go 真实算法。v1.18-v1.19 数字低估了 Go 算法能力。
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

from functools import lru_cache

@lru_cache(maxsize=20000)
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


@lru_cache(maxsize=100000)
def name_similarity(a, b):
    na, nb = normalize(a), normalize(b)
    if not na or not nb:
        return 0.0
    if na == nb:
        return 1.0
    seq = difflib.SequenceMatcher(None, na, nb).ratio()
    jw = jaro_winkler(na, nb)
    return max(seq, jw) * 0.6 + min(seq, jw) * 0.4


_ALIAS_GROUPS = None
_ALIAS_NORM_TO_CANONICAL = None
_ALIAS_CANONICAL_TO_ALIASES = None


def _load_alias_index():
    global _ALIAS_GROUPS, _ALIAS_NORM_TO_CANONICAL, _ALIAS_CANONICAL_TO_ALIASES
    if _ALIAS_GROUPS is not None:
        return
    p = os.path.join(DATA_DIR, 'league_alias_groups.json')
    with open(p, 'r', encoding='utf-8') as f:
        _ALIAS_GROUPS = json.load(f)
    _ALIAS_NORM_TO_CANONICAL = {}
    _ALIAS_CANONICAL_TO_ALIASES = {}
    for g in _ALIAS_GROUPS:
        canonical = g['canonical']
        aliases = g['aliases']
        _ALIAS_CANONICAL_TO_ALIASES[canonical] = aliases
        for alias in aliases:
            n = normalize(alias)
            if n:
                _ALIAS_NORM_TO_CANONICAL[n] = canonical


def alias_lookup(name):
    _load_alias_index()
    return _ALIAS_NORM_TO_CANONICAL.get(normalize(name))


def alias_get_all_by_name(name):
    _load_alias_index()
    canonical = _ALIAS_NORM_TO_CANONICAL.get(normalize(name))
    if not canonical:
        return []
    return _ALIAS_CANONICAL_TO_ALIASES.get(canonical, [])


# v1.33 P0: canonical → default_country 反查表（懒加载）
_ALIAS_CANONICAL_TO_DEFAULT_COUNTRY = None

def alias_default_country(name):
    """v1.33: 返回 name 所在 alias group 的 default_country。"""
    global _ALIAS_CANONICAL_TO_DEFAULT_COUNTRY
    _load_alias_index()
    if _ALIAS_CANONICAL_TO_DEFAULT_COUNTRY is None:
        # 从 league_alias_groups.json 反查 default_country
        d = {}
        for g in _ALIAS_GROUPS:
            if g.get('default_country'):
                d[g['canonical']] = g['default_country']
        _ALIAS_CANONICAL_TO_DEFAULT_COUNTRY = d
    canonical = _ALIAS_NORM_TO_CANONICAL.get(normalize(name))
    if not canonical:
        return ''
    return _ALIAS_CANONICAL_TO_DEFAULT_COUNTRY.get(canonical, '')


@lru_cache(maxsize=200000)
def league_name_similarity_with_alias(a, b):
    """等价 Go leagueNameSimilarityWithAlias + 短路优化。"""
    base = name_similarity(a, b)
    ca = alias_lookup(a)
    cb = alias_lookup(b)
    if ca and cb and ca == cb:
        return 0.98
    # 短路：base 极低且都不在 alias 字典 → 直接 return（省 alias 展开）
    if not ca and not cb:
        return base
    best = base
    if ca:
        for alias in alias_get_all_by_name(a):
            s = name_similarity(alias, b)
            if s > best:
                best = s
    if cb:
        for alias in alias_get_all_by_name(b):
            s = name_similarity(a, alias)
            if s > best:
                best = s
    return best



def is_international(s):
    """v1.32: 等价 Go lsInternationalCategory 关键词集（含 UEFA/CONMEBOL/AFC/CAF/FIFA/CONCACAF
    等国际组织名）。'cup' 单独命中跨国杯赛（FA Cup 之类是国内 cup，但本函数主要给
    international vs domestic veto 用，会与 'cup' 之外的更强信号配合，不会误伤）。"""
    if not s:
        return False
    return bool(re.search(
        r'international|world|continental|uefa|conmebol|caf|afc|fifa|concacaf|'
        r'europe|europa|asia|africa|america|oceania', s.lower()))


# v1.37b country alias 等价（等价 Go geoAliasIndex）
_COUNTRY_ALIASES = {
    'usa': 'usa', 'united states': 'usa', 'us': 'usa', 'america': 'usa',
    'uk': 'uk', 'united kingdom': 'uk', 'britain': 'uk', 'great britain': 'uk',
    'south korea': 'korea', 'korea republic': 'korea', 'republic of korea': 'korea', 'korea': 'korea',
    'czech republic': 'czech', 'czech': 'czech',
    'macedonia': 'macedonia', 'north macedonia': 'macedonia',
    'ivory coast': 'civ', "cote d'ivoire": 'civ', 'cote divoire': 'civ',
    'congo dr': 'cd', 'dr congo': 'cd', 'democratic republic of the congo': 'cd',
}

def country_canonical(c):
    if not c:
        return ''
    n = c.strip().lower()
    return _COUNTRY_ALIASES.get(n, n)


def country_equal(a, b):
    """v1.37b: country 是否同 (用 alias 表 + 字符串相等)。"""
    return country_canonical(a) == country_canonical(b)




# v1.36: word-based tier map（次级缩写如 "Liga EBA"=4, "Liga B"=2）
_WORD_TIER_MAP = {
    'liga eba': 4, 'liga leb': 2, 'leb oro': 2, 'leb plata': 3,
    'serie a2': 2, 'serie b basketball': 2, 'serie c basketball': 3,
    'liga argentina b': 2, 'argentina liga b': 2,
    'serie c': 3,  # 基础 fallback
}

# v1.23/v1.35 tier 提取 + veto，等价 Go league_features.go::extractTierNumber + CheckLeagueVeto
_TIER_PATTERNS = [
    # "2. Bundesliga", "3. Liga", "2.Bundesliga"
    (re.compile(r'\b(\d+)\s*\.?\s*(?:bundesliga|liga|division|tier)\b', re.I), lambda m: int(m.group(1))),
    # "Liga 1", "Liga 2", "League 1", "Series A2"
    (re.compile(r'\b(?:liga|league|serie|series)\s+(\d+)\b', re.I), lambda m: int(m.group(1))),
    # "Premier League 2", "Premier League 3"
    (re.compile(r'\bpremier league\s+(\d+)\b', re.I), lambda m: int(m.group(1))),
    # "Serie A2", "Serie B3" — 罗马数字 + 数字组合
    (re.compile(r'\bserie\s+[a-z]+(\d+)\b', re.I), lambda m: int(m.group(1))),
    # 通用 "N. xxx" 前缀（德国式）
    (re.compile(r'^\s*(\d+)\.\s*\w', re.I), lambda m: int(m.group(1))),
    # v1.35: "Bundesliga 2" (liga 前是字母如 "s") / "League 2" 同理
    (re.compile(r'(?:bundes|euro|champions|primer[ao]|segunda)?liga\s+(\d+)\b', re.I), lambda m: int(m.group(1))),
    # v1.35: "K2 League" / "A2 League" — 字母 + 数字 + league 模式
    (re.compile(r'\b[a-z](\d+)\s+(?:league|liga|serie|division)\b', re.I), lambda m: int(m.group(1))),
]


def extract_tier_number(name):
    """从联赛名提取层级数字，0 表示未检测到（隐式 1 级或非分级赛事）。"""
    if not name:
        return 0
    for pat, fn in _TIER_PATTERNS:
        m = pat.search(name)
        if m:
            try:
                v = fn(m)
                if 1 <= v <= 9:
                    return v
            except (ValueError, IndexError):
                pass
    # v1.36: word-based tier map fallback
    lower = name.lower()
    for phrase, t in _WORD_TIER_MAP.items():
        if phrase in lower:
            return t
    return 0


_YOUTH_KEYWORDS = ['youth', 'junior', 'juniors', 'reserve', 'reserves',
                   'academy', 'b team', 'b-team', 'ii team',
                   'mloda', 'mloda', 'jovenes', 'jeunes', 'giovani',
                   'primavera', 'sub', 'feminin']

def has_youth_marker(name):
    """检测联赛名是否含 youth/reserve 标记词（多语言）。"""
    if not name:
        return False
    lower = name.lower()
    for kw in _YOUTH_KEYWORDS:
        if kw in lower:
            return True
    return False


def check_youth_veto(name_a, name_b):
    """一侧含 youth/reserve 标记另一侧没有 → veto（v1.36 多语言扩展）。"""
    a_yth = has_youth_marker(name_a)
    b_yth = has_youth_marker(name_b)
    return a_yth != b_yth


def check_tier_veto(name_a, name_b):
    """两侧 tier 冲突即 veto。
    - 两侧都 >0 且不同 → veto
    - 一侧 =0 另一侧 >=2 → veto（隐式 tier 1 vs N）
    """
    ta = extract_tier_number(name_a)
    tb = extract_tier_number(name_b)
    if ta > 0 and tb > 0 and ta != tb:
        return True
    if ta == 0 and tb >= 2:
        return True
    if tb == 0 and ta >= 2:
        return True
    return False



# ─────────────────────────────────────────────────────────────────────────────
# 联赛匹配（strict mode）
# ─────────────────────────────────────────────────────────────────────────────

def match_league_topk(src_name, src_category, src_sport, ts_pool, k=5):
    """v1.20-v1.22: 用 league_name_similarity_with_alias 等价 Go 真实算法。

    v1.22 P0-B 增量：alias canonical hit (base>=0.95) 但 country 完全不同
    （非 international 且 locSim<0.4）→ 强降到 0.55（低于 NAME_LOW 阈值），
    避免 Russian Premier League / Serie A Brazil 这类跨地域错配。"""
    scored = []
    for ts in ts_pool:
        if ts.get('sport') != src_sport:
            continue
        ts_name = ts.get('name', '')
        # v1.30 P0: 不再对 basketball 强制清空 ts_country（v1.20 是因 fetch 脚本给了脏 hash ID
        # 才强制清空，但 v1.27 后 wide_baseline 加载 real pool 时用 _infer_country_from_tsname
        # 推断，所以 country 字段是合法推断值，可以参与匹配）
        ts_country = ts.get('country', '') or ''
        # v1.23: tier veto（等价 Go CheckLeagueVeto P0-C）
        if check_tier_veto(src_name, ts_name):
            continue
        # v1.36: youth/reserve veto（多语言 mloda/junior 等）
        if check_youth_veto(src_name, ts_name):
            continue
        # v1.32 P0: international vs domestic veto
        # 一侧含 UEFA/CONMEBOL/CAF/AFC 等关键词、另一侧无 → veto
        # （National League vs UEFA Nations League / EFL Cup vs Leagues Cup *  非典型）
        if is_international(src_name) != is_international(ts_name):
            continue
        base = league_name_similarity_with_alias(src_name, ts_name)
        if src_category and ts_country:
            loc = name_similarity(src_category, ts_country)
            sr_intl = is_international(src_category)
            ts_intl = is_international(ts_country)
            # v1.22 P0-B: alias canonical hit + country 不一致 → 强降
            if base >= 0.95 and loc < 0.4 and not sr_intl and not ts_intl:
                base = 0.55
            # v1.29 P0-2: alias canonical hit + country 高匹配 → 加分让 country 一致 ts_id 赢
            elif base >= 0.95 and loc >= 0.8:
                base = min(base + 0.015, 0.999)
            else:
                if not sr_intl and not ts_intl:
                    if loc < 0.4:
                        continue
                if loc >= 0.6:
                    base = base * 0.75 + 0.25 * loc
                elif loc >= 0.4:
                    base = base * (0.70 + 0.30 * (loc - 0.4) / 0.2)
        # v1.37b/v1.38: alias default_country vs src.category 二次约束（扩 base>=0.70）
        # 攻 Premier League (Egypt) → Laos Premier League 0.912 类、
        # Primera Division Apertura (Argentina) → Spanish La Liga 0.728 类。
        # 用 country_equal 表（USA = United States）避免字面拐弯相似误判。
        # v1.38 改：base>=0.70 (NAME_MED 阈值) 才检查，不只是 alias canonical hit。
        # 既检查 alias default_country (ts.name 命中 alias) 也检查 ts.name 直接推断 country。
        if base >= 0.70 and not ts_country and src_category and not is_international(src_category):
            def_c = alias_default_country(ts_name)
            if def_c and not is_international(def_c):
                if not country_equal(src_category, def_c):
                    base = 0.55
        if base < 0.30:
            continue
        scored.append((base, ts))
    scored.sort(key=lambda x: -x[0])
    return [{'ts_id': ts['id'], 'ts_name': ts['name'], 'ts_country': ts.get('country', ''),
             'score': round(s, 4)} for s, ts in scored[:k]]


K_LIST = [1, 2, 3, 4, 5, 6, 7]


def _aggregate_topk(rows, evaluable):
    """从 per-league rows 聚合 Top-K 命中数。"""
    out = {}
    for k in K_LIST:
        hits = sum(1 for r in rows
                   if r['gt_ts_id']
                   and r['gt_in_pool']
                   and r['gt_ts_id'] in r['topk_ids'][:k])
        out[f'top{k}_hits'] = hits
        out[f'top{k}_accuracy'] = round(hits / evaluable, 4) if evaluable else 0.0
    return out


def eval_side(side, src_leagues, gt_map, ts_pool, topk_k=7):
    rows = []
    no_gt = gt_not_in_pool = 0
    misclass = []
    pool_ids_set = {t['id'] for t in ts_pool}
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
        topk = match_league_topk(src_name, src_cat, src_sport, ts_pool, k=topk_k)
        topk_ids = [c['ts_id'] for c in topk]
        gt_in_pool = bool(gt_ts) and gt_ts in pool_ids_set
        if not gt_ts:
            no_gt += 1
            row_status = 'NO_GT'
        elif not gt_in_pool:
            gt_not_in_pool += 1
            row_status = 'GT_NOT_IN_POOL'
        else:
            if topk and topk[0]['ts_id'] == gt_ts:
                row_status = 'TOP1_HIT'
            elif gt_ts in topk_ids:
                row_status = 'TOPN_HIT'
            else:
                row_status = 'MISS'
            if row_status != 'TOP1_HIT':
                misclass.append({
                    'src_id': src_id, 'src_name': src_name, 'src_cat': src_cat, 'sport': src_sport,
                    'gt_ts_id': gt_ts,
                    'top1_ts_id': topk[0]['ts_id'] if topk else '',
                    'top1_ts_name': topk[0]['ts_name'] if topk else '',
                    'top1_score': topk[0]['score'] if topk else 0.0,
                    'topk_ids': topk_ids, 'status': row_status,
                })
        rows.append({
            'src_id': src_id, 'src_name': src_name, 'src_category': src_cat, 'sport': src_sport,
            'gt_ts_id': gt_ts, 'gt_in_pool': gt_in_pool,
            'top1_ts_id': topk[0]['ts_id'] if topk else '',
            'top1_ts_name': topk[0]['ts_name'] if topk else '',
            'top1_score': topk[0]['score'] if topk else 0.0,
            'topk': topk, 'topk_ids': topk_ids, 'status': row_status,
        })
    total = len(src_leagues)
    evaluable = total - no_gt - gt_not_in_pool
    agg_all = _aggregate_topk(rows, evaluable)
    # 按 sport 聚合
    by_sport = {}
    for sp in {r['sport'] for r in rows}:
        sub = [r for r in rows if r['sport'] == sp]
        sub_eval = sum(1 for r in sub if r['gt_ts_id'] and r['gt_in_pool'])
        sub_agg = _aggregate_topk(sub, sub_eval)
        sub_agg['evaluable'] = sub_eval
        sub_agg['total_leagues'] = len(sub)
        by_sport[sp] = sub_agg
    return {
        'side': side, 'total_leagues': total,
        'no_gt': no_gt, 'gt_not_in_pool': gt_not_in_pool, 'evaluable': evaluable,
        'overall': agg_all,
        'by_sport': by_sport,
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
        'mode': 'strict-no-mapping (v1.19)',
        'description': '只靠 league 名+国别相似度，不使用任何 KnownLeagueMap',
        'ts_pool_size': len(ts_pool),
        'eval_set_source': 'sr_ts_ground_truth.json (反推真实匹配集)',
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

    print(f"\n=== Strict-No-Mapping 基线（v1.19）===")
    print(f"TS 候选池: {len(ts_pool)} 联赛 (base={len(ts_pool_base)}, +GT 反推 {len(ts_pool) - len(ts_pool_base)})")
    for res in (sr_result, ls_result):
        side = res['side']
        print(f"\n[{side}] 总联赛 {res['total_leagues']}, 评估有效 {res['evaluable']} (no_gt={res['no_gt']}, gt_not_in_pool={res['gt_not_in_pool']})")
        print(f"  整体 Top-K 准确率:")
        header = '  K=' + '  '.join(f'{k}' for k in K_LIST)
        print(header)
        row = '  '.join(f"{res['overall'][f'top{k}_accuracy']:.1%}" for k in K_LIST)
        print('  ' + row)
        for sp, agg in sorted(res['by_sport'].items()):
            print(f"\n  [sport={sp}] 总{agg['total_leagues']} 有效{agg['evaluable']}")
            row = '  '.join(f"{agg[f'top{k}_accuracy']:.1%}" for k in K_LIST)
            print('  K:  ' + '  '.join(f'{k}' for k in K_LIST))
            print('  '  + row)
        if res['misclassified']:
            print(f"\n  Top-1 误匹配 ({len(res['misclassified'])} 条):")
            for m in res['misclassified'][:10]:
                print(f"    ✗ [{m['sport']}] {m['src_id']} {m['src_name']!r} ({m['src_cat']}) → Top1={m['top1_ts_name']!r} (score={m['top1_score']:.3f}) GT={m['gt_ts_id']}")
    print(f"\n→ JSON 输出: {out_path}")


if __name__ == '__main__':
    main()
