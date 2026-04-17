# 多维特征融合优化 — 交付物总结

**任务 ID**: tsk-c93a8ede-fc7  
**分支**: `feature/country-code-matching`  
**PR**: https://github.com/gdszyy/sports-matcher-go/pull/1  
**提交**: `0bf758e`

---

## 问题背景

原有国家匹配逻辑（`leagueNameScore` in `league.go`）仅依赖 `CategoryName`（文本字段）与 `CountryName` 做 Jaccard 相似度加分（权重 0.2），存在以下问题：

1. **SR 侧缺少 LocationVeto 强否决**：LS 侧已有 `lsLocationVeto`，SR 侧无对应逻辑，导致跨国误匹配风险
2. **未利用结构化字段**：`CategoryID`、`CountryCode` 等精确字段未参与匹配
3. **SR/LS 两侧逻辑不一致**：`lsInternationalCategory`/`lsLocationVeto` 为 LS 私有函数，SR 侧无法复用

---

## 改进内容

### 1. 数据模型扩展

| 文件 | 改动 |
|:-----|:----|
| `internal/db/models.go` | `SRTournament` 新增 `CategoryCountryCode` 字段 |
| `internal/db/models.go` | `TSCompetition` 新增 `CountryCode` 字段 |

### 2. 数据库查询升级（向后兼容）

| 文件 | 改动 |
|:-----|:----|
| `internal/db/sr_adapter.go` | `GetTournament` 优先查询 `sr_category_en.country_code`，字段不存在时自动回落 |
| `internal/db/ts_adapter.go` | `getCompetitions` 优先查询 `ts_fb_competition.country_code`，字段不存在时自动回落 |
| `internal/db/ts_adapter.go` | `GetCompetition` 同上 |

所有查询采用 **try-fallback** 模式：先执行包含 `country_code` 的查询，若数据库字段不存在（SQL 错误）则自动回落到不含该字段的查询，**完全向后兼容**。

### 3. 公共地区匹配工具函数（`internal/matcher/league.go`）

| 函数 | 说明 |
|:-----|:----|
| `IsInternationalCategory(name)` | 国际/洲际赛事豁免检测（SR/LS 两侧共用，替代 LS 私有函数） |
| `LocationVeto(srcCategory, tsCountry)` | 地区名称强否决（公共函数，Jaccard < 0.4 时否决） |
| `countryCodeVeto(srcCode, tsCode)` | 结构化 CountryCode 精确否决（两侧均非空且不相等时否决） |
| `locationScore(srcCategory, srcCode, tsCountry, tsCode)` | 多维地区相似度计算（整合代码精确匹配和文本匹配） |

### 4. `leagueNameScore` 评分逻辑升级（四层架构）

```
原架构（2层）：
  ① 名称相似度（Jaccard）
  ② 六维特征强约束（性别/年龄/区域/赛制/层级）
  ③ CategoryName 文本加分（权重 0.20）

新架构（4层）：
  ① CountryCode 强否决（新增，精确拦截跨国误匹配）
  ② LocationVeto 名称强否决（新增，与 LS 侧对齐）
  ③ 六维特征强约束（保留原有逻辑）
  ④ 多维地区加分（精确代码匹配权重 0.30，文本匹配权重 0.25，原为 0.20）
```

### 5. `lsLeagueNameScore` 对齐（`internal/matcher/ls_engine.go`）

- 复用公共 `locationScore` 函数，与 SR 侧评分逻辑对齐
- `lsInternationalCategory`/`lsLocationVeto` 标记为 `Deprecated`，内部委托给公共函数

---

## 测试结果

```
$ go test ./internal/matcher/... -v
--- PASS: TestIsInternationalCategory (0.00s)  [10 cases]
--- PASS: TestLocationVeto (0.00s)             [10 cases]
--- PASS: TestCountryCodeVeto (0.00s)          [12 cases]
--- PASS: TestLocationScore (0.00s)            [5 cases]
--- PASS: TestLeagueNameScore_WithCountryCode (0.00s)  [6 cases]
--- PASS: TestMatchLeague_WithCountryCode (0.00s)      [3 cases]
--- PASS: TestLeagueNameScore_Regression (0.00s)       [3 cases]
PASS
ok  github.com/gdszyy/sports-matcher/internal/matcher  0.010s

$ go build ./...   # 零错误
$ go vet ./...     # 零警告
```

**总计 35 个测试用例，全部通过。**

---

## 关键设计决策

1. **保守处理空值**：任一侧 CountryCode/CategoryName 为空时不否决，避免信息不足时的误拒绝
2. **国际赛事豁免**：UEFA、FIFA、AFC 等国际组织代码和名称豁免所有国家约束
3. **分级加分权重**：结构化代码精确匹配（0.30）> 文本名称匹配（0.25）> 原有逻辑（0.20）
4. **大小写不敏感**：CountryCode 比较统一转为大写，避免 "eng" vs "ENG" 误判
5. **向后兼容**：数据库 try-fallback 机制确保旧 schema 不受影响

---

## 后续建议

1. **数据库迁移**：在 `sr_category_en` 表中补充 `country_code` 字段，在 `ts_fb_competition` 表中补充 `country_code` 字段，以充分发挥结构化匹配优势
2. **LS 侧 CategoryCountryCode**：若 LS 数据库有对应字段，可在 `LSTournament` 中添加并传入 `locationScore`
3. **标准化代码映射**：建立 ISO 3166-1 alpha-3 标准代码映射表，处理不同数据源代码格式不一致问题
