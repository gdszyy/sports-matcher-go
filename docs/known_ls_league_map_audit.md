# KnownLSLeagueMap GT 错配审计报告 (v1.39)

> 2026-05-16 | 配套 PI-006 v1.38
> 脚本：`scripts/audit_known_ls_league_map.py`

## 总览

- KnownLSLeagueMap 总条目：64
- 🟢 算法验证 OK (GT 是算法 Top-1)：43
- 🔵 GT_PLACEHOLDER (注释含 closest/alt id)：12
- 🟡 GT_DRIFT (ts_id 不在生产真实池)：0
- 🔴 SUSPECT (GT 不是算法 Top-1)：9

## 🔴 SUSPECT — 建议 Ops review

算法在生产真实池找到的 Top-1 跟当前 GT ts_id 不同。可能：
- (a) GT ts_id 标错 → 应改为算法 Top-1
- (b) 算法仍有 bug → 保持 GT，记录给 v1.40+ 算法改进

### football:8203

- **LS 注释**: `National League (England) → English National League`
- **LS src**: name='National League', category='England', sport=football
- **当前 GT ts_id**: `z318q66hv8qo9jd` → 真实 TS name='English National League', country='England'
  - GT 在算法 Top-5 位置：第 2 位
- **算法 Top-1 建议**: `z8yomo4h0llq0j6` name='National Super League', country='', score=0.879
- **算法 Top-5**:
  - 0.879 | `z8yomo4h0llq0j6` 'National Super League'
  - 0.821 | `z318q66hv8qo9jd` 'English National League' ← GT
  - 0.814 | `jednm9whgw3ryox` 'Niger National League'
  - 0.800 | `e4wyrn4hd4lq86p` 'Fijian National League'
  - 0.789 | `gy0or5jh42nqwzv` 'Rwandan National League'

### football:3799

- **LS 注释**: `FNL (Russia) → Russian Premier League`
- **LS src**: name='FNL', category='Russia', sport=football
- **当前 GT ts_id**: `8y39mp1hwxmojxg` → 真实 TS name='Russian Premier League', country='Russia'
  - GT 在算法 Top-5 位置：第 2 位
- **算法 Top-1 建议**: `9k82rekh6lrepzj` name='Russian First League', country='Russia', score=0.995
- **算法 Top-5**:
  - 0.995 | `9k82rekh6lrepzj` 'Russian First League'
  - 0.885 | `8y39mp1hwxmojxg` 'Russian Premier League' ← GT
  - 0.865 | `vl7oqdeh48kr510` 'Russian Amateur Football League'
  - 0.865 | `j1l4rjnh1wdm7vx` 'Russian Futsal Super League'
  - 0.853 | `l965mkyh1v6r1ge` 'Russian National Student League'

### football:61243

- **LS 注释**: `HNL (Croatia) → Croatian First Football League`
- **LS src**: name='HNL', category='Croatia', sport=football
- **当前 GT ts_id**: `gx7lm7pho13m2wd` → 真实 TS name='Croatian First Football League', country='Croatia'
  - GT 在算法 Top-5 位置：第 2 位
- **算法 Top-1 建议**: `gx7lm7phjem2wdk` name='Croatian Football League', country='Croatia', score=0.995
- **算法 Top-5**:
  - 0.995 | `gx7lm7phjem2wdk` 'Croatian Football League'
  - 0.995 | `gx7lm7pho13m2wd` 'Croatian First Football League' ← GT
  - 0.953 | `l965mkyhy02r1ge` 'Croatian Third Football League'
  - 0.921 | `kjw2r09hwwyrz84` 'Croatian Second Football League'
  - 0.876 | `vl7oqdeh07xr510` 'Croatian Regional League'

### football:41558

- **LS 注释**: `Liga Profesional (Argentina) → Argentine Division 1`
- **LS src**: name='Liga Profesional', category='Argentina', sport=football
- **当前 GT ts_id**: `p3glrw7hevqdyjv` → 真实 TS name='Argentine Division 1', country='Argentina'
  - GT 在算法 Top-5 位置：不在 Top-5
- **算法 Top-1 建议**: `9vjxm8ghonr6odg` name='ARG Primera Nacional', country='Argentina', score=0.713
- **算法 Top-5**:
  - 0.713 | `9vjxm8ghonr6odg` 'ARG Primera Nacional'
  - 0.688 | `d23xmvkhjoqg8ny` 'Copa Argentina'
  - 0.687 | `p3glrw7hjo2qdyj` 'Liga Dominicana de Fútbol'
  - 0.683 | `56ypq3nhyymd7oj` 'Categoría Primera A'
  - 0.668 | `z8yomo4hkwzq0j6` 'Indian Goa Professional League'

### football:71

- **LS 注释**: `Primera Division Apertura (Argentina) → Argentine Division 1`
- **LS src**: name='Primera Division Apertura', category='Argentina', sport=football
- **当前 GT ts_id**: `p3glrw7hevqdyjv` → 真实 TS name='Argentine Division 1', country='Argentina'
  - GT 在算法 Top-5 位置：第 2 位
- **算法 Top-1 建议**: `vl7oqdehlyr510j` name='Spanish La Liga', country='Spain', score=0.728
- **算法 Top-5**:
  - 0.728 | `vl7oqdehlyr510j` 'Spanish La Liga'
  - 0.716 | `p3glrw7hevqdyjv` 'Argentine Division 1' ← GT
  - 0.698 | `9vjxm8ghonr6odg` 'ARG Primera Nacional'
  - 0.677 | `56ypq3nhyymd7oj` 'Categoría Primera A'
  - 0.668 | `yl5ergphx4r8k0o` 'Paraguayan Primera Division'

### football:2018

- **LS 注释**: `Premier League (Egypt) → Egyptian Premier League`
- **LS src**: name='Premier League', category='Egypt', sport=football
- **当前 GT ts_id**: `56ypq3nh01nmd7o` → 真实 TS name='Egyptian Premier League', country='Egypt'
  - GT 在算法 Top-5 位置：第 4 位
- **算法 Top-1 建议**: `kn54qllhx18qvy9` name='Barbados Premier League', country='', score=0.911
- **算法 Top-5**:
  - 0.911 | `kn54qllhx18qvy9` 'Barbados Premier League'
  - 0.877 | `gpxwrxlh9xdryk0` 'Bhutan Premier League'
  - 0.876 | `kjw2r09hje5rz84` 'Mongolia Premier League'
  - 0.871 | `56ypq3nh01nmd7o` 'Egyptian Premier League' ← GT
  - 0.851 | `d23xmvkh4g8qg8n` 'Congo Premier League'

### basketball:293

- **LS 注释**: `Serie A (Italy) → Lega Basket Serie A`
- **LS src**: name='Serie A', category='Italy', sport=basketball
- **当前 GT ts_id**: `x4zp5rzkt1r82w1` → 真实 TS name='Lega Basket Serie A', country='Italy'
  - GT 在算法 Top-5 位置：第 2 位
- **算法 Top-1 建议**: `jednm9ktkenryox` name='Italy Serie B', country='Italy', score=0.877
- **算法 Top-5**:
  - 0.877 | `jednm9ktkenryox` 'Italy Serie B'
  - 0.877 | `x4zp5rzkt1r82w1` 'Lega Basket Serie A' ← GT
  - 0.835 | `kn54ql7tdwervy9` 'Italian Regional league'
  - 0.830 | `kjw2r02tnelqz84` 'Italy Serie B Cup'
  - 0.825 | `9vjxm8xtxl6q6od` 'Serie B Baske'

### basketball:15340

- **LS 注释**: `Nationale 1 (France) → France Nationale 1`
- **LS src**: name='Nationale 1', category='France', sport=basketball
- **当前 GT ts_id**: `gx7lm73tp6gr2wd` → 真实 TS name='France Nationale 1', country='France'
  - GT 在算法 Top-5 位置：第 3 位
- **算法 Top-1 建议**: `9dn1m17teoomoep` name='France Nationale 1', country='France', score=0.807
- **算法 Top-5**:
  - 0.807 | `9dn1m17teoomoep` 'France Nationale 1'
  - 0.807 | `e4wyrn1tnoq86pv` 'France Nationale 1'
  - 0.807 | `gx7lm73tp6gr2wd` 'France Nationale 1' ← GT
  - 0.720 | `xkn54ql7t8rvy9d` 'National Basketball League'
  - 0.694 | `e4wyrn1t9knq86p` 'National Basketball League1 South'

### basketball:21272

- **LS 注释**: `LNB Elite (France) → Ligue Nationale de Basket Pro A`
- **LS src**: name='LNB Elite', category='France', sport=basketball
- **当前 GT ts_id**: `7p4jwq25t6q0veo` → 真实 TS name='Ligue Nationale de Basket Pro A', country='France'
  - GT 在算法 Top-5 位置：第 4 位
- **算法 Top-1 建议**: `gx7lm73t5k7r2wd` name='NBL1 Eastern', country='', score=0.658
- **算法 Top-5**:
  - 0.658 | `gx7lm73t5k7r2wd` 'NBL1 Eastern'
  - 0.606 | `kn54ql7tj60rvy9` 'France LNB Espoirs U21'
  - 0.606 | `v2y8m4ptj59ml07` 'France LNB Espoirs U21'
  - 0.594 | `7p4jwq25t6q0veo` 'Ligue Nationale de Basket Pro A' ← GT
  - 0.594 | `jednm9kt9xryox8` 'Ligue Nationale de Basket Pro B'

## 🟡 GT_DRIFT — ts_id 不在生产真实池

这些 KnownLSLeagueMap 注释的 ts_id 在生产 `ts_fb_competition`/`ts_bb_competition` 表里找不到，可能是过期或拼错。


## 🔵 GT_PLACEHOLDER — `(closest)` / `(alt id)` 占位 (v1.34 已识别)

共 12 条。详见 PI-006 v1.34。

- **basketball:132** GT `49vjxm8xt4q6odg` 注释='NBA (alt id)'
- **basketball:34184** GT `ngy0or5gteqwzv3` 注释='B.League - B1 (Japan) → Chinese Basketball Association (closest)'
- **basketball:25357** GT `v2y8m4ptx1ml074` 注释='Orlen Basket Liga (Poland) → VTB United League (closest)'
- **basketball:19164** GT `v2y8m4ptx1ml074` 注释='Liga Nacional de Básquet (Argentina) → VTB United League (closest)'
- **basketball:15301** GT `x4zp5rzkt1r82w1` 注释='NBB (Brazil) → Lega Basket Serie A (closest)'
- **basketball:1834** GT `x4zp5rzkt1r82w1` 注释='NBL (Australia) → Lega Basket Serie A (closest)'
- **basketball:2558** GT `x4zp5rzkt1r82w1` 注释='Super League (Israel) → Lega Basket Serie A (closest)'
- **basketball:36** GT `0l965mk8tom1ge4` 注释='Basketligan (Sweden) → Basketball Bundesliga (closest)'
- **basketball:841** GT `0l965mk8tom1ge4` 注释='Korisliiga (Finland) → Basketball Bundesliga (closest)'
- **basketball:12855** GT `0l965mk8tom1ge4` 注释='Basketligaen (Denmark) → Basketball Bundesliga (closest)'
- **basketball:7666** GT `49vjxm8xt4q6odg` 注释='BSN (Puerto Rico) → National Basketball Association (closest)'
- **basketball:4379** GT `49vjxm8xt4q6odg` 注释='NBL (New Zealand) → National Basketball Association (closest)'
