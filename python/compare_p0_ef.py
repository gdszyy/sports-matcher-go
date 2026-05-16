#!/usr/bin/env python3
"""
compare_p0_ef.py — Evidence-First vs P0 对照工具
================================================================================

用法：
  # 同一联赛分别跑 P0 与 EF，输出 JSON
  ./sports-matcher match2 "sr:tournament:17" --json > /tmp/p0.json
  ./sports-matcher match2 "sr:tournament:17" --use-evidence-first --json > /tmp/ef.json

  # 对照
  python3 python/compare_p0_ef.py /tmp/p0.json /tmp/ef.json

  # 或批量对照（两边都是 batch2 输出）
  python3 python/compare_p0_ef.py /tmp/p0_batch.json /tmp/ef_batch.json --batch

输入：
  UniversalEngine.RunLeague 的 *UniversalMatchResult JSON（match2/ls-match 单联赛输出
  即一个 dict；batch2/ls-batch 输出是 array of dict）。

输出：
  - 联赛层差异表（rule / confidence / TSCompetitionID）
  - 比赛层关键指标差（matched_total、各规则计数、avg confidence、RCR）
  - 新增匹配 / 失去匹配 / TS 重定向的 SR event_id 清单（最多前 20 条）
  - 球队映射差异（数量、新增/失去的 SR team_id）

退出码：
  0 = 解析成功（无论差异多大）
  1 = 输入文件读取失败 / JSON 解析失败
"""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter
from typing import Any, Dict, List, Optional, Tuple


# ─── 数据访问辅助 ────────────────────────────────────────────────────────────

def load(path: str) -> Any:
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, json.JSONDecodeError) as e:
        print(f"[ERROR] 无法读取 {path}: {e}", file=sys.stderr)
        sys.exit(1)


def normalize_input(obj: Any, batch: bool) -> List[Dict[str, Any]]:
    """统一成 list of UniversalMatchResult。"""
    if batch:
        if not isinstance(obj, list):
            print("[ERROR] --batch 模式期望 JSON 是 array", file=sys.stderr)
            sys.exit(1)
        return obj
    if isinstance(obj, list):
        return obj  # 兼容意外的 array 输入
    return [obj]


# ─── 联赛级 diff ─────────────────────────────────────────────────────────────

def league_summary(r: Dict[str, Any]) -> Dict[str, Any]:
    league = r.get("league") or r.get("League") or {}
    return {
        "src_name":           league.get("src_name") or league.get("SrcName") or "",
        "ts_competition_id":  league.get("ts_competition_id") or league.get("TSCompetitionID") or "",
        "ts_name":            league.get("ts_name") or league.get("TSName") or "",
        "matched":            league.get("matched") if league.get("matched") is not None else league.get("Matched"),
        "match_rule":         league.get("match_rule") or league.get("MatchRule") or "",
        "confidence":         league.get("confidence", league.get("Confidence", 0.0)),
    }


# ─── 比赛级 diff ─────────────────────────────────────────────────────────────

def events_from(r: Dict[str, Any]) -> List[Dict[str, Any]]:
    return r.get("events") or r.get("Events") or []


def event_key(ev: Dict[str, Any]) -> str:
    return ev.get("sr_event_id") or ev.get("SREventID") or ""


def event_ts_id(ev: Dict[str, Any]) -> str:
    return ev.get("ts_match_id") or ev.get("TSMatchID") or ""


def event_rule(ev: Dict[str, Any]) -> str:
    return ev.get("match_rule") or ev.get("MatchRule") or ""


def event_matched(ev: Dict[str, Any]) -> bool:
    v = ev.get("matched")
    if v is None:
        v = ev.get("Matched")
    return bool(v)


def event_conf(ev: Dict[str, Any]) -> float:
    return float(ev.get("confidence", ev.get("Confidence", 0.0)) or 0.0)


def teams_from(r: Dict[str, Any]) -> List[Dict[str, Any]]:
    return r.get("teams") or r.get("Teams") or []


def team_key(tm: Dict[str, Any]) -> str:
    return tm.get("src_team_id") or tm.get("SrcTeamID") or ""


# ─── 单联赛对照 ─────────────────────────────────────────────────────────────

def compare_one(p0: Dict[str, Any], ef: Dict[str, Any]) -> None:
    lp = league_summary(p0)
    le = league_summary(ef)
    src = lp["src_name"] or le["src_name"] or "<unknown league>"
    print(f"\n══ 联赛: {src} ══")

    # league diff
    league_rows = []
    for k in ("ts_competition_id", "ts_name", "matched", "match_rule", "confidence"):
        a, b = lp[k], le[k]
        if isinstance(a, float) or isinstance(b, float):
            same = abs(float(a) - float(b)) < 1e-9
        else:
            same = a == b
        mark = "  " if same else "*"
        league_rows.append((mark, k, a, b))
    if any(r[0] == "*" for r in league_rows):
        print("[联赛层差异]")
        print(f"  {'':2}  {'field':<22} {'P0':<30} {'EF':<30}")
        for mark, k, a, b in league_rows:
            print(f"  {mark:2}  {k:<22} {str(a):<30} {str(b):<30}")
    else:
        print("[联赛层] 无差异")

    # event-level counts
    evs_p0 = events_from(p0)
    evs_ef = events_from(ef)
    print(f"\n[比赛层] events count: P0={len(evs_p0)}  EF={len(evs_ef)}")

    m_p0 = sum(1 for e in evs_p0 if event_matched(e))
    m_ef = sum(1 for e in evs_ef if event_matched(e))
    print(f"  matched=true:        P0={m_p0:>4}    EF={m_ef:>4}    Δ={m_ef-m_p0:+d}")

    rules_p0 = Counter(event_rule(e) for e in evs_p0 if event_matched(e))
    rules_ef = Counter(event_rule(e) for e in evs_ef if event_matched(e))
    all_rules = sorted(set(rules_p0) | set(rules_ef))
    print(f"  rule distribution (matched only):")
    for r in all_rules:
        a, b = rules_p0.get(r, 0), rules_ef.get(r, 0)
        mark = "  " if a == b else "*"
        print(f"    {mark:2}  {r:<22} P0={a:>4}  EF={b:>4}  Δ={b-a:+d}")

    conf_p0 = [event_conf(e) for e in evs_p0 if event_matched(e)]
    conf_ef = [event_conf(e) for e in evs_ef if event_matched(e)]
    avg_p0 = sum(conf_p0) / len(conf_p0) if conf_p0 else 0.0
    avg_ef = sum(conf_ef) / len(conf_ef) if conf_ef else 0.0
    print(f"  avg confidence:      P0={avg_p0:.4f}  EF={avg_ef:.4f}  Δ={avg_ef-avg_p0:+.4f}")

    # SR-level diff
    p0_by_id = {event_key(e): e for e in evs_p0}
    ef_by_id = {event_key(e): e for e in evs_ef}
    all_ids = sorted(set(p0_by_id) | set(ef_by_id))

    gained = []      # P0 unmatched but EF matched
    lost = []        # P0 matched but EF unmatched
    redirected = []  # both matched but TS 不同

    for sid in all_ids:
        p = p0_by_id.get(sid)
        e = ef_by_id.get(sid)
        if p is None or e is None:
            continue
        mp, me = event_matched(p), event_matched(e)
        if not mp and me:
            gained.append((sid, event_ts_id(e), event_rule(e), event_conf(e)))
        elif mp and not me:
            lost.append((sid, event_ts_id(p), event_rule(p), event_conf(p)))
        elif mp and me and event_ts_id(p) != event_ts_id(e):
            redirected.append((sid, event_ts_id(p), event_ts_id(e)))

    def show(rows: List[Tuple], label: str, limit: int = 20) -> None:
        if not rows:
            return
        print(f"  {label}: {len(rows)} 条" + (f"（前 {limit}）" if len(rows) > limit else ""))
        for row in rows[:limit]:
            print(f"    - {row}")

    show(gained, "新增匹配 (gained: P0 fail → EF ok)")
    show(lost, "失去匹配 (lost: P0 ok → EF fail)")
    show(redirected, "TS 重定向 (redirected: 同 SR 但 TS 不同)")

    # team mappings
    tm_p0 = teams_from(p0)
    tm_ef = teams_from(ef)
    print(f"\n[球队映射] count: P0={len(tm_p0)}  EF={len(tm_ef)}  Δ={len(tm_ef)-len(tm_p0):+d}")
    p0_team_keys = {team_key(t) for t in tm_p0}
    ef_team_keys = {team_key(t) for t in tm_ef}
    new_teams = ef_team_keys - p0_team_keys
    lost_teams = p0_team_keys - ef_team_keys
    if new_teams:
        sample = list(sorted(new_teams))[:10]
        print(f"  新增 SR team_id: {len(new_teams)} 条，前 10: {sample}")
    if lost_teams:
        sample = list(sorted(lost_teams))[:10]
        print(f"  失去 SR team_id: {len(lost_teams)} 条，前 10: {sample}")


# ─── 入口 ───────────────────────────────────────────────────────────────────

def main() -> None:
    ap = argparse.ArgumentParser(description="Evidence-First vs P0 对照工具")
    ap.add_argument("p0_json", help="P0 模式 UniversalMatchResult JSON（match2/ls-match 单联赛 or batch2/ls-batch 批量）")
    ap.add_argument("ef_json", help="EF 模式 UniversalMatchResult JSON（同形态）")
    ap.add_argument("--batch", action="store_true", help="将两边都解析为 array（batch2/ls-batch 输出）")
    args = ap.parse_args()

    p0_obj = normalize_input(load(args.p0_json), args.batch)
    ef_obj = normalize_input(load(args.ef_json), args.batch)

    if len(p0_obj) != len(ef_obj):
        print(f"[WARN] 两边联赛数不一致：P0={len(p0_obj)}  EF={len(ef_obj)}。按 src_name 配对，未匹配的跳过。",
              file=sys.stderr)
        p0_map = {league_summary(r)["src_name"]: r for r in p0_obj}
        ef_map = {league_summary(r)["src_name"]: r for r in ef_obj}
        keys = sorted(set(p0_map) & set(ef_map))
        pairs = [(p0_map[k], ef_map[k]) for k in keys]
    else:
        pairs = list(zip(p0_obj, ef_obj))

    print(f"对照 {len(pairs)} 个联赛 ──")
    for p0, ef in pairs:
        compare_one(p0, ef)

    print(f"\n────────────────────────────")
    print(f"完成。共对照 {len(pairs)} 个联赛。")


if __name__ == "__main__":
    main()
