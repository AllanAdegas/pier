package stack

import (
	"context"
	"errors"
	"testing"

	"pier/internal/gitprovider"
)

type recordingFeedback struct{ reports []gitprovider.RunReport }

func (f *recordingFeedback) EnqueueRun(_ context.Context, r gitprovider.RunReport) error {
	f.reports = append(f.reports, r)
	return nil
}

func TestStackFeedbackReportsRunAndOutcome(t *testing.T) {
	for _, failure := range []bool{false, true} {
		q := &recordingFeedback{}
		s := &Service{}
		s.SetGitFeedback(q, nil, nil)
		run := s.startGitRun(t.Context(), Stack{ID: "stack", Name: "demo"}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		var err error
		expected := gitprovider.PhaseSucceeded
		if failure {
			err = errors.New("private deployment reason")
			expected = gitprovider.PhaseFailed
		}
		run.finish(t.Context(), err)
		if len(q.reports) != 2 || q.reports[0].Phase != gitprovider.PhaseInProgress || q.reports[1].Phase != expected || q.reports[0].Run != q.reports[1].Run || q.reports[0].Run == "" {
			t.Fatal(q.reports)
		}
	}
}

func TestStackFeedbackCancellation(t *testing.T) {
	q := &recordingFeedback{}
	s := &Service{}
	s.SetGitFeedback(q, nil, nil)
	run := s.startGitRun(t.Context(), Stack{ID: "stack", Name: "demo"}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	run.finish(t.Context(), context.Canceled)
	if len(q.reports) != 2 || q.reports[1].Phase != gitprovider.PhaseCancelled {
		t.Fatal(q.reports)
	}
}
