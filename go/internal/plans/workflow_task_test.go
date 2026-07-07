package plans

import "testing"

func TestApplyTaskAction(t *testing.T) {
	cases := []struct {
		name      string
		status    string
		action    string
		delegated bool
		want      string
		wantErr   bool
	}{
		{"start", TaskPending, "start", false, TaskInProgress, false},
		{"submit без делегата → done", TaskInProgress, "submit", false, TaskDone, false},
		{"submit с делегатом → review", TaskInProgress, "submit", true, TaskReview, false},
		{"accept review → done", TaskReview, "accept", true, TaskDone, false},
		{"return из review → in_progress", TaskReview, "return", true, TaskInProgress, false},
		{"return из done → returned", TaskDone, "return", false, TaskReturned, false},
		{"reopen done → in_progress", TaskDone, "reopen", false, TaskInProgress, false},
		{"start из returned", TaskReturned, "start", false, TaskInProgress, false},
		{"accept не из review — ошибка", TaskInProgress, "accept", true, "", true},
		{"submit из pending — ошибка", TaskPending, "submit", false, "", true},
		{"неизвестное действие", TaskInProgress, "foo", false, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := applyTaskAction(c.status, c.action, c.delegated)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
