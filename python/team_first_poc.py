#!/usr/bin/env python3
"""
team_first_poc.py — Evidence-First 球队优先路径 PoC（PI-006 v1.16 候选）

直接连 DB 复现球队优先候选池逻辑（无需 Go 二进制），用于快速验证假设：
当 league name matching 不可靠时，用 SR 球队名直接查 TS 事件能否
还原 P0 漏掉、传统 EF 难找的事件匹配。

测试目标：sr:tournament:929 Jordan League
  P0:           0/78 events matched
  EF TopN=5:   76/78 (跨 5 个 league candidates 凑齐)
  Team-first?  期望 ≥ 76 (绕开 league，直查 team)
"""

from __future__ import annotations

import sys
import time
import pymysql
from typing import Dict, List, Tuple

sys.path.insert(0, '/sessions/keen-determined-tesla/mnt/sports-matcher-go')
from python.db.connector import setup_tunnel, get_conn, TS_DB

# 用 Python 直接复现 teamNameSimilarity (Jaccard)
import re
def normalize(s: str) -> str:
    s = re.sub(r'[^\w\s]', ' ', s.lower())
    s = re.sub(r'\s+', ' ', s).strip()
    return s

def tokens(s: str) -> set:
    return set(t for t in normalize(s).split() if len(t) >= 3)

def jaccard(a: str, b: str) -> float:
    ta, tb = tokens(a), tokens(b)
    if not ta or not tb:
        return 0.0
    return len(ta & tb) / len(ta | tb)


def main():
    sr_tournament_id = 'sr:tournament:929'
    sport = 'football'

    proc = setup_tunnel()

    # 1. 从 SR DB 拉 Jordan League 球队 + 事件时间窗
    print("=== Step 1: SR 球队 + 时间窗 ===")
    sr_conn = pymysql.connect(host='127.0.0.1', port=3308, user='root',
                              password='r74pqyYtgdjlYB41jmWA',
                              db='xp-bet-test', charset='utf8mb4')
    with sr_conn.cursor() as c:
        # SR 球队名（从 sr_competitor_en）
        c.execute("""
            SELECT DISTINCT c.competitor_id, c.name
            FROM sr_sport_event e
            JOIN sr_competitor_en c ON c.competitor_id IN (e.home_competitor_id, e.away_competitor_id)
            WHERE e.tournament_id = %s
            LIMIT 100
        """, (sr_tournament_id,))
        sr_teams = {row[0]: row[1] for row in c.fetchall() if row[1]}
        print(f"  SR teams: {len(sr_teams)}")
        for tid, name in list(sr_teams.items())[:5]:
            print(f"    {tid}: {name}")

        # SR 事件时间范围
        c.execute("""
            SELECT MIN(scheduled), MAX(scheduled), COUNT(*)
            FROM sr_sport_event
            WHERE tournament_id = %s AND scheduled IS NOT NULL
        """, (sr_tournament_id,))
        tmin_str, tmax_str, total = c.fetchone()
        print(f"  SR events: {total}, time {tmin_str} ~ {tmax_str}")
    sr_conn.close()

    # 解析时间窗为 Unix（加 7 天 padding）
    from datetime import datetime, timezone, timedelta
    def to_unix(s):
        return int(datetime.fromisoformat(s.replace('Z', '+00:00')).timestamp())
    sr_tmin_unix = to_unix(tmin_str) - 7 * 86400
    sr_tmax_unix = to_unix(tmax_str) + 7 * 86400
    print(f"  Time window (Unix ±7d padding): {sr_tmin_unix} ~ {sr_tmax_unix}")

    # 2. TS 球队 fuzzy 匹配
    print("\n=== Step 2: TS 球队 fuzzy 匹配 ===")
    ts_conn = get_conn(TS_DB)
    t_start = time.time()
    with ts_conn.cursor() as c:
        c.execute("SELECT team_id, name, competition_id FROM ts_fb_team LIMIT 200000")
        ts_teams = c.fetchall()
    print(f"  Loaded {len(ts_teams):,} TS football teams in {time.time()-t_start:.2f}s")

    # 对每个 SR 球队名找 top-3 候选
    sr_to_ts: Dict[str, List[Tuple[str, str, float, str]]] = {}
    matched_ts_team_ids: set = set()
    for sr_id, sr_name in sr_teams.items():
        scored = []
        for ts_id, ts_name, ts_comp in ts_teams:
            sim = jaccard(sr_name, ts_name or '')
            if sim >= 0.5:
                scored.append((ts_id, ts_name, sim, ts_comp))
        scored.sort(key=lambda x: -x[2])
        top3 = scored[:3]
        sr_to_ts[sr_id] = top3
        for t in top3:
            matched_ts_team_ids.add(t[0])

    matched_count = sum(1 for v in sr_to_ts.values() if v)
    print(f"  SR teams matched to TS: {matched_count}/{len(sr_teams)}")
    print(f"  Unique TS team_ids: {len(matched_ts_team_ids)}")
    print("\n  Sample mappings:")
    for sr_id, ts_list in list(sr_to_ts.items())[:5]:
        sr_name = sr_teams[sr_id]
        if ts_list:
            top = ts_list[0]
            print(f"    SR {sr_name[:25]:25s} → TS {top[1][:30]:30s} sim={top[2]:.3f} comp={top[3]}")

    # 3. 按 team_id 拉事件
    print("\n=== Step 3: 按 TS team_ids 拉事件 ===")
    if not matched_ts_team_ids:
        print("  No TS team matched, abort.")
        return
    t_start = time.time()
    team_id_list = list(matched_ts_team_ids)
    placeholders = ','.join(['%s'] * len(team_id_list))
    with ts_conn.cursor() as c:
        sql = f"""
            SELECT competition_id, match_id, match_time, home_team_id, away_team_id
            FROM ts_fb_match
            WHERE (home_team_id IN ({placeholders}) OR away_team_id IN ({placeholders}))
              AND match_time BETWEEN %s AND %s
            ORDER BY match_time
            LIMIT 15000
        """
        args = team_id_list + team_id_list + [sr_tmin_unix, sr_tmax_unix]
        c.execute(sql, args)
        ts_events = c.fetchall()
    print(f"  TS events fetched: {len(ts_events)} in {time.time()-t_start:.2f}s")

    # 按 competition_id 分布
    from collections import Counter
    comp_counter = Counter(ev[0] for ev in ts_events)
    print(f"  Competition distribution (top 10):")
    for comp_id, cnt in comp_counter.most_common(10):
        print(f"    {comp_id}: {cnt} events")

    # 4. 简化 PoC 评估：SR 事件 vs TS 事件按 (home_team_id_via_alias, away_team_id_via_alias, ±3h) 配对
    print("\n=== Step 4: 简易事件级配对（仅做存在性证明，非完整 EF P3）===")

    sr_conn = pymysql.connect(host='127.0.0.1', port=3308, user='root',
                              password='r74pqyYtgdjlYB41jmWA',
                              db='xp-bet-test', charset='utf8mb4')
    with sr_conn.cursor() as c:
        c.execute("""
            SELECT sport_event_id, home_competitor_id, away_competitor_id, scheduled
            FROM sr_sport_event
            WHERE tournament_id = %s AND scheduled >= '2025-01-01'
            ORDER BY scheduled
        """, (sr_tournament_id,))
        sr_events = c.fetchall()
    sr_conn.close()

    # 建 SR team_id → top1 TS team_id 映射
    sr_to_top1_ts = {sr_id: ts_list[0][0] if ts_list else None for sr_id, ts_list in sr_to_ts.items()}

    # 索引 TS events by (home, away)
    ts_by_pair = {}
    for ev in ts_events:
        comp_id, mid, mt, home, away = ev
        ts_by_pair.setdefault((home, away), []).append((comp_id, mid, mt))

    matched = 0
    matched_samples = []
    for sr_ev in sr_events:
        sr_eid, sr_home, sr_away, sr_sched = sr_ev
        if not sr_home or not sr_away or not sr_sched:
            continue
        ts_home = sr_to_top1_ts.get(sr_home)
        ts_away = sr_to_top1_ts.get(sr_away)
        if not ts_home or not ts_away:
            continue
        sr_unix = to_unix(sr_sched)
        # 查正向
        candidates = ts_by_pair.get((ts_home, ts_away), [])
        # 查反向
        candidates += ts_by_pair.get((ts_away, ts_home), [])
        for comp_id, mid, mt in candidates:
            if abs(mt - sr_unix) <= 3 * 3600:  # ±3h
                matched += 1
                if len(matched_samples) < 5:
                    matched_samples.append((sr_eid, sr_home, sr_away, sr_sched, comp_id, mid))
                break

    print(f"  Total SR events: {len(sr_events)}")
    print(f"  Team-first matched: {matched}")
    print(f"  Match rate: {matched/max(1,len(sr_events))*100:.1f}%")
    print(f"\n  对照基线:")
    print(f"    P0:            0/78    (league 错误)")
    print(f"    EF TopN=5:    76/78    (97.4% via league Top-N 救援)")
    print(f"    Team-first:   {matched}/{len(sr_events)}    ({matched/max(1,len(sr_events))*100:.1f}% via team-first 路径)")

    if matched_samples:
        print(f"\n  样本匹配（前 5）:")
        for s in matched_samples:
            print(f"    SR {s[0]} ({s[3][:10]}) → TS {s[5]} @ comp {s[4]}")

    # competition 的多数表决，看能否反推 league
    if matched > 0:
        matched_comps = Counter()
        for sr_ev in sr_events:
            sr_eid, sr_home, sr_away, sr_sched = sr_ev
            if not sr_home or not sr_away or not sr_sched:
                continue
            ts_home = sr_to_top1_ts.get(sr_home)
            ts_away = sr_to_top1_ts.get(sr_away)
            if not ts_home or not ts_away:
                continue
            sr_unix = to_unix(sr_sched)
            candidates = ts_by_pair.get((ts_home, ts_away), []) + ts_by_pair.get((ts_away, ts_home), [])
            for comp_id, mid, mt in candidates:
                if abs(mt - sr_unix) <= 3 * 3600:
                    matched_comps[comp_id] += 1
                    break
        print(f"\n  League 回灌（matched events 的 competition_id 分布 top 5）:")
        for cid, cnt in matched_comps.most_common(5):
            print(f"    {cid}: {cnt}")

    ts_conn.close()
    proc.terminate()


if __name__ == "__main__":
    main()
