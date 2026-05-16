// Package matcher — Evidence-First P2 球队级先验填充
//
// 本文件实现 Evidence-First 流水线的 P2 阶段：
//
//	P1（candidate_pool.go）：把多 competition TS 候选 + 联赛先验 + 强约束
//	                          转成 []EvidenceEventCandidate（球队级先验留 0）。
//	P2（本文件）          ：对每条 EvidenceEventCandidate 的 TS home/away 队，
//	                          扫描全部 SR 球队名，取最佳相似度作为
//	                          HomeTeamCandidateScore / AwayTeamCandidateScore。
//	P3（evidence_event_matcher.go）：scoreEdge 通过 normalizePrior 把联赛先验、
//	                          home/away 队先验做平均，按 candidatePriorWeight
//	                          注入最终边得分。
//
// 设计原则：
//   - 纯函数：原地修改候选切片的 prior 字段，其他字段保持不变；
//   - 别名感知：用 *TeamAliasIndex.NameSimWithAlias，已注册别名命中走 aliasApplyScore；
//   - O(N_cand × N_sr_teams)：实际典型规模 100 × 50 = 5K 次相似度比较，可接受；
//   - 防御性：空候选 / 空 SR 球队名 / nil aliasIdx 都不应 panic。
//
// 相关流程洞察：
//   - PI-006 Evidence-First 比赛级匹配流程（v1.2）

package matcher

// EnrichTeamPriors 为每条 EvidenceEventCandidate 填充
// HomeTeamCandidateScore / AwayTeamCandidateScore。
//
// 计算方式：对 TS event 的 home/away 队各自，遍历 srTeamNames 找出最佳相似度
// （使用 aliasIdx.NameSimWithAlias，未注册别名时退化为 teamNameSimilarity）。
//
// 调用方约定：
//   - 必须先调用 P1（CandidatePoolBuilder.Build）拿到 candidates 和 unioned tsTeamNames；
//   - srTeamNames 由调用方从源侧加载（map[srTeamID]srTeamName）；
//   - aliasIdx 可为 nil（内部会构造一个空索引，等价于纯 Jaccard）；
//   - 该函数原地修改 candidates 元素的两个 prior 字段，其他字段保持不变；
//   - 返回同一切片（不复制），方便链式调用。
//
// 行为保证：
//   - 当 srTeamNames 为空 → 所有 priors 保持入参原值（通常是 0）；
//   - 当某 TS 队的 ID 与 name 都为空 → 跳过该队的扫描，prior 保持 0；
//   - 不依赖 strong constraint，veto 候选也会被填先验（filtering 是 P3 职责）。
func EnrichTeamPriors(
	candidates []EvidenceEventCandidate,
	tsTeamNames map[string]string,
	srTeamNames map[string]string,
	aliasIdx *TeamAliasIndex,
) []EvidenceEventCandidate {
	if len(candidates) == 0 {
		return candidates
	}
	if aliasIdx == nil {
		aliasIdx = newTeamAliasIndex()
	}
	for i := range candidates {
		ev := candidates[i].Event
		tsHomeName := firstNonEmpty(tsTeamNames[ev.HomeID], ev.HomeName)
		tsAwayName := firstNonEmpty(tsTeamNames[ev.AwayID], ev.AwayName)
		candidates[i].HomeTeamCandidateScore = bestSimAgainstSRTeams(
			ev.HomeID, tsHomeName, srTeamNames, aliasIdx,
		)
		candidates[i].AwayTeamCandidateScore = bestSimAgainstSRTeams(
			ev.AwayID, tsAwayName, srTeamNames, aliasIdx,
		)
	}
	return candidates
}

// bestSimAgainstSRTeams 把 (tsTeamID, tsTeamName) 与全部 SR 球队比对，返回最高相似度。
// 当 srTeamNames 为空、或 (tsTeamID, tsTeamName) 都为空字符串时，返回 0。
func bestSimAgainstSRTeams(
	tsTeamID, tsTeamName string,
	srTeamNames map[string]string,
	aliasIdx *TeamAliasIndex,
) float64 {
	if tsTeamID == "" && tsTeamName == "" {
		return 0
	}
	if len(srTeamNames) == 0 {
		return 0
	}
	best := 0.0
	for srTeamID, srTeamName := range srTeamNames {
		if srTeamID == "" && srTeamName == "" {
			continue
		}
		sim := aliasIdx.NameSimWithAlias(srTeamID, srTeamName, tsTeamID, tsTeamName)
		if sim > best {
			best = sim
		}
	}
	return best
}
