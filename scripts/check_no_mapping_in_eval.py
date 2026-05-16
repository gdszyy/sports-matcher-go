#!/usr/bin/env python3
"""check_no_mapping_in_eval.py — 防止测评代码自证算法（v1.18 决议）

扫描"测评相关"代码路径，若发现直接调用 KnownLeagueMap[...] / KnownLSLeagueMap[...]
做算法输入（而非作为 GT 对照），立即 fail。

白名单：在该行末尾加 `# ALLOW-KNOWNMAP-IN-EVAL`（Python）或 `// ALLOW-KNOWNMAP-IN-EVAL`（Go）。
适用于：(1) 把 KnownMap 当作"事实答案"做 GT 对照；(2) 测试自身的存储读写；
       (3) 历史评估脚本（标记为 v1.18 之前的设计）。

检测策略：精确匹配"代码使用"模式，剔除注释/字符串/import：
  - `xxx[key]` 或 `xxx.get(...)` 等读取操作
  - 排除：注释行、纯 docstring、import as、变量定义行
"""
from __future__ import annotations
import ast
import os
import re
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

SCAN_PATHS = [
    "python/evidence_first_strict_baseline.py",
    "python/evidence_first_baseline_eval.py",
    "python/test_sr_2026.py",
    "python/match_2026.py",
]

# 被监控的 mapping 变量名（Python 风格 + Go 风格）
MAP_NAMES = {
    "KnownLeagueMap", "KnownLSLeagueMap",
    "KNOWN_LEAGUE_MAP", "KNOWN_LS_LEAGUE_MAP", "KNOWN_LS_TS_MAP",
    "SR_KNOWN_LEAGUE_MAP", "LS_KNOWN_TS_MAP",
}

# 视为"使用"的代码模式：变量名后紧跟 `[`、`.get(`、`.lookup(` 等
USE_RE = re.compile(
    r"\b(" + "|".join(re.escape(n) for n in MAP_NAMES) + r")\s*(\[|\.get\(|\.lookup\()"
)

ALLOW_TAG = "ALLOW-KNOWNMAP-IN-EVAL"


def is_definition_line(stripped: str) -> bool:
    """定义行：var/KEY = {...} 形式视为合法定义。"""
    if re.match(r"^(var|const|[A-Z_]+)\s*=", stripped):
        return True
    if re.match(r"^[A-Z_]+\s*:\s*Dict", stripped):  # 类型注解
        return True
    return False


def is_import_line(stripped: str) -> bool:
    return bool(re.match(r"^(import\s|from\s+\S+\s+import)", stripped))


def check_file(path: str) -> list[tuple[int, str]]:
    """返回违规行列表 [(lineno, content), ...]"""
    abs_path = os.path.join(REPO_ROOT, path)
    if not os.path.exists(abs_path):
        return []
    violations = []
    with open(abs_path, "r", encoding="utf-8") as f:
        src = f.read()
    # 先用 ast 提取所有字符串字面值的行号范围，跳过它们
    string_lines = set()
    try:
        tree = ast.parse(src)
        for node in ast.walk(tree):
            if isinstance(node, (ast.Str, ast.Constant)) and isinstance(getattr(node, "s", node.value if hasattr(node, "value") else None), str):
                start = getattr(node, "lineno", 0)
                end = getattr(node, "end_lineno", start) or start
                for i in range(start, end + 1):
                    string_lines.add(i)
    except SyntaxError:
        pass

    for lineno, line in enumerate(src.splitlines(), start=1):
        if not USE_RE.search(line):
            continue
        stripped = line.lstrip()
        if stripped.startswith("#") or stripped.startswith("//"):
            continue
        if is_import_line(stripped):
            continue
        if is_definition_line(stripped):
            continue
        if ALLOW_TAG in line:
            continue
        # 完全在字符串字面值范围内 → 跳过
        if lineno in string_lines:
            # 但需确认整行只是字符串（match 的部分确实在字符串内）
            # 简化处理：若 stripped 以引号开头或以 `'description'` 之类的字典 key 开头，视为字符串
            if (
                stripped.startswith('"') or stripped.startswith("'")
                or re.match(r"^['\"][^'\"]+['\"]\s*:", stripped)
            ):
                continue
        violations.append((lineno, line.rstrip()))
    return violations


def main():
    total = 0
    for p in SCAN_PATHS:
        vs = check_file(p)
        if not vs:
            continue
        for ln, content in vs:
            print(
                f"::error file={p},line={ln}::测评代码直接读取 KnownLeagueMap/KnownLSLeagueMap (v1.18 决议禁止)"
            )
            print(f"    {content}")
            print(
                f"    → 修复：去除该读取改走纯算法路径；或加行尾 `# {ALLOW_TAG}` 注释明示意图（如用作 GT 对照）"
            )
        total += len(vs)
    if total > 0:
        print(f"\n✗ 发现 {total} 处测评代码直接读取 KnownLeagueMap，违反 v1.18 决议")
        return 1
    print("✓ 测评代码无 KnownLeagueMap 自证算法的引用")
    return 0


if __name__ == "__main__":
    sys.exit(main())
