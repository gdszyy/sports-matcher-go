---
description: "internal/db 模块的设计规范，包含 SSH 隧道、数据库适配器、别名存储等核心逻辑"
globs: ["internal/db/**/*"]
---

# internal/db 模块规范

## 1. 模块职责

`internal/db` 负责所有数据库连接管理和数据查询，通过 SSH 隧道安全访问 AWS RDS，并为各数据源（SR、TS、LSports）提供统一的适配器接口。

| 文件 | 职责 |
|------|------|
| `tunnel.go` | SSH 隧道 + MySQL 连接池管理（180 行） |
| `models.go` | 数据模型（SR/TS 联赛、比赛、球队、球员） |
| `canonical_models.go` | 标准化数据模型 |
| `sr_adapter.go` | SportRadar 数据库查询适配器 |
| `ts_adapter.go` | TheSports 数据库查询适配器（266 行） |
| `ls_adapter.go` | LSports 数据库查询适配器（208 行） |
| `ls_player_adapter.go` | LSports 球员数据适配器（349 行） |
| `alias_store.go` | 球队别名存储（402 行） |
| `league_alias_store.go` | 联赛别名存储（336 行） |
| `normalizer.go` | 数据归一化处理（276 行） |
| `data_router.go` | 标准化数据路由（241 行） |

## 2. 核心数据模型

### 数据库连接配置

所有连接参数通过环境变量配置，严禁硬编码：

```
SSH_HOST    # SSH 跳板机地址
SSH_USER    # SSH 用户名
DB_HOST     # 数据库地址
DB_USER     # 数据库用户名
DB_PASSWORD # 数据库密码
```

### 核心数据结构（models.go）

- `SRLeague` / `TSLeague`：联赛数据模型
- `SREvent` / `TSEvent`：比赛数据模型
- `SRTeam` / `TSTeam`：球队数据模型
- `SRPlayer` / `TSPlayer`：球员数据模型

## 3. 状态流转 / 业务规则

### SSH 隧道生命周期

1. 服务启动时通过 `tunnel.go` 建立 SSH 隧道
2. 隧道断开时自动重连（内置重试机制）
3. 服务关闭时优雅断开隧道
4. **严禁**绕过隧道直连数据库外网地址

### 适配器查询规范

- 所有查询必须通过对应适配器（`sr_adapter.go`、`ts_adapter.go`、`ls_adapter.go`）执行
- 禁止在 `matcher` 层直接执行 SQL
- 查询结果统一转换为标准化模型后再传递给 `matcher` 层

### 别名存储

- **球队别名**（`alias_store.go`）：存储 `sr_team_id → ts_team_id` 的映射关系
- **联赛别名**（`league_alias_store.go`）：存储联赛名称的多语言别名，用于跨语言匹配

## 4. 详细设计文档索引

- [标准化数据路由设计](../../docs/standardized_data_router_design.md)
- [联赛别名索引（流程洞察）](../../.cursor/rules/process_insights/PI-002_league_alias_index.md)
