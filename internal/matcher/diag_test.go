package matcher

import (
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

func TestDiag_Similarity(t *testing.T) {
	// 诊断 Arsenal vs Juventus 相似度来源
	pairs := [][2]string{
		{"arsenal", "juventus"},
		{"barcelona", "bayern munich"},
	}
	for _, p := range pairs {
		a, b := p[0], p[1]
		jaccard := jaccardSimilarity(a, b)
		jw := jaroWinklerSimilarity(a, b)
		norm_a := NormalizeTeamName(a, false)
		norm_b := NormalizeTeamName(b, false)
		norm_jw := jaroWinklerSimilarity(norm_a, norm_b)
		final := teamNameSimilarity(a, b)
		t.Logf("%q vs %q: jaccard=%.3f jw=%.3f norm_jw=%.3f final=%.3f",
			a, b, jaccard, jw, norm_jw, final)
	}

	// 诊断 Liga 2 Peru vs Liga 1 Peru 层级否决
	f1 := ExtractLeagueFeatures("Liga 2 Peru")
	f2 := ExtractLeagueFeatures("Liga 1 Peru")
	t.Logf("Liga 2 Peru: tier=%d type=%q", f1.TierNumber, f1.CompetitionType)
	t.Logf("Liga 1 Peru: tier=%d type=%q", f2.TierNumber, f2.CompetitionType)

	veto := CheckLeagueVeto(f1, f2, "hi")
	t.Logf("Veto (hi): vetoed=%v reason=%v", veto.Vetoed, veto.Reason)
	veto2 := CheckLeagueVeto(f1, f2, "med")
	t.Logf("Veto (med): vetoed=%v reason=%v", veto2.Vetoed, veto2.Reason)
	veto3 := CheckLeagueVeto(f1, f2, "low")
	t.Logf("Veto (low): vetoed=%v reason=%v", veto3.Vetoed, veto3.Reason)

	sr := db.SRTournament{ID: "sr:test", Name: "Liga 2 Peru", CategoryName: "Peru", Sport: "football"}
	ts := db.TSCompetition{ID: "ts:test", Name: "Liga 1 Peru", CountryName: "Peru", Sport: "football"}
	score := leagueNameScore(&sr, &ts)
	t.Logf("leagueNameScore Liga 2 Peru vs Liga 1 Peru = %.3f", score)
}
