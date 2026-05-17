#!/usr/bin/env python3
"""sync_alias.py — 从 Go 源同步生成 Python alias 字典 (v1.21)

读 internal/matcher/league_alias.go::staticLeagueAliasGroups，
写 python/data/league_alias_groups.json。

CI 上由 check_alias_sync.py 校验同步性。
"""
import json
import os
import re
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def extract_go_groups():
    go_file = os.path.join(REPO_ROOT, 'internal/matcher/league_alias.go')
    with open(go_file) as f:
        src = f.read()
    m = re.search(
        r'var staticLeagueAliasGroups = \[\]LeagueAliasGroup\{(.*?)\n\}\n',
        src, re.DOTALL,
    )
    if not m:
        print('ERROR: staticLeagueAliasGroups not found')
        sys.exit(2)
    pattern = re.compile(
        r'CanonicalName:\s*"([^"]+)",\s*Aliases:\s*\[\]string\{([^}]+)\}',
        re.DOTALL,
    )
    out = []
    for g in pattern.finditer(m.group(1)):
        aliases = re.findall(r'"([^"]+)"', g.group(2))
        out.append({'canonical': g.group(1), 'aliases': aliases})
    return out


if __name__ == '__main__':
    groups = extract_go_groups()
    out_path = os.path.join(REPO_ROOT, 'python/data/league_alias_groups.json')
    with open(out_path, 'w', encoding='utf-8') as f:
        json.dump(groups, f, ensure_ascii=False, indent=2)
    print(f'✓ 写入 {out_path}: {len(groups)} 组')
