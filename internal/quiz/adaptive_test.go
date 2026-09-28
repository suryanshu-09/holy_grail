package quiz

import (
	"strings"
	"testing"
	"time"
)

func TestAdaptiveOptionsValidate(t *testing.T) {
	var nilOpts *AdaptiveOptions
	if err := nilOpts.Validate(); err != nil {
		t.Fatalf("nil AdaptiveOptions should validate: %v", err)
	}
	for _, tc := range []AdaptiveOptions{
		{},
		{Enabled: true},
		{WeakThreshold: 70, WeakBoost: 1.5, PreferUnseen: true},
		{WeakThreshold: 100, PreferIncorrect: true, SkipDueReview: true},
	} {
		if err := tc.Validate(); err != nil {
			t.Fatalf("%+v should validate: %v", tc, err)
		}
	}
	for _, tc := range []AdaptiveOptions{
		{WeakThreshold: -5},
		{WeakThreshold: 101},
		{WeakBoost: -1},
	} {
		if err := tc.Validate(); err == nil {
			t.Fatalf("%+v should fail validation", tc)
		}
	}
}

func TestAdaptiveEffectiveDefaults(t *testing.T) {
	var nilOpts *AdaptiveOptions
	if got := nilOpts.EffectiveWeakThreshold(); got != DefaultWeakTopicThreshold {
		t.Fatalf("nil threshold = %v, want %v", got, DefaultWeakTopicThreshold)
	}
	if got := nilOpts.EffectiveWeakBoost(); got != DefaultWeakTopicBoost {
		t.Fatalf("nil boost = %v, want %v", got, DefaultWeakTopicBoost)
	}
	o := &AdaptiveOptions{WeakThreshold: 50, WeakBoost: 2}
	if got := o.EffectiveWeakThreshold(); got != 50 {
		t.Fatalf("threshold = %v, want 50", got)
	}
	if got := o.EffectiveWeakBoost(); got != 2 {
		t.Fatalf("boost = %v, want 2", got)
	}
	bad := &AdaptiveOptions{WeakThreshold: 500, WeakBoost: -3}
	if got := bad.EffectiveWeakThreshold(); got != DefaultWeakTopicThreshold {
		t.Fatalf("out-of-range threshold = %v, want default", got)
	}
	if got := bad.EffectiveWeakBoost(); got != DefaultWeakTopicBoost {
		t.Fatalf("negative boost = %v, want default", got)
	}
}

func TestEstimateDifficulty(t *testing.T) {
	short := "What is paging?"
	if got := EstimateDifficulty(short, 2, 95); got != "easy" {
		t.Fatalf("short/high-accuracy = %q, want easy", got)
	}
	long := strings.Repeat("Explain deadlock detection with a worked example. ", 30)
	if got := EstimateDifficulty(long, 5, 20); got != "hard" {
		t.Fatalf("long/many-options/low-accuracy = %q, want hard", got)
	}
	medium := strings.Repeat("word ", 60) // 300 chars, unknown history
	if got := EstimateDifficulty(medium, 4, -1); got != "medium" {
		t.Fatalf("mid-length/unknown-history = %q, want medium", got)
	}
	// Accuracy history alone shifts the grade.
	if got := EstimateDifficulty(short, 2, 10); got != "medium" {
		t.Fatalf("short/low-accuracy = %q, want medium", got)
	}
	if got := EstimateDifficulty(long, 5, 99); got != "hard" {
		t.Fatalf("extreme length/options stay hard despite high accuracy = %q", got)
	}
	midHigh := strings.Repeat("word ", 60) // 300 chars, 4 options, high accuracy
	if got := EstimateDifficulty(midHigh, 4, 99); got != "medium" {
		t.Fatalf("mid-length/high-accuracy = %q, want medium", got)
	}
	// Empty stem with unknown history and no options is easy.
	if got := EstimateDifficulty("", 0, -1); got != "easy" {
		t.Fatalf("empty/unknown = %q, want easy", got)
	}
}

func TestAdjustDifficulty(t *testing.T) {
	cases := []struct {
		current  string
		accuracy float64
		want     string
	}{
		{"easy", 90, "medium"},
		{"medium", 90, "hard"},
		{"hard", 90, "hard"}, // clamped at top
		{"hard", 30, "medium"},
		{"medium", 30, "easy"},
		{"easy", 30, "easy"}, // clamped at bottom
		{"medium", 65, "medium"},
		{"easy", 65, "easy"},
		{" MEDIUM ", 95, "hard"}, // trimmed/case-insensitive
		{"", 90, "hard"},         // empty seeds from accuracy
		{"", 30, "easy"},
		{"", 65, "medium"},
		{"bogus", 90, "hard"},
		{"easy", 80, "medium"}, // boundary steps up
		{"hard", 49.9, "medium"},
	}
	for _, tc := range cases {
		if got := AdjustDifficulty(tc.current, tc.accuracy); got != tc.want {
			t.Fatalf("AdjustDifficulty(%q, %v) = %q, want %q", tc.current, tc.accuracy, got, tc.want)
		}
	}
}

func TestWeightByWeakTopics(t *testing.T) {
	breakdown := []TopicBreakdown{
		{Topic: "Deadlock", Attempted: 10, Correct: 3, Accuracy: 30},
		{Topic: "Paging", Attempted: 10, Correct: 9, Accuracy: 90},
	}
	w := WeightByWeakTopics([]string{"Deadlock", "Paging", "Scheduling"}, breakdown, 1.0)
	if w["Deadlock"] <= 1.0 {
		t.Fatalf("weak topic weight = %v, want > 1", w["Deadlock"])
	}
	if w["Paging"] != 1.0 {
		t.Fatalf("strong topic weight = %v, want 1", w["Paging"])
	}
	if w["Scheduling"] != 1.0 {
		t.Fatalf("unknown topic weight = %v, want 1", w["Scheduling"])
	}
	// Weaker topics weigh more.
	w2 := WeightByWeakTopics([]string{"Deadlock"}, []TopicBreakdown{
		{Topic: "Deadlock", Accuracy: 10},
	}, 1.0)
	if w2["Deadlock"] <= w["Deadlock"] {
		t.Fatalf("10%% accuracy weight %v should exceed 30%% weight %v", w2["Deadlock"], w["Deadlock"])
	}
	// Case-insensitive match, non-positive boost falls back to default.
	w3 := WeightByWeakTopics([]string{"deadlock"}, breakdown, 0)
	if w3["deadlock"] != w["Deadlock"] {
		t.Fatalf("default-boost weight %v != explicit weight %v", w3["deadlock"], w["Deadlock"])
	}
	// Empty inputs yield an empty (non-nil) map; blanks skipped.
	if got := WeightByWeakTopics(nil, nil, 1); len(got) != 0 || got == nil {
		t.Fatalf("empty input should give empty non-nil map, got %#v", got)
	}
	if got := WeightByWeakTopics([]string{"", "  "}, breakdown, 1); len(got) != 0 {
		t.Fatalf("blank topics should be skipped, got %#v", got)
	}
}

func TestNextReview(t *testing.T) {
	last := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := NextReview(last, 1); !got.Equal(last.Add(24 * time.Hour)) {
		t.Fatalf("1-day review = %v", got)
	}
	if got := NextReview(last, 0.5); !got.Equal(last.Add(12 * time.Hour)) {
		t.Fatalf("half-day review = %v", got)
	}
	if got := NextReview(last, -3); !got.Equal(last) {
		t.Fatalf("negative interval should be 0, got %v", got)
	}
}

func attempt(qid, src string, correct bool, at time.Time) QuizAttempt {
	return QuizAttempt{
		SessionID: "s1", QuestionID: qid, SourceQuestionID: src,
		SelectedAnswer: 0, CorrectAnswer: 0, IsCorrect: correct, CreatedAt: at,
	}
}

func TestScheduleReviews(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if got := ScheduleReviews(nil, now); len(got) != 0 {
		t.Fatalf("empty attempts should give empty schedule, got %#v", got)
	}
	// One correct attempt: streak 1, 1-day interval.
	got := ScheduleReviews([]QuizAttempt{attempt("q1", "src1", true, now.Add(-time.Hour))}, now)
	if len(got) != 1 {
		t.Fatalf("want 1 schedule, got %#v", got)
	}
	s := got[0]
	if s.Repetitions != 1 || s.IntervalDays != 1 || s.Easiness != 2.5 {
		t.Fatalf("unexpected schedule: %#v", s)
	}
	if !s.NextReview.Equal(s.LastAttempt.Add(24 * time.Hour)) {
		t.Fatalf("next review = %v", s.NextReview)
	}
	if s.SourceQuestionID != "src1" {
		t.Fatalf("source = %q", s.SourceQuestionID)
	}

	// Three-in-a-row streak: intervals 1, 6, 6*2.5.
	streak := []QuizAttempt{
		attempt("q2", "src2", true, now.Add(-72*time.Hour)),
		attempt("q2", "src2", true, now.Add(-48*time.Hour)),
		attempt("q2", "src2", true, now.Add(-24*time.Hour)),
	}
	got = ScheduleReviews(streak, now)
	if len(got) != 1 || got[0].Repetitions != 3 || got[0].IntervalDays != 15 {
		t.Fatalf("streak schedule = %#v", got)
	}

	// Incorrect resets the streak and lowers easiness.
	mixed := []QuizAttempt{
		attempt("q3", "", true, now.Add(-72*time.Hour)),
		attempt("q3", "", true, now.Add(-48*time.Hour)),
		attempt("q3", "", false, now.Add(-24*time.Hour)),
	}
	got = ScheduleReviews(mixed, now)
	if len(got) != 1 || got[0].Repetitions != 0 || got[0].IntervalDays != 1 {
		t.Fatalf("reset schedule = %#v", got)
	}
	if got[0].Easiness != 2.3 {
		t.Fatalf("easiness after one miss = %v, want 2.3", got[0].Easiness)
	}

	// Grouping falls back to source ID; attempts without IDs are skipped;
	// output is sorted by question ID.
	multi := []QuizAttempt{
		attempt("qb", "s", true, now),
		attempt("", "src-only", true, now),
		{SessionID: "s1", CreatedAt: now, IsCorrect: true},
		attempt("qa", "s", true, now.Add(-time.Hour)),
	}
	got = ScheduleReviews(multi, now)
	if len(got) != 3 || got[0].QuestionID != "qa" || got[1].QuestionID != "qb" || got[2].QuestionID != "src-only" {
		t.Fatalf("grouped/sorted schedule = %#v", got)
	}
	// Zero CreatedAt falls back to now.
	zeroTime := []QuizAttempt{{SessionID: "s1", QuestionID: "qz", IsCorrect: true}}
	got = ScheduleReviews(zeroTime, now)
	if len(got) != 1 || !got[0].LastAttempt.Equal(now) {
		t.Fatalf("zero-time fallback = %#v", got)
	}
}

func TestDueQuestions(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	sched := []ReviewSchedule{
		{QuestionID: "future", NextReview: now.Add(24 * time.Hour)},
		{QuestionID: "due-later", NextReview: now.Add(-time.Hour)},
		{QuestionID: "due-earlier", NextReview: now.Add(-48 * time.Hour)},
		{QuestionID: "zero"},
	}
	due := DueQuestions(sched, now)
	if len(due) != 3 {
		t.Fatalf("want 3 due, got %#v", due)
	}
	// Zero NextReview sorts first, then by time.
	if due[0].QuestionID != "zero" || due[1].QuestionID != "due-earlier" || due[2].QuestionID != "due-later" {
		t.Fatalf("due order = %v", []string{due[0].QuestionID, due[1].QuestionID, due[2].QuestionID})
	}
	if got := DueQuestions(nil, now); len(got) != 0 {
		t.Fatalf("nil schedules should give empty due, got %#v", got)
	}
}

func personalizedResult() SessionResult {
	return NewSessionResult(QuizSession{ID: "s1", Status: SessionStatusCompleted}, []QuizAttempt{
		{SessionID: "s1", QuestionID: "q1", Topic: "Deadlock", SelectedAnswer: 0, CorrectAnswer: 1, IsCorrect: false},
		{SessionID: "s1", QuestionID: "q2", Topic: "Deadlock", SelectedAnswer: 0, CorrectAnswer: 1, IsCorrect: false},
		{SessionID: "s1", QuestionID: "q3", Topic: "Paging", SelectedAnswer: 0, CorrectAnswer: 1, IsCorrect: false},
		{SessionID: "s1", QuestionID: "q4", Topic: "Paging", SelectedAnswer: 0, CorrectAnswer: 0, IsCorrect: true},
	})
}

func TestBuildPersonalizedRequest(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	result := personalizedResult() // Deadlock 0%, Paging 50% (both weak), 25% overall.
	if len(result.WeakTopic) != 2 || result.WeakTopic[0].Topic != "Deadlock" {
		t.Fatalf("fixture weak topics = %#v", result.WeakTopic)
	}
	due := []ReviewSchedule{
		{QuestionID: "quiz-q9", SourceQuestionID: "src9", NextReview: now.Add(-time.Hour)},
	}

	base := QuizRequest{
		Mode: ModeMCQ, NumQuestions: 5, Difficulty: "medium",
		Topics: []string{"Paging"}, OnlySourceIDs: []string{"src1"},
	}
	out := BuildPersonalizedRequest(base, result, due)
	// Weak topic merged after base topics; due source merged after base IDs.
	if len(out.Topics) != 2 || out.Topics[0] != "Paging" || out.Topics[1] != "Deadlock" {
		t.Fatalf("topics = %v", out.Topics)
	}
	if len(out.OnlySourceIDs) != 2 || out.OnlySourceIDs[0] != "src1" || out.OnlySourceIDs[1] != "src9" {
		t.Fatalf("only_source_ids = %v", out.OnlySourceIDs)
	}
	// 25% session accuracy steps medium down to easy.
	if out.Difficulty != "easy" {
		t.Fatalf("difficulty = %q, want easy", out.Difficulty)
	}
	// Base request is not mutated.
	if len(base.Topics) != 1 || len(base.OnlySourceIDs) != 1 || base.Difficulty != "medium" {
		t.Fatalf("base mutated: %#v", base)
	}
	if err := out.Validate(); err != nil {
		t.Fatalf("personalized request should validate: %v", err)
	}

	// PreferIncorrect wins over PreferUnseen (they are mutually exclusive).
	pref := QuizRequest{Mode: ModeMCQ, Adaptive: &AdaptiveOptions{Enabled: true, PreferUnseen: true, PreferIncorrect: true}}
	out = BuildPersonalizedRequest(pref, result, nil)
	if !out.OnlyIncorrect || out.OnlyUnseen {
		t.Fatalf("conflicting prefs = unseen %v incorrect %v", out.OnlyUnseen, out.OnlyIncorrect)
	}
	// PreferUnseen alone maps through.
	out = BuildPersonalizedRequest(QuizRequest{Mode: ModeMCQ, Adaptive: &AdaptiveOptions{PreferUnseen: true}}, result, nil)
	if !out.OnlyUnseen || out.OnlyIncorrect {
		t.Fatalf("unseen pref = unseen %v incorrect %v", out.OnlyUnseen, out.OnlyIncorrect)
	}
	// SkipDueReview drops the due merge but keeps weak topics.
	out = BuildPersonalizedRequest(QuizRequest{Mode: ModeMCQ, Adaptive: &AdaptiveOptions{SkipDueReview: true}}, result, due)
	if len(out.OnlySourceIDs) != 0 {
		t.Fatalf("skipped due merge = %v", out.OnlySourceIDs)
	}
	if len(out.Topics) != 2 || out.Topics[0] != "Deadlock" || out.Topics[1] != "Paging" {
		t.Fatalf("weak topics still merge = %v", out.Topics)
	}
	// Zero NumQuestions defaults; empty difficulty is untouched.
	out = BuildPersonalizedRequest(QuizRequest{Mode: ModeMCQ}, result, nil)
	if out.NumQuestions != DefaultNumQuestions {
		t.Fatalf("num_questions = %d", out.NumQuestions)
	}
	if out.Difficulty != "" {
		t.Fatalf("empty difficulty should stay empty, got %q", out.Difficulty)
	}
	// Duplicate due IDs and existing weak topics are not duplicated.
	dupDue := []ReviewSchedule{{QuestionID: "src1", NextReview: now}}
	out = BuildPersonalizedRequest(QuizRequest{Mode: ModeMCQ, Topics: []string{"deadlock"}, OnlySourceIDs: []string{"src1"}}, result, dupDue)
	if len(out.Topics) != 2 || out.Topics[0] != "deadlock" || out.Topics[1] != "Paging" || len(out.OnlySourceIDs) != 1 {
		t.Fatalf("dup merge = topics %v ids %v", out.Topics, out.OnlySourceIDs)
	}
}

func TestQuizRequestAdaptiveBackwardCompat(t *testing.T) {
	// Legacy request without Adaptive still validates and stays nil.
	legacy := QuizRequest{Mode: ModeMCQ, NumQuestions: 5}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("legacy request should validate: %v", err)
	}
	if legacy.Adaptive != nil {
		t.Fatalf("legacy Adaptive should stay nil")
	}
	// Valid Adaptive passes through Validate.
	withAdaptive := QuizRequest{Mode: ModeMCQ, Adaptive: &AdaptiveOptions{Enabled: true, WeakThreshold: 60}}
	if err := withAdaptive.Validate(); err != nil {
		t.Fatalf("adaptive request should validate: %v", err)
	}
	// Invalid Adaptive is rejected without changing other rules.
	bad := QuizRequest{Mode: ModeMCQ, Adaptive: &AdaptiveOptions{WeakThreshold: -1}}
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid adaptive options should fail validation")
	}
}
