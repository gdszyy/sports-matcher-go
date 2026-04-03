// Package matcher — 球队映射推导和球员匹配
package matcher

import (
	"math"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// DeriveTeamMappings 从比赛匹配结果中投票推导球队映射
func DeriveTeamMappings(
	events []EventMatch,
	srTeamNames map[string]string,
	tsTeamNames map[string]string,
) []TeamMapping {
	// 投票：SR team_id → (TS team_id → 票数)
	homeVotes := make(map[string]map[string]int)
	awayVotes := make(map[string]map[string]int)

	for _, ev := range events {
		if !ev.Matched || ev.TSHomeID == "" {
			continue
		}
		if homeVotes[ev.SRHomeID] == nil {
			homeVotes[ev.SRHomeID] = make(map[string]int)
		}
		homeVotes[ev.SRHomeID][ev.TSHomeID]++

		if awayVotes[ev.SRAwayID] == nil {
			awayVotes[ev.SRAwayID] = make(map[string]int)
		}
		awayVotes[ev.SRAwayID][ev.TSAwayID]++
	}

	// 合并主客队投票
	allVotes := make(map[string]map[string]int)
	for srID, votes := range homeVotes {
		if allVotes[srID] == nil {
			allVotes[srID] = make(map[string]int)
		}
		for tsID, cnt := range votes {
			allVotes[srID][tsID] += cnt
		}
	}
	for srID, votes := range awayVotes {
		if allVotes[srID] == nil {
			allVotes[srID] = make(map[string]int)
		}
		for tsID, cnt := range votes {
			allVotes[srID][tsID] += cnt
		}
	}

	// 取每个 SR 球队得票最多的 TS 球队
	var mappings []TeamMapping
	for srID, votes := range allVotes {
		bestTSID := ""
		bestVotes := 0
		totalVotes := 0
		for tsID, cnt := range votes {
			totalVotes += cnt
			if cnt > bestVotes {
				bestVotes = cnt
				bestTSID = tsID
			}
		}
		if bestTSID == "" {
			continue
		}

		confidence := float64(bestVotes) / float64(totalVotes)
		nameSim := teamNameSimilarity(srTeamNames[srID], tsTeamNames[bestTSID])
		finalConf := math.Round((confidence*0.6+nameSim*0.4)*1000) / 1000

		mappings = append(mappings, TeamMapping{
			SRTeamID:   srID,
			SRTeamName: srTeamNames[srID],
			TSTeamID:   bestTSID,
			TSTeamName: tsTeamNames[bestTSID],
			MatchRule:  RuleTeamDerived,
			Confidence: finalConf,
			VoteCount:  bestVotes,
		})
	}

	return mappings
}

// MatchPlayersForTeam 对单个球队的球员进行匹配
func MatchPlayersForTeam(
	srPlayers []db.SRPlayer,
	tsPlayers []db.TSPlayer,
	srTeamID, tsTeamID string,
) []PlayerMatch {
	results := make([]PlayerMatch, 0, len(srPlayers))
	usedTSIDs := make(map[string]bool)

	for _, srP := range srPlayers {
		pm := PlayerMatch{
			SRPlayerID: srP.ID,
			SRName:     srP.Name,
			SRDOB:      srP.DateOfBirth,
			SRTeamID:   srTeamID,
			Matched:    false,
			MatchRule:  RulePlayerNoMatch,
		}

		bestScore := -1.0
		var bestTS *db.TSPlayer

		for i := range tsPlayers {
			tsP := &tsPlayers[i]
			if usedTSIDs[tsP.ID] {
				continue
			}

			nameSim := playerNameSimilarity(srP.Name, tsP.Name)
			if nameSim < 0.60 {
				continue
			}

			// 生日辅助消歧
			dobBonus := 0.0
			if srP.DateOfBirth != "" && tsP.Birthday != "" {
				if normalizeDOB(srP.DateOfBirth) == normalizeDOB(tsP.Birthday) {
					dobBonus = 0.15
				}
			}

			score := nameSim*0.85 + dobBonus
			if score > 1.0 {
				score = 1.0
			}

			if score > bestScore {
				bestScore = score
				bestTS = tsP
			}
		}

		if bestTS != nil && bestScore >= 0.70 {
			pm.TSPlayerID = bestTS.ID
			pm.TSName = bestTS.Name
			pm.TSDOB = bestTS.Birthday
			pm.TSTeamID = tsTeamID
			pm.Matched = true
			pm.Confidence = math.Round(bestScore*1000) / 1000
			usedTSIDs[bestTS.ID] = true

			switch {
			case bestScore >= 0.85:
				pm.MatchRule = RulePlayerNameHi
			case bestTS.Birthday != "" && normalizeDOB(bestTS.Birthday) == normalizeDOB(pm.SRDOB):
				pm.MatchRule = RulePlayerDOB
			default:
				pm.MatchRule = RulePlayerNameMed
			}
		}

		results = append(results, pm)
	}

	return results
}

// ApplyBottomUp 自底向上反向验证：用球员重叠率校正球队和比赛置信度
func ApplyBottomUp(
	teams []TeamMapping,
	players []PlayerMatch,
	events []EventMatch,
	srTeamPlayerCounts map[string]int,
) ([]TeamMapping, []EventMatch) {
	// 计算每个球队的球员重叠率
	teamOverlap := make(map[string]float64) // SR team_id → overlap rate
	teamPlayerCounts := make(map[string]int)
	teamMatchedCounts := make(map[string]int)

	for _, p := range players {
		teamPlayerCounts[p.SRTeamID]++
		if p.Matched {
			teamMatchedCounts[p.SRTeamID]++
		}
	}

	for _, tm := range teams {
		total := teamPlayerCounts[tm.SRTeamID]
		matched := teamMatchedCounts[tm.SRTeamID]
		if total > 0 {
			teamOverlap[tm.SRTeamID] = float64(matched) / float64(total)
		}
	}

	// 校正球队置信度
	for i := range teams {
		overlap := teamOverlap[teams[i].SRTeamID]
		teams[i].PlayerOverlapRate = math.Round(overlap*1000) / 1000

		bonus := 0.0
		switch {
		case overlap >= 0.60:
			bonus = 0.15
		case overlap >= 0.40:
			bonus = 0.08
		case overlap >= 0.20:
			bonus = 0.03
		}
		teams[i].BottomUpBonus = bonus
		newConf := teams[i].Confidence + bonus
		if newConf > 1.0 {
			newConf = 1.0
		}
		teams[i].Confidence = math.Round(newConf*1000) / 1000
	}

	// 构建球队置信度加成映射（SR team_id → bonus）
	teamBonus := make(map[string]float64)
	for _, tm := range teams {
		teamBonus[tm.SRTeamID] = tm.BottomUpBonus
	}

	// 校正比赛置信度（主客队加成之和，上限 1.0）
	for i := range events {
		if !events[i].Matched {
			continue
		}
		homeBonus := teamBonus[events[i].SRHomeID]
		awayBonus := teamBonus[events[i].SRAwayID]
		totalBonus := (homeBonus + awayBonus) / 2.0
		events[i].BottomUpBonus = math.Round(totalBonus*1000) / 1000
		newConf := events[i].Confidence + totalBonus
		if newConf > 1.0 {
			newConf = 1.0
		}
		events[i].Confidence = math.Round(newConf*1000) / 1000
	}

	return teams, events
}

// normalizeDOB 归一化生日格式（统一为 YYYY-MM-DD）
func normalizeDOB(dob string) string {
	if len(dob) >= 10 {
		return dob[:10]
	}
	return dob
}
