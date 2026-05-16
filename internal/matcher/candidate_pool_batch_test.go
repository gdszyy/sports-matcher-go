package matcher

import (
	"errors"
	"testing"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// batchStubLoader 实现 BatchTSEventLoader（v1.9 batch 路径）。
type batchStubLoader struct {
	stubTSLoader                              // 内嵌单 comp 版本（fallback）
	batchEvents     map[string][]db.TSEvent   // compID -> events
	batchTeamNames  map[string]map[string]string
	batchEventsErr  error
	batchTeamsErr   error
	batchCallCount  int // 验证 batch 路径确实被调用
}

func (b *batchStubLoader) GetEventsBatch(competitionIDs []string, sport string) (map[string][]db.TSEvent, error) {
	b.batchCallCount++
	if b.batchEventsErr != nil {
		return nil, b.batchEventsErr
	}
	out := make(map[string][]db.TSEvent, len(competitionIDs))
	for _, id := range competitionIDs {
		if evs, ok := b.batchEvents[id]; ok {
			out[id] = evs
		}
	}
	return out, nil
}

func (b *batchStubLoader) GetTeamNamesBatch(competitionIDs []string, sport string) (map[string]map[string]string, error) {
	if b.batchTeamsErr != nil {
		return nil, b.batchTeamsErr
	}
	out := make(map[string]map[string]string, len(competitionIDs))
	for _, id := range competitionIDs {
		if tn, ok := b.batchTeamNames[id]; ok {
			out[id] = tn
		}
	}
	return out, nil
}

// 编译期断言：batchStubLoader 必须满足 BatchTSEventLoader 接口
var _ BatchTSEventLoader = (*batchStubLoader)(nil)

func TestCandidatePool_UsesBatchLoaderWhenAvailable(t *testing.T) {
	loader := &batchStubLoader{
		batchEvents: map[string][]db.TSEvent{
			"c1": {cpMakeTSEvent("m1", "TA", "Alpha", "TB", "Beta", 1000)},
			"c2": {cpMakeTSEvent("m2", "TC", "Gamma", "TD", "Delta", 2000)},
		},
		batchTeamNames: map[string]map[string]string{
			"c1": {"TA": "Alpha", "TB": "Beta"},
			"c2": {"TC": "Gamma", "TD": "Delta"},
		},
	}
	b := NewCandidatePoolBuilder(loader)
	res, err := b.Build([]TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "c1"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
		{Competition: db.TSCompetition{ID: "c2"}, LeaguePriorScore: 0.7, StrongConstraintOK: true},
	}, "football")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loader.batchCallCount != 1 {
		t.Errorf("expected GetEventsBatch called once, got %d times", loader.batchCallCount)
	}
	if len(res.Candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(res.Candidates))
	}
	if len(res.TSTeamNames) != 4 {
		t.Errorf("expected 4 unioned TSTeamNames, got %d", len(res.TSTeamNames))
	}
}

func TestCandidatePool_BatchLoaderError_PropagatesAndRollsBack(t *testing.T) {
	wantErr := errors.New("batch SQL exploded")
	loader := &batchStubLoader{batchEventsErr: wantErr}
	b := NewCandidatePoolBuilder(loader)
	_, err := b.Build([]TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "c1"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
	}, "football")
	if err == nil {
		t.Fatal("expected error from batch loader")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped wantErr, got %v", err)
	}
}

func TestCandidatePool_FallsBackToSingleCompWhenLoaderDoesNotImplementBatch(t *testing.T) {
	// stubTSLoader 不实现 BatchTSEventLoader（只有 GetEvents / GetTeamNames）
	loader := &stubTSLoader{
		events: map[string][]db.TSEvent{
			"c1": {cpMakeTSEvent("m1", "TA", "Alpha", "TB", "Beta", 1000)},
		},
		teamNames: map[string]map[string]string{
			"c1": {"TA": "Alpha", "TB": "Beta"},
		},
	}
	b := NewCandidatePoolBuilder(loader)
	res, err := b.Build([]TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "c1"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
	}, "football")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Errorf("expected 1 candidate from fallback path, got %d", len(res.Candidates))
	}
}
