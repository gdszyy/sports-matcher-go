#!/usr/bin/env python3
"""
fetch_2026_data.py — SR / LS / TS 2026 年热门联赛数据拉取脚本
=================================================================
功能：
  1. 从 SR（xp-bet-test）拉取 2026 年热门 + 常规联赛的比赛/球队数据
  2. 从 LS（test-xp-lsports）拉取 2026 年热门 + 常规联赛的比赛/球队数据
  3. 从 TS（test-thesports-db）拉取 2026 年对应联赛的比赛/球队数据
  4. 导出 JSON 数据文件到 python/data/ 目录，供算法测试使用

热门联赛判断标准：
  - 使用 xp_tournament_hot 表中标记的联赛（业务热门）
  - 补充按比赛数量排名 Top 20 的联赛（足球 + 篮球）
  - 覆盖 ts_sr_match_mapping 中已有映射的联赛（保证有 ground truth）

输出文件（python/data/ 目录）：
  - sr_leagues_2026.json     SR 联赛列表
  - sr_events_2026.json      SR 比赛数据（含球队名）
  - ls_leagues_2026.json     LS 联赛列表
  - ls_events_2026.json      LS 比赛数据（含球队名）
  - ts_leagues_2026.json     TS 联赛列表
  - ts_events_2026.json      TS 比赛数据（含球队名）
  - fetch_summary.json       拉取统计摘要
"""

import pymysql
import json
import os
from datetime import datetime

# ─── 配置 ────────────────────────────────────────────────────────────────────
DB_HOST      = '127.0.0.1'
DB_PORT_SR   = 3308   # SR + TS 共用
DB_PORT_TS   = 3308
DB_PORT_LS   = 3309
DB_USER      = 'root'
DB_PASSWORD  = 'r74pqyYtgdjlYB41jmWA'

TS_2026_START = 1767225600  # 2026-01-01 00:00:00 UTC
TS_2026_END   = 1798761600  # 2027-01-01 00:00:00 UTC

OUTPUT_DIR = os.path.join(os.path.dirname(__file__), 'data')
os.makedirs(OUTPUT_DIR, exist_ok=True)

def ts_log(msg):
    print(f"[{datetime.now().strftime('%H:%M:%S')}] {msg}", flush=True)

def get_conn(db, port=DB_PORT_SR):
    return pymysql.connect(
        host=DB_HOST, port=port, user=DB_USER, password=DB_PASSWORD,
        database=db, charset='utf8mb4', connect_timeout=30
    )

def save_json(data, filename):
    path = os.path.join(OUTPUT_DIR, filename)
    with open(path, 'w', encoding='utf-8') as f:
        json.dump(data, f, ensure_ascii=False, indent=2, default=str)
    ts_log(f"  已保存: {filename} ({len(data) if isinstance(data, list) else len(data.get('leagues', data))} 条)")
    return path

# ─── SR 数据拉取 ──────────────────────────────────────────────────────────────
def fetch_sr():
    ts_log("=" * 60)
    ts_log("【SR】开始拉取 SportRadar 2026 年数据")
    conn_sr = get_conn('xp-bet-test', DB_PORT_SR)
    conn_ts = get_conn('test-thesports-db', DB_PORT_TS)

    leagues = []
    events_all = []

    with conn_sr.cursor() as c_sr, conn_ts.cursor() as c_ts:
        # Step 1: 获取热门联赛（xp_tournament_hot）
        c_sr.execute("""
            SELECT tournament_id, sport_id, name_en, category_id
            FROM xp_tournament_hot
            WHERE is_deleted = 0
            ORDER BY sort_order, id
        """)
        hot_rows = c_sr.fetchall()
        hot_ids = {r[0] for r in hot_rows}
        ts_log(f"  SR 热门联赛（xp_tournament_hot）: {len(hot_rows)} 条")

        # Step 2: 获取 ts_sr_match_mapping 中已有映射的联赛（ground truth 联赛）
        c_ts.execute("""
            SELECT DISTINCT sr_tournament_id, sport_id
            FROM ts_sr_match_mapping_3
            WHERE sr_tournament_id != ''
        """)
        mapped_leagues = c_ts.fetchall()
        mapped_ids = {r[0] for r in mapped_leagues}
        ts_log(f"  已有 SR↔TS 映射的联赛: {len(mapped_ids)} 个 → {sorted(mapped_ids)}")

        # Step 3: 获取 2026 年比赛数量 Top 20 足球联赛
        c_sr.execute("""
            SELECT e.tournament_id, t.name, t.sport_id,
                   COALESCE(cat.name, '') AS category_name,
                   COUNT(DISTINCT e.sport_event_id) AS event_count
            FROM sr_sport_event e
            LEFT JOIN sr_tournament_en t ON e.tournament_id = t.tournament_id
            LEFT JOIN sr_category_en cat ON t.category_id = cat.category_id
            WHERE e.scheduled LIKE '2026%'
              AND t.sport_id = 'sr:sport:1'
            GROUP BY e.tournament_id, t.name, t.sport_id, cat.name
            ORDER BY event_count DESC
            LIMIT 20
        """)
        top_fb = c_sr.fetchall()

        # Step 4: 获取 2026 年比赛数量 Top 10 篮球联赛
        c_sr.execute("""
            SELECT e.tournament_id, t.name, t.sport_id,
                   COALESCE(cat.name, '') AS category_name,
                   COUNT(DISTINCT e.sport_event_id) AS event_count
            FROM sr_sport_event e
            LEFT JOIN sr_tournament_en t ON e.tournament_id = t.tournament_id
            LEFT JOIN sr_category_en cat ON t.category_id = cat.category_id
            WHERE e.scheduled LIKE '2026%'
              AND t.sport_id = 'sr:sport:2'
            GROUP BY e.tournament_id, t.name, t.sport_id, cat.name
            ORDER BY event_count DESC
            LIMIT 10
        """)
        top_bb = c_sr.fetchall()

        # 合并联赛列表（热门 + 已映射 + Top N）
        target_ids = set()
        target_ids.update(hot_ids)
        target_ids.update(mapped_ids)
        target_ids.update(r[0] for r in top_fb)
        target_ids.update(r[0] for r in top_bb)
        ts_log(f"  合并后目标联赛数: {len(target_ids)}")

        # Step 5: 查询目标联赛详情
        for tid in sorted(target_ids):
            if not tid:
                continue
            c_sr.execute("""
                SELECT t.tournament_id, COALESCE(t.name,'') AS name,
                       t.sport_id, t.category_id,
                       COALESCE(cat.name,'') AS category_name
                FROM sr_tournament_en t
                LEFT JOIN sr_category_en cat ON t.category_id = cat.category_id
                WHERE t.tournament_id = %s
                LIMIT 1
            """, (tid,))
            row = c_sr.fetchone()
            if not row:
                continue
            sport_name = _sr_sport_name(row[2])
            leagues.append({
                'tournament_id': row[0],
                'name': row[1],
                'sport_id': row[2],
                'sport': sport_name,
                'category_id': row[3],
                'category_name': row[4],
                'is_hot': tid in hot_ids,
                'has_ts_mapping': tid in mapped_ids,
            })

        ts_log(f"  SR 目标联赛总数: {len(leagues)}")

        # Step 6: 拉取每个联赛的 2026 年比赛数据
        for league in leagues:
            tid = league['tournament_id']
            c_sr.execute("""
                SELECT
                    e.sport_event_id,
                    e.tournament_id,
                    COALESCE(e.scheduled, '') AS scheduled,
                    COALESCE(e.home_competitor_id, '') AS home_id,
                    COALESCE(h.name, '') AS home_name,
                    COALESCE(e.away_competitor_id, '') AS away_id,
                    COALESCE(aw.name, '') AS away_name,
                    COALESCE(e.status_code, 0) AS status_code
                FROM sr_sport_event e
                LEFT JOIN sr_competitor_en h ON e.home_competitor_id = h.competitor_id
                LEFT JOIN sr_competitor_en aw ON e.away_competitor_id = aw.competitor_id
                WHERE e.tournament_id = %s
                  AND e.scheduled LIKE '2026%%'
                ORDER BY e.scheduled
            """, (tid,))
            rows = c_sr.fetchall()
            for r in rows:
                events_all.append({
                    'source': 'SR',
                    'event_id': r[0],
                    'tournament_id': r[1],
                    'scheduled': r[2],
                    'home_id': r[3],
                    'home_name': r[4],
                    'away_id': r[5],
                    'away_name': r[6],
                    'status_code': r[7],
                    'sport': league['sport'],
                })
            league['event_count_2026'] = len(rows)
            ts_log(f"    SR [{tid}] {league['name']}: {len(rows)} 场")

    conn_sr.close()
    conn_ts.close()

    ts_log(f"  SR 总比赛数: {len(events_all)}")
    save_json(leagues, 'sr_leagues_2026.json')
    save_json(events_all, 'sr_events_2026.json')
    return leagues, events_all

# ─── LS 数据拉取 ──────────────────────────────────────────────────────────────
def fetch_ls():
    ts_log("=" * 60)
    ts_log("【LS】开始拉取 LSports 2026 年数据")
    conn = get_conn('test-xp-lsports', DB_PORT_LS)

    leagues = []
    events_all = []

    with conn.cursor() as c:
        # 足球 Top 30 联赛
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
            LIMIT 30
        """)
        top_fb = c.fetchall()

        # 篮球 Top 15 联赛
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
            LIMIT 15
        """)
        top_bb = c.fetchall()

        all_ls_leagues = []
        for r in top_fb:
            all_ls_leagues.append({'id': str(r[0]), 'name': r[1], 'category': r[2],
                                    'event_count': r[3], 'sport': 'football', 'sport_id': '6046'})
        for r in top_bb:
            all_ls_leagues.append({'id': str(r[0]), 'name': r[1], 'category': r[2],
                                    'event_count': r[3], 'sport': 'basketball', 'sport_id': '48242'})

        ts_log(f"  LS 目标联赛总数: {len(all_ls_leagues)}")

        # 拉取每个联赛的 2026 年比赛
        for lg in all_ls_leagues:
            tid = lg['id']
            sport_id = lg['sport_id']
            c.execute("""
                SELECT
                    e.event_id,
                    e.tournament_id,
                    COALESCE(e.scheduled, '') AS scheduled,
                    COALESCE(e.home_competitor_id, '') AS home_id,
                    COALESCE(hc.name, '') AS home_name,
                    COALESCE(e.away_competitor_id, '') AS away_id,
                    COALESCE(ac.name, '') AS away_name,
                    COALESCE(e.status, 0) AS status
                FROM ls_sport_event e
                LEFT JOIN ls_competitor_en hc
                    ON CAST(e.home_competitor_id AS CHAR) = CAST(hc.competitor_id AS CHAR)
                LEFT JOIN ls_competitor_en ac
                    ON CAST(e.away_competitor_id AS CHAR) = CAST(ac.competitor_id AS CHAR)
                WHERE e.tournament_id = %s
                  AND e.sport_id = %s
                  AND e.scheduled LIKE '2026%%'
                ORDER BY e.scheduled
            """, (tid, sport_id))
            rows = c.fetchall()
            for r in rows:
                events_all.append({
                    'source': 'LS',
                    'event_id': str(r[0]),
                    'tournament_id': str(r[1]),
                    'scheduled': r[2],
                    'home_id': str(r[3]),
                    'home_name': r[4],
                    'away_id': str(r[5]),
                    'away_name': r[6],
                    'status': r[7],
                    'sport': lg['sport'],
                })
            lg['event_count_fetched'] = len(rows)
            leagues.append(lg)
            ts_log(f"    LS [{tid}] {lg['name']}: {len(rows)} 场")

    conn.close()

    ts_log(f"  LS 总比赛数: {len(events_all)}")
    save_json(leagues, 'ls_leagues_2026.json')
    save_json(events_all, 'ls_events_2026.json')
    return leagues, events_all

# ─── TS 数据拉取 ──────────────────────────────────────────────────────────────
def fetch_ts():
    ts_log("=" * 60)
    ts_log("【TS】开始拉取 TheSports 2026 年数据")
    conn = get_conn('test-thesports-db', DB_PORT_TS)

    leagues = []
    events_all = []

    with conn.cursor() as c:
        # 足球 Top 30 联赛
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
            LIMIT 30
        """, (TS_2026_START, TS_2026_END))
        top_fb = c.fetchall()

        # 篮球 Top 15 联赛
        c.execute("""
            SELECT m.competition_id,
                   COALESCE(comp.name, '') AS competition_name,
                   COALESCE(comp.category_id, '') AS category_id,
                   COUNT(DISTINCT m.match_id) AS event_count
            FROM ts_bb_match m
            LEFT JOIN ts_bb_competition comp ON m.competition_id = comp.competition_id
            WHERE m.match_time >= %s AND m.match_time < %s
            GROUP BY m.competition_id, comp.name, comp.category_id
            ORDER BY event_count DESC
            LIMIT 15
        """, (TS_2026_START, TS_2026_END))
        top_bb = c.fetchall()

        all_ts_leagues = []
        for r in top_fb:
            all_ts_leagues.append({'id': r[0], 'name': r[1], 'country': r[2],
                                    'event_count': r[3], 'sport': 'football'})
        for r in top_bb:
            all_ts_leagues.append({'id': r[0], 'name': r[1], 'country': r[2],
                                    'event_count': r[3], 'sport': 'basketball'})

        ts_log(f"  TS 目标联赛总数: {len(all_ts_leagues)}")

        # 拉取每个联赛的 2026 年比赛
        for lg in all_ts_leagues:
            cid = lg['id']
            sport = lg['sport']
            if sport == 'football':
                c.execute("""
                    SELECT
                        m.match_id,
                        m.competition_id,
                        m.match_time,
                        COALESCE(m.home_team_id, '') AS home_id,
                        COALESCE(ht.name, '') AS home_name,
                        COALESCE(m.away_team_id, '') AS away_id,
                        COALESCE(at2.name, '') AS away_name,
                        COALESCE(m.status_id, 0) AS status_id
                    FROM ts_fb_match m
                    LEFT JOIN ts_fb_team ht ON m.home_team_id = ht.team_id
                    LEFT JOIN ts_fb_team at2 ON m.away_team_id = at2.team_id
                    WHERE m.competition_id = %s
                      AND m.match_time >= %s AND m.match_time < %s
                    ORDER BY m.match_time
                """, (cid, TS_2026_START, TS_2026_END))
            else:
                c.execute("""
                    SELECT
                        m.match_id,
                        m.competition_id,
                        m.match_time,
                        COALESCE(m.home_team_id, '') AS home_id,
                        COALESCE(ht.name, '') AS home_name,
                        COALESCE(m.away_team_id, '') AS away_id,
                        COALESCE(at2.name, '') AS away_name,
                        COALESCE(m.status_id, 0) AS status_id
                    FROM ts_bb_match m
                    LEFT JOIN ts_bb_team ht ON m.home_team_id = ht.team_id
                    LEFT JOIN ts_bb_team at2 ON m.away_team_id = at2.team_id
                    WHERE m.competition_id = %s
                      AND m.match_time >= %s AND m.match_time < %s
                    ORDER BY m.match_time
                """, (cid, TS_2026_START, TS_2026_END))

            rows = c.fetchall()
            for r in rows:
                events_all.append({
                    'source': 'TS',
                    'match_id': r[0],
                    'competition_id': r[1],
                    'match_time': r[2],
                    'match_time_str': datetime.utcfromtimestamp(r[2]).strftime('%Y-%m-%dT%H:%M:%SZ') if r[2] else '',
                    'home_id': r[3],
                    'home_name': r[4],
                    'away_id': r[5],
                    'away_name': r[6],
                    'status_id': r[7],
                    'sport': sport,
                })
            lg['event_count_fetched'] = len(rows)
            leagues.append(lg)
            ts_log(f"    TS [{cid}] {lg['name']}: {len(rows)} 场")

    conn.close()

    ts_log(f"  TS 总比赛数: {len(events_all)}")
    save_json(leagues, 'ts_leagues_2026.json')
    save_json(events_all, 'ts_events_2026.json')
    return leagues, events_all

# ─── 辅助函数 ─────────────────────────────────────────────────────────────────
def _sr_sport_name(sport_id):
    mapping = {
        'sr:sport:1': 'football',
        'sr:sport:2': 'basketball',
        'sr:sport:5': 'tennis',
        'sr:sport:4': 'ice_hockey',
        'sr:sport:3': 'baseball',
        'sr:sport:20': 'table_tennis',
        'sr:sport:23': 'volleyball',
    }
    return mapping.get(sport_id, 'unknown')

# ─── 入口 ─────────────────────────────────────────────────────────────────────
if __name__ == '__main__':
    ts_log("开始 SR/LS/TS 2026 年数据拉取任务")
    ts_log(f"输出目录: {OUTPUT_DIR}")

    summary = {
        'fetch_time': datetime.now().isoformat(),
        'year': 2026,
    }

    sr_leagues, sr_events = fetch_sr()
    summary['sr'] = {
        'leagues': len(sr_leagues),
        'events': len(sr_events),
        'hot_leagues': sum(1 for l in sr_leagues if l.get('is_hot')),
        'mapped_leagues': sum(1 for l in sr_leagues if l.get('has_ts_mapping')),
    }

    ls_leagues, ls_events = fetch_ls()
    summary['ls'] = {
        'leagues': len(ls_leagues),
        'events': len(ls_events),
    }

    ts_leagues, ts_events = fetch_ts()
    summary['ts'] = {
        'leagues': len(ts_leagues),
        'events': len(ts_events),
    }

    save_json(summary, 'fetch_summary.json')

    ts_log("=" * 60)
    ts_log("拉取完成！统计摘要：")
    ts_log(f"  SR: {summary['sr']['leagues']} 联赛, {summary['sr']['events']} 场比赛")
    ts_log(f"  LS: {summary['ls']['leagues']} 联赛, {summary['ls']['events']} 场比赛")
    ts_log(f"  TS: {summary['ts']['leagues']} 联赛, {summary['ts']['events']} 场比赛")
    ts_log(f"  输出目录: {OUTPUT_DIR}")
