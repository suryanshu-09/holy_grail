// Package analytics provides deterministic, dependency-free learning
// analytics over quiz attempts (Phase 27: Optional Advanced Features).
//
// Every function here is pure: it folds []quiz.QuizAttempt (and the
// per-topic rows from quiz.ComputeTopicBreakdown) into mastery, trend,
// difficulty, timing, and exam-readiness summaries. There is no I/O, no
// database access, and no schema change; HTTP handlers supply the attempts
// via QuizEvaluator.GetResult and render these summaries as JSON.
package analytics

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

// Mastery level bands for TopicMastery.Level, keyed on the 0..100 mastery
// score (not raw accuracy, so low-attempt topics rank lower).
const (
	MasteryLevelMastered   = "mastered"   // mastery >= 80
	MasteryLevelProficient = "proficient" // mastery >= 60
	MasteryLevelDeveloping = "developing" // mastery >= 40
	MasteryLevelBeginner   = "beginner"   // mastery >= 20
	MasteryLevelNovice     = "novice"     // mastery < 20
)

// ConfidenceHalfLife is the attempt count at which a topic reaches half
// confidence: weight = attempted / (attempted + ConfidenceHalfLife). Topics
// with few attempts are discounted so a single lucky answer does not read
// as mastery.
const ConfidenceHalfLife = 5.0

// TopicMastery is the per-topic mastery row: raw accuracy discounted by a
// confidence weight that grows with the number of attempts.
type TopicMastery struct {
	// Topic is the topic name as recorded on the attempts.
	Topic string `json:"topic"`
	// Attempted/Correct/Incorrect mirror the quiz.TopicBreakdown counts.
	Attempted int `json:"attempted"`
	Correct   int `json:"correct"`
	Incorrect int `json:"incorrect"`
	// Accuracy is percent correct within the topic (0..100).
	Accuracy float64 `json:"accuracy"`
	// Mastery is Accuracy weighted by attempt confidence (0..100).
	Mastery float64 `json:"mastery"`
	// Level is the MasteryLevel* band for Mastery.
	Level string `json:"level"`
}

// MasteryLevel maps a 0..100 mastery score to its level band.
func MasteryLevel(mastery float64) string {
	switch {
	case mastery >= 80:
		return MasteryLevelMastered
	case mastery >= 60:
		return MasteryLevelProficient
	case mastery >= 40:
		return MasteryLevelDeveloping
	case mastery >= 20:
		return MasteryLevelBeginner
	default:
		return MasteryLevelNovice
	}
}

// ConfidenceWeight returns attempted/(attempted+ConfidenceHalfLife): 0 for
// no attempts, approaching 1 as attempts grow. Non-positive counts yield 0.
func ConfidenceWeight(attempted int) float64 {
	if attempted <= 0 {
		return 0
	}
	n := float64(attempted)
	return n / (n + ConfidenceHalfLife)
}

// ComputeTopicMastery converts per-topic breakdown rows into mastery rows,
// sorted by topic name for determinism. Mastery is
// Accuracy * ConfidenceWeight(Attempted), so rarely-attempted topics score
// below their raw accuracy.
func ComputeTopicMastery(breakdown []quiz.TopicBreakdown) []TopicMastery {
	out := make([]TopicMastery, 0, len(breakdown))
	for _, row := range breakdown {
		mastery := row.Accuracy * ConfidenceWeight(row.Attempted)
		out = append(out, TopicMastery{
			Topic:     row.Topic,
			Attempted: row.Attempted,
			Correct:   row.Correct,
			Incorrect: row.Incorrect,
			Accuracy:  row.Accuracy,
			Mastery:   mastery,
			Level:     MasteryLevel(mastery),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Topic < out[j].Topic })
	return out
}

// DailyAccuracy is one per-day point of the historical accuracy trend.
type DailyAccuracy struct {
	// Date is the UTC calendar day (YYYY-MM-DD) of the attempts.
	Date string `json:"date"`
	// Attempted is the number of attempts recorded that day.
	Attempted int `json:"attempted"`
	// Correct is the number of correct attempts that day.
	Correct int `json:"correct"`
	// Accuracy is percent correct that day (0..100).
	Accuracy float64 `json:"accuracy"`
}

// HistoricalAccuracy folds attempts into per-day accuracy rows sorted
// ascending by date. Attempts with a zero timestamp are grouped under
// their UTC date like any other attempt, keeping the function total.
func HistoricalAccuracy(attempts []quiz.QuizAttempt) []DailyAccuracy {
	byDay := make(map[string]*DailyAccuracy)
	for _, a := range attempts {
		day := a.CreatedAt.UTC().Format("2006-01-02")
		row, ok := byDay[day]
		if !ok {
			row = &DailyAccuracy{Date: day}
			byDay[day] = row
		}
		row.Attempted++
		if a.IsCorrect {
			row.Correct++
		}
	}
	out := make([]DailyAccuracy, 0, len(byDay))
	for _, row := range byDay {
		if row.Attempted > 0 {
			row.Accuracy = float64(row.Correct) / float64(row.Attempted) * 100
		}
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// DifficultyStats is the per-difficulty accuracy and timing row.
type DifficultyStats struct {
	// Difficulty is the normalized difficulty label ("unknown" when the
	// resolver reports nothing usable).
	Difficulty string `json:"difficulty"`
	// Attempted/Correct/Incorrect count attempts at this difficulty.
	Attempted int `json:"attempted"`
	Correct   int `json:"correct"`
	Incorrect int `json:"incorrect"`
	// Accuracy is percent correct at this difficulty (0..100).
	Accuracy float64 `json:"accuracy"`
	// AvgTimeSeconds is the mean TimeTakenSeconds at this difficulty.
	AvgTimeSeconds float64 `json:"avg_time_seconds"`
}

// QuestionDifficultyStats groups attempts by difficulty and reports
// per-difficulty accuracy plus average time, sorted by difficulty name for
// determinism.
//
// quiz.QuizAttempt carries no difficulty column (no DB migration), so the
// caller supplies difficultyOf to resolve one per attempt (e.g. via the
// source question record). A nil resolver — or an empty/blank label —
// groups the attempt under "unknown". Labels are lowercased and trimmed.
func QuestionDifficultyStats(attempts []quiz.QuizAttempt, difficultyOf func(quiz.QuizAttempt) string) []DifficultyStats {
	byDiff := make(map[string]*DifficultyStats)
	times := make(map[string]float64)
	for _, a := range attempts {
		label := "unknown"
		if difficultyOf != nil {
			if d := strings.ToLower(strings.TrimSpace(difficultyOf(a))); d != "" {
				label = d
			}
		}
		row, ok := byDiff[label]
		if !ok {
			row = &DifficultyStats{Difficulty: label}
			byDiff[label] = row
		}
		row.Attempted++
		if a.IsCorrect {
			row.Correct++
		}
		times[label] += a.TimeTakenSeconds
	}
	out := make([]DifficultyStats, 0, len(byDiff))
	for label, row := range byDiff {
		row.Incorrect = row.Attempted - row.Correct
		if row.Attempted > 0 {
			row.Accuracy = float64(row.Correct) / float64(row.Attempted) * 100
			row.AvgTimeSeconds = times[label] / float64(row.Attempted)
		}
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Difficulty < out[j].Difficulty })
	return out
}

// TimingStats summarizes per-question response times in seconds.
type TimingStats struct {
	// Count is the number of attempts measured.
	Count int `json:"count"`
	// AvgSeconds is the mean time per question (0 when Count == 0).
	AvgSeconds float64 `json:"avg_seconds"`
	// MedianSeconds is the median time per question (0 when Count == 0).
	MedianSeconds float64 `json:"median_seconds"`
	// P90Seconds is the 90th percentile (nearest-rank) time (0 when empty).
	P90Seconds float64 `json:"p90_seconds"`
}

// TimePerQuestion folds attempt times into average/median/p90 statistics.
// Negative times (should not occur; Validate rejects them) are clamped to 0
// so a corrupt record cannot poison the summary.
func TimePerQuestion(attempts []quiz.QuizAttempt) TimingStats {
	stats := TimingStats{Count: len(attempts)}
	if len(attempts) == 0 {
		return stats
	}
	times := make([]float64, 0, len(attempts))
	var total float64
	for _, a := range attempts {
		t := a.TimeTakenSeconds
		if math.IsNaN(t) || t < 0 {
			t = 0
		}
		times = append(times, t)
		total += t
	}
	sort.Float64s(times)
	stats.AvgSeconds = total / float64(len(times))
	n := len(times)
	if n%2 == 1 {
		stats.MedianSeconds = times[n/2]
	} else {
		stats.MedianSeconds = (times[n/2-1] + times[n/2]) / 2
	}
	// Nearest-rank p90: the smallest value with at least 90% of data <= it.
	rank := int(math.Ceil(0.9 * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	stats.P90Seconds = times[rank-1]
	return stats
}

// Exam-readiness bands for ReadinessScore.Band.
const (
	ReadinessExamReady    = "exam-ready"      // score >= 80
	ReadinessAlmostReady  = "almost-ready"    // score >= 60
	ReadinessDeveloping   = "developing"      // score >= 40
	ReadinessGettingStart = "getting-started" // score < 40
)

// Readiness weights: mastery dominates, then accuracy, coverage, recency.
const (
	ReadinessWeightMastery  = 0.4
	ReadinessWeightAccuracy = 0.25
	ReadinessWeightCoverage = 0.2
	ReadinessWeightRecency  = 0.15
)

// ReadinessInput bundles the signals for ExamReadinessScore.
type ReadinessInput struct {
	// Attempts is the raw attempt history to score.
	Attempts []quiz.QuizAttempt
	// Breakdown is the precomputed per-topic breakdown; when empty it is
	// derived from Attempts via quiz.ComputeTopicBreakdown.
	Breakdown []quiz.TopicBreakdown
	// TotalTopics is the known curriculum size used for coverage
	// (distinct attempted topics / TotalTopics, capped at 1). When <= 0,
	// coverage is 1 when anything was attempted, else 0.
	TotalTopics int
	// Now anchors the recency computation; zero means time.Now().
	Now time.Time
}

// ReadinessScore is the 0..100 exam-readiness summary plus its components.
type ReadinessScore struct {
	// Score is the weighted 0..100 readiness score.
	Score float64 `json:"score"`
	// Band is the Readiness* band for Score.
	Band string `json:"band"`
	// Mastery is the mean per-topic mastery (0..100).
	Mastery float64 `json:"mastery"`
	// Accuracy is the overall percent correct (0..100).
	Accuracy float64 `json:"accuracy"`
	// Coverage is the share of the curriculum attempted (0..100).
	Coverage float64 `json:"coverage"`
	// Recency scores how recently the learner practiced (0..100).
	Recency float64 `json:"recency"`
}

// ReadinessBand maps a 0..100 readiness score to its band.
func ReadinessBand(score float64) string {
	switch {
	case score >= 80:
		return ReadinessExamReady
	case score >= 60:
		return ReadinessAlmostReady
	case score >= 40:
		return ReadinessDeveloping
	default:
		return ReadinessGettingStart
	}
}

// recencyScore maps days since the latest attempt to 0..100: practice
// within a day scores 100, decaying to 5 after a month of silence.
func recencyScore(days float64) float64 {
	switch {
	case days <= 1:
		return 100
	case days <= 3:
		return 85
	case days <= 7:
		return 65
	case days <= 14:
		return 40
	case days <= 30:
		return 20
	default:
		return 5
	}
}

// ExamReadinessScore combines mean topic mastery, overall accuracy, topic
// coverage, and practice recency into a 0..100 score plus band:
//
//	score = 0.40*mastery + 0.25*accuracy + 0.20*coverage + 0.15*recency
//
// Empty history yields a zero score in the "getting-started" band.
func ExamReadinessScore(in ReadinessInput) ReadinessScore {
	breakdown := in.Breakdown
	if len(breakdown) == 0 && len(in.Attempts) > 0 {
		breakdown = quiz.ComputeTopicBreakdown(in.Attempts)
	}
	masteryRows := ComputeTopicMastery(breakdown)
	var masteryMean float64
	if len(masteryRows) > 0 {
		var total float64
		for _, row := range masteryRows {
			total += row.Mastery
		}
		masteryMean = total / float64(len(masteryRows))
	}
	var correct int
	var latest time.Time
	seenTopics := make(map[string]struct{})
	for _, a := range in.Attempts {
		if a.IsCorrect {
			correct++
		}
		if t := strings.TrimSpace(a.Topic); t != "" {
			seenTopics[strings.ToLower(t)] = struct{}{}
		}
		if !a.CreatedAt.IsZero() && (latest.IsZero() || a.CreatedAt.After(latest)) {
			latest = a.CreatedAt
		}
	}
	var accuracy float64
	if len(in.Attempts) > 0 {
		accuracy = float64(correct) / float64(len(in.Attempts)) * 100
	}
	var coverage float64
	switch {
	case in.TotalTopics > 0:
		coverage = float64(len(seenTopics)) / float64(in.TotalTopics) * 100
		if coverage > 100 {
			coverage = 100
		}
	case len(in.Attempts) > 0:
		coverage = 100
	}
	var recency float64
	if !latest.IsZero() {
		now := in.Now
		if now.IsZero() {
			now = time.Now()
		}
		days := now.Sub(latest).Hours() / 24
		if days < 0 {
			days = 0
		}
		recency = recencyScore(days)
	}
	score := ReadinessWeightMastery*masteryMean +
		ReadinessWeightAccuracy*accuracy +
		ReadinessWeightCoverage*coverage +
		ReadinessWeightRecency*recency
	return ReadinessScore{
		Score:    score,
		Band:     ReadinessBand(score),
		Mastery:  masteryMean,
		Accuracy: accuracy,
		Coverage: coverage,
		Recency:  recency,
	}
}
