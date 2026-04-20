---
description: "docs 目录的文档索引，包含架构设计、算法设计和评估报告"
globs: ["docs/**/*"]
---

# docs 模块规范

## 1. 模块职责

`docs/` 目录存放系统的设计文档、算法说明和评估报告，是理解系统设计决策的重要参考。

## 2. 文档索引

| 文档 | 说明 |
|------|------|
| [architecture.png](../../docs/architecture.png) | 系统架构图 |
| [universal_matching_algorithm_design.md](../../docs/universal_matching_algorithm_design.md) | 通用匹配算法设计文档 |
| [league_match_evaluation_rule.md](../../docs/league_match_evaluation_rule.md) | 联赛匹配评估规则 |
| [ls_ts_matching_assessment.md](../../docs/ls_ts_matching_assessment.md) | LS/TS 匹配评估报告 |
| [optimization_test_report.md](../../docs/optimization_test_report.md) | 优化测试报告 |
| [standardized_data_router_design.md](../../docs/standardized_data_router_design.md) | 标准化数据路由设计 |
| [league_guard_keywords.json](../../docs/league_guard_keywords.json) | 联赛守卫关键词配置 |

## 3. 文档维护规范

- 架构或算法发生重大变更时，必须同步更新对应设计文档
- 新增设计文档时，必须在本文件的索引表中注册
- 评估报告为历史记录，不得修改，如需更新应新建版本文件
