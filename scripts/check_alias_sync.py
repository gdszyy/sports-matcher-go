#!/usr/bin/env python3
"""check_alias_sync.py — 校验 Python alias 字典与 Go 源同步（v1.21 哨兵）

Python eval 算法 (evidence_first_strict_baseline.py) 依赖
python/data/league_alias_groups.json 跟 Go internal/matcher/league_alias.go
的 staticLeagueAliasGroups 保持完全一致。否则 Python eval 偏离 Go 真实算法，
strict 基线数字失真。

本脚本：
  1. 从 internal/matcher/league_alias.go 重新提取 LeagueAliasGroup
  2. 跟 python/data/league_alias_groups.json 逐项对比
  3. 不一致即 fail（提示重新生成 JSON）

GitHub Actions 集成示例：
  - name: Check alias sync
    run: python3 scripts/check_alias_sync.py
"""
from __future__ import annotations
import json
import os
import re
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def extract_go_groups():
    """从 internal/matcher/league_alias.go 提取 LeagueAliasGroup。"""
    go_file = os.path.join(REPO_ROOT, 'internal/matcher/league_alias.go')
    with open(go_file) as f:
        src = f.read()
    m = re.search(
        r'var staticLeagueAliasGroups = \[\]LeagueAliasGroup\{(.*?)\n\}\n',
        src, re.DOTALL,
    )
    if not m:
        print('ERROR: staticLeagueAliasGroups not found in Go source')
        sys.exit(2)
    block = m.group(1)
    # v1.33: 同时提取 DefaultCountry 字段（可选）
    pattern = re.compile(
        r'CanonicalName:\s*"([^"]+)",\s*Aliases:\s*\[\]string\{([^}]+)\},'
        r'(?:\s*DefaultCountry:\s*"([^"]+)",)?',
        re.DOTALL,
    )
    out = []
    for g in pattern.finditer(block):
        canonical = g.group(1)
        aliases = re.findall(r'"([^"]+)"', g.group(2))
        entry = {'canonical': canonical, 'aliases': aliases}
        if g.group(3):
            entry['default_country'] = g.group(3)
        out.append(entry)
    return out


def load_python_groups():
    """加载 python/data/league_alias_groups.json。"""
    p = os.path.join(REPO_ROOT, 'python/data/league_alias_groups.json')
    if not os.path.exists(p):
        print(f'ERROR: {p} 不存在；运行：python3 scripts/sync_alias.py')
        sys.exit(2)
    return json.load(open(p))


def main():
    go_groups = extract_go_groups()
    py_groups = load_python_groups()
    if len(go_groups) != len(py_groups):
        print(f'✗ Group 数量不一致: Go={len(go_groups)} Python={len(py_groups)}')
        print(f'  → 运行 scripts/sync_alias.py 重新生成 JSON')
        return 1
    # 逐组对比
    go_idx = {g['canonical']: set(g['aliases']) for g in go_groups}
    py_idx = {g['canonical']: set(g['aliases']) for g in py_groups}
    failures = []
    for canonical in go_idx:
        if canonical not in py_idx:
            failures.append(f'  Go 有 canonical={canonical!r} 但 Python 没有')
            continue
        diff_go_only = go_idx[canonical] - py_idx[canonical]
        diff_py_only = py_idx[canonical] - go_idx[canonical]
        if diff_go_only or diff_py_only:
            failures.append(
                f'  [{canonical!r}] Go-only={sorted(diff_go_only)} Python-only={sorted(diff_py_only)}'
            )
    for canonical in py_idx:
        if canonical not in go_idx:
            failures.append(f'  Python 有 canonical={canonical!r} 但 Go 没有')
    if failures:
        print(f'✗ Python alias 字典与 Go 不一致 ({len(failures)} 处):')
        for f in failures[:20]:
            print(f)
        if len(failures) > 20:
            print(f'  ... 还有 {len(failures) - 20} 处')
        print('')
        print('  → 修复：python3 scripts/sync_alias.py')
        return 1
    print(f'✓ Python alias 字典与 Go 一致 ({len(go_groups)} 组)')
    return 0


if __name__ == '__main__':
    sys.exit(main())
