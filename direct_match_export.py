#!/usr/bin/env python3
"""
LSports ↔ TheSports 批量联赛匹配导出脚本
改进版：引入国家/地区（Location/Category）维度的强约束校验，消除跨国误匹配。

改进说明：
1. match_league() 函数新增 ls_category 与 comp['host_country'] 的相似度比较。
2. 引入 is_international_category() 判断洲际/国际赛事，跳过地区约束。
3. 地区相似度低于阈值（0.4）且双方均非国际赛事时，直接跳过该候选联赛。
4. 同国联赛匹配时，地区相似度作为加权项提升最终置信度。
"""

import pymysql
import difflib
import unicodedata
import re
import json
from openpyxl import Workbook
from openpyxl.styles import PatternFill, Font
from datetime import datetime

# ─────────────────────────────────────────────────────────────────────────────
# 数据库连接配置（使用已建立的 SSH 隧道）
# ─────────────────────────────────────────────────────────────────────────────
DB_PASSWORD = 'r74pqyYtgdjlYB41jmWA'
LS_PORT = 3309   # LSports 隧道端口
TS_PORT = 3308   # TheSports 隧道端口

def get_conn(port, db):
    return pymysql.connect(
        host='127.0.0.1', port=port,
        user='root', password=DB_PASSWORD,
        database=db, charset='utf8mb4', connect_timeout=10
    )

# ─────────────────────────────────────────────────────────────────────────────
# 已知映射表：LS tournament_id → TS competition_id
# key 格式: "sport:ls_tournament_id"
# ─────────────────────────────────────────────────────────────────────────────
KNOWN_LS_TS_MAP = {
    # 足球热门
    "football:67":    "jednm9whz0ryox8",  # Premier League (England)
    "football:8363":  "vl7oqdehlyr510j",  # LaLiga (Spain)
    "football:61":    "yl5ergphnzr8k0o",  # Ligue 1 (France)
    "football:66":    "gy0or5jhg6qwzv3",  # Bundesliga (Germany)
    # UEFA 赛事
    "football:32644": "z8yomo4h7wq0j6l",  # UEFA Champions League
    "football:30444": "56ypq3nh0xmd7oj",  # UEFA Europa League
    # 篮球热门
    "basketball:132": "49vjxm8xt4q6odg",  # NBA
}

# ─────────────────────────────────────────────────────────────────────────────
# 名称归一化与相似度
# ─────────────────────────────────────────────────────────────────────────────

def normalize_name(s: str) -> str:
    """归一化名称：去变音符号、转小写、去标点、合并空格"""
    if not s:
        return ''
    # NFD 分解后去掉组合字符（变音符号）
    s = ''.join(c for c in unicodedata.normalize('NFD', s)
                if unicodedata.category(c) != 'Mn')
    s = s.lower()
    # 替换常见分隔符
    s = re.sub(r'[.\-_,\'\"·]', ' ', s)
    # 合并多余空格
    s = ' '.join(s.split())
    return s


def seq_similarity(a: str, b: str) -> float:
    """使用 SequenceMatcher 计算两个归一化字符串的相似度"""
    na, nb = normalize_name(a), normalize_name(b)
    if not na and not nb:
        return 1.0
    if not na or not nb:
        return 0.0
    return difflib.SequenceMatcher(None, na, nb).ratio()


def jaccard_similarity(a: str, b: str) -> float:
    """基于 token 集合的 Jaccard 相似度"""
    na, nb = normalize_name(a), normalize_name(b)
    set_a = set(na.split())
    set_b = set(nb.split())
    if not set_a and not set_b:
        return 1.0
    if not set_a or not set_b:
        return 0.0
    intersection = len(set_a & set_b)
    union = len(set_a | set_b)
    return intersection / union


def name_similarity(a: str, b: str) -> float:
    """取 Jaccard 和 SequenceMatcher 的最大值"""
    return max(jaccard_similarity(a, b), seq_similarity(a, b))


# ─────────────────────────────────────────────────────────────────────────────
# 国家/地区校验
# ─────────────────────────────────────────────────────────────────────────────

# 洲际/国际赛事关键词（不应约束国家匹配）
INTERNATIONAL_KEYWORDS = {
    'world', 'international', 'europe', 'europa', 'asia', 'africa',
    'america', 'oceania', 'concacaf', 'conmebol', 'afc', 'caf',
    'uefa', 'fifa', 'south america', 'north america', 'central america',
}


def is_international_category(name: str) -> bool:
    """判断地区名称是否属于洲际/国际赛事（快速集合查找，避免昂贵相似度计算）"""
    if not name:
        return False
    norm = normalize_name(name)
    # 直接精确匹配（O(1) 查找）
    if norm in INTERNATIONAL_KEYWORDS:
        return True
    # 检查是否包含关键词
    tokens = set(norm.split())
    return bool(tokens & INTERNATIONAL_KEYWORDS)


def location_veto(ls_category: str, ts_country: str) -> bool:
    """
    判断是否应否决该联赛匹配（跨国误匹配检测）。
    返回 True 表示地区明显不匹配，应跳过该候选。
    """
    # 任一侧为空时不否决（信息不足时保守处理）
    if not ls_category or not ts_country:
        return False
    # 洲际/国际赛事不约束国家
    if is_international_category(ls_category) or is_international_category(ts_country):
        return False
    # 地区相似度低于 0.4 时否决
    loc_sim = name_similarity(ls_category, ts_country)
    return loc_sim < 0.4


# ─────────────────────────────────────────────────────────────────────────────
# 联赛匹配核心函数
# ─────────────────────────────────────────────────────────────────────────────

def match_league(ls_name: str, ls_category: str, ts_competitions: list,
                 sport: str = 'football', ls_id: str = '') -> dict:
    """
    为一个 LSports 联赛在 TheSports 中找到最佳匹配。

    改进：
    - 优先查 KNOWN_LS_TS_MAP 已知映射
    - 引入 location_veto() 跨国否决
    - 同国联赛时地区相似度作为加权项

    返回 dict: {ts_id, ts_name, ts_country, score, rule, matched}
    """
    result = {
        'ts_id': '', 'ts_name': '', 'ts_country': '',
        'score': 0.0, 'rule': 'NO_MATCH', 'matched': False
    }

    # 1. 已知映射
    map_key = f"{sport}:{ls_id}"
    if ls_id and map_key in KNOWN_LS_TS_MAP:
        ts_id = KNOWN_LS_TS_MAP[map_key]
        for comp in ts_competitions:
            if comp['competition_id'] == ts_id:
                result.update({
                    'ts_id': comp['competition_id'],
                    'ts_name': comp['name'],
                    'ts_country': comp.get('host_country', ''),
                    'score': 1.0, 'rule': 'KNOWN', 'matched': True
                })
                return result
        # 有映射但列表中无该 ID
        result.update({'ts_id': ts_id, 'score': 1.0, 'rule': 'KNOWN', 'matched': True})
        return result

    # 2. 名称相似度匹配（含国家/地区强约束）
    best_score = 0.0
    best_comp = None

    for comp in ts_competitions:
        ts_country = comp.get('host_country', '') or ''

        # 强约束：地区明显不匹配时跳过
        if location_veto(ls_category, ts_country):
            continue

        # 计算联赛名称相似度
        base_score = name_similarity(ls_name, comp['name'])

        # 同国加权：地区相似度高时提升置信度
        if ls_category and ts_country:
            loc_sim = name_similarity(ls_category, ts_country)
            if loc_sim >= 0.6:
                base_score = base_score * 0.75 + 0.25 * loc_sim
        elif ls_category and not ts_country and not is_international_category(ls_category):
            # LS 有地区信息但 TS 无 host_country：
            # 仅对低置信度区间（< 0.75）施加轻度惩罚，高置信度匹配不受影响
            if base_score < 0.75:
                base_score = base_score * 0.90

        if base_score > best_score:
            best_score = base_score
            best_comp = comp

    if best_comp is None:
        return result

    # 根据分数确定匹配规则
    if best_score >= 0.85:
        rule = 'NAME_HI'
    elif best_score >= 0.70:
        rule = 'NAME_MED'
    elif best_score >= 0.55:
        rule = 'NAME_LOW'
    else:
        return result  # 分数过低，不匹配

    result.update({
        'ts_id': best_comp['competition_id'],
        'ts_name': best_comp['name'],
        'ts_country': best_comp.get('host_country', ''),
        'score': round(best_score, 4),
        'rule': rule,
        'matched': True
    })
    return result


# ─────────────────────────────────────────────────────────────────────────────
# 数据加载
# ─────────────────────────────────────────────────────────────────────────────

def load_ls_tournaments(sport_id: int = 6046) -> list:
    """加载 LSports 联赛列表（含地区信息）
    注意：ls_category_en 有 sport_id 字段，JOIN 时需同时匹配避免重复行
    """
    conn = get_conn(LS_PORT, 'test-xp-lsports')
    with conn.cursor(pymysql.cursors.DictCursor) as cur:
        cur.execute("""
            SELECT t.tournament_id, t.name as tournament_name,
                   COALESCE(c.name, '') as location
            FROM ls_tournament_en t
            LEFT JOIN ls_category_en c
                ON t.category_id = c.category_id
                AND c.sport_id = %s
            WHERE t.sport_id = %s
            GROUP BY t.tournament_id, t.name, c.name
            ORDER BY t.tournament_id
        """, (str(sport_id), int(sport_id)))
        rows = cur.fetchall()
    conn.close()
    return rows


def load_ts_competitions(sport: str = 'football') -> list:
    """加载 TheSports 联赛列表"""
    conn = get_conn(TS_PORT, 'test-thesports-db')
    with conn.cursor(pymysql.cursors.DictCursor) as cur:
        if sport == 'football':
            cur.execute("""
                SELECT competition_id, name,
                       COALESCE(host_country, '') as host_country
                FROM ts_fb_competition
            """)
        elif sport == 'basketball':
            cur.execute("""
                SELECT competition_id, name, '' as host_country
                FROM ts_bb_competition
            """)
        rows = cur.fetchall()
    conn.close()
    return rows


# ─────────────────────────────────────────────────────────────────────────────
# 主流程：批量匹配并导出 Excel
# ─────────────────────────────────────────────────────────────────────────────

def run_batch_match(sport: str = 'football', output_path: str = None):
    """执行批量联赛匹配并导出结果"""
    print(f"[{datetime.now().strftime('%H:%M:%S')}] 加载 LSports 联赛...")
    sport_id_map = {'football': 6046, 'basketball': 48242}
    ls_tours = load_ls_tournaments(sport_id_map.get(sport, 6046))
    print(f"  LSports 联赛数: {len(ls_tours)}")

    print(f"[{datetime.now().strftime('%H:%M:%S')}] 加载 TheSports 联赛...")
    ts_comps = load_ts_competitions(sport)
    print(f"  TheSports 联赛数: {len(ts_comps)}")

    print(f"[{datetime.now().strftime('%H:%M:%S')}] 开始批量匹配...")
    results = []
    matched_count = 0
    known_count = 0
    vetoed_count = 0

    for i, tour in enumerate(ls_tours):
        ls_id = str(tour['tournament_id'])
        ls_name = tour['tournament_name'] or ''
        ls_category = tour['location'] or ''

        match = match_league(ls_name, ls_category, ts_comps, sport, ls_id)

        if match['matched']:
            matched_count += 1
            if match['rule'] == 'KNOWN':
                known_count += 1

        results.append({
            'ls_tournament_id': ls_id,
            'ls_name': ls_name,
            'ls_category': ls_category,
            'ts_competition_id': match['ts_id'],
            'ts_name': match['ts_name'],
            'ts_country': match['ts_country'],
            'score': match['score'],
            'rule': match['rule'],
            'matched': match['matched'],
        })

        if (i + 1) % 100 == 0:
            print(f"  进度: {i+1}/{len(ls_tours)}, 已匹配: {matched_count}")

    print(f"\n[{datetime.now().strftime('%H:%M:%S')}] 匹配完成:")
    print(f"  总联赛数: {len(ls_tours)}")
    print(f"  已匹配:   {matched_count} ({matched_count/len(ls_tours)*100:.1f}%)")
    print(f"  已知映射: {known_count}")
    print(f"  未匹配:   {len(ls_tours) - matched_count}")

    # 导出 Excel
    if output_path is None:
        output_path = f'/home/ubuntu/lsports_ts_match_result_{sport}.xlsx'

    export_excel(results, output_path, sport)
    print(f"\n[{datetime.now().strftime('%H:%M:%S')}] 结果已导出: {output_path}")
    return results, output_path


def export_excel(results: list, output_path: str, sport: str):
    """将匹配结果导出为 Excel 文件"""
    wb = Workbook()
    ws = wb.active
    ws.title = f'LS-TS Match ({sport})'

    # 表头
    headers = [
        'LS Tournament ID', 'LS League Name', 'LS Category (Location)',
        'TS Competition ID', 'TS League Name', 'TS Country',
        'Score', 'Match Rule', 'Matched'
    ]
    ws.append(headers)

    # 表头样式
    header_fill = PatternFill(start_color='4472C4', end_color='4472C4', fill_type='solid')
    header_font = Font(color='FFFFFF', bold=True)
    for cell in ws[1]:
        cell.fill = header_fill
        cell.font = header_font

    # 颜色方案
    fill_known = PatternFill(start_color='C6EFCE', end_color='C6EFCE', fill_type='solid')   # 绿
    fill_hi    = PatternFill(start_color='DDEEFF', end_color='DDEEFF', fill_type='solid')   # 蓝
    fill_med   = PatternFill(start_color='FFEB9C', end_color='FFEB9C', fill_type='solid')   # 黄
    fill_low   = PatternFill(start_color='FFD7B5', end_color='FFD7B5', fill_type='solid')   # 橙
    fill_none  = PatternFill(start_color='FFC7CE', end_color='FFC7CE', fill_type='solid')   # 红

    fill_map = {
        'KNOWN': fill_known,
        'NAME_HI': fill_hi,
        'NAME_MED': fill_med,
        'NAME_LOW': fill_low,
        'NO_MATCH': fill_none,
    }

    for row in results:
        ws.append([
            row['ls_tournament_id'],
            row['ls_name'],
            row['ls_category'],
            row['ts_competition_id'],
            row['ts_name'],
            row['ts_country'],
            row['score'],
            row['rule'],
            'YES' if row['matched'] else 'NO',
        ])
        fill = fill_map.get(row['rule'], fill_none)
        for cell in ws[ws.max_row]:
            cell.fill = fill

    # 调整列宽
    col_widths = [18, 45, 25, 22, 45, 20, 8, 12, 8]
    for i, width in enumerate(col_widths, 1):
        ws.column_dimensions[ws.cell(1, i).column_letter].width = width

    # 统计 Sheet
    ws2 = wb.create_sheet('统计')
    matched = [r for r in results if r['matched']]
    rule_counts = {}
    for r in results:
        rule_counts[r['rule']] = rule_counts.get(r['rule'], 0) + 1

    ws2.append(['指标', '数值'])
    ws2.append(['总联赛数', len(results)])
    ws2.append(['已匹配', len(matched)])
    ws2.append(['匹配率', f"{len(matched)/len(results)*100:.1f}%" if results else '0%'])
    ws2.append([''])
    ws2.append(['匹配规则', '数量'])
    for rule, cnt in sorted(rule_counts.items()):
        ws2.append([rule, cnt])

    wb.save(output_path)


# ─────────────────────────────────────────────────────────────────────────────
# 入口
# ─────────────────────────────────────────────────────────────────────────────

if __name__ == '__main__':
    import sys
    sport = sys.argv[1] if len(sys.argv) > 1 else 'football'
    output = sys.argv[2] if len(sys.argv) > 2 else None
    run_batch_match(sport=sport, output_path=output)
