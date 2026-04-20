---
description: "internal/api 模块的设计规范，包含 HTTP API 路由、请求处理和响应格式"
globs: ["internal/api/**/*"]
---

# internal/api 模块规范

## 1. 模块职责

`internal/api` 提供 HTTP API 服务层（基于 Gin 框架），将匹配引擎的能力对外暴露为 RESTful 接口。

| 文件 | 职责 |
|------|------|
| `server.go` | HTTP 服务器、路由注册、请求处理（265 行） |

## 2. 核心 API 接口

| 接口 | 方法 | 参数 | 说明 |
|------|------|------|------|
| `/health` | GET | — | 健康检查 |
| `/api/v1/match/league` | GET | `tournament_id`, `sport`, `tier`, `ts_competition_id`, `run_players` | 单联赛匹配 |
| `/api/v1/match/batch` | POST | JSON body（leagues 数组） | 批量联赛匹配 |

### 批量匹配请求格式

```json
{
  "leagues": [
    {
      "tournament_id": "sr:tournament:17",
      "sport": "football",
      "tier": "hot",
      "ts_competition_id": "jednm9whz0ryox8"
    }
  ],
  "include_players": false
}
```

## 3. 状态流转 / 业务规则

- 所有接口统一返回 JSON 格式
- 错误响应包含 `error` 字段和 HTTP 状态码
- 批量接口支持并发匹配，结果按输入顺序返回
- `run_players` / `include_players` 控制是否执行球员匹配（耗时较长）

## 4. 详细设计文档索引

- [README API 接口说明](../../README.md#api-接口)
