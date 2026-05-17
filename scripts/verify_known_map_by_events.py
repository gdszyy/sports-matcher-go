#!/usr/bin/env python3
"""scripts/verify_known_map_by_events.py — 事件级 GT 真伪验证 (v1.40)

对每条 KnownLSLeagueMap SUSPECT：
1. 拉 LS 联赛 2026 球队 + 比赛 (test-xp-lsports.ls_*)
2. 拉两个 TS 候选 competition 的球队 + 比赛 (test-thesports-db.ts_*)
   - 当前 GT ts_id
   - 算法 Top-1 建议 ts_id
3. 比较球队名重叠度 + 同日同队事件重叠度
4. 哪条 TS 重叠高就是真 GT

这才是杜绝 mapping 循环论证的真 GT 判定方式（PI-006 v1.10 Jordan League 模式）。

依赖：sshtunnel + pymysql；SSH 隧道连 AWS RDS。
"""
from __future__ import annotations
import json
import os
import re
import sys
import time
import unicodedata
from collections import defaultdict

try:
    from sshtunnel import SSHTunnelForwarder
    import pymysql
except ImportError as e:
    print(f"需要 sshtunnel + pymysql: {e}")
    sys.exit(2)

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SSH_HOST = os.environ.get("SSH_HOST", "54.69.237.139")
SSH_USER = os.environ.get("SSH_USER", "ubuntu")
SSH_KEY = os.environ.get("SSH_KEY_PATH", "/tmp/sm-key")
DB_HOST = "test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com"
DB_USER, DB_PASS = "root", "r74pqyYtgdjlYB41jmWA"
TS_DB, LS_DB = "test-thesports-db", "test-xp-lsports"


def normalize_team(name):
    """简单球队名规范化（小写 + 去标点 + 去常见后缀）。"""
    if not name:
        return ""
    s = unicodedata.normalize('NFKD', name)
    s = ''.join(c for c in s if not unicodedata.combining(c)).lower()
    s = re.sub(r'\bfc\b|\bsc\b|\bcf\b|\bac\b|\bclub\b|\bteam\b', '', s)
    s = re.sub(r'[^a-z0-9 ]', ' ', s)
    s = re.sub(r'\s+', ' ', s).strip()
    return s


def fetch_ls_teams(conn, tournament_id):
    cur = conn.cursor()
    cur.execute("""
        SELECT DISTINCT COALESCE(t.name,'') FROM ls_sport_event e
        LEFT JOIN ls_competitor_en t ON e.home_competitor_id = t.competitor_id
        WHERE e.tournament_id = %s AND e.scheduled >= '2024-01-01' AND t.name IS NOT NULL
        UNION
        SELECT DISTINCT COALESCE(t.name,'') FROM ls_sport_event e
        LEFT JOIN ls_competitor_en t ON e.away_competitor_id = t.competitor_id
        WHERE e.tournament_id = %s AND e.scheduled >= '2024-01-01' AND t.name IS NOT NULL
    """, (tournament_id, tournament_id))
    return {normalize_team(r[0]) for r in cur.fetchall() if r[0]}


def fetch_ls_events(conn, tournament_id):
    cur = conn.cursor()
    cur.execute("""
        SELECT e.scheduled, COALESCE(h.name,''), COALESCE(a.name,'')
        FROM ls_sport_event e
        LEFT JOIN ls_competitor_en h ON e.home_competitor_id = h.competitor_id
        LEFT JOIN ls_competitor_en a ON e.away_competitor_id = a.competitor_id
        WHERE e.tournament_id = %s AND e.scheduled >= '2024-01-01'
        LIMIT 2000
    """, (tournament_id,))
    out = []
    for r in cur.fetchall():
        if r[1] and r[2]:
            if r[0]:
                if hasattr(r[0], 'strftime'):
                    day = r[0].strftime('%Y-%m-%d')
                else:
                    day = str(r[0])[:10]
            else:
                day = ''
            out.append((day, frozenset({normalize_team(r[1]), normalize_team(r[2])})))
    return out


def fetch_ts_teams(conn, comp_id, sport):
    table_e = 'ts_fb_match' if sport == 'football' else 'ts_bb_match'
    table_t = 'ts_fb_team' if sport == 'football' else 'ts_bb_team'
    cur = conn.cursor()
    cur.execute(f"""
        SELECT DISTINCT COALESCE(t.name,'') FROM {table_e} m
        LEFT JOIN {table_t} t ON m.home_team_id = t.team_id
        WHERE m.competition_id = %s AND m.match_time >= UNIX_TIMESTAMP('2024-01-01') AND t.name IS NOT NULL
        UNION
        SELECT DISTINCT COALESCE(t.name,'') FROM {table_e} m
        LEFT JOIN {table_t} t ON m.away_team_id = t.team_id
        WHERE m.competition_id = %s AND m.match_time >= UNIX_TIMESTAMP('2024-01-01') AND t.name IS NOT NULL
    """, (comp_id, comp_id))
    return {normalize_team(r[0]) for r in cur.fetchall() if r[0]}


def fetch_ts_events(conn, comp_id, sport):
    table_e = 'ts_fb_match' if sport == 'football' else 'ts_bb_match'
    table_t = 'ts_fb_team' if sport == 'football' else 'ts_bb_team'
    cur = conn.cursor()
    cur.execute(f"""
        SELECT m.match_time, COALESCE(h.name,''), COALESCE(a.name,'')
        FROM {table_e} m
        LEFT JOIN {table_t} h ON m.home_team_id = h.team_id
        LEFT JOIN {table_t} a ON m.away_team_id = a.team_id
        WHERE m.competition_id = %s AND m.match_time >= UNIX_TIMESTAMP('2024-01-01')
        LIMIT 2000
    """, (comp_id,))
    out = []
    for r in cur.fetchall():
        if r[1] and r[2]:
            day = time.strftime('%Y-%m-%d', time.gmtime(r[0])) if r[0] else ''
            out.append((day, frozenset({normalize_team(r[1]), normalize_team(r[2])})))
    return out


def verify_one(ls_conn, ts_conn, sport, ls_id, gt_id, suggested_id, label_gt, label_alt):
    ls_teams = fetch_ls_teams(ls_conn, ls_id)
    ls_events = fetch_ls_events(ls_conn, ls_id)
    print(f"  LS teams={len(ls_teams)} events={len(ls_events)}")
    for tid, label in [(gt_id, label_gt), (suggested_id, label_alt)]:
        ts_teams = fetch_ts_teams(ts_conn, tid, sport)
        ts_events = fetch_ts_events(ts_conn, tid, sport)
        team_overlap = len(ls_teams & ts_teams)
        # 事件重叠：同日同队
        ts_evset = set(ts_events)
        ev_overlap = sum(1 for ev in ls_events if ev in ts_evset)
        print(f"  → [{label}] ts_id={tid}")
        print(f"      ts_teams={len(ts_teams)} | team_overlap={team_overlap}/{len(ls_teams)} ({100*team_overlap/max(1,len(ls_teams)):.0f}%)")
        print(f"      ts_events={len(ts_events)} | ev_overlap={ev_overlap}/{len(ls_events)} ({100*ev_overlap/max(1,len(ls_events)):.0f}%)")


def main():
    # 默认验证 v1.39 audit 报告的 3 条最有代表性 SUSPECT
    samples = [
        ('football', '3799', '8y39mp1hwxmojxg', '9k82rekh6lrepzj',
         'GT Russian Premier League', 'Suggested Russian First League'),
        ('football', '61243', 'gx7lm7pho13m2wd', 'gx7lm7phjem2wdk',
         'GT Croatian First Football League', 'Suggested Croatian Football League'),
        ('football', '41558', 'p3glrw7hevqdyjv', '9vjxm8ghonr6odg',
         'GT Argentine Division 1', 'Suggested ARG Primera Nacional'),
    ]
    print(f"SSH {SSH_USER}@{SSH_HOST} → DB {DB_HOST}")
    with SSHTunnelForwarder(
        (SSH_HOST, 22), ssh_username=SSH_USER, ssh_pkey=SSH_KEY,
        remote_bind_address=(DB_HOST, 3306),
        local_bind_address=('127.0.0.1', 0),
    ) as tunnel:
        print(f"tunnel up @ port {tunnel.local_bind_port}")
        ls_conn = pymysql.connect(
            host='127.0.0.1', port=tunnel.local_bind_port,
            user=DB_USER, password=DB_PASS, database=LS_DB,
            charset='utf8mb4', connect_timeout=10,
        )
        ts_conn = pymysql.connect(
            host='127.0.0.1', port=tunnel.local_bind_port,
            user=DB_USER, password=DB_PASS, database=TS_DB,
            charset='utf8mb4', connect_timeout=10,
        )
        for sport, ls_id, gt_id, sug_id, label_gt, label_alt in samples:
            print(f"\n=== LS {sport}:{ls_id} ===")
            verify_one(ls_conn, ts_conn, sport, ls_id, gt_id, sug_id, label_gt, label_alt)
        ls_conn.close(); ts_conn.close()


if __name__ == '__main__':
    main()
