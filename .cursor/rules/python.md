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
| `match_2026.py` | 2026 年足球+篮球匹配主脚本，包含联赛匹配+比赛匹配+Excel 导出 |

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
- 时间窗口：使用 `bisect` 二分查找 ±24h 内的候选比赛
- 匹配级别：`L1`（主客队名都匹配）/ `L2`（主队匹配）/ `L3`（序列匹配）

### 虚拟体育识别（待完善）

以下联赛命名特征高度关联虚拟/电竞赛事，匹配时应单独标注：
- 联赛名以 `E-`、`E |` 开头（如 `E-Football | Battle (E)`）
- 联赛名包含 `Ebasketball`、`Efootball`（合写）
- 联赛名包含 `(E)` 后缀
- 联赛名包含 `eSports`、`Cyber`、`2K25`、`2K26`、`H2H GG`、`Blitz`
- 联赛名包含时间标注如 `8 Minutes`、`2x5 Minutes`、`4X5 Mins`

## 5. Excel 输出格式

- **Sheet 1 `联赛匹配统计`**：汇总统计 + 所有联赛匹配详情
- **后续 Sheet**：每个联赛一个 Sheet，命名格式 `FB_{LS_ID}_{LS联赛名}` / `BB_{LS_ID}_{LS联赛名}`
- 左列 LS 数据，右列 TS 匹配数据
- 已匹配行绿色标注，未匹配行灰色标注

## 6. 详细设计文档索引

- 匹配主脚本：[`python/match_2026.py`](../../python/match_2026.py)
- 数据库连接：[`python/db/connector.py`](../../python/db/connector.py)
- SQL 参考：[`python/db/db_queries.md`](../../python/db/db_queries.md)
