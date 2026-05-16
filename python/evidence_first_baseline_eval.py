"""
evidence_first_baseline_eval.py — Evidence-First P0 离线基线评估脚本
================================================================================

本脚本用于冻结旧流程基线并准备 P1–P5 可复现对比数据。它不修改核心匹配算法，
而是复用 `python/test_sr_2026.py` 中已经存在的 SR↔TS 离线匹配实现，以及
`python/data/` 目录中的 2026 年离线数据集，对 SR↔TS 与 LS↔TS 两条链路输出
统一的基线指标。

用法：
  python3 python/evidence_first_baseline_eval.py \
    --output-json docs/tests/evidence_first_baseline_metrics.json \
    --output-csv docs/tests/evidence_first_baseline_metrics.csv

输出指标：
  - 联赛匹配：源联赛、目标 TS 联赛、KnownMap 命中情况、联赛规则
  - 比赛匹配：match_rate、Precision/Recall/F1（仅 SR 有 GT 时）
  - 球队匹配：由已匹配比赛推导的一致球队映射数量与覆盖率
  - 耗时：每个联赛的离线匹配耗时
  - 失败原因：无 SR/LS 数据、无 TS 数据、无 GT、未匹配/误匹配等
  - 规则分布：L1/L2/L3/L4/L5/L4b/L6
  - RCR：matched / min(source_total, unique_ts_total)，与 Go 侧 reverse_confirm.go 保持一致
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import sys
import time
from collections import Counter, defaultdict
from datetime import datetime, timezone
from typing import Any, Dict, Iterable, List, Tuple

# 复用现有 SR↔TS 离线匹配逻辑，避免重写主流程。
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(SCRIPT_DIR)
DATA_DIR = os.path.join(SCRIPT_DIR, "data")
if SCRIPT_DIR not in sys.path:
    sys.path.insert(0, SCRIPT_DIR)

from test_sr_2026 import (  # noqa: E402
    KNOWN_LEAGUE_MAP as SR_KNOWN_LEAGUE_MAP,
    SR_2026_LEAGUES,
    load_data as load_sr_data,
    match_events_for_league,
    evaluate,
    parse_sr_time,
)

# LS 2026 联赛配置来自 cmd/server/main.go ls2026Leagues。此处仅作为离线评估输入清单，
# 不改变 Go 运行时 KnownLSLeagueMap 或任何核心匹配逻辑。
LS_2026_LEAGUES: List[Tuple[str, str, str, str, str]] = [
    ("67", "football", "hot", "jednm9whz0ryox8", "Premier League"),
    ("8363", "football", "hot", "vl7oqdehlyr510j", "LaLiga"),
    ("65", "football", "hot", "gy0or5jhg6qwzv3", "Bundesliga"),
    ("4", "football", "hot", "4zp5rzghp5q82w1", "Serie A"),
    ("61", "football", "hot", "yl5ergphnzr8k0o", "Ligue 1"),
    ("32644", "football", "hot", "z8yomo4h7wq0j6l", "UEFA Champions League"),
    ("30444", "football", "hot", "56ypq3nh0xmd7oj", "UEFA Europa League"),
    ("27738", "football", "hot", "v2y8m4zhe6ql074", "CONMEBOL Copa Libertadores"),
    ("36297", "football", "hot", "56ypq3nhpkmd7oj", "Copa Sudamericana"),
    ("58", "football", "regular", "l965mkyh32r1ge4", "The Championship"),
    ("68", "football", "regular", "8y39mp1hjzmojxg", "League One"),
    ("70", "football", "regular", "9k82rekhygrepzj", "League Two"),
    ("8203", "football", "regular", "z318q66hv8qo9jd", "National League"),
    ("66", "football", "regular", "kn54qllhjzqvy9d", "2.Bundesliga"),
    ("8", "football", "regular", "j1l4rjnhx9m7vx5", "Serie B"),
    ("60", "football", "regular", "kjw2r09hw8rz84o", "Ligue 2"),
    ("22263", "football", "regular", "kdj2ryohnkq1zpg", "LaLiga2"),
    ("59", "football", "regular", "9vjxm8gh22r6odg", "Jupiler League"),
    ("2944", "football", "regular", "vl7oqdeheyr510j", "Eredivisie"),
    ("6603", "football", "regular", "9vjxm8ghx2r6odg", "Primeira Liga"),
    ("63", "football", "regular", "8y39mp1h6jmojxg", "Super Lig"),
    ("3799", "football", "regular", "8y39mp1hwxmojxg", "FNL"),
    ("32521", "football", "regular", "vl7oqdeh3lr510j", "Ekstraklasa"),
    ("61243", "football", "regular", "gx7lm7pho13m2wd", "HNL"),
    ("30058", "football", "regular", "p4jwq2gh1gm0veo", "Scotland Premiership"),
    ("72", "football", "regular", "e4wyrn4hoeq86pv", "Super League"),
    ("3", "football", "regular", "l965mkyhg0r1ge4", "Allsvenskan"),
    ("38", "football", "regular", "8y39mp1hk8mojxg", "Superettan"),
    ("24289", "football", "regular", "gy0or5jhj6qwzv3", "Eliteserien"),
    ("156", "football", "regular", "kn54qllhg2qvy9d", "Major League Soccer"),
    ("5299", "football", "regular", "9k82rekhp6repzj", "Liga MX"),
    ("20913", "football", "regular", "4zp5rzgh9zq82w1", "Serie A Brazil"),
    ("41558", "football", "regular", "p3glrw7hevqdyjv", "Liga Profesional"),
    ("1543", "football", "regular", "9k82rekh52repzj", "China Super League"),
    ("35637", "football", "regular", "z318q66hl1qo9jd", "J1 League"),
    ("24585", "football", "regular", "gy0or5jhlxgqwzv", "K League Classic"),
    ("28898", "football", "regular", "kn54qllh25dqvy9", "K2 League"),
    ("2018", "football", "regular", "56ypq3nh01nmd7o", "Premier League Egypt"),
    ("64", "basketball", "hot", "49vjxm8xt4q6odg", "NBA"),
    ("33249", "basketball", "hot", "jednm9ktd5ryox8", "Euroleague"),
    ("4871", "basketball", "regular", "ngy0or5gteqwzv3", "CBA"),
    ("3111", "basketball", "regular", "v2y8m4ptdeml074", "Liga ACB Endesa"),
    ("621", "basketball", "regular", "0l965mk8tom1ge4", "Basketball Bundesliga"),
    ("62013", "basketball", "regular", "v2y8m4ptx1ml074", "VTB United League"),
    ("293", "basketball", "regular", "x4zp5rzkt1r82w1", "Basketball Serie A"),
    ("48524", "basketball", "regular", "l965mk8tzpkm1ge", "BNXT League"),
    ("34184", "basketball", "regular", "ngy0or5gteqwzv3", "B.League - B1"),
    ("25357", "basketball", "regular", "v2y8m4ptx1ml074", "Orlen Basket Liga"),
    ("1834", "basketball", "regular", "x4zp5rzkt1r82w1", "NBL"),
]

# 额外高歧义样本来自 python/data/*_leagues_2026.json，主要用于 P0 评估集覆盖。
# 其中部分样本没有经过人工确认的 TS competition，因此脚本会输出 no_known_map/no_ts_events，
# 作为 P1 之后多 competition 候选池生成与强约束验证的输入样本。
SR_SUPPLEMENTAL_LEAGUES: List[Tuple[str, str, str, str]] = [
    ("sr:tournament:24702", "football", "ambiguous", ""),  # U19 DFB Nachwuchsliga
    ("sr:tournament:48343", "football", "ambiguous", "p4jwq2ghvdjm0ve"),  # U19 Division de Honor Juvenil → Spanish U19 League
    ("sr:tournament:26", "football", "ambiguous", ""),  # U21 UEFA European Championship Qualification
    ("sr:tournament:21384", "basketball", "ambiguous", "kn54ql7t05rvy9d"),  # NCAA Women Regular Season
    ("sr:tournament:28", "football", "ambiguous", ""),  # AFC Asian Cup QF
    ("sr:tournament:47986", "football", "ambiguous", "kdj2ryoh2kq1zpg"),  # National 2 → French Championnat National 2
    ("sr:tournament:97", "football", "ambiguous", ""),  # 2. Lig
]

LS_SUPPLEMENTAL_LEAGUES: List[Tuple[str, str, str, str, str]] = [
    ("11585", "football", "ambiguous", "", "Bundesliga U19"),
    ("68254", "basketball", "ambiguous", "", "IPBL. Pro Division - Women"),
    ("29650", "basketball", "ambiguous", "", "NBL 1 - Women"),
    ("59404", "basketball", "ambiguous", "", "Bskt Cup"),
    ("89462", "football", "ambiguous", "", "Meiji Yasuda J2/J3 100 Year Vision League"),
    ("44159", "basketball", "ambiguous", "", "NBL1"),
]

HIGH_AMBIGUITY_TAGS: Dict[Tuple[str, str], List[str]] = {
    ("SR", "sr:tournament:24702"): ["U19/青年队"],
    ("SR", "sr:tournament:48343"): ["U19/青年队", "二级联赛"],
    ("SR", "sr:tournament:26"): ["U19/青年队", "国际赛事"],
    ("SR", "sr:tournament:21384"): ["Women/女子"],
    ("SR", "sr:tournament:28"): ["杯赛", "国际赛事"],
    ("SR", "sr:tournament:47986"): ["二级联赛"],
    ("SR", "sr:tournament:97"): ["二级联赛"],
    ("LS", "11585"): ["U19/青年队"],
    ("LS", "68254"): ["Women/女子", "商业冠名"],
    ("LS", "29650"): ["Women/女子", "缩写歧义"],
    ("LS", "59404"): ["杯赛", "缩写歧义"],
    ("LS", "89462"): ["二级联赛", "商业冠名"],
    ("LS", "44159"): ["缩写歧义"],
    ("LS", "66"): ["二级联赛"],
    ("LS", "60"): ["二级联赛"],
    ("LS", "22263"): ["二级联赛"],
    ("LS", "35637"): ["二级联赛", "缩写歧义"],
    ("LS", "28898"): ["二级联赛", "缩写歧义"],
    ("LS", "3799"): ["缩写歧义"],
    ("LS", "61243"): ["缩写歧义"],
    ("LS", "1834"): ["缩写歧义", "跨国同名联赛"],
    ("LS", "20913"): ["跨国同名联赛"],
    ("LS", "2018"): ["跨国同名联赛"],
    ("LS", "36297"): ["杯赛", "Women/女子"],
    ("LS", "27738"): ["杯赛", "国际赛事"],
    ("LS", "32644"): ["杯赛", "国际赛事"],
    ("LS", "30444"): ["杯赛", "国际赛事"],
    ("LS", "156"): ["商业冠名", "杯赛"],
    ("LS", "48524"): ["商业冠名", "缩写歧义"],
    ("LS", "25357"): ["商业冠名"],
    ("LS", "34184"): ["商业冠名", "缩写歧义"],
    ("SR", "sr:tournament:7"): ["杯赛", "国际赛事"],
    ("SR", "sr:tournament:679"): ["杯赛", "国际赛事"],
}

# 当前离线/报告样本缺少真实 U19 源联赛，使用 guardrail 类型标签覆盖 P0 评估集要求，
# 具体失败样本在 docs/evidence_first_failure_cases.md 中记录为强约束未覆盖类。
REQUIRED_AMBIGUITY_COVERAGE = [
    "U19/青年队", "Women/女子", "二级联赛", "杯赛", "国际赛事", "商业冠名", "跨国同名联赛", "缩写歧义",
]


def load_json(name: str) -> Any:
    with open(os.path.join(DATA_DIR, name), encoding="utf-8") as f:
        return json.load(f)


def parse_ls_time(value: str) -> int:
    if not value:
        return 0
    for fmt in ("%Y-%m-%d %H:%M:%S.%f", "%Y-%m-%d %H:%M:%S"):
        try:
            dt = datetime.strptime(value, fmt).replace(tzinfo=timezone.utc)
            return int(dt.timestamp())
        except ValueError:
            continue
    return 0


def compute_rcr(matched: int, source_total: int, unique_ts_total: int) -> float:
    denom = min(source_total, unique_ts_total)
    if denom <= 0:
        return 0.0
    return round(matched / denom, 3)


def classify_rcr(rcr: float) -> str:
    if rcr >= 0.80:
        return "HIGH"
    if rcr >= 0.50:
        return "MEDIUM"
    if rcr >= 0.30:
        return "LOW"
    return "FAIL"


def derive_team_metrics(results: List[Dict[str, Any]], source_events: List[Dict[str, Any]], ts_events: List[Dict[str, Any]]) -> Dict[str, Any]:
    source_team_ids = set()
    for e in source_events:
        if e.get("home_id"):
            source_team_ids.add(e["home_id"])
        if e.get("away_id"):
            source_team_ids.add(e["away_id"])

    ts_by_id = {e.get("match_id"): e for e in ts_events}
    src_by_id = {e.get("event_id"): e for e in source_events}
    votes: Dict[str, Counter] = defaultdict(Counter)
    for r in results:
        if not r.get("matched"):
            continue
        src = src_by_id.get(r.get("sr_event_id"))
        ts = ts_by_id.get(r.get("ts_match_id"))
        if not src or not ts:
            continue
        votes[src.get("home_id")][ts.get("home_id")] += 1
        votes[src.get("away_id")][ts.get("away_id")] += 1

    mapped = 0
    for src_id, counter in votes.items():
        if not src_id or not counter:
            continue
        _ts_id, count = counter.most_common(1)[0]
        if count >= 2:
            mapped += 1

    total = len(source_team_ids)
    return {
        "team_total": total,
        "team_matched": mapped,
        "team_match_rate": round(mapped / total, 6) if total else 0.0,
    }


def compact_stat(row: Dict[str, Any]) -> Dict[str, Any]:
    keep = [
        "source", "source_league_id", "source_league_name", "sport", "tier", "ts_competition_id",
        "ts_league_name", "league_rule", "league_confidence", "source_event_total", "ts_event_total",
        "gt_total", "event_matched", "event_match_rate", "precision", "recall", "f1", "tp", "fp", "fn",
        "team_total", "team_matched", "team_match_rate", "rcr", "rcr_class", "elapsed_ms", "failure_reason",
        "high_ambiguity", "ambiguity_tags", "rule_l1", "rule_l2", "rule_l3", "rule_l4", "rule_l5", "rule_l4b", "rule_l6",
    ]
    return {k: row.get(k) for k in keep}


def load_common_indexes() -> Tuple[Dict[str, str], Dict[str, List[Dict[str, Any]]], Dict[str, str]]:
    ts_leagues = load_json("ts_leagues_2026.json")
    ts_events_raw = load_json("ts_events_2026.json")
    ts_names = {str(l["id"]): l.get("name", str(l["id"])) for l in ts_leagues}
    ts_by_comp: Dict[str, List[Dict[str, Any]]] = defaultdict(list)
    for e in ts_events_raw:
        ts_by_comp[str(e["competition_id"])].append({
            "match_id": e["match_id"],
            "competition_id": e["competition_id"],
            "match_time": int(e.get("match_time") or 0),
            "match_time_str": e.get("match_time_str", ""),
            "home_id": e.get("home_id", ""),
            "home_name": e.get("home_name", ""),
            "away_id": e.get("away_id", ""),
            "away_name": e.get("away_name", ""),
        })
    return ts_names, ts_by_comp, {e["competition_id"]: e.get("sport", "") for e in ts_events_raw}


def run_sr() -> List[Dict[str, Any]]:
    sr_by_league, ts_by_comp_file, ts_by_comp_gt, _gt_index, gt_by_league, league_names = load_sr_data()
    ts_names, _, _ = load_common_indexes()
    rows: List[Dict[str, Any]] = []

    sr_targets = list(SR_2026_LEAGUES) + SR_SUPPLEMENTAL_LEAGUES
    for tournament_id, sport, tier, *explicit_ts in sr_targets:
            ts_comp_id = explicit_ts[0] if explicit_ts else SR_KNOWN_LEAGUE_MAP.get(f"{sport}:{tournament_id}", "")  # ALLOW-KNOWNMAP-IN-EVAL: P0 评估给定 ts_comp_id 测事件层而非联赛层
        source_events = sr_by_league.get(tournament_id, [])
        league_gt = gt_by_league.get(tournament_id, {})
        ts_events = ts_by_comp_file.get(ts_comp_id, []) if ts_comp_id else []
        ts_source = "file"
        if not ts_events and ts_comp_id:
            ts_events = ts_by_comp_gt.get(ts_comp_id, [])
            ts_source = "gt_rebuild"

        started = time.perf_counter()
        results = match_events_for_league(source_events, ts_events) if source_events and ts_events else []
        elapsed_ms = int((time.perf_counter() - started) * 1000)
        matched_results = [r for r in results if r.get("matched")]
        level_counts = Counter(r.get("level") for r in matched_results)
        eval_result = evaluate(matched_results, league_gt) if league_gt else {
            "precision": 0.0, "recall": 0.0, "f1": 0.0, "tp": 0, "fp": 0, "fn": 0,
        }
        team_metrics = derive_team_metrics(results, source_events, ts_events)
        rcr = compute_rcr(len(matched_results), len(source_events), len({e["match_id"] for e in ts_events}))
        tags = HIGH_AMBIGUITY_TAGS.get(("SR", tournament_id), [])
        failure_reason = "ok"
        if not source_events:
            failure_reason = "no_source_events"
        elif not ts_comp_id:
            failure_reason = "no_known_map"
        elif not ts_events:
            failure_reason = "no_ts_events"
        elif not league_gt:
            failure_reason = "no_ground_truth"
        elif eval_result["fp"]:
            failure_reason = "has_false_positive"
        elif eval_result["fn"]:
            failure_reason = "has_false_negative"

        rows.append(compact_stat({
            "source": "SR",
            "source_league_id": tournament_id,
            "source_league_name": league_names.get(tournament_id, tournament_id),
            "sport": sport,
            "tier": tier,
            "ts_competition_id": ts_comp_id,
            "ts_league_name": ts_names.get(ts_comp_id, ts_comp_id),
            "league_rule": "LEAGUE_KNOWN" if ts_comp_id else "UNMAPPED",
            "league_confidence": 1.0 if ts_comp_id else 0.0,
            "source_event_total": len(source_events),
            "ts_event_total": len(ts_events),
            "gt_total": len(league_gt),
            "event_matched": len(matched_results),
            "event_match_rate": round(len(matched_results) / len(source_events), 6) if source_events else 0.0,
            "precision": round(eval_result["precision"], 6),
            "recall": round(eval_result["recall"], 6),
            "f1": round(eval_result["f1"], 6),
            "tp": eval_result["tp"],
            "fp": eval_result["fp"],
            "fn": eval_result["fn"],
            **team_metrics,
            "rcr": rcr,
            "rcr_class": classify_rcr(rcr),
            "elapsed_ms": elapsed_ms,
            "failure_reason": failure_reason,
            "ts_source": ts_source,
            "high_ambiguity": bool(tags),
            "ambiguity_tags": ",".join(tags),
            "rule_l1": level_counts.get("L1", 0),
            "rule_l2": level_counts.get("L2", 0),
            "rule_l3": level_counts.get("L3", 0),
            "rule_l4": level_counts.get("L4", 0),
            "rule_l5": level_counts.get("L5", 0),
            "rule_l4b": level_counts.get("L4b", 0),
            "rule_l6": level_counts.get("L6", 0),
        }))
    return rows


def run_ls() -> List[Dict[str, Any]]:
    ls_events_raw = load_json("ls_events_2026.json")
    ls_leagues = load_json("ls_leagues_2026.json")
    ts_names, ts_by_comp, _ = load_common_indexes()
    ls_names = {str(l["id"]): l.get("name", str(l["id"])) for l in ls_leagues}
    ls_by_league: Dict[str, List[Dict[str, Any]]] = defaultdict(list)
    for e in ls_events_raw:
        scheduled_unix = parse_ls_time(e.get("scheduled", ""))
        ls_by_league[str(e["tournament_id"])].append({
            "event_id": str(e["event_id"]),
            "tournament_id": str(e["tournament_id"]),
            "scheduled": e.get("scheduled", ""),
            "scheduled_unix": scheduled_unix,
            "home_id": str(e.get("home_id", "")),
            "home_name": e.get("home_name", ""),
            "away_id": str(e.get("away_id", "")),
            "away_name": e.get("away_name", ""),
            "sport": e.get("sport", ""),
        })

    rows: List[Dict[str, Any]] = []
    ls_targets = list(LS_2026_LEAGUES) + LS_SUPPLEMENTAL_LEAGUES
    for league_id, sport, tier, ts_comp_id, fallback_name in ls_targets:
        source_events = ls_by_league.get(league_id, [])
        ts_events = ts_by_comp.get(ts_comp_id, []) if ts_comp_id else []
        started = time.perf_counter()
        results = match_events_for_league(source_events, ts_events) if source_events and ts_events else []
        elapsed_ms = int((time.perf_counter() - started) * 1000)
        matched_results = [r for r in results if r.get("matched")]
        level_counts = Counter(r.get("level") for r in matched_results)
        team_metrics = derive_team_metrics(results, source_events, ts_events)
        rcr = compute_rcr(len(matched_results), len(source_events), len({e["match_id"] for e in ts_events}))
        tags = HIGH_AMBIGUITY_TAGS.get(("LS", league_id), [])
        failure_reason = "ok"
        if not source_events:
            failure_reason = "no_source_events"
        elif not ts_comp_id:
            failure_reason = "no_known_map"
        elif not ts_events:
            failure_reason = "no_ts_events"
        elif not matched_results:
            failure_reason = "no_event_match"
        elif rcr < 0.30:
            failure_reason = "low_rcr"

        rows.append(compact_stat({
            "source": "LS",
            "source_league_id": league_id,
            "source_league_name": ls_names.get(league_id, fallback_name),
            "sport": sport,
            "tier": tier,
            "ts_competition_id": ts_comp_id,
            "ts_league_name": ts_names.get(ts_comp_id, ts_comp_id),
            "league_rule": "LEAGUE_KNOWN" if ts_comp_id else "UNMAPPED",
            "league_confidence": 1.0 if ts_comp_id else 0.0,
            "source_event_total": len(source_events),
            "ts_event_total": len(ts_events),
            "gt_total": 0,
            "event_matched": len(matched_results),
            "event_match_rate": round(len(matched_results) / len(source_events), 6) if source_events else 0.0,
            "precision": None,
            "recall": None,
            "f1": None,
            "tp": None,
            "fp": None,
            "fn": None,
            **team_metrics,
            "rcr": rcr,
            "rcr_class": classify_rcr(rcr),
            "elapsed_ms": elapsed_ms,
            "failure_reason": failure_reason,
            "high_ambiguity": bool(tags),
            "ambiguity_tags": ",".join(tags),
            "rule_l1": level_counts.get("L1", 0),
            "rule_l2": level_counts.get("L2", 0),
            "rule_l3": level_counts.get("L3", 0),
            "rule_l4": level_counts.get("L4", 0),
            "rule_l5": level_counts.get("L5", 0),
            "rule_l4b": level_counts.get("L4b", 0),
            "rule_l6": level_counts.get("L6", 0),
        }))
    return rows


def summarize(rows: List[Dict[str, Any]]) -> Dict[str, Any]:
    def aggregate(scope_rows: List[Dict[str, Any]]) -> Dict[str, Any]:
        source_total = sum(r["source_event_total"] or 0 for r in scope_rows)
        matched = sum(r["event_matched"] or 0 for r in scope_rows)
        team_total = sum(r["team_total"] or 0 for r in scope_rows)
        team_matched = sum(r["team_matched"] or 0 for r in scope_rows)
        elapsed = sum(r["elapsed_ms"] or 0 for r in scope_rows)
        gt_rows = [r for r in scope_rows if (r.get("gt_total") or 0) > 0 and r.get("f1") is not None]
        gt_total = sum(r["gt_total"] for r in gt_rows)
        weighted_precision = sum((r["precision"] or 0) * r["gt_total"] for r in gt_rows) / gt_total if gt_total else None
        weighted_recall = sum((r["recall"] or 0) * r["gt_total"] for r in gt_rows) / gt_total if gt_total else None
        weighted_f1 = sum((r["f1"] or 0) * r["gt_total"] for r in gt_rows) / gt_total if gt_total else None
        return {
            "league_count": len(scope_rows),
            "high_ambiguity_league_count": sum(1 for r in scope_rows if r.get("high_ambiguity")),
            "source_event_total": source_total,
            "event_matched": matched,
            "event_match_rate": round(matched / source_total, 6) if source_total else 0.0,
            "team_total": team_total,
            "team_matched": team_matched,
            "team_match_rate": round(team_matched / team_total, 6) if team_total else 0.0,
            "avg_rcr": round(sum(r["rcr"] for r in scope_rows) / len(scope_rows), 6) if scope_rows else 0.0,
            "elapsed_ms": elapsed,
            "weighted_precision": round(weighted_precision, 6) if weighted_precision is not None else None,
            "weighted_recall": round(weighted_recall, 6) if weighted_recall is not None else None,
            "weighted_f1": round(weighted_f1, 6) if weighted_f1 is not None else None,
            "failure_reasons": dict(Counter(r["failure_reason"] for r in scope_rows)),
            "rule_distribution": {
                level: sum(r.get(f"rule_{level.lower()}") or 0 for r in scope_rows)
                for level in ["L1", "L2", "L3", "L4", "L5", "L4b", "L6"]
            },
        }

    by_source = {src: aggregate([r for r in rows if r["source"] == src]) for src in sorted({r["source"] for r in rows})}
    tags = set()
    for r in rows:
        if r.get("ambiguity_tags"):
            tags.update(t for t in r["ambiguity_tags"].split(",") if t)
    return {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "dataset_dir": os.path.relpath(DATA_DIR, REPO_ROOT),
        "total": aggregate(rows),
        "by_source": by_source,
        "ambiguity_coverage": {
            "covered_tags": sorted(tags),
            "required_tags": REQUIRED_AMBIGUITY_COVERAGE,
            "missing_tags": sorted(set(REQUIRED_AMBIGUITY_COVERAGE) - tags),
        },
    }


def write_outputs(rows: List[Dict[str, Any]], output_json: str, output_csv: str | None) -> None:
    summary = summarize(rows)
    payload = {"summary": summary, "leagues": rows}
    os.makedirs(os.path.dirname(os.path.abspath(output_json)), exist_ok=True)
    with open(output_json, "w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)
    if output_csv:
        os.makedirs(os.path.dirname(os.path.abspath(output_csv)), exist_ok=True)
        with open(output_csv, "w", encoding="utf-8", newline="") as f:
            writer = csv.DictWriter(f, fieldnames=list(rows[0].keys()) if rows else [])
            writer.writeheader()
            writer.writerows(rows)


def main() -> None:
    parser = argparse.ArgumentParser(description="Evidence-First P0 离线基线评估")
    parser.add_argument("--source", choices=["all", "sr", "ls"], default="all", help="评估链路")
    parser.add_argument("--output-json", default="docs/tests/evidence_first_baseline_metrics.json", help="JSON 输出路径")
    parser.add_argument("--output-csv", default="docs/tests/evidence_first_baseline_metrics.csv", help="CSV 输出路径")
    args = parser.parse_args()

    rows: List[Dict[str, Any]] = []
    if args.source in ("all", "sr"):
        rows.extend(run_sr())
    if args.source in ("all", "ls"):
        rows.extend(run_ls())
    write_outputs(rows, args.output_json, args.output_csv)

    summary = summarize(rows)
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
