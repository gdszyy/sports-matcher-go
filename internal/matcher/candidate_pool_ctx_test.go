package matcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gdszyy/sports-matcher/internal/db"
)

// ctxStubLoader 实现 ContextBatchTSEventLoader（v1.14）。
type ctxStubLoader struct {
	stubTSLoader
	batchStubLoader // also embed batch
	// blockDur 模拟慢 SQL，会等到 ctx.Done() 或 blockDur 之后返回。
	blockDur time.Duration
	calls    int
}

func (c *ctxStubLoader) GetEventsBatchCtx(ctx context.Context, competitionIDs []string, sport string) (map[string][]db.TSEvent, error) {
	c.calls++
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(c.blockDur):
		return c.batchStubLoader.GetEventsBatch(competitionIDs, sport)
	}
}

func (c *ctxStubLoader) GetTeamNamesBatchCtx(ctx context.Context, competitionIDs []string, sport string) (map[string]map[string]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(c.blockDur):
		return c.batchStubLoader.GetTeamNamesBatch(competitionIDs, sport)
	}
}

// 编译期断言：实现 ContextBatchTSEventLoader 接口
var _ ContextBatchTSEventLoader = (*ctxStubLoader)(nil)

func TestCandidatePool_BuildCtx_RespectsCancellation(t *testing.T) {
	loader := &ctxStubLoader{
		batchStubLoader: batchStubLoader{
			batchEvents:    map[string][]db.TSEvent{"c1": {cpMakeTSEvent("m1", "TA", "Alpha", "TB", "Beta", 1000)}},
			batchTeamNames: map[string]map[string]string{"c1": {"TA": "Alpha"}},
		},
		blockDur: 500 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	b := NewCandidatePoolBuilder(loader)
	_, err := b.BuildCtx(ctx, []TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "c1"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
	}, "football")
	if err == nil {
		t.Fatal("expected ctx cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestCandidatePool_BuildCtx_SucceedsWithoutCancel(t *testing.T) {
	loader := &ctxStubLoader{
		batchStubLoader: batchStubLoader{
			batchEvents:    map[string][]db.TSEvent{"c1": {cpMakeTSEvent("m1", "TA", "Alpha", "TB", "Beta", 1000)}},
			batchTeamNames: map[string]map[string]string{"c1": {"TA": "Alpha"}},
		},
		blockDur: 1 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	b := NewCandidatePoolBuilder(loader)
	res, err := b.BuildCtx(ctx, []TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "c1"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
	}, "football")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(res.Candidates))
	}
}

func TestCandidatePool_BuildCtx_FallsBackWhenNonContextLoader(t *testing.T) {
	// stubTSLoader 不实现 ContextBatchTSEventLoader → BuildCtx 走 Build 回退
	loader := &stubTSLoader{
		events:    map[string][]db.TSEvent{"c1": {cpMakeTSEvent("m1", "TA", "A", "TB", "B", 1000)}},
		teamNames: map[string]map[string]string{"c1": {"TA": "A"}},
	}
	b := NewCandidatePoolBuilder(loader)
	res, err := b.BuildCtx(context.Background(), []TSCompetitionCandidate{
		{Competition: db.TSCompetition{ID: "c1"}, LeaguePriorScore: 1.0, StrongConstraintOK: true},
	}, "football")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Errorf("expected 1 candidate from fallback, got %d", len(res.Candidates))
	}
}
