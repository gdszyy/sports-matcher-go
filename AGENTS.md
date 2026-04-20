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

## 5. 流程洞察索引 (Process Insights)

流程洞察是 Agent 在完成任务后沉淀的经验文档，记录非直观的隐蔽逻辑、跨模块耦合陷阱和关键操作流程。与静态模块规范不同，流程洞察随任务持续积累，并通过版本号管理演进。

*   **洞察注册表**：[`.cursor/rules/process_insights/index.md`](.cursor/rules/process_insights/index.md) - 所有活跃与废弃洞察的版本索引。

> **注意**：随着架构的演进，本索引应持续更新。负责重构的 Agent 需维护对应的规则文档和流程洞察。
