package analytics

import (
	"testing"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

func testAttempts() []quiz.QuizAttempt {
	day1 := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	return []quiz.QuizAttempt{
		{SessionID: "s1", QuestionID: "q1", Topic: "Deadlock", IsCorrect: true, TimeTakenSeconds: 30, CreatedAt: day1},
		{SessionID: "s1", QuestionID: "q2", Topic: "Deadlock", IsCorrect: true, TimeTakenSeconds: 40, CreatedAt: day1},
		{SessionID: "s1", QuestionID: "q3", Topic: "Paging", IsCorrect: false, TimeTakenSeconds: 60, CreatedAt: day2},
		{SessionID: "s1", QuestionID: "q4", Topic: "Paging", IsCorrect: false, TimeTakenSeconds: 120, CreatedAt: day2},
	}
}

func TestComputeTopicMastery(t *testing.T) {
	breakdown := quiz.ComputeTopicBreakdown(testAttempts())
	got := ComputeTopicMastery(breakdown)
	if len(got) != 2 {
		t.Fatalf("expected 2 mastery rows, got %d", len(got))
	}
	// Sorted by topic name: Deadlock first.
	if got[0].Topic != "Deadlock" || got[1].Topic != "Paging" {
		t.Fatalf("unexpected order: %+v", got)
	}
	deadlock := got[0]
	if deadlock.Accuracy != 100 {
		t.Fatalf("deadlock accuracy = %.2f, want 100", deadlock.Accuracy)
	}
	// 2 attempts: weight = 2/7, mastery = 100*2/7 ~= 28.57 -> beginner.
	want := 100 * 2.0 / 7.0
	if diff := deadlock.Mastery - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("deadlock mastery = %.4f, want %.4f", deadlock.Mastery, want)
	}
	if deadlock.Level != MasteryLevelBeginner {
		t.Fatalf("deadlock level = %q, want beginner", deadlock.Level)
	}
	if got[1].Mastery != 0 || got[1].Level != MasteryLevelNovice {
		t.Fatalf("paging should score 0/novice, got %+v", got[1])
	}
	// High-attempt topic converges near raw accuracy.
	many := make([]quiz.TopicBreakdown, 0)
	_ = many
	big := quiz.TopicBreakdown{Topic: "OS", Attempted: 100, Correct: 90, Incorrect: 10, Accuracy: 90}
	rows := ComputeTopicMastery([]quiz.TopicBreakdown{big})
	if rows[0].Mastery < 80 || rows[0].Level != MasteryLevelMastered {
		t.Fatalf("high-attempt topic should be mastered, got %+v", rows[0])
	}
}

func TestMasteryLevelBands(t *testing.T) {
	cases := map[float64]string{
		100: MasteryLevelMastered, 80: MasteryLevelMastered,
		79.9: MasteryLevelProficient, 60: MasteryLevelProficient,
		59.9: MasteryLevelDeveloping, 40: MasteryLevelDeveloping,
		39.9: MasteryLevelBeginner, 20: MasteryLevelBeginner,
		19.9: MasteryLevelNovice, 0: MasteryLevelNovice,
	}
	for score, want := range cases {
		if got := MasteryLevel(score); got != want {
			t.Fatalf("MasteryLevel(%.1f) = %q, want %q", score, got, want)
		}
	}
}

func TestHistoricalAccuracy(t *testing.T) {
	got := HistoricalAccuracy(testAttempts())
	if len(got) != 2 {
		t.Fatalf("expected 2 days, got %d: %+v", len(got), got)
	}
	if got[0].Date != "2026-09-20" || got[0].Accuracy != 100 || got[0].Attempted != 2 {
		t.Fatalf("day1 wrong: %+v", got[0])
	}
	if got[1].Date != "2026-09-21" || got[1].Accuracy != 0 || got[1].Correct != 0 {
		t.Fatalf("day2 wrong: %+v", got[1])
	}
	if len(HistoricalAccuracy(nil)) != 0 {
		t.Fatal("empty input should yield no days")
	}
}

func TestQuestionDifficultyStats(t *testing.T) {
	diffOf := func(a quiz.QuizAttempt) string {
		if a.QuestionID == "q1" || a.QuestionID == "q2" {
			return "Easy"
		}
		return "hard"
	}
	got := QuestionDifficultyStats(testAttempts(), diffOf)
	if len(got) != 2 {
		t.Fatalf("expected 2 difficulty rows, got %+v", got)
	}
	// Sorted by name: easy first.
	if got[0].Difficulty != "easy" || got[0].Accuracy != 100 || got[0].AvgTimeSeconds != 35 {
		t.Fatalf("easy row wrong: %+v", got[0])
	}
	if got[1].Difficulty != "hard" || got[1].Accuracy != 0 || got[1].AvgTimeSeconds != 90 {
		t.Fatalf("hard row wrong: %+v", got[1])
	}
	// Nil resolver groups everything under unknown.
	unknown := QuestionDifficultyStats(testAttempts(), nil)
	if len(unknown) != 1 || unknown[0].Difficulty != "unknown" || unknown[0].Attempted != 4 {
		t.Fatalf("nil resolver should yield one unknown row, got %+v", unknown)
	}
}

func TestTimePerQuestion(t *testing.T) {
	got := TimePerQuestion(testAttempts())
	if got.Count != 4 {
		t.Fatalf("count = %d, want 4", got.Count)
	}
	// Times: 30,40,60,120 -> avg 62.5, median 50, p90 rank ceil(3.6)=4 -> 120.
	if got.AvgSeconds != 62.5 {
		t.Fatalf("avg = %.2f, want 62.5", got.AvgSeconds)
	}
	if got.MedianSeconds != 50 {
		t.Fatalf("median = %.2f, want 50", got.MedianSeconds)
	}
	if got.P90Seconds != 120 {
		t.Fatalf("p90 = %.2f, want 120", got.P90Seconds)
	}
	empty := TimePerQuestion(nil)
	if empty.Count != 0 || empty.AvgSeconds != 0 || empty.MedianSeconds != 0 || empty.P90Seconds != 0 {
		t.Fatalf("empty timing should be zero, got %+v", empty)
	}
}

func TestExamReadinessScore(t *testing.T) {
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	got := ExamReadinessScore(ReadinessInput{
		Attempts:    testAttempts(),
		TotalTopics: 4,
		Now:         now,
	})
	// Mastery: deadlock 100*2/7 ~= 28.57, paging 0 -> mean ~= 14.29.
	// Accuracy 50, coverage 2/4 = 50, recency (1 day) = 100.
	// Score = .4*14.29 + .25*50 + .2*50 + .15*100 ~= 43.21 -> developing.
	if got.Band != ReadinessDeveloping {
		t.Fatalf("band = %q, want developing (score %.2f)", got.Band, got.Score)
	}
	if got.Accuracy != 50 || got.Coverage != 50 || got.Recency != 100 {
		t.Fatalf("components wrong: %+v", got)
	}
	// Empty history -> zero/getting-started.
	empty := ExamReadinessScore(ReadinessInput{})
	if empty.Score != 0 || empty.Band != ReadinessGettingStart {
		t.Fatalf("empty readiness should be zero/getting-started, got %+v", empty)
	}
	// Strong recent history across the full curriculum -> exam-ready.
	strong := make([]quiz.QuizAttempt, 0)
	base := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		strong = append(strong,
			quiz.QuizAttempt{SessionID: "s", QuestionID: "a", Topic: "T1", IsCorrect: true, TimeTakenSeconds: 20, CreatedAt: base},
			quiz.QuizAttempt{SessionID: "s", QuestionID: "b", Topic: "T2", IsCorrect: true, TimeTakenSeconds: 20, CreatedAt: base},
		)
	}
	ready := ExamReadinessScore(ReadinessInput{Attempts: strong, TotalTopics: 2, Now: now})
	if ready.Band != ReadinessExamReady {
		t.Fatalf("strong history should be exam-ready, got %+v", ready)
	}
}
