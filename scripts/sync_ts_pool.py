#!/usr/bin/env python3
"""sync_ts_pool.py — 从生产 DB 拉真实 TS competition pool（v1.26）

替换 python/data/ts_leagues_2026.json 的 stub 池（45 个不全且 country 缺）为
生产 DB 真实 ts_fb_competition + ts_bb_competition 全表数据。

设计：
- football: ts_fb_competition.host_country 真值
- basketball: ts_bb_competition 无 host_country 字段（与 Go 一致），country=""
- 输出 python/data/ts_pool_real.json（不替换 ts_leagues_2026.json，作为补充池供
  wide_baseline.py 加载）

依赖：SSH 隧道（cfg = config.Default()），与 sports-matcher Go 端共用 SSH 配置。
"""
from __future__ import annotations
import json
import os
import sys

# 让 Python 能 import sshtunnel + pymysql（pip 已装）
try:
    from sshtunnel import SSHTunnelForwarder
    import pymysql
except ImportError as e:
    print(f"缺依赖: {e}; pip install sshtunnel pymysql cryptography --break-system-packages")
    sys.exit(2)

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

SSH_HOST = os.environ.get("SSH_HOST", "54.69.237.139")
SSH_USER = os.environ.get("SSH_USER", "ubuntu")
SSH_KEY = os.environ.get("SSH_KEY_PATH", "/tmp/sm-key")
SSH_PORT = int(os.environ.get("SSH_PORT", "22"))

DB_HOST = os.environ.get("DB_HOST", "test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com")
DB_PORT = int(os.environ.get("DB_PORT", "3306"))
DB_USER = os.environ.get("DB_USER", "root")
DB_PASS = os.environ.get("DB_PASSWORD", "r74pqyYtgdjlYB41jmWA")
DB_NAME = "test-thesports-db"


def fetch_pool():
    print(f"[sync_ts_pool] SSH {SSH_USER}@{SSH_HOST}:{SSH_PORT} → DB {DB_HOST}:{DB_PORT}")
    with SSHTunnelForwarder(
        (SSH_HOST, SSH_PORT),
        ssh_username=SSH_USER,
        ssh_pkey=SSH_KEY,
        remote_bind_address=(DB_HOST, DB_PORT),
        local_bind_address=('127.0.0.1', 0),
    ) as tunnel:
        print(f"[sync_ts_pool] tunnel up, local port = {tunnel.local_bind_port}")
        conn = pymysql.connect(
            host='127.0.0.1', port=tunnel.local_bind_port,
            user=DB_USER, password=DB_PASS, database=DB_NAME,
            charset='utf8mb4', connect_timeout=10,
        )
        print(f"[sync_ts_pool] DB connected")
        out = []
        with conn.cursor() as cur:
            cur.execute("SELECT competition_id, COALESCE(name,''), COALESCE(host_country,'') FROM ts_fb_competition")
            rows = cur.fetchall()
            print(f"[sync_ts_pool] football: {len(rows)} 行")
            for cid, name, country in rows:
                out.append({'id': cid, 'name': name, 'country': country, 'sport': 'football'})
            cur.execute("SELECT competition_id, COALESCE(name,'') FROM ts_bb_competition")
            rows = cur.fetchall()
            print(f"[sync_ts_pool] basketball: {len(rows)} 行")
            for cid, name in rows:
                out.append({'id': cid, 'name': name, 'country': '', 'sport': 'basketball'})
        conn.close()
    return out


if __name__ == '__main__':
    pool = fetch_pool()
    out_path = os.path.join(REPO_ROOT, 'python/data/ts_pool_real.json')
    with open(out_path, 'w', encoding='utf-8') as f:
        json.dump(pool, f, ensure_ascii=False, indent=2)
    print(f"[sync_ts_pool] 写入 {out_path}: {len(pool)} 行")
