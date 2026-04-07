# sports-matcher-go

**跨库体育赛事数据匹配服务（Go 实现）**

将 `xp-bet-test`（SportRadar）的联赛/比赛/球队/球员数据，单向匹配到 `test-thesports-db`（TheSports）。

---

## 架构

```
cmd/server/main.go          CLI 入口（serve / match 命令）
internal/
  config/config.go          配置读取（环境变量 / 默认值）
  db/
    tunnel.go               SSH 隧道 + MySQL 连接池管理
    models.go               数据模型（SR/TS 联赛、比赛、球队、球员）
    sr_adapter.go           SR 数据库查询适配器
    ts_adapter.go           TS 数据库查询适配器
  matcher/
    result.go               匹配结果数据结构和规则常量
    name.go                 名称归一化（变音符/先后名/中间名/Unicode）
    league.go               联赛匹配（已知映射表 + 名称相似度 + 全局占用机制）
    event.go                比赛匹配（五级降级规则 L1/L2/L3/L4/L4b + TeamAliasIndex）
    team_player.go          球队映射推导 + 球员匹配（多格式名称）
    engine.go               主流程编排（两轮迭代 + 自底向上校验）
  api/server.go             HTTP API 服务（Gin）
```

---

## 五级匹配规则（v3）

| 级别 | 触发条件 | 时间窗口 | 名称阈值 | 最低置信度 | 特殊约束 |
|:----:|:--------|:-------:|:-------:|:---------:|:--------|
| **L1** | 精确时间 | ≤ 5 min | 0.40 | 0.50 | — |
| **L2** | 宽松时间 | ≤ 6 h | 0.65 | 0.60 | — |
| **L3** | 同一 UTC 日期 | ≤ 24 h（同日） | 0.75 | 0.70 | — |
| **L4** ✨ | 超宽时间 + 别名强匹配 | ≤ 72 h | 0.85 | 0.80 | `require_alias=true` |
| **L4b** | 球队 ID 精确对兜底 | 无限制 | — | 0.75 | 需第一轮推导的 `teamIDMap` |

### L4 说明

L4 专门处理 SR/TS 时间戳来源不同（UTC vs 本地时区、赛程延期）导致时差超过 24 小时的漏匹配。`require_alias=true` 要求至少一支队伍已在 `TeamAliasIndex` 中注册，防止跨联赛误匹配。

### TeamAliasIndex（联赛级队伍别名学习）

在同一联赛的比赛匹配过程中，每当 L1/L2/L3/L4 成功匹配一场比赛，自动将 `(sr_team_id → ts_team_id)` 写入别名索引。后续比赛若两队均在索引中命中，直接返回高置信度分数（0.92），不再依赖字面名称相似度。

**解决的问题**：`Chelsea FC`（SR）vs `Chelsea`（TS）等名称细微差异导致置信度偏低，进而漏匹配的问题。

### 两轮迭代流程

```
第一轮：MatchEvents(teamIDMap=nil)
        → L1 / L2 / L3 / L4（TeamAliasIndex 内部驱动）
        → DeriveTeamMappings → teamIDMap

第二轮：MatchEvents(teamIDMap=<第一轮推导>)
        → L4b 球队 ID 精确对兜底
        → DeriveTeamMappings（最终）
```

---

## API 接口

### 健康检查
```
GET /health
```

### 单联赛匹配
```
GET /api/v1/match/league?tournament_id=sr:tournament:17&sport=football&tier=hot&ts_competition_id=jednm9whz0ryox8&run_players=false
```

### 批量联赛匹配
```
POST /api/v1/match/batch
Content-Type: application/json

{
  "leagues": [
    {"tournament_id": "sr:tournament:17", "sport": "football", "tier": "hot", "ts_competition_id": "jednm9whz0ryox8"},
    {"tournament_id": "sr:tournament:35", "sport": "football", "tier": "hot", "ts_competition_id": "gy0or5jhg6qwzv3"},
    {"tournament_id": "sr:tournament:132", "sport": "basketball", "tier": "hot", "ts_competition_id": "49vjxm8xt4q6odg"}
  ],
  "include_players": false
}
```

---

## 快速启动

### 编译
```bash
export PATH=$PATH:/usr/local/go/bin
go build -o sports-matcher ./cmd/server/main.go
```

### 启动 HTTP 服务
```bash
./sports-matcher serve --port 8080
```

### CLI 单联赛匹配
```bash
./sports-matcher match "sr:tournament:17" \
  --sport football \
  --tier hot \
  --ts-id jednm9whz0ryox8 \
  --no-players
```

### Docker 部署
```bash
docker build -t sports-matcher .
docker run -d \
  -p 8080:8080 \
  -v /path/to/ssh/keys:/app/keys \
  -e SSH_HOST=your-ssh-host \
  -e SSH_USER=your-user \
  -e DB_HOST=your-db-host \
  -e DB_USER=your-db-user \
  -e DB_PASSWORD=your-password \
  sports-matcher
```

---

## 已知联赛映射（league.go）

| 联赛 | SR tournament_id | TS competition_id |
|:-----|:----------------|:-----------------|
| Premier League | sr:tournament:17 | jednm9whz0ryox8 |
| LaLiga | sr:tournament:8 | vl7oqdehlyr510j |
| Bundesliga | sr:tournament:35 | gy0or5jhg6qwzv3 |
| Serie A | sr:tournament:23 | 4zp5rzghp5q82w1 |
| Ligue 1 | sr:tournament:34 | yl5ergphnzr8k0o |
| UEFA Champions League | sr:tournament:7 | z8yomo4h7wq0j6l |
| UEFA Europa League | sr:tournament:679 | 56ypq3nh0xmd7oj |
| NBA | sr:tournament:132 | 49vjxm8xt4q6odg |

完整映射见 `internal/matcher/league.go`。

---

## 验证结果（v3）

| 联赛 | 比赛匹配率 | 球队匹配率 | L4 贡献 | L4b 贡献 |
|:-----|:--------:|:--------:|:------:|:-------:|
| Premier League | **99.5%** | 100% | +8 场 | +4 场 |
| Bundesliga | **99.7%** | 100% | +5 场 | +3 场 |
| Serie A | **99.3%** | 100% | +6 场 | +1 场 |
| LaLiga | **97.1%** | 100% | +9 场 | +4 场 |
| NBA | **100%** | 100% | +1 场 | 0 场 |
| Greek Super League | **84.0%** | 100% | +51 场 | +10 场 |

> **L4**（超宽时间+别名）在时区偏移严重的联赛（如 Greek Super League）贡献显著；**L4b**（球队ID兜底）负责处理剩余的名称极度不规则场次。

---

## 版本历史

| 版本 | commit | 说明 |
|:-----|:-------|:----|
| v1.0 | `aa7d1f4` | 初始版本，L1/L2/L3 + 原 L4（球队ID兜底） |
| **v3.0** | `e5e3f35` | 新增 TeamAliasIndex、L4（超宽时间+别名）；原 L4 重命名为 L4b |
