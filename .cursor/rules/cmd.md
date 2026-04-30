---
description: "cmd/server 模块的设计规范，包含 CLI 入口、serve 和 match 命令"
globs: ["cmd/**/*"]
---

# cmd/server 模块规范

## 1. 模块职责

`cmd/server` 是服务的 CLI 入口，提供 `serve`（启动 HTTP 服务）和 `match`（CLI 单联赛匹配）两个子命令。

| 文件 | 职责 |
|------|------|
| `main.go` | CLI 命令定义与入口（260 行） |

## 2. 核心命令

### serve 命令

```bash
./sports-matcher serve --port 8080
```

启动 HTTP API 服务，监听指定端口。

### match 命令

```bash
./sports-matcher match "sr:tournament:17" \
  --sport football \
  --tier hot \
  --ts-id jednm9whz0ryox8 \
  --no-players
```

CLI 模式执行单联赛匹配，结果输出到 stdout。

## 3. 状态流转 / 业务规则

- 服务启动时自动建立 SSH 隧道（通过 `internal/db/tunnel.go`）
- 配置优先级：命令行参数 > 环境变量 > 默认值
- `--no-players` 跳过球员匹配以提升速度

## 4. 编译与部署

```bash
# 编译
export PATH=$PATH:/usr/local/go/bin
go build -o sports-matcher ./cmd/server/main.go

# Docker 部署
docker build -t sports-matcher .
```


## Evidence-First 实验命令

P5 新增两个显式实验命令，旧 `match`、`match2`、`batch`、`batch2`、`ls-match`、`ls-batch` 保持兼容不变。

```bash
./sports-matcher match-evidence "sr:tournament:17"   --sport football   --tier hot   --ts-id jednm9whz0ryox8   --candidate-limit 4   --review-out /tmp/epl_evidence_review.json   --json
```

`batch-evidence` 支持 `--config`、`--candidate-limit`、`--review-dir` 和 `--json`。`--allow-write-back` 默认关闭；开启后仍需通过 Evidence-First 安全门槛才会写入 `TeamAliasStore`，且不会自动覆盖 KnownMap 强映射。
