---
description: "Python 批量匹配脚本规范：2026 年 LSports→TheSports 匹配主脚本、Excel 导出、虚拟体育识别"
globs: ["python/*.py", "python/README.md"]
---

# Python 匹配脚本规范 (python)

## 1. 模块职责

`python/` 目录包含批量匹配脚本，负责执行 LSports → TheSports 的联赛/比赛匹配任务并导出 Excel 报表。

## 2. 核心文件

| 文件 | 职责 |
|------|------|
| `match_2026.py` | 2026 年足球+篮球匹配主脚本（v4），含联赛匹配+虚拟体育识别+比赛匹配+Excel 导出 |

## 3. 运行方式

```bash
cd /path/to/sports-matcher-go
python3 python/match_2026.py
# 输出：lsports_ts_match_2026.xlsx
```

## 4. 匹配算法说明

### 联赛匹配
- `KNOWN_LS_TS_MAP`：硬编码已验证映射，优先使用，匹配级别 = `KNOWN`
- 名称相似度：Jaccard 词集交并比 + SequenceMatcher，取最大值
- 地理过滤：`ls_category`（地区）与 TS `host_country` 相似度 < 0.4 时否决
- 阈值：`NAME_HI`（≥0.85）/ `NAME_MED`（≥0.70）/ `NAME_LOW`（≥0.55）

### 比赛匹配
- 性能优化：一次性预加载所有 TS 2026 年比赛，内存中按 `competition_id` 分组
- 时间窗口：使用 `bisect` 二分查找，多级窗口（L1=±5min, L2=±6h, L3=同日, L4=±3天）
- 匹配级别：`L1`（精确时间+队名）/ `L2`（近似时间+队名）/ `L3`（同日+队名）/ `L4`（宽松）

### 虚拟体育识别（v4 已实现，`is_virtual_sport()` 函数）

虚拟体育联赛**跳过 TS 匹配**，在统计 Sheet 中单独计数，联赛 Sheet 使用橙色标注（⚡前缀）。

| 规则 | 示例 | 识别原因标签 |
|------|------|------------|
| `E-` 前缀 | `E-Football \| Battle (E)` | `E-前缀` |
| `E \|` 前缀 | `E \| Football League` | `E\|前缀` |
| E 合写前缀 | `Ebasketball`, `Efootball` | `E合写前缀` |
| `(E)` 后缀 | `NBA 2K25 (E)` | `(E)标记` |
| eSports/e-sports | `NBA eSports Basketball` | `eSports关键词` |
| Cyber | `NBA 2K25. Cyber League` | `Cyber关键词` |
| NBA 2K 系列 | `NBA 2K26. Cyber League` | `NBA 2K系列` |
| Blitz | `NBA Blitz H2H GG League` | `Blitz关键词` |
| H2H GG 组合 | `NBA Blitz H2H GG League` | `H2H GG关键词` |
| 时间格式 | `8 Minutes`, `2x5 Minutes`, `4X5 Mins` | `时间格式标注` |

**排除规则**：`3X3 Microfutsal` 等含 `microfutsal`/`futsal` 的联赛不视为虚拟体育。

## 5. Excel 输出格式

- **Sheet 1 `联赛匹配统计`**：
  - 汇总统计区分真实/虚拟联赛数量
  - 联赛详情列表新增"是否虚拟"列和"虚拟识别原因"列
  - 虚拟体育行使用橙色（`#F4B942`）填充
- **真实体育 Sheet**：命名格式 `FB_{LS_ID}_{LS联赛名}` / `BB_{LS_ID}_{LS联赛名}`，蓝色标题
- **虚拟体育 Sheet**：命名格式 `⚡FB_{LS_ID}_{LS联赛名}` / `⚡BB_{LS_ID}_{LS联赛名}`，深橙色标题，浅橙色行
- 左列 LS 数据，右列 TS 匹配数据

## 6. 详细设计文档索引

- 匹配主脚本：[`python/match_2026.py`](../../python/match_2026.py)
- 数据库连接：[`python/db/connector.py`](../../python/db/connector.py)
- SQL 参考：[`python/db/db_queries.md`](../../python/db/db_queries.md)
