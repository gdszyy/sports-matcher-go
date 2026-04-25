"""
XP-BET 数据库连接模块（SSH 隧道版）
=====================================
本模块封装了通过 SSH 隧道连接 test-db 数据库集群的完整逻辑。

数据库集群信息
--------------
- RDS Host  : test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com
- RDS Port  : 3306
- User      : root
- Password  : r74pqyYtgdjlYB41jmWA

SSH 跳板机信息
--------------
- SSH Host  : 54.69.237.139
- SSH User  : ubuntu
- SSH Key   : /home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519

本地隧道端口映射
-----------------
- 3308 → test-db:3306  （主要使用端口）
- 3309 → test-db:3306  （备用端口）

主要数据库
----------
- test-xp-lsports     : LSports 体育数据（联赛/比赛/球队）
- test-thesports-db   : TheSports 体育数据（联赛/比赛/球队）

快速使用
--------
    from python.db.connector import setup_tunnel, get_conn, LS_DB, TS_DB

    # 1. 建立 SSH 隧道（仅需一次）
    proc = setup_tunnel()

    # 2. 获取连接
    conn = get_conn(LS_DB)
    with conn.cursor() as c:
        c.execute("SELECT COUNT(*) FROM ls_sport_event")
        print(c.fetchone())
    conn.close()

    # 3. 关闭隧道（可选）
    proc.terminate()
"""

import os
import subprocess
import time
import sys
import pymysql

# ─── 配置 ────────────────────────────────────────────────────────────────────

# RDS 连接信息
RDS_HOST     = 'test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com'
RDS_PORT     = 3306
DB_USER      = 'root'
DB_PASSWORD  = 'r74pqyYtgdjlYB41jmWA'

# SSH 跳板机
SSH_HOST     = '54.69.237.139'
SSH_USER     = 'ubuntu'
# 私钥路径：相对于本文件的位置，也可以用绝对路径
_THIS_DIR    = os.path.dirname(os.path.abspath(__file__))
SSH_KEY      = os.path.join(_THIS_DIR, '..', '..', 'keys', 'id_ed25519')
# 如果私钥在 xp-bet-db-connector 技能目录下，使用以下路径：
SSH_KEY_SKILL = os.path.expanduser(
    '~/skills/xp-bet-db-connector/templates/id_ed25519'
)

# 本地隧道端口
LOCAL_PORT   = 3308
LOCAL_PORT_2 = 3309  # 备用

# 数据库名称常量
LS_DB  = 'test-xp-lsports'      # LSports 数据库
TS_DB  = 'test-thesports-db'    # TheSports 数据库


# ─── SSH 隧道 ─────────────────────────────────────────────────────────────────

def _resolve_key() -> str:
    """自动选择可用的私钥路径"""
    for path in [SSH_KEY, SSH_KEY_SKILL]:
        expanded = os.path.expanduser(path)
        if os.path.exists(expanded):
            return expanded
    raise FileNotFoundError(
        f"SSH 私钥未找到，请检查路径：\n  {SSH_KEY}\n  {SSH_KEY_SKILL}"
    )


def setup_tunnel(local_port: int = LOCAL_PORT, wait: int = 6) -> subprocess.Popen:
    """
    建立 SSH 隧道，将本地端口映射到 RDS。

    Parameters
    ----------
    local_port : int
        本地监听端口，默认 3308
    wait : int
        等待隧道就绪的秒数，默认 6

    Returns
    -------
    subprocess.Popen
        SSH 子进程，调用方负责在结束时调用 proc.terminate()
    """
    key = _resolve_key()
    # Ensure SSH key has correct permissions (SSH client requires 0600)
    os.chmod(key, 0o600)
    cmd = [
        'ssh',
        '-o', 'StrictHostKeyChecking=no',
        '-o', 'UserKnownHostsFile=/dev/null',
        '-i', key,
        '-N', '-L',
        f'{local_port}:{RDS_HOST}:{RDS_PORT}',
        f'{SSH_USER}@{SSH_HOST}',
    ]
    print(f"[SSH Tunnel] localhost:{local_port} → {RDS_HOST}:{RDS_PORT}", flush=True)
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    time.sleep(wait)
    if proc.poll() is not None:
        _, err = proc.communicate()
        raise RuntimeError(f"SSH 隧道启动失败：{err.decode()}")
    print(f"[SSH Tunnel] 隧道就绪（PID={proc.pid}）", flush=True)
    return proc


def get_conn(database: str, local_port: int = LOCAL_PORT) -> pymysql.Connection:
    """
    获取指定数据库的 pymysql 连接（通过本地隧道端口）。

    Parameters
    ----------
    database : str
        数据库名，例如 LS_DB 或 TS_DB
    local_port : int
        本地隧道端口，默认 3308

    Returns
    -------
    pymysql.Connection
    """
    return pymysql.connect(
        host='127.0.0.1',
        port=local_port,
        user=DB_USER,
        password=DB_PASSWORD,
        database=database,
        charset='utf8mb4',
        connect_timeout=20,
    )


# ─── 快速测试 ─────────────────────────────────────────────────────────────────

if __name__ == '__main__':
    proc = setup_tunnel()
    try:
        for db in [LS_DB, TS_DB]:
            conn = get_conn(db)
            with conn.cursor() as c:
                c.execute('SHOW TABLES')
                tables = [r[0] for r in c.fetchall()]
            conn.close()
            print(f"[{db}] {len(tables)} tables: {tables[:5]} ...")
    finally:
        proc.terminate()
        print("隧道已关闭")
