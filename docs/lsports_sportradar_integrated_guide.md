# LSports / SportRadar 数据库接入与表结构一体化指引

> 一体化、自包含手册。所有 SSH、数据库连接参数、表结构、字段语义、坑点和最短接入代码均在本文中，无需再回到原仓库逐文件查证。
>
> 范围：仅覆盖 **LSports（LS）** 与 **SportRadar（SR）** 两条数据链路；不展开 TS 侧算法细节。

---

## 0. 安全须知（务必先读）

本文为方便实施保留了仓库中**默认硬编码**的 SSH 跳板机地址、私钥路径、RDS 域名和数据库口令。这些值只是当前测试环境的默认值：

- 私钥本身**不在本文档中**，仅给出文件路径；请向运维索取或落到部署机指定位置。
- 数据库口令为测试环境口令；上线或对外分享时务必通过环境变量注入，并**轮换密码**。
- 任何将本文档外发/上传到第三方平台（包括在线渲染工具、IM、Wiki）前，请确认接收方权限。

---

## 1. 结论先行

Go 项目通过一个统一的 `Tunnel` 同时管理 **SR / TS / LS** 三个 MySQL 连接：

- **SR 数据库**：`xp-bet-test`
- **LS 数据库**：`test-xp-lsports`
- **TS 数据库**：`test-thesports-db`（本文不展开）

业务代码分别用 `NewSRAdapter(tunnel.SRDb)` 与 `NewLSAdapter(tunnel.LSDb)` 接入匹配引擎。

| 数据源 | 数据库 | Go 连接对象 | 适配器 | 主要用途 |
|---|---|---|---|---|
| SportRadar / SR | `xp-bet-test` | `Tunnel.SRDb` | `SRAdapter` | 拉 SR 联赛、比赛、球队、球员，用于 SR↔TS 匹配与 GT 构建 |
| LSports / LS | `test-xp-lsports` | `Tunnel.LSDb` | `LSAdapter`、`LSPlayerAdapter` | 拉 LS 联赛、比赛、球队；球员优先查库，失败走 Snapshot API |

---

## 2. 连接参数与密钥

### 2.1 默认参数表

下表来自项目内 `config.Default()` 的默认值，可直接复制到 `.env` 文件：

| 配置项 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| SSH 跳板机地址 | `SSH_HOST` | `54.69.237.139` | EC2/堡垒机公网 IP |
| SSH 用户名 | `SSH_USER` | `ubuntu` | 跳板机登录用户 |
| SSH 私钥路径 | `SSH_KEY_PATH` | `/home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519` | ED25519 私钥，本机绝对路径 |
| SSH 端口 | `SSH_PORT` | `22` | 跳板机 SSH 端口 |
| 数据库 Host | `DB_HOST` | `test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com` | Aurora MySQL 集群地址 |
| 数据库端口 | `DB_PORT` | `3306` | RDS MySQL 端口 |
| 数据库用户 | `DB_USER` | `root` | 测试环境管理账号 |
| 数据库口令 | `DB_PASSWORD` | `r74pqyYtgdjlYB41jmWA` | 测试环境口令，**勿外发** |
| 本地隧道端口 | `LOCAL_PORT` | `13400` | Go 内置隧道使用 |
| HTTP 服务端口 | `SERVER_PORT` | `8080` | 接口监听端口 |
| HTTP 服务地址 | `SERVER_HOST` | `0.0.0.0` | 监听地址 |
| 是否跑球员匹配 | `RUN_PLAYERS` | `true` | 关闭可只跑联赛/比赛/球队匹配 |

### 2.2 `.env` 模板

```dotenv
# SSH 隧道
SSH_HOST=54.69.237.139
SSH_USER=ubuntu
SSH_KEY_PATH=/home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519
SSH_PORT=22

# RDS 数据库（同一集群，三个 schema）
DB_HOST=test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com
DB_PORT=3306
DB_USER=root
DB_PASSWORD=r74pqyYtgdjlYB41jmWA
LOCAL_PORT=13400

# 服务
SERVER_PORT=8080
SERVER_HOST=0.0.0.0
RUN_PLAYERS=true
```

### 2.3 SSH 私钥获取与放置

私钥文件名 `id_ed25519`，建议步骤：

1. 向项目运维或仓库管理员申请该 ED25519 私钥；
2. 放置到 `SSH_KEY_PATH` 指向的位置（默认 `/home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519`），或自定义路径并覆盖 `SSH_KEY_PATH`；
3. 文件权限必须收紧：

```bash
mkdir -p /home/ubuntu/skills/xp-bet-db-connector/templates
chmod 700 /home/ubuntu/skills/xp-bet-db-connector/templates
chmod 600 /home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519
```

> 切勿把私钥提交到 Git。仓库 `.gitignore` 已默认忽略 `*.pem`、`id_*` 等常见命名。

### 2.4 两种连接模式

服务启动会**优先尝试 Go 内置 SSH 隧道**，失败后回退到**本地端口直连**模式：

| 模式 | SR 连接 | LS 连接 | 适用场景 |
|---|---|---|---|
| Go 内置 SSH 隧道 | 通过同一个 RDS 目标打开 `xp-bet-test` | 通过同一个 RDS 目标打开 `test-xp-lsports` | **默认路径**，服务启动自动建立 |
| 本地端口回退 | `127.0.0.1:3308/xp-bet-test` | `127.0.0.1:3309/test-xp-lsports` | 外部已手动建立 SSH 隧道时使用 |

### 2.5 手动建立 SSH 隧道（可选）

调试或排查问题时可手动开隧道，再让服务走"本地端口回退"模式：

```bash
# SR / TS 共用 3308，LS 走 3309（与 connectDirect 中的端口约定一致）
ssh -i /home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519 \
    -o StrictHostKeyChecking=no \
    -L 3308:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
    -L 3309:test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com:3306 \
    -N ubuntu@54.69.237.139
```

通过 `mysql` 客户端验证：

```bash
mysql -h 127.0.0.1 -P 3308 -u root -p'r74pqyYtgdjlYB41jmWA' -e 'SHOW DATABASES;'
mysql -h 127.0.0.1 -P 3309 -u root -p'r74pqyYtgdjlYB41jmWA' -e 'USE `test-xp-lsports`; SHOW TABLES;'
```

### 2.6 Go 端连接链路（无需再回仓库看）

服务端实际链路非常短：

```
config.Default()
   ↓
db.NewTunnel(cfg)
   ├── connectViaSSH()    ← 默认走这里：读 SSH_KEY_PATH → ssh.Dial → 注册 mysql custom dialer → sql.Open × 3
   └── connectDirect()    ← 回退：直接 sql.Open("mysql", "root:pwd@tcp(127.0.0.1:3308/3309)/db")
   ↓
tunnel.SRDb / tunnel.LSDb / tunnel.TSDb
   ↓
db.NewSRAdapter(tunnel.SRDb)
db.NewLSAdapter(tunnel.LSDb)
db.NewLSPlayerAdapter(tunnel.LSDb, db.DefaultLSPlayerConfig)
```

连接池默认参数（Tunnel 内统一设置，三个 schema 一致）：

- `SetMaxOpenConns(20)`
- `SetMaxIdleConns(5)`
- `SetConnMaxLifetime(10 * time.Minute)`
- `SetConnMaxIdleTime(5 * time.Minute)`
- DSN 统一带 `charset=utf8mb4&parseTime=true&loc=UTC`

---

## 3. SR 表结构与业务意义

SR 侧查询集中在 `internal/db/sr_adapter.go`，模型定义在 `internal/db/models.go`。下方为项目实际读取到的**有效表结构投影**。

| 表名 | 关键字段 | 业务意义 | 被谁消费 |
|---|---|---|---|
| `sr_tournament_en` | `tournament_id`、`name`、`sport_id`、`category_id` | SR 联赛主数据。`tournament_id` 使用 `sr:tournament:xxx` 格式；`sport_id` 用于映射 football、basketball 等运动 | `SRAdapter.GetTournament`、SR 联赛抽取脚本 |
| `sr_category_en` | `category_id`、`name` | SR 国家/地区或分类名称，用于联赛上下文 | 联赛详情 JOIN |
| `sr_sport_event` | `sport_event_id`、`tournament_id`、`scheduled`、`home_competitor_id`、`away_competitor_id`、`status_code` | SR 比赛主表。`scheduled` 是 ISO8601 字符串，需转 Unix 秒后参与时间窗口匹配 | `SRAdapter.GetEvents`、GT 构建 |
| `sr_competitor_en` | `competitor_id`、`name`、`player_ids` | SR 球队/参赛方英文名；`player_ids` 是 JSON 数组，保存该队球员 ID | `GetTeamNames`、`GetPlayersByTeam` |
| `sr_player_en` | `player_id`、`name`、`full_name`、`date_of_birth`、`nationality` | SR 球员详情。`full_name` 比 `name` 更完整，匹配时优先使用 | SR 球员反向验证 |
| `xp_tournament_hot` | `tournament_id`、`sport_id`、`name_en`、`category_id`、`is_deleted`、`sort_order` | XP 业务热门联赛表，用于挑选 SR 重点联赛样本 | `fetch_2026_data.py` |

SR 的业务模型：`SRTournament`（联赛）/ `SREvent`（比赛）/ `SRTeam`（球队）/ `SRPlayer`（球员）。

- 比赛匹配核心字段：**联赛 ID、开赛时间、主客队 ID/名称、状态码**
- 球员匹配核心字段：**player_id、姓名、生日、国籍、所属队**

### SR sport_id 映射

| SR sport_id | 项目内运动名 |
|---|---|
| `sr:sport:1` | `football` |
| `sr:sport:2` | `basketball` |
| `sr:sport:5` | `tennis` |
| `sr:sport:4` | `ice_hockey` |
| `sr:sport:3` | `baseball` |

---

## 4. LSports 表结构与业务意义

LS 侧查询集中在 `internal/db/ls_adapter.go` 与 `internal/db/ls_player_adapter.go`。LS 主表来自 `test-xp-lsports`，球员数据采用**本地表优先、Snapshot API 兜底**的双路策略。

| 表名 | 关键字段 | 业务意义 | 被谁消费 |
|---|---|---|---|
| `ls_tournament_en` | `tournament_id`、`name`、`sport_id`、`category_id` | LS 联赛主数据。`tournament_id` 是整数字符串，不是 SR 风格 URI | `LSAdapter.GetTournament`、`GetTournamentsBySport` |
| `ls_category_en` | `category_id`、`name`、`sport_id` | LS 地区/国家分类。JOIN 时必须同时带 `category_id` 与 `sport_id`，否则会放大统计结果 | 联赛详情与联赛统计 |
| `ls_sport_event` | `event_id`、`tournament_id`、`sport_id`、`category_id`、`scheduled`、`home_competitor_id`、`away_competitor_id`、`status` | LS 比赛主表。项目只取近两年比赛，按 `event_id` 去重，并把 `scheduled` 转 Unix 秒 | `LSAdapter.GetEvents` |
| `ls_competitor_en` | `competitor_id`、`name`、`sport_id` | LS 球队/参赛方英文名 | `LSAdapter.GetTeamNames` |
| `ls_player_en` | `player_id`、`name`、`competitor_id` | 可选球员表；存在时优先读取，不存在则回退 Snapshot API | `LSPlayerAdapter` |

LS 的业务模型对应 `LSTournament`、`LSEvent`、`LSTeam`、`LSPlayer`。其中 `LSPlayer` 只有 **ID、英文名、球队 ID**，没有生日和国籍，所以 LS 球员匹配只能靠名称相似度，不能像 SR 一样用生日/国籍辅助消歧。

### LS sport_id 映射

| LS sport_id | 项目内运动名 |
|---|---|
| `6046` | `football` |
| `48242` | `basketball` |
| `54094` | `tennis` |
| `131506` | `ice_hockey` |
| `154914` | `baseball` |

---

## 5. SR / LS 共用业务知识表

项目还有两张自动建表的知识表，按 `source_side` 区分 `sr`、`ls`。它们不是原始数据源表，而是匹配过程沉淀的跨任务知识缓存。

| 表名 | 关键字段 | 业务意义 |
|---|---|---|
| `team_alias_knowledge` | `source_side`、`src_team_id`、`ts_team_id`、`vote_count`、`confidence`、`sport`、`competition_id`、`last_seen` | 保存 SR/LS 球队 ID 到 TS 球队 ID 的历史验证映射；成功匹配累计投票，启动时加载高票映射进内存 |
| `league_alias_knowledge` | `source_side`、`canonical_name`、`alias_name`、`sport`、`vote_count`、`confidence`、`last_seen` | 保存联赛官方名、常用名、人工别名之间的映射；优先级为 `manual > sr/ls` |

---

## 6. 必须记住的坑

| 场景 | 正确做法 |
|---|---|
| SR 时间字段 | `sr_sport_event.scheduled` 是 ISO8601 字符串，先解析成 Unix 秒再算时间差 |
| LS 时间字段 | `ls_sport_event.scheduled` 格式不统一，代码需兼容 `T`、`Z`、`+00:00` 和日期格式 |
| LS 参赛方 ID | `ls_competitor_en.competitor_id` 可能是 bigint，而 `ls_sport_event.home_competitor_id` / `away_competitor_id` 是 varchar；JOIN 或构建 map 时统一转字符串 |
| LS category JOIN | `ls_category_en` 必须按 `category_id + sport_id` JOIN；只按 `category_id` 会造成统计放大 |
| LS event 去重 | `ls_sport_event` 可能存在重复 `event_id`，代码用 `seen[event_id]` 去重 |
| SR 联赛 ID | SR 使用 `sr:tournament:xxx`；不要和 LS 的纯数字 `tournament_id` 混用 |
| 球员匹配 | SR 有生日/国籍可辅助消歧；LS Snapshot 球员通常只有 ID 与 Name，置信度天然更弱 |
| 连接回退 | 若 Go 启动日志出现 "SSH 连接失败"，先检查私钥权限与跳板机网络；不要简单依赖回退到 3308/3309 |
| 字符集 | 所有 DSN 强制 `utf8mb4`；新建表时不要手动指定其它字符集，避免与 SR/LS 中文/特殊字符冲突 |
| 私钥/口令 | 不要把 `SSH_KEY_PATH` 指向的私钥或 `DB_PASSWORD` 写进任何脚本、commit 或日志输出中 |

---

## 7. 最短接入路径

只想在 Go 里复用 SR/LS 数据时，按下面链路走即可。**不要在 `matcher` 层直接写 SQL**，SQL 应留在 `internal/db/*_adapter.go`。

```go
package main

import (
    "log"

    "github.com/gdszyy/sports-matcher/internal/config"
    "github.com/gdszyy/sports-matcher/internal/db"
)

func main() {
    // 1. 读取配置（会自动注入 .env / 环境变量；否则用默认值）
    cfg := config.Default()

    // 2. 建立 Tunnel（默认走 SSH，失败回退本地端口）
    tunnel, err := db.NewTunnel(cfg)
    if err != nil {
        log.Fatalf("建立隧道失败: %v", err)
    }
    defer tunnel.Close()

    // 3. 注入到适配器
    srAdapter := db.NewSRAdapter(tunnel.SRDb)
    lsAdapter := db.NewLSAdapter(tunnel.LSDb)
    lsPlayerAdapter := db.NewLSPlayerAdapter(tunnel.LSDb, db.DefaultLSPlayerConfig)

    // 4. SR 示例：取联赛 + 比赛
    srTournament, _ := srAdapter.GetTournament("sr:tournament:17")
    srEvents, _ := srAdapter.GetEvents("sr:tournament:17")

    // 5. LS 示例：取联赛 + 比赛 + 球队下的球员
    lsTournament, _ := lsAdapter.GetTournament("67")
    lsEvents, _ := lsAdapter.GetEvents("67")
    lsPlayers, _ := lsPlayerAdapter.GetPlayersByTeam("12345")

    _ = srTournament
    _ = srEvents
    _ = lsTournament
    _ = lsEvents
    _ = lsPlayers
}
```

---

## 8. 排障速查

| 现象 | 排查方向 |
|---|---|
| `读取 SSH 私钥失败` | 私钥文件不存在或路径错误。校验 `SSH_KEY_PATH` 与文件权限 600 |
| `解析 SSH 私钥失败` | 私钥格式错（非 OpenSSH/ED25519，或被 PEM 头损坏）。重新向运维索取 |
| `SSH 连接失败 54.69.237.139:22` | 跳板机不可达：检查本机出网/安全组；或跳板机 SSH 关闭 |
| `XX 数据库 ping 失败` | 隧道已建立但 RDS 不通：检查 `DB_HOST` 域名解析、安全组放行 3306、口令是否已被轮换 |
| 启动后查询全空 | 走到了直连回退分支但本地 3308/3309 没开。手动建立 2.5 节的 SSH 隧道，或修复 SSH 私钥后重启 |
| LS 比赛数明显放大 | 命中 6 节"LS category JOIN"或"LS event 去重"两个坑，复核 SQL |
| SR 时间窗口匹配不上 | 没把 `scheduled` 从 ISO8601 转 Unix 秒，或时区误把 UTC 当本地 |

---

## 9. 变更与轮换流程

需要轮换数据库口令或更换跳板机私钥时：

1. 提前在运维侧准备好新口令/新公钥；
2. 通过环境变量先在测试机覆盖 `DB_PASSWORD` 或 `SSH_KEY_PATH`，跑一次 `go run ./cmd/server` 校验连通；
3. 通过后再在生产部署侧滚动替换；
4. 替换完成后，**回到本文档 2.1、2.2 节同步默认值并提交**（口令仅写到内部文档/密钥管理服务，不要 commit 进公共仓库）；
5. 通知所有持有旧私钥/口令的开发者销毁本机副本。
