#!/usr/bin/env python3
"""
build_sr_ts_ground_truth.py — 生成 SR↔TS 比赛匹配 Ground Truth 数据集
=======================================================================
数据来源：
  - ts_sr_match_mapping_3  （主映射表，使用 sr:tournament:xxx 格式，2789 条）
  - ts_sr_match_mapping    （辅助映射表，使用数字 tournament_id，2553 条）
  - ts_sr_match_mapping_2  （与主表相同数据，2553 条）

输出文件（python/data/ 目录）：
  - sr_ts_ground_truth.json        完整匹配记录（含双方比赛详情）
  - sr_ts_ground_truth_by_league.json  按联赛分组的匹配记录
  - sr_ts_match_mapping_raw.json   原始映射表数据（三张表合并去重）
  - ground_truth_summary.json      统计摘要

数据集字段说明：
  - sr_event_id       SR 比赛 ID（sr:match:xxx）
  - sr_tournament_id  SR 联赛 ID（sr:tournament:xxx）
  - sr_tournament_name SR 联赛名称
  - sr_scheduled      SR 比赛时间（ISO8601）
  - sr_home_id        SR 主队 ID
  - sr_home_name      SR 主队名称
  - sr_away_id        SR 客队 ID
  - sr_away_name      SR 客队名称
  - ts_match_id       TS 比赛 ID
  - ts_competition_id TS 联赛 ID
  - ts_competition_name TS 联赛名称
  - ts_match_time     TS 比赛时间（Unix 时间戳）
  - ts_match_time_str TS 比赛时间（ISO8601）
  - ts_home_id        TS 主队 ID
  - ts_home_name      TS 主队名称
  - ts_away_id        TS 客队 ID
  - ts_away_name      TS 客队名称
  - mapping_score     映射置信分数（0.0~1.0）
  - sport_id          运动类型（sr:sport:1=足球, sr:sport:2=篮球）
  - sport             运动名称（football/basketball）
"""

import pymysql
import json
import os
from datetime import datetime
from collections import defaultdict

# ─── 配置 ────────────────────────────────────────────────────────────────────
DB_HOST     = '127.0.0.1'
DB_PORT     = 3308
DB_USER     = 'root'
DB_PASSWORD = 'r74pqyYtgdjlYB41jmWA'

OUTPUT_DIR = os.path.join(os.path.dirname(__file__), 'data')
os.makedirs(OUTPUT_DIR, exist_ok=True)

def ts_log(msg):
    print(f"[{datetime.now().strftime('%H:%M:%S')}] {msg}", flush=True)

def get_conn(db):
    return pymysql.connect(
        host=DB_HOST, port=DB_PORT, user=DB_USER, password=DB_PASSWORD,
        database=db, charset='utf8mb4', connect_timeout=30
    )

def save_json(data, filename):
    path = os.path.join(OUTPUT_DIR, filename)
    with open(path, 'w', encoding='utf-8') as f:
        json.dump(data, f, ensure_ascii=False, indent=2, default=str)
    size_kb = os.path.getsize(path) // 1024
    if isinstance(data, list):
        ts_log(f"  已保存: {filename} ({len(data)} 条, {size_kb} KB)")
    else:
        ts_log(f"  已保存: {filename} ({size_kb} KB)")
    return path

def sport_name(sport_id):
    return {
        'sr:sport:1': 'football',
        'sr:sport:2': 'basketball',
        '6046': 'football',
        '48242': 'basketball',
    }.get(sport_id, 'unknown')

# ─── Step 1: 读取所有映射表，合并去重 ─────────────────────────────────────────
def load_raw_mappings():
    ts_log("Step 1: 读取原始映射表（ts_sr_match_mapping_3 + ts_sr_match_mapping）")
    conn_ts = get_conn('test-thesports-db')
    raw_maps = {}  # key=(ts_match_id, sr_event_id)

    with conn_ts.cursor() as c:
        # 主映射表（使用 sr:tournament:xxx 格式，数据最全）
        c.execute("""
            SELECT ts_match_id, sr_sport_event_id, sport_id, score, sr_tournament_id
            FROM ts_sr_match_mapping_3
        """)
        rows = c.fetchall()
        ts_log(f"  ts_sr_match_mapping_3: {len(rows)} 条")
        for r in rows:
            key = (r[0], r[1])
            raw_maps[key] = {
                'ts_match_id': r[0],
                'sr_event_id': r[1],
                'sport_id': r[2],
                'score': float(r[3]) if r[3] else 0.0,
                'sr_tournament_id': r[4] or '',
                'source_table': 'ts_sr_match_mapping_3',
            }

        # 辅助映射表（数字格式 tournament_id，补充未覆盖的记录）
        c.execute("""
            SELECT ts_match_id, sr_sport_event_id, sport_id, score, type
            FROM ts_sr_match_mapping
        """)
        rows2 = c.fetchall()
        ts_log(f"  ts_sr_match_mapping: {len(rows2)} 条")
        added = 0
        for r in rows2:
            key = (r[0], r[1])
            if key not in raw_maps:
                # 将数字 tournament_id 转为 sr:tournament:xxx 格式
                tid = r[4] or ''
                if tid and not tid.startswith('sr:'):
                    tid = f'sr:tournament:{tid}'
                # sport_id 转换
                sid = r[2] or ''
                if sid and not sid.startswith('sr:'):
                    sid = {'6046': 'sr:sport:1', '48242': 'sr:sport:2'}.get(sid, sid)
                raw_maps[key] = {
                    'ts_match_id': r[0],
                    'sr_event_id': r[1],
                    'sport_id': sid,
                    'score': float(r[3]) if r[3] else 0.0,
                    'sr_tournament_id': tid,
                    'source_table': 'ts_sr_match_mapping',
                }
                added += 1
        ts_log(f"  补充新增: {added} 条")

    conn_ts.close()
    result = list(raw_maps.values())
    ts_log(f"  合并去重后总计: {len(result)} 条")
    save_json(result, 'sr_ts_match_mapping_raw.json')
    return result

# ─── Step 2: 批量查询 SR 比赛详情 ─────────────────────────────────────────────
def load_sr_events(sr_event_ids):
    ts_log(f"Step 2: 批量查询 SR 比赛详情（{len(sr_event_ids)} 个 ID）")
    conn = get_conn('xp-bet-test')
    sr_events = {}

    with conn.cursor() as c:
        # 分批查询（每批 500 个）
        ids = list(sr_event_ids)
        batch_size = 500
        for i in range(0, len(ids), batch_size):
            batch = ids[i:i+batch_size]
            placeholders = ','.join(['%s'] * len(batch))
            c.execute(f"""
                SELECT
                    e.sport_event_id,
                    e.tournament_id,
                    COALESCE(t.name, '') AS tournament_name,
                    COALESCE(e.scheduled, '') AS scheduled,
                    COALESCE(e.home_competitor_id, '') AS home_id,
                    COALESCE(h.name, '') AS home_name,
                    COALESCE(e.away_competitor_id, '') AS away_id,
                    COALESCE(aw.name, '') AS away_name,
                    COALESCE(e.status_code, 0) AS status_code,
                    COALESCE(t.sport_id, '') AS sport_id
                FROM sr_sport_event e
                LEFT JOIN sr_tournament_en t ON e.tournament_id = t.tournament_id
                LEFT JOIN sr_competitor_en h ON e.home_competitor_id = h.competitor_id
                LEFT JOIN sr_competitor_en aw ON e.away_competitor_id = aw.competitor_id
                WHERE e.sport_event_id IN ({placeholders})
            """, batch)
            rows = c.fetchall()
            for r in rows:
                sr_events[r[0]] = {
                    'event_id': r[0],
                    'tournament_id': r[1],
                    'tournament_name': r[2],
                    'scheduled': r[3],
                    'home_id': r[4],
                    'home_name': r[5],
                    'away_id': r[6],
                    'away_name': r[7],
                    'status_code': r[8],
                    'sport_id': r[9],
                }
            ts_log(f"  批次 {i//batch_size+1}: 查到 {len(rows)} 条")

    conn.close()
    ts_log(f"  SR 比赛详情总计: {len(sr_events)} 条")
    return sr_events

# ─── Step 3: 批量查询 TS 比赛详情 ─────────────────────────────────────────────
def load_ts_events(ts_match_ids, sport_map):
    """
    sport_map: {ts_match_id: sport_id}，用于区分足球/篮球表
    """
    ts_log(f"Step 3: 批量查询 TS 比赛详情（{len(ts_match_ids)} 个 ID）")
    conn = get_conn('test-thesports-db')
    ts_events = {}

    # 按运动类型分组
    fb_ids = [mid for mid in ts_match_ids if sport_map.get(mid, '') in ('sr:sport:1', '6046', 'football')]
    bb_ids = [mid for mid in ts_match_ids if sport_map.get(mid, '') in ('sr:sport:2', '48242', 'basketball')]
    # 未知的也尝试足球
    unknown_ids = [mid for mid in ts_match_ids if mid not in fb_ids and mid not in bb_ids]
    fb_ids.extend(unknown_ids)

    with conn.cursor() as c:
        def query_batch(ids, table, team_table):
            batch_size = 500
            for i in range(0, len(ids), batch_size):
                batch = ids[i:i+batch_size]
                placeholders = ','.join(['%s'] * len(batch))
                c.execute(f"""
                    SELECT
                        m.match_id,
                        m.competition_id,
                        COALESCE(comp.name, '') AS competition_name,
                        COALESCE(m.match_time, 0) AS match_time,
                        COALESCE(m.home_team_id, '') AS home_id,
                        COALESCE(ht.name, '') AS home_name,
                        COALESCE(m.away_team_id, '') AS away_id,
                        COALESCE(at2.name, '') AS away_name,
                        COALESCE(m.status_id, 0) AS status_id
                    FROM {table} m
                    LEFT JOIN {team_table.replace('team', 'competition')} comp
                        ON m.competition_id = comp.competition_id
                    LEFT JOIN {team_table} ht ON m.home_team_id = ht.team_id
                    LEFT JOIN {team_table} at2 ON m.away_team_id = at2.team_id
                    WHERE m.match_id IN ({placeholders})
                """, batch)
                rows = c.fetchall()
                for r in rows:
                    ts_events[r[0]] = {
                        'match_id': r[0],
                        'competition_id': r[1],
                        'competition_name': r[2],
                        'match_time': r[3],
                        'match_time_str': datetime.utcfromtimestamp(r[3]).strftime('%Y-%m-%dT%H:%M:%SZ') if r[3] else '',
                        'home_id': r[4],
                        'home_name': r[5],
                        'away_id': r[6],
                        'away_name': r[7],
                        'status_id': r[8],
                    }

        if fb_ids:
            ts_log(f"  查询足球 TS 比赛: {len(fb_ids)} 个")
            query_batch(fb_ids, 'ts_fb_match', 'ts_fb_team')
        if bb_ids:
            ts_log(f"  查询篮球 TS 比赛: {len(bb_ids)} 个")
            query_batch(bb_ids, 'ts_bb_match', 'ts_bb_team')

    conn.close()
    ts_log(f"  TS 比赛详情总计: {len(ts_events)} 条")
    return ts_events

# ─── Step 4: 合并生成完整 Ground Truth ────────────────────────────────────────
def build_ground_truth(raw_maps, sr_events, ts_events):
    ts_log("Step 4: 合并生成完整 Ground Truth 数据集")
    ground_truth = []
    missing_sr = 0
    missing_ts = 0

    for m in raw_maps:
        sr_eid = m['sr_event_id']
        ts_mid = m['ts_match_id']

        sr = sr_events.get(sr_eid)
        ts = ts_events.get(ts_mid)

        if not sr:
            missing_sr += 1
        if not ts:
            missing_ts += 1

        # 确定运动类型
        sid = m['sport_id'] or (sr['sport_id'] if sr else '')
        sname = sport_name(sid)

        record = {
            # SR 侧
            'sr_event_id': sr_eid,
            'sr_tournament_id': m['sr_tournament_id'] or (sr['tournament_id'] if sr else ''),
            'sr_tournament_name': sr['tournament_name'] if sr else '',
            'sr_scheduled': sr['scheduled'] if sr else '',
            'sr_home_id': sr['home_id'] if sr else '',
            'sr_home_name': sr['home_name'] if sr else '',
            'sr_away_id': sr['away_id'] if sr else '',
            'sr_away_name': sr['away_name'] if sr else '',
            'sr_status_code': sr['status_code'] if sr else 0,
            # TS 侧
            'ts_match_id': ts_mid,
            'ts_competition_id': ts['competition_id'] if ts else '',
            'ts_competition_name': ts['competition_name'] if ts else '',
            'ts_match_time': ts['match_time'] if ts else 0,
            'ts_match_time_str': ts['match_time_str'] if ts else '',
            'ts_home_id': ts['home_id'] if ts else '',
            'ts_home_name': ts['home_name'] if ts else '',
            'ts_away_id': ts['away_id'] if ts else '',
            'ts_away_name': ts['away_name'] if ts else '',
            'ts_status_id': ts['status_id'] if ts else 0,
            # 映射元数据
            'mapping_score': m['score'],
            'sport_id': sid,
            'sport': sname,
            'source_table': m['source_table'],
            # 完整性标记
            'sr_found': sr is not None,
            'ts_found': ts is not None,
        }
        ground_truth.append(record)

    ts_log(f"  Ground Truth 总记录: {len(ground_truth)}")
    ts_log(f"  SR 比赛未找到: {missing_sr}")
    ts_log(f"  TS 比赛未找到: {missing_ts}")
    ts_log(f"  完整记录（SR+TS均找到）: {sum(1 for r in ground_truth if r['sr_found'] and r['ts_found'])}")

    return ground_truth

# ─── Step 5: 按联赛分组 ───────────────────────────────────────────────────────
def group_by_league(ground_truth):
    ts_log("Step 5: 按联赛分组")
    by_league = defaultdict(list)
    for r in ground_truth:
        key = r['sr_tournament_id'] or 'unknown'
        by_league[key].append(r)

    result = {}
    for tid, records in sorted(by_league.items(), key=lambda x: -len(x[1])):
        if not records:
            continue
        sample = records[0]
        result[tid] = {
            'sr_tournament_id': tid,
            'sr_tournament_name': sample['sr_tournament_name'],
            'sport': sample['sport'],
            'total_matches': len(records),
            'complete_matches': sum(1 for r in records if r['sr_found'] and r['ts_found']),
            'avg_score': round(sum(r['mapping_score'] for r in records) / len(records), 4),
            'matches': records,
        }
        ts_log(f"  [{tid}] {sample['sr_tournament_name']}: {len(records)} 场 (完整: {result[tid]['complete_matches']})")

    return result

# ─── 入口 ─────────────────────────────────────────────────────────────────────
if __name__ == '__main__':
    ts_log("开始生成 SR↔TS Ground Truth 数据集")

    # Step 1: 读取原始映射
    raw_maps = load_raw_mappings()

    # Step 2: 收集所有需要查询的 ID
    sr_event_ids = set(m['sr_event_id'] for m in raw_maps if m['sr_event_id'])
    ts_match_ids = set(m['ts_match_id'] for m in raw_maps if m['ts_match_id'])
    sport_map = {m['ts_match_id']: m['sport_id'] for m in raw_maps}

    # Step 3: 批量查询详情
    sr_events = load_sr_events(sr_event_ids)
    ts_events = load_ts_events(list(ts_match_ids), sport_map)

    # Step 4: 合并
    ground_truth = build_ground_truth(raw_maps, sr_events, ts_events)

    # Step 5: 按联赛分组
    by_league = group_by_league(ground_truth)

    # 保存
    save_json(ground_truth, 'sr_ts_ground_truth.json')
    save_json(by_league, 'sr_ts_ground_truth_by_league.json')

    # 生成统计摘要
    complete = [r for r in ground_truth if r['sr_found'] and r['ts_found']]
    fb_complete = [r for r in complete if r['sport'] == 'football']
    bb_complete = [r for r in complete if r['sport'] == 'basketball']

    summary = {
        'generated_at': datetime.now().isoformat(),
        'total_mappings': len(ground_truth),
        'complete_mappings': len(complete),
        'football_mappings': len(fb_complete),
        'basketball_mappings': len(bb_complete),
        'leagues_covered': len(by_league),
        'avg_mapping_score': round(sum(r['mapping_score'] for r in ground_truth) / len(ground_truth), 4) if ground_truth else 0,
        'score_distribution': {
            '1.0': sum(1 for r in ground_truth if r['mapping_score'] == 1.0),
            '>=0.9': sum(1 for r in ground_truth if r['mapping_score'] >= 0.9),
            '>=0.75': sum(1 for r in ground_truth if r['mapping_score'] >= 0.75),
            '<0.75': sum(1 for r in ground_truth if r['mapping_score'] < 0.75),
        },
        'leagues': [
            {
                'sr_tournament_id': tid,
                'sr_tournament_name': info['sr_tournament_name'],
                'sport': info['sport'],
                'total': info['total_matches'],
                'complete': info['complete_matches'],
                'avg_score': info['avg_score'],
            }
            for tid, info in sorted(by_league.items(), key=lambda x: -x[1]['total_matches'])
        ]
    }
    save_json(summary, 'ground_truth_summary.json')

    ts_log("=" * 60)
    ts_log("Ground Truth 生成完成！")
    ts_log(f"  总映射记录: {summary['total_mappings']}")
    ts_log(f"  完整记录（SR+TS均有详情）: {summary['complete_mappings']}")
    ts_log(f"  足球: {summary['football_mappings']} 场")
    ts_log(f"  篮球: {summary['basketball_mappings']} 场")
    ts_log(f"  覆盖联赛: {summary['leagues_covered']} 个")
    ts_log(f"  平均置信分: {summary['avg_mapping_score']}")
    ts_log(f"  置信分分布: {summary['score_distribution']}")
