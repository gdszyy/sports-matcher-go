# SR 与 LSports 数据库连接固化文档

作者：**Manus AI**

更新时间：2026-06-02

本文档提炼并固化 `sports-matcher-go` 仓库中连接 **SportRadar（SR）数据库** 与 **LSports 数据库** 的方法。仓库当前通过 **SSH 跳板机 + AWS RDS MySQL** 访问业务数据，Go 服务与 Python 脚本都围绕同一套连接参数工作，但入口与本地端口约定略有差异。

> **安全说明**：当前 GitHub 仓库为公开仓库，因此本文档不粘贴 SSH 私钥正文，也不新增私钥文件。连接所需的密钥与密码应通过环境变量、受控密钥文件或既有安全目录提供。本文只固化**连接方法、配置项、密钥路径、默认来源与排障规则**；若后续需要彻底治理，建议将代码中已有的真实默认密码迁移到环境变量或密钥管理系统。

## 1. 结论摘要

仓库内的 SR 连接指向 **处理后的 SR MySQL 数据库** `xp-bet-test`，LSports 连接指向 **LSports MySQL 数据库** `test-xp-lsports`。二者位于同一个 RDS MySQL 集群 `test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306`，复用同一组 MySQL 用户名与密码，并统一通过 `ubuntu@54.69.237.139` 跳板机访问。

| 数据源 | 数据库名 | RDS Host | RDS Port | 仓库内主要入口 | 直接本地端口约定 | 说明 |
|---|---|---:|---:|---|---:|---|
| SR / SportRadar | `xp-bet-test` | `test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com` | `3306` | `tunnel.SRDb`、`SRAdapter` | `3308` | 处理后的 SR 结构化数据与多语言字典。 |
| LSports | `test-xp-lsports` | `test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com` | `3306` | `tunnel.LSDb`、`LSAdapter`、`LSPlayerAdapter` | `3309` | LSports 赛事、盘口、球队与联赛数据。 |
| TheSports | `test-thesports-db` | `test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com` | `3306` | `tunnel.TSDb`、`TSAdapter` | `3308` | 匹配目标库，本文只作为 SR/LS 匹配链路依赖说明。 |

## 2. 密钥、密码与配置项固化

连接凭据在仓库中分为两层：第一层是 **SSH 跳板机密钥**，用于从本机或服务容器打通到 RDS 的安全通道；第二层是 **MySQL 账号密码**，用于通过隧道连接具体数据库。Go 服务默认从环境变量读取配置，环境变量缺失时使用 `internal/config/config.go` 中的默认值；Python 脚本则集中在 `python/db/connector.py` 与少量历史脚本中读取相同参数。

| 类别 | 配置项 | 当前仓库默认来源 | 推荐运行时注入方式 | 备注 |
|---|---|---|---|---|
| SSH Host | `SSH_HOST` | `internal/config/config.go`、`python/db/connector.py` | 环境变量 | 跳板机地址为 `54.69.237.139`。 |
| SSH Port | `SSH_PORT` | `internal/config/config.go` | 环境变量 | 默认 `22`。 |
| SSH User | `SSH_USER` | `internal/config/config.go`、`python/db/connector.py` | 环境变量 | 默认 `ubuntu`。 |
| SSH Key | `SSH_KEY_PATH` | `internal/config/config.go`、`python/db/connector.py` | 环境变量或挂载文件 | Go 默认使用 `/home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519`；Python 优先尝试仓库 `keys/id_ed25519`，失败后回退到技能目录密钥。 |
| MySQL Host | `DB_HOST` | `internal/config/config.go`、`python/db/connector.py` | 环境变量 | 指向 `test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com`。 |
| MySQL Port | `DB_PORT` | `internal/config/config.go`、`python/db/connector.py` | 环境变量 | RDS 真实端口为 `3306`。 |
| MySQL User | `DB_USER` | `internal/config/config.go`、`python/db/connector.py` | 环境变量 | 默认用户为 `root`。 |
| MySQL Password | `DB_PASSWORD` | `internal/config/config.go`、`python/db/connector.py`、历史查询文档 | 环境变量或密钥管理 | 仓库既有代码中存在默认密码。公开仓库不建议继续在新文档中扩散该值，应以 `DB_PASSWORD` 注入。 |
| 本地端口 | `LOCAL_PORT` / 手动 `-L` | `internal/db/tunnel.go`、`python/fetch_2026_data.py` | 命令行或环境变量 | Go 内置 SSH 模式不依赖本地监听端口；直连回退与 Python 手动隧道使用 `3308`、`3309`。 |

> **密钥落点约定**：不要把 SSH 私钥正文写入 Markdown，也不要提交到 Git。仓库 `.gitignore` 已忽略 `keys/`、`*.pem`、`*.key`、`id_rsa` 等私钥文件，因此本地运行时可把私钥放在 `keys/id_ed25519`，或使用技能目录 `/home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519`。使用前需执行 `chmod 600 <key_path>`。

## 3. Go 服务连接方法

Go 主服务的统一入口是 `db.NewTunnel(cfg)`。`cmd/server/main.go` 中的 HTTP 服务、SR 单联赛匹配、SR 批量匹配、LS 单联赛匹配与 LS 批量匹配都会先创建 `Tunnel`，随后把 `tunnel.SRDb`、`tunnel.LSDb` 或 `tunnel.TSDb` 注入对应 Adapter。服务入口 `internal/api/server.go` 也沿用同一链路，在创建 HTTP 服务时一次性构造 SR、TS 与 LS 连接。

| Go 文件 | 角色 | 连接相关职责 |
|---|---|---|
| `internal/config/config.go` | 配置入口 | 从环境变量读取 `SSH_*`、`DB_*`、`LOCAL_PORT`，并提供默认值。 |
| `internal/db/tunnel.go` | 核心连接管理 | 优先通过 Go SSH Client 建立隧道，并注册 MySQL 自定义 Dialer；失败时回退到本地端口直连。 |
| `cmd/server/main.go` | CLI 入口 | `serve`、`match`、`match2`、`ls-match`、`batch`、`batch2`、`ls-batch` 均通过 `db.NewTunnel(cfg)` 触发连接。 |
| `internal/api/server.go` | HTTP 服务入口 | 调用 `db.NewTunnel(cfg)` 后创建 `SRAdapter`、`TSAdapter`、`LSAdapter` 与 `LSPlayerAdapter`。 |
| `internal/db/sr_adapter.go` | SR 查询层 | 使用外部传入的 `*sql.DB` 查询 `sr_*` 表，不负责建连。 |
| `internal/db/ls_adapter.go` | LS 查询层 | 使用外部传入的 `*sql.DB` 查询 `ls_*` 表，不负责建连。 |

### 3.1 Go 内置 SSH 隧道模式

Go 内置模式不需要用户手动执行 `ssh -L`。`tunnel.go` 会读取 `SSH_KEY_PATH` 指向的 ED25519 私钥，连接 `SSH_HOST:SSH_PORT`，随后用 `go-sql-driver/mysql` 的自定义 Dialer 将 MySQL 请求转发到 RDS。连接成功后同一个 Tunnel 会同时打开三个 MySQL 连接池。

```go
// 伪代码：实际实现见 internal/db/tunnel.go
cfg := config.Default()
tunnel, err := db.NewTunnel(cfg)
if err != nil {
    return err
}
defer tunnel.Close()

srAdapter := db.NewSRAdapter(tunnel.SRDb) // xp-bet-test
lsAdapter := db.NewLSAdapter(tunnel.LSDb) // test-xp-lsports
tsAdapter := db.NewTSAdapter(tunnel.TSDb) // test-thesports-db
```

默认环境变量示例如下。生产或共享环境应通过 CI/CD、容器密钥、运行时环境变量注入 `DB_PASSWORD` 与 `SSH_KEY_PATH`，不要在命令历史中明文粘贴真实密码。

```bash
export SSH_HOST=54.69.237.139
export SSH_PORT=22
export SSH_USER=ubuntu
export SSH_KEY_PATH=/home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519
export DB_HOST=test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com
export DB_PORT=3306
export DB_USER=root
export DB_PASSWORD='<从受控密钥源注入>'

./sports-matcher serve --port 8080
```

### 3.2 Go 本地端口回退模式

如果 Go 内置 SSH 连接失败，`tunnel.go` 会回退到本地端口直连模式。该模式假设外部已经建立两个本地端口映射：`3308` 用于 `xp-bet-test` 与 `test-thesports-db`，`3309` 用于 `test-xp-lsports`。这也是 Python 数据生成脚本采用的端口约定。

```bash
ssh -i keys/id_ed25519 -N \
  -L 3308:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
  -L 3309:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
  ubuntu@54.69.237.139 &
```

建立隧道后，Go 回退模式会按下表打开数据库连接。

| 数据库 | 本地 Host | 本地 Port | 用户 | 密码来源 |
|---|---|---:|---|---|
| `xp-bet-test` | `127.0.0.1` | `3308` | `DB_USER` | `DB_PASSWORD` |
| `test-thesports-db` | `127.0.0.1` | `3308` | `DB_USER` | `DB_PASSWORD` |
| `test-xp-lsports` | `127.0.0.1` | `3309` | `DB_USER` | `DB_PASSWORD` |

## 4. Python 脚本连接方法

Python 侧推荐统一使用 `python/db/connector.py`，该模块封装了 `setup_tunnel()` 与 `get_conn()`。历史脚本中仍存在直接写 `127.0.0.1`、`3308`、`3309` 的连接方式，后续重构时应逐步回收至统一连接模块，避免重复硬编码。

```python
from python.db.connector import setup_tunnel, get_conn, LS_DB

proc = setup_tunnel(local_port=3308)
try:
    conn = get_conn(LS_DB, local_port=3308)
    with conn.cursor() as cur:
        cur.execute("SELECT COUNT(*) FROM ls_sport_event")
        print(cur.fetchone())
    conn.close()
finally:
    proc.terminate()
```

需要注意的是，`python/db/connector.py` 默认只打开一个本地端口 `3308`，而 `python/fetch_2026_data.py` 的历史约定是 SR/TS 走 `3308`、LS 走 `3309`。如果要同时运行 `fetch_2026_data.py` 拉取 SR 与 LS 数据，应使用双端口手动隧道。

```bash
ssh -i keys/id_ed25519 -N \
  -L 3308:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
  -L 3309:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
  ubuntu@54.69.237.139 &

python3 python/fetch_2026_data.py
python3 python/build_sr_ts_ground_truth.py
```

| Python 文件 | 当前连接方式 | SR 端口 | LS 端口 | 备注 |
|---|---|---:|---:|---|
| `python/db/connector.py` | `setup_tunnel()` + `get_conn()` | `3308` | 可复用 `3308` | 统一封装入口，适合新脚本使用。 |
| `python/fetch_2026_data.py` | 假设本地隧道已建 | `3308` | `3309` | 同时拉取 SR、LS、TS 的 2026 数据。 |
| `python/build_sr_ts_ground_truth.py` | 假设本地隧道已建 | `3308` | 不涉及 | 读取 SR 与 TS，生成 SR↔TS Ground Truth。 |

## 5. SR 数据库访问说明

仓库内 SR 数据库默认指 **MySQL `xp-bet-test`**，主要存放处理后的 SR 联赛、赛事、球队、球员、赔率与多语言字典。Go 侧通过 `tunnel.SRDb` 注入 `SRAdapter`，Python 侧通过 `get_conn('xp-bet-test', 3308)` 访问。

| 常用表 | 用途 | 仓库调用位置 |
|---|---|---|
| `sr_sport_event` | SR 赛事主表，含开始时间、主客队、状态。 | `internal/db/sr_adapter.go`、`python/fetch_2026_data.py`、`python/build_sr_ts_ground_truth.py` |
| `sr_tournament_en` | SR 联赛英文名称与运动类型。 | `internal/db/sr_adapter.go`、`python/fetch_2026_data.py` |
| `sr_competitor_en` | SR 球队/参赛者英文名称。 | `internal/db/sr_adapter.go`、`python/fetch_2026_data.py` |
| `sr_player_en` | SR 球员信息。 | `internal/db/sr_adapter.go` |
| `xp_tournament_hot` | XP-BET 热门联赛配置。 | `python/fetch_2026_data.py` |

如果需要查询 **SR 原始 XML PostgreSQL 库**，应注意这不是当前 Go 服务的运行时连接对象。原始 XML 位于 PostgreSQL `xp-bet-test`，Host 为 `test-pg.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:5432`，需要通过独立的 PostgreSQL 隧道和查询脚本访问。此能力属于数据库连接器技能范围，不是仓库 Go 运行时路径。

## 6. LSports 数据库访问说明

仓库内 LSports 数据库为 **MySQL `test-xp-lsports`**。Go 侧通过 `tunnel.LSDb` 注入 `LSAdapter` 与 `LSPlayerAdapter`，Python 数据生成脚本通过 `get_conn('test-xp-lsports', 3309)` 访问。由于 LSports 与 SR/TS 共用同一个 RDS Host 和 MySQL 凭据，因此连接差异主要体现在数据库名与本地端口约定。

| 常用表 | 用途 | 仓库调用位置 |
|---|---|---|
| `ls_sport_event` | LSports 赛事主表，含联赛、赛程、球队、状态。 | `internal/db/ls_adapter.go`、`python/fetch_2026_data.py` |
| `ls_tournament_en` | LSports 联赛英文名称。 | `internal/db/ls_adapter.go`、`python/fetch_2026_data.py` |
| `ls_category_en` | LSports 地区/国家名称。 | `internal/db/ls_adapter.go`、`python/fetch_2026_data.py` |
| `ls_competitor_en` | LSports 球队/参赛者名称。 | `internal/db/ls_adapter.go`、`python/fetch_2026_data.py` |
| `ls_odds_change` | LSports 赔率变化记录。 | 查询参考文档与外部连接器常用。 |

LSports 查询时需要注意两个历史坑点。第一，`ls_competitor_en.competitor_id` 与 `ls_sport_event.home_competitor_id` 的类型可能不一致，Python 构建字典时应统一转为字符串。第二，`ls_category_en` 与赛事表 JOIN 时应同时约束 `cat.sport_id = e.sport_id`，否则同一 `category_id` 在多运动下会放大统计结果。

## 7. 连接验证命令

下面命令用于最小化验证。为避免命令历史暴露密码，建议先把密码写入当前 Shell 的临时环境变量，或通过受控密钥工具注入。

```bash
export DB_PASSWORD='<从受控密钥源注入>'

# 建立双端口隧道
ssh -i keys/id_ed25519 -N \
  -L 3308:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
  -L 3309:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
  ubuntu@54.69.237.139 &

# 验证 SR MySQL
mysql -h 127.0.0.1 -P 3308 -u root -p"${DB_PASSWORD}" xp-bet-test \
  -e "SELECT sport_id, name FROM sr_sport_en LIMIT 5;"

# 验证 LSports MySQL
mysql -h 127.0.0.1 -P 3309 -u root -p"${DB_PASSWORD}" test-xp-lsports \
  -e "SELECT COUNT(*) AS cnt FROM ls_sport_event;"
```

如果使用仓库 Python 封装验证，可运行：

```bash
python3 python/db/connector.py
```

如果使用连接器技能验证，可运行：

```bash
python3 /home/ubuntu/skills/xp-bet-db-connector/scripts/mysql_connect.py \
  --db "xp-bet-test" \
  --query "SELECT sport_id, name FROM sr_sport_en LIMIT 5"

python3 /home/ubuntu/skills/xp-bet-db-connector/scripts/mysql_connect.py \
  --db "test-xp-lsports" \
  --query "SELECT COUNT(*) FROM ls_odds_change"
```

## 8. 排障清单

| 现象 | 常见原因 | 处理方法 |
|---|---|---|
| `读取 SSH 私钥失败` | `SSH_KEY_PATH` 不存在，或容器未挂载私钥。 | 检查 `SSH_KEY_PATH`，或把私钥放入被 `.gitignore` 忽略的 `keys/id_ed25519`。 |
| `解析 SSH 私钥失败` | 私钥格式错误，或文件不是私钥正文。 | 确认使用 ED25519 私钥，并执行 `chmod 600 <key_path>`。 |
| `SSH 连接失败` | 跳板机不可达、端口被拦截、私钥不匹配。 | 验证 `ssh -i <key> ubuntu@54.69.237.139` 是否可登录。 |
| `数据库 ping 失败` | 隧道未建立、端口映射错误、密码错误。 | Go 内置模式检查 `DB_*`；本地回退模式检查 `3308/3309` 是否被监听。 |
| LS 查询为空但 SR 正常 | LS 误连到 `3308` 或数据库名错误。 | 对历史脚本按约定使用 `3309 + test-xp-lsports`。 |
| SR 查询为空但 TS 正常 | 数据库名或查询表错误。 | SR 应连接 `xp-bet-test` 并查询 `sr_*` 表。 |

## 9. 建议后续治理

当前仓库既有代码与历史文档中已经出现真实数据库密码，这与公开仓库的安全边界不一致。建议后续单独发起一次凭据治理：轮换 MySQL 密码与 SSH 密钥，把 `internal/config/config.go`、`python/db/connector.py`、`python/db/db_queries.md` 中的真实默认值改为占位符，并在 CI/CD 或运行环境中注入 `DB_PASSWORD` 与 `SSH_KEY_PATH`。在完成轮换前，应把本文档视为连接方法索引，而不是长期密钥存储介质。

## 10. 仓库证据索引

| 证据文件 | 关键内容 |
|---|---|
| `internal/config/config.go` | Go 服务默认读取 `SSH_*`、`DB_*`、`LOCAL_PORT`，并提供默认连接参数。 |
| `internal/db/tunnel.go` | 实现 Go 内置 SSH 隧道、自定义 MySQL Dialer、SR/TS/LS 三个连接池与本地端口回退。 |
| `internal/api/server.go` | HTTP 服务创建时调用 `db.NewTunnel(cfg)`，随后创建 SR、TS、LS 适配器。 |
| `cmd/server/main.go` | CLI 命令族统一通过 `db.NewTunnel(cfg)` 建立数据库连接。 |
| `python/db/connector.py` | Python 侧 SSH 隧道与 `pymysql` 连接封装。 |
| `python/fetch_2026_data.py` | 历史数据拉取脚本，明确 SR/TS 使用 `3308`，LS 使用 `3309`。 |
| `.cursor/rules/python_data.md` | 数据重新生成流程中固化双端口 SSH 隧道命令。 |
| `.cursor/rules/python_db.md` | Python 数据库模块规范，强调 `python/db/` 作为统一访问入口。 |
| `python/db/db_queries.md` | 查询参考手册，列出 RDS、SSH、LSports 表结构与常用查询。 |
