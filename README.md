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
    league.go               联赛匹配（已知映射表 + 名称相似度）
    event.go                比赛匹配（四级降级规则 L1/L2/L3/L4）
    team_player.go          球队映射推导 + 球员匹配（多格式名称）
    engine.go               主流程编排（两轮迭代 + 自底向上校验）
  api/server.go             HTTP API 服务（Gin）
```

---

## 四级匹配规则

| 级别 | 触发条件 | 时间窗口 | 名称阈值 | 置信度 |
|:----:|:--------|:-------:|:-------:|:------:|
| **L1** | 时差 ≤ 5 分钟 | 300s | 0.40 | ≥ 0.50 |
| **L2** | 时差 ≤ 6 小时 | 21600s | 0.65 | ≥ 0.60 |
| **L3** | 同一 UTC 日期 | 同日 | 0.75 | ≥ 0.70 |
| **L4** | 球队 ID 对精确匹配 | 无限制 | — | 0.75 |

**两轮迭代**：第一轮（L1/L2/L3）建立球队映射，第二轮（L4）用已知球队 ID 对兜底未匹配比赛。

---

## API 接口

### 健康检查
```
GET /health
```

### 单联赛匹配
```
POST /api/v1/match/league
{
  "tournament_id": "sr:tournament:17",
  "sport": "football",
  "tier": "hot",
  "ts_competition_id": "jednm9whz0ryox8",
  "include_players": false
}
```

### 批量联赛匹配
```
POST /api/v1/match/batch
{
  "leagues": [
    {"tournament_id": "sr:tournament:17", "sport": "football", "tier": "hot", "ts_competition_id": "jednm9whz0ryox8"},
    {"tournament_id": "sr:tournament:35", "sport": "football", "tier": "hot", "ts_competition_id": "gy0or5jhg6qwzv3"},
    {"tournament_id": "sr:tournament:132", "sport": "basketball", "tier": "hot", "ts_competition_id": "49vjxm8xt4q6odg"}
  ],
  "include_players": false
}
```

### 联赛列表
```
GET /api/v1/leagues?sport=football
GET /api/v1/leagues?sport=basketball
```

---

## 快速启动

### 编译
```bash
export PATH=$PATH:/usr/local/go/bin
go build -o sports-matcher cmd/server/main.go
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
| EuroLeague | sr:tournament:23 | jednm9ktd5ryox8 |

完整映射见 `internal/matcher/league.go`。

---

## 验证结果

| 联赛 | 比赛匹配率 | 球队匹配率 | L4 贡献 |
|:-----|:--------:|:--------:|:------:|
| Premier League | **99.1%** | 100% | +10 场 |
| Bundesliga | **99.5%** | 100% | +7 场 |
| Serie A | **99.0%** | 100% | +1 场 |
| LaLiga | **99.0%** | 100% | +13 场 |
| NBA | **97.8%** | 100% | +5 场 |
