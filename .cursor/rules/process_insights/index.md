# 流程洞察注册表 (Process Insights Index)

本文档是 `.cursor/rules/process_insights/` 目录下所有洞察文档的注册表。
每次新增、更新或废弃洞察时，**必须同步更新本文件**。

> **使用指南**：当你的任务涉及复杂的跨模块流程或历史上曾出现过 Bug 的逻辑区域时，
> 请先查阅本索引，找到相关洞察文档并读取，以避免重复踩坑。

---

## 活跃洞察 (Active Insights)

当前共有 **6** 条活跃洞察。

| ID | 标题 | 版本 | 关联模块 | 最后更新 | 文档链接 |
|----|------|------|---------|---------|---------|  
| PI-001 | 通用匹配算法设计与五级降级规则 | v1.0 | internal/matcher | 2026-04-20 | [PI-001](PI-001_universal_matching_algorithm_design.md) |
| PI-002 | 联赛别名索引与匹配防崩指南 | v1.0 | internal/matcher | 2026-04-20 | [PI-002](PI-002_league_alias_index.md) |
| PI-003 | SR/LS/TS 2026 年算法测试数据集使用指南 | v1.0 | python, python/data | 2026-04-20 | [PI-003](PI-003_2026_dataset_usage.md) |
| PI-004 | SR 2026 算法测试结果与最佳实践 | v1.0 | python, python/data, internal/matcher, cmd, internal/api | 2026-04-20 | [PI-004](PI-004_sr_2026_test_results.md) |
| PI-005 | LS→TS 联赛匹配高频算法误匹配修复指南 | v1.0 | internal/matcher, docs | 2026-04-21 | [PI-005](PI-005_ls_ts_league_match_algo_fix.md) |
| PI-006 | Evidence-First 比赛级匹配流程 | v1.0 | internal/matcher, docs | 2026-04-30 | [PI-006](PI-006_evidence_first_matching_flow.md) |

---

## 已废弃洞察 (Deprecated Insights)

*（暂无废弃洞察。）*

| ID | 标题 | 废弃版本 | 废弃原因 | 废弃日期 |
|----|------|---------|---------|---------|
| — | — | — | — | — |

---

## 新增洞察指南

当你需要创建新的流程洞察时，请遵循以下步骤：

1. 在本目录下创建新文件，命名格式：`PI-{编号:03d}_{slug}.md`（如 `PI-001_combat_damage_flow.md`）。
2. 按照以下模板填写内容：

```markdown
---
id: "PI-{编号}"
version: "v1.0"
last_updated: "2026-04-20"
author: "{Agent ID 或 Task ID}"
related_modules: ["{模块1}", "{模块2}"]
status: "active"
---

# PI-{编号}: {流程标题}

## 流程概述

（一段话描述该流程的核心目标）

## 核心防坑指南

### 坑 1: {坑位名称}

**现象**：（描述触发该问题的操作或场景）
**根因**：（解释为什么会发生）
**正确做法**：（给出明确的操作步骤或代码示例）
**关键位置**：`{文件路径}` → `{函数名}` 约第 {N} 行

## 关键耦合点

（描述该流程与其他模块的隐性依赖关系）

## 版本变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|---------|------|
| v1.0 | 2026-04-20 | 初始记录 | {Agent ID} |
```

3. 在本文件的"活跃洞察"表格中添加一行注册记录。
4. 将新增洞察的创建包含在当前任务的 Git Commit 中。

### 版本号管理规则

| 场景 | 操作 |
|------|------|
| 新增洞察 | 从 `v1.0` 开始 |
| 小幅修正（修正错误、补充细节） | 次版本号 +1（如 `v1.0 → v1.1`） |
| 重大更新（流程因重构发生根本变化） | 主版本号 +1（如 `v1.1 → v2.0`），并在 Changelog 中说明原因 |
| 废弃洞察 | 将 `status` 改为 `deprecated`，文档顶部添加废弃警告，并迁移到本文件的废弃区 |
