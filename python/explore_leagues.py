#!/usr/bin/env python3
"""
探查 SR/LS/TS 数据库中 2026 年热门联赛
- SR: xp-bet-test
- LS: test-xp-lsports
- TS: test-thesports-db
"""
import pymysql
import json
from datetime import datetime

DB_HOST = '127.0.0.1'
DB_PORT = 3308
DB_PORT_LS = 3309
DB_USER = 'root'
DB_PASSWORD = 'r74pqyYtgdjlYB41jmWA'

TS_2026_START = 1767225600  # 2026-01-01 00:00:00 UTC
TS_2026_END   = 1798761600  # 2027-01-01 00:00:00 UTC

def get_conn(db, port=DB_PORT):
    return pymysql.connect(
        host=DB_HOST, port=port, user=DB_USER, password=DB_PASSWORD,
        database=db, charset='utf8mb4', connect_timeout=20
    )

def ts(msg):
    print(f"[{datetime.now().strftime('%H:%M:%S')}] {msg}", flush=True)

# ─── SR 探查 ─────────────────────────────────────────────────────────────────
def explore_sr():
    ts("=== 探查 SR 数据库 (xp-bet-test) ===")
    conn = get_conn('xp-bet-test')
    results = {}
    with conn.cursor() as c:
        # 查看 SR 数据库表
        c.execute("SHOW TABLES")
        tables = [r[0] for r in c.fetchall()]
        ts(f"SR 表数量: {len(tables)}")
        ts(f"SR 表列表: {tables}")

        # 查询 2026 年足球联赛（按比赛数排序）
        ts("查询 SR 2026 年足球联赛...")
        c.execute("""
            SELECT 
                e.tournament_id,
                COALESCE(t.name, '') AS tournament_name,
                COALESCE(cat.name, '') AS category_name,
                t.sport_id,
                COUNT(DISTINCT e.sport_event_id) AS event_count
            FROM sr_sport_event e
            LEFT JOIN sr_tournament_en t ON e.tournament_id = t.tournament_id
            LEFT JOIN sr_category_en cat ON t.category_id = cat.category_id
            WHERE e.scheduled LIKE '2026%'
              AND t.sport_id = 'sr:sport:1'
            GROUP BY e.tournament_id, t.name, cat.name, t.sport_id
            ORDER BY event_count DESC
            LIMIT 60
        """)
        sr_fb = c.fetchall()
        ts(f"SR 足球联赛数: {len(sr_fb)}")
        results['sr_football'] = [
            {'id': r[0], 'name': r[1], 'category': r[2], 'sport_id': r[3], 'event_count': r[4]}
            for r in sr_fb
        ]

        # 查询 2026 年篮球联赛
        ts("查询 SR 2026 年篮球联赛...")
        c.execute("""
            SELECT 
                e.tournament_id,
                COALESCE(t.name, '') AS tournament_name,
                COALESCE(cat.name, '') AS category_name,
                t.sport_id,
                COUNT(DISTINCT e.sport_event_id) AS event_count
            FROM sr_sport_event e
            LEFT JOIN sr_tournament_en t ON e.tournament_id = t.tournament_id
            LEFT JOIN sr_category_en cat ON t.category_id = cat.category_id
            WHERE e.scheduled LIKE '2026%'
              AND t.sport_id = 'sr:sport:2'
            GROUP BY e.tournament_id, t.name, cat.name, t.sport_id
            ORDER BY event_count DESC
            LIMIT 30
        """)
        sr_bb = c.fetchall()
        ts(f"SR 篮球联赛数: {len(sr_bb)}")
        results['sr_basketball'] = [
            {'id': r[0], 'name': r[1], 'category': r[2], 'sport_id': r[3], 'event_count': r[4]}
            for r in sr_bb
        ]

        # 查看 SR 数据总量
        c.execute("SELECT COUNT(*) FROM sr_sport_event WHERE scheduled LIKE '2026%'")
        total = c.fetchone()[0]
        ts(f"SR 2026 年比赛总数: {total}")
        results['sr_total_2026'] = total

        # 查看 SR 所有运动类型
        c.execute("""
            SELECT t.sport_id, COUNT(DISTINCT e.sport_event_id) as cnt
            FROM sr_sport_event e
            JOIN sr_tournament_en t ON e.tournament_id = t.tournament_id
            WHERE e.scheduled LIKE '2026%'
            GROUP BY t.sport_id
            ORDER BY cnt DESC
        """)
        sports = c.fetchall()
        ts(f"SR 运动类型分布: {sports}")
        results['sr_sports'] = [{'sport_id': r[0], 'count': r[1]} for r in sports]

    conn.close()
    return results

# ─── LS 探查 ─────────────────────────────────────────────────────────────────
def explore_ls():
    ts("=== 探查 LS 数据库 (test-xp-lsports) ===")
    conn = get_conn('test-xp-lsports', port=DB_PORT_LS)
    results = {}
    with conn.cursor() as c:
        # 查看 LS 数据库表
        c.execute("SHOW TABLES")
        tables = [r[0] for r in c.fetchall()]
        ts(f"LS 表数量: {len(tables)}")
        ts(f"LS 表列表: {tables}")

        # 查询 2026 年足球联赛
        ts("查询 LS 2026 年足球联赛...")
        c.execute("""
            SELECT e.tournament_id,
                   COALESCE(t.name, '') AS tournament_name,
                   COALESCE(cat.name, '') AS category_name,
                   COUNT(DISTINCT e.event_id) AS event_count
            FROM ls_sport_event e
            LEFT JOIN ls_tournament_en t ON e.tournament_id = t.tournament_id
            LEFT JOIN ls_category_en cat
                   ON t.category_id = cat.category_id AND cat.sport_id = e.sport_id
            WHERE e.sport_id = '6046'
              AND e.scheduled LIKE '2026%'
            GROUP BY e.tournament_id, t.name, cat.name
            ORDER BY event_count DESC
            LIMIT 80
        """)
        ls_fb = c.fetchall()
        ts(f"LS 足球联赛数: {len(ls_fb)}")
        results['ls_football'] = [
            {'id': str(r[0]), 'name': r[1], 'category': r[2], 'event_count': r[3]}
            for r in ls_fb
        ]

        # 查询 2026 年篮球联赛
        ts("查询 LS 2026 年篮球联赛...")
        c.execute("""
            SELECT e.tournament_id,
                   COALESCE(t.name, '') AS tournament_name,
                   COALESCE(cat.name, '') AS category_name,
                   COUNT(DISTINCT e.event_id) AS event_count
            FROM ls_sport_event e
            LEFT JOIN ls_tournament_en t ON e.tournament_id = t.tournament_id
            LEFT JOIN ls_category_en cat
                   ON t.category_id = cat.category_id AND cat.sport_id = e.sport_id
            WHERE e.sport_id = '48242'
              AND e.scheduled LIKE '2026%'
            GROUP BY e.tournament_id, t.name, cat.name
            ORDER BY event_count DESC
            LIMIT 40
        """)
        ls_bb = c.fetchall()
        ts(f"LS 篮球联赛数: {len(ls_bb)}")
        results['ls_basketball'] = [
            {'id': str(r[0]), 'name': r[1], 'category': r[2], 'event_count': r[3]}
            for r in ls_bb
        ]

        # 总量
        c.execute("SELECT COUNT(DISTINCT event_id) FROM ls_sport_event WHERE scheduled LIKE '2026%' AND sport_id='6046'")
        results['ls_fb_total_2026'] = c.fetchone()[0]
        c.execute("SELECT COUNT(DISTINCT event_id) FROM ls_sport_event WHERE scheduled LIKE '2026%' AND sport_id='48242'")
        results['ls_bb_total_2026'] = c.fetchone()[0]
        ts(f"LS 2026 足球比赛总数: {results['ls_fb_total_2026']}, 篮球: {results['ls_bb_total_2026']}")

    conn.close()
    return results

# ─── TS 探查 ─────────────────────────────────────────────────────────────────
def explore_ts():
    ts("=== 探查 TS 数据库 (test-thesports-db) ===")
    conn = get_conn('test-thesports-db')
    results = {}
    with conn.cursor() as c:
        # 查询 2026 年足球联赛
        ts("查询 TS 2026 年足球联赛...")
        c.execute("""
            SELECT m.competition_id,
                   COALESCE(comp.name, '') AS competition_name,
                   COALESCE(comp.host_country, '') AS country,
                   COUNT(DISTINCT m.match_id) AS event_count
            FROM ts_fb_match m
            LEFT JOIN ts_fb_competition comp ON m.competition_id = comp.competition_id
            WHERE m.match_time >= %s AND m.match_time < %s
            GROUP BY m.competition_id, comp.name, comp.host_country
            ORDER BY event_count DESC
            LIMIT 80
        """, (TS_2026_START, TS_2026_END))
        ts_fb = c.fetchall()
        ts(f"TS 足球联赛数: {len(ts_fb)}")
        results['ts_football'] = [
            {'id': r[0], 'name': r[1], 'country': r[2], 'event_count': r[3]}
            for r in ts_fb
        ]

        # 查询 2026 年篮球联赛
        ts("查询 TS 2026 年篮球联赛...")
        c.execute("""
            SELECT m.competition_id,
                   COALESCE(comp.name, '') AS competition_name,
                   COALESCE(comp.host_country, '') AS country,
                   COUNT(DISTINCT m.match_id) AS event_count
            FROM ts_bb_match m
            LEFT JOIN ts_bb_competition comp ON m.competition_id = comp.competition_id
            WHERE m.match_time >= %s AND m.match_time < %s
            GROUP BY m.competition_id, comp.name, comp.host_country
            ORDER BY event_count DESC
            LIMIT 40
        """, (TS_2026_START, TS_2026_END))
        ts_bb = c.fetchall()
        ts(f"TS 篮球联赛数: {len(ts_bb)}")
        results['ts_basketball'] = [
            {'id': r[0], 'name': r[1], 'country': r[2], 'event_count': r[3]}
            for r in ts_bb
        ]

        # 查看 SR↔TS 映射表结构
        ts("查看 SR↔TS 映射表...")
        for tbl in ['ts_sr_match_mapping', 'ts_sr_team_mapping']:
            c.execute(f"DESCRIBE {tbl}")
            cols = c.fetchall()
            ts(f"  {tbl} 字段: {[col[0] for col in cols]}")
            c.execute(f"SELECT COUNT(*) FROM {tbl}")
            cnt = c.fetchone()[0]
            ts(f"  {tbl} 总记录数: {cnt}")
            results[f'{tbl}_count'] = cnt
            results[f'{tbl}_cols'] = [col[0] for col in cols]

        # 查看 ts_sr_match_mapping 样本
        c.execute("SELECT * FROM ts_sr_match_mapping LIMIT 5")
        samples = c.fetchall()
        ts(f"  ts_sr_match_mapping 样本: {samples}")
        results['ts_sr_match_mapping_sample'] = [list(r) for r in samples]

        # 查看 ts_sr_team_mapping 样本
        c.execute("SELECT * FROM ts_sr_team_mapping LIMIT 5")
        samples2 = c.fetchall()
        ts(f"  ts_sr_team_mapping 样本: {samples2}")
        results['ts_sr_team_mapping_sample'] = [list(r) for r in samples2]

        # 总量
        c.execute("SELECT COUNT(*) FROM ts_fb_match WHERE match_time >= %s AND match_time < %s", (TS_2026_START, TS_2026_END))
        results['ts_fb_total_2026'] = c.fetchone()[0]
        c.execute("SELECT COUNT(*) FROM ts_bb_match WHERE match_time >= %s AND match_time < %s", (TS_2026_START, TS_2026_END))
        results['ts_bb_total_2026'] = c.fetchone()[0]
        ts(f"TS 2026 足球比赛总数: {results['ts_fb_total_2026']}, 篮球: {results['ts_bb_total_2026']}")

    conn.close()
    return results

if __name__ == '__main__':
    all_results = {}
    all_results['sr'] = explore_sr()
    all_results['ls'] = explore_ls()
    all_results['ts'] = explore_ts()

    # 保存探查结果
    out_path = '/home/ubuntu/sports-matcher-go/python/explore_results.json'
    with open(out_path, 'w', encoding='utf-8') as f:
        json.dump(all_results, f, ensure_ascii=False, indent=2)
    ts(f"\n探查完成，结果已保存到: {out_path}")
