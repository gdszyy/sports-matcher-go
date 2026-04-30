# sports-matcher-go 全局协作规范 (AGENTS.md)

本文档是 `sports-matcher-go` 仓库中所有 AI Agent（包括临时 Agent 和常驻 Agent）必须遵守的协作规范和工作指南。它定义了全局的编辑策略、代码风格约定以及各模块的规范文档索引。

## 1. 快速导航与核心入口规范 (Quick Navigation)

为了确保各技能 (Skills) 与本项目之间的索引契约一致，所有 Agent 在介入本项目时，**必须**优先阅读以下入口文档：

*   **项目全局规范入口**：本文档 (`AGENTS.md`)，包含核心编辑策略、禁止行为与文档索引。
*   **架构与防坑指南**：[`.cursor/rules/global.md`](.cursor/rules/global.md)（必读，包含系统整体架构与设计原则）。
*   **流程洞察索引**：[`.cursor/rules/process_insights/index.md`](.cursor/rules/process_insights/index.md)（在涉及复杂跨模块流程时必读，包含历次任务沉淀的防坑经验）。
*   **自动函数索引**：[`.cursor/rules/auto_index/INDEX.md`](.cursor/rules/auto_index/INDEX.md)（在涉及大文件修改时必读，包含函数名、行号范围和 @section 内部节点映射）。

## 2. 全局 AI 编辑策略规范

本项目为了保证代码/文档库的高信噪比和结构一致性，所有 AI Agent 在修改文件时，必须遵循以下**智能编辑策略决策树**：

### 智能编辑策略决策树

1.  **微型修改（< 20 行）**
    *   **策略**：使用搜索替换（Search and Replace）或行内编辑。
    *   **适用场景**：修复错别字、调整局部排版、更新少量逻辑。
2.  **中型修改（20 - 200 行）**
    *   **策略**：局部重写或追加内容。
    *   **适用场景**：重构单一函数、添加新段落、调整局部结构。
3.  **大型修改（> 200 行）**
    *   **策略**：全文件重写（Full File Rewrite）。
    *   **适用场景**：仅在文件本身较小且需要进行彻底重构时使用。对于超长文件，必须先将其拆分为多个子文件。

### 强制要求

*   **活文档契约（代码-文档同步）**：任何对系统架构、API 设计或核心逻辑的实质性修改，**必须在同一个 Commit 中同步更新对应模块的规范文档**（位于 `.cursor/rules/` 目录下）。
*   **流程洞察沉淀**：当 Agent 在任务中发现非直观的隐蔽逻辑或跨模块耦合陷阱时，**必须在任务完成后**在 `.cursor/rules/process_insights/` 中创建或更新对应的洞察文档，并同步更新 `index.md`。
*   **历史文档归档**：过期的设计方案和旧版本的任务记录应移动到 `tasks/` 等归档目录，保持活跃文档区的整洁。

## 3. 上下文防污染策略

> **⚠️ 核心警告：跳过归档区**
> 本仓库包含以下历史归档目录，Agent 默认**严禁**主动读取这些目录下的文件，除非任务明确要求追溯历史：
*   `tasks/`
*   `output/`

## 4. 子模块规范文档索引

为了指导各个子模块的设计和演进，项目在 `.cursor/rules/` 目录下维护了详细的模块规范文档。以下是当前的文档索引：

*   **全局架构规范**：[`.cursor/rules/global.md`](.cursor/rules/global.md) - 包含系统全景架构、核心工作流串联及全局禁止行为清单。
*   **cmd 模块规范**：[`.cursor/rules/cmd.md`](.cursor/rules/cmd.md) - cmd 模块的专属设计规范与 SOP。
*   **internal_matcher 模块规范**：[`.cursor/rules/internal_matcher.md`](.cursor/rules/internal_matcher.md) - internal_matcher 模块的专属设计规范与 SOP。
*   **internal_db 模块规范**：[`.cursor/rules/internal_db.md`](.cursor/rules/internal_db.md) - internal_db 模块的专属设计规范与 SOP。
*   **internal_api 模块规范**：[`.cursor/rules/internal_api.md`](.cursor/rules/internal_api.md) - internal_api 模块的专属设计规范与 SOP。
*   **python 模块规范**：[`.cursor/rules/python.md`](.cursor/rules/python.md) - python 模块的专属设计规范与 SOP。
*   **python/data 数据集规范**：[`.cursor/rules/python_data.md`](.cursor/rules/python_data.md) - SR/LS/TS 2026 年算法测试数据集结构、字段说明与使用方式（**算法测试必读**）。
*   **docs 模块规范**：[`.cursor/rules/docs.md`](.cursor/rules/docs.md) - docs 模块的专属设计规范与 SOP。

## 5. 核心算法入口与测试脚本快速路由

> 本节记录最新算法实现和测试脚本的精确入口，Agent 在执行算法相关任务时应优先查阅。

### 5.1 最新匹配算法（UniversalEngine）

| 组件 | 路径 | 说明 |
|------|------|------|
| 通用引擎主体 | `internal/matcher/universal_engine.go` | UniversalEngine、SRSourceAdapter、TSSourceAdapter、RunLeague 主流程 |
| 事件级匹配核心 | `internal/matcher/event.go` | MatchEvents、高斯时间衰减、TeamAliasIndex、L1~L6 七级策略 |
| 联赛特征约束 | `internal/matcher/league_features.go` | 六维强约束一票否决（性别/年龄/赛制/层级/区域/国家） |
| 联赛别名知识图谱 | `internal/matcher/league_alias.go` | LeagueAliasIndex、AliasStore 持久化 |
| 已知映射验证 | `internal/matcher/known_map_validator.go` | KnownLeagueMapValidator、RCR < 0.30 自动降级 |
| Fellegi-Sunter EM | `internal/matcher/fs_model.go` | 无监督参数估计 |
| EventDTW 兜底 | `internal/matcher/event_dtw.go` | 动态时间规整兜底匹配 |
| 已知联赛映射表 | `internal/matcher/league.go` | KnownLeagueMap（SR tournament_id → TS competition_id） |
| 命令行入口 | `cmd/server/main.go` | `match2`（单联赛）、`batch2`（批量 SR 2026）子命令 |
| HTTP API 入口 | `internal/api/server.go` | `/api/v2/match/league`、`/api/v2/match/batch` |

### 5.2 SR 2026 算法测试脚本

| 脚本 | 路径 | 说明 |
|------|------|------|
| SR↔TS 2026 测试（最新） | `python/test_sr_2026.py` | 复现 UniversalEngine 完整逻辑，支持 GT 重建 TS 候选集，覆盖 14 个 GT 联赛 |
| LS↔TS 2026 测试 | `python/match_2026.py` | LS 链路测试脚本（旧版，仅供参考） |

**SR 2026 测试最新结果**（2026-04-20，commit `afd4e5e`）：

| 维度 | 加权 Precision | 加权 Recall | 加权 F1 | 平均置信度 |
|------|--------------|------------|---------|----------|
| 全量（14 GT 联赛） | 0.8927 | 0.8681 | 0.8799 | 0.9737 |
| 热门联赛（7足球+NBA） | 0.851 | 0.815 | 0.832 | — |
| 常规联赛（6个有GT） | 0.973 | 0.970 | 0.972 | — |

详见流程洞察：[PI-004](`.cursor/rules/process_insights/PI-004_sr_2026_test_results.md`)

---

## 6. 流程洞察索引 (Process Insights)

流程洞察是 Agent 在完成任务后沉淀的经验文档，记录非直观的隐蔽逻辑、跨模块耦合陷阱和关键操作流程。与静态模块规范不同，流程洞察随任务持续积累，并通过版本号管理演进。

*   **洞察注册表**：[`.cursor/rules/process_insights/index.md`](.cursor/rules/process_insights/index.md) - 所有活跃与废弃洞察的版本索引。

> **注意**：随着架构的演进，本索引应持续更新。负责重构的 Agent 需维护对应的规则文档和流程洞察。


## Evidence-First P5 生产化入口

Evidence-First 已以**显式实验入口**接入，不替换旧 `match`、`match2`、`batch2`、`ls-match`、`ls-batch` 路径。端到端入口为 `UniversalEngine.RunLeagueEvidenceFirst`，CLI 使用 `match-evidence` / `batch-evidence`，HTTP 使用 `/api/v2/match/evidence` / `/api/v2/match/evidence/batch`。默认只读运行；只有显式开启 `--allow-write-back` 或服务端 `EVIDENCE_FIRST_ALLOW_WRITE_BACK=true` 后，且通过安全门槛，才允许写入 `TeamAliasStore`。Evidence-First 不会静默覆盖 `KnownLeagueMap` 强映射资产；KnownMap 低 RCR 仅记录 suspect 证据，人工 override 仍通过 `KnownLeagueMapValidator` 保留。
