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

### Evidence-First 开关（match2 / batch2 / ls-match / ls-batch 均支持）

```bash
./sports-matcher match2  "sr:tournament:17" --use-evidence-first --evidence-first-topn 5
./sports-matcher batch2                     --use-evidence-first
./sports-matcher ls-match <id>              --use-evidence-first
./sports-matcher ls-match <id>              --use-evidence-first --no-known-map  # 纯算法 EF
./sports-ma