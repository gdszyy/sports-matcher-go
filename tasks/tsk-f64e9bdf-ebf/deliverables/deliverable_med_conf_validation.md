# 任务交付物：med 置信度场景强制触发球员匹配反向验证

**任务 ID**: tsk-f64e9bdf-ebf  
**任务标题**: 强化自底向上校验机制  
**提交时间**: 2026-04-17  
**Git Commit**: `dd5cddd`

---

## 问题背景

在 `ls_engine.go` 的 `RunLeague` 流程中，Step 8 球员匹配的触发条件为 `e.RunPlayers == true`，这是一个全局开关，与联赛匹配的置信度无关。

当联赛匹配置信度处于 **med 区间（0.70-0.85）**，对应规则 `LEAGUE_NAME_MED` 时，联赛匹配的可靠性存疑，需要额外的验证手段来确认匹配结果是否正确。然而原有代码在 `RunPlayers=false` 时完全跳过球员匹配，导致 med 置信度联赛缺乏反向验证机制。

---

## 解决方案

### 核心设计原则

> **自底向上验证**：通过球员层（最细粒度）的匹配结果，反向验证联赛层（最粗粒度）的匹配可靠性。

当联赛匹配置信度处于 med 区间时，即使 `RunPlayers=false`，也强制触发球员匹配，并将球员匹配率作为信号回灌到联赛置信度。

### 置信度调整规则

| 球员匹配率 | 含义 | 联赛置信度调整 |
|:--------:|:----:|:------------:|
| ≥ 0.60 | 球员高度重叠，联赛匹配可信 | **+0.05** |
| 0.40 - 0.60 | 中等重叠，维持原判断 | **0.00** |
| 0.30 - 0.40 | 重叠偏低，轻微存疑 | **-0.03** |
| < 0.30 | 重叠很低，联赛匹配可能错误 | **-0.08** |

---

## 代码变更

### 1. `internal/matcher/result.go`

新增两个字段，标记 med 置信度强制触发状态：

```go
// LSMatchStats 新增
MedConfPlayerValidation bool `json:"med_conf_player_validation,omitempty"`

// LSMatchResult 新增
MedConfPlayerValidation bool `json:"med_conf_player_validation,omitempty"`
```

### 2. `internal/matcher/ls_engine.go`（Step 8 核心变更）

```go
// 触发条件：
//   1. 常规路径：e.RunPlayers == true（由调用方显式开启）
//   2. med 置信度强制路径：联赛匹配规则为 LEAGUE_NAME_MED（0.70-0.85）时强制触发
isMedConf := leagueMatch.MatchRule == RuleLeagueNameMed
shouldRunPlayers := (e.RunPlayers || isMedConf) && e.LSPlayer != nil && len(teamMappings) > 0

if shouldRunPlayers {
    if isMedConf && !e.RunPlayers {
        // 标记为 med 置信度强制触发
        result.MedConfPlayerValidation = true
    }
    // ... 球员匹配逻辑 ...
    
    // med 场景：将球员匹配率回灌到联赛置信度
    if isMedConf {
        plMatchRate := float64(matchedPl) / float64(totalPl)
        var leagueAdjust float64
        switch {
        case plMatchRate >= 0.60: leagueAdjust = +0.05
        case plMatchRate >= 0.40: leagueAdjust = 0.0
        case plMatchRate >= 0.30: leagueAdjust = -0.03
        default:                  leagueAdjust = -0.08
        }
        result.League.Confidence = math.Round((result.League.Confidence + leagueAdjust)*1000) / 1000
    }
}
```

### 3. `internal/api/server.go`

```go
// 创建 LS 球员适配器（共享 LS 数据库连接）
lsPlayerAdapter := db.LSPlayerAdapterFromLSAdapter(lsAdapter, db.DefaultLSPlayerConfig)

// NewLSEngine → NewLSEngineWithPlayers（注入 LSPlayerAdapter）
lsEng := matcher.NewLSEngineWithPlayers(lsAdapter, tsAdapter, lsPlayerAdapter)
```

---

## 执行流程（修改后）

```
RunLeague(lsTournamentID, sport, tier, tsCompetitionID)
  │
  ├── Step 1: 加载 LS 联赛
  ├── Step 2: 联赛匹配 → leagueMatch
  │           ├── LEAGUE_KNOWN (conf=1.0)
  │           ├── LEAGUE_NAME_HI (conf≥0.85)
  │           ├── LEAGUE_NAME_MED (conf 0.70-0.85) ← 本次关注
  │           └── LEAGUE_NAME_LOW (conf 0.55-0.70)
  │
  ├── Step 3: 加载比赛数据
  ├── Step 4: 加载球队名称
  ├── Step 5: 比赛匹配第一轮（L1/L2/L3/L4）
  ├── Step 6: 比赛匹配第二轮（L4b）
  ├── Step 7b: 已知映射验证（仅 LEAGUE_KNOWN）
  │
  └── Step 8: 球员匹配 + 自底向上反向验证
              ├── 触发条件（新增 med 强制路径）:
              │   shouldRunPlayers = (RunPlayers || isMedConf) && LSPlayer != nil
              │
              ├── 球员匹配 → ApplyBottomUpLS（校正球队/比赛置信度）
              │
              └── [med 场景专属] 球员匹配率 → 联赛置信度调整
                  ├── plMatchRate ≥ 0.60 → +0.05
                  ├── plMatchRate ≥ 0.40 → 0.00
                  ├── plMatchRate ≥ 0.30 → -0.03
                  └── plMatchRate < 0.30 → -0.08
```

---

## 向后兼容性

- 当 `e.RunPlayers=true` 且联赛为 `LEAGUE_NAME_MED` 时：触发球员匹配（原有行为），但额外执行联赛置信度回灌（新增行为）
- 当 `e.RunPlayers=false` 且联赛为 `LEAGUE_NAME_MED` 时：强制触发球员匹配（新增行为）
- 当联赛为 `LEAGUE_KNOWN`/`LEAGUE_NAME_HI`/`LEAGUE_NAME_LOW` 时：行为不变
- `MedConfPlayerValidation=true` 仅在 `RunPlayers=false` 且 `isMedConf=true` 时设置，便于调用方区分触发原因

---

## 测试场景

| 场景 | RunPlayers | 联赛规则 | 期望行为 |
|:----:|:----------:|:-------:|:-------:|
| 常规球员匹配 | true | 任意 | 触发球员匹配，不设 MedConfPlayerValidation |
| med 强制触发 | false | LEAGUE_NAME_MED | 强制触发，设 MedConfPlayerValidation=true |
| 非 med 跳过 | false | LEAGUE_NAME_HI | 跳过球员匹配 |
| med + 球员适配器为空 | false | LEAGUE_NAME_MED | 跳过（LSPlayer==nil 保护） |
