package quiz

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Smart quiz generation (Phase 27: Optional Advanced Features).
//
// This file holds the adaptive/personalized quiz helpers: difficulty
// estimation, adaptive difficulty selection, weak-topic weighting, a
// spaced-repetition (SM-2 lite) scheduler, and a personalized request
// builder. Everything here is deterministic and dependency-free so it can
// be unit tested without a database or LLM.
//
// QuizRequest backward compatibility: personalization rides on the optional
// QuizRequest.Adaptive pointer (see AdaptiveOptions). A nil Adaptive means
// legacy behavior; Validate accepts nil and validates a non-nil value
// without changing any existing validation rules.

// DefaultWeakTopicBoost is the default multiplier used by
// WeightByWeakTopics when the caller passes a non-positive boost. A topic
// with 0% accuracy then weighs 1+DefaultWeakTopicBoost.
const DefaultWeakTopicBoost = 1.0

// AdaptiveOptions carries optional smart-quiz personalization for a
// QuizRequest. All fields are optional; the zero value disables nothing by
// itself (a nil *AdaptiveOptions preserves legacy behavior).
type AdaptiveOptions struct {
	// Enabled marks the request as adaptive. Informational: the builder
	// helpers work whether or not it is set when they are called directly.
	Enabled bool `json:"enabled,omitempty"`
	// WeakThreshold overrides the accuracy percentage below which a topic
	// counts as weak. <=0 means DefaultWeakTopicThreshold.
	WeakThreshold float64 `json:"weak_threshold,omitempty"`
	// WeakBoost scales the weak-topic weight bonus. <=0 means
	// DefaultWeakTopicBoost.
	WeakBoost float64 `json:"weak_topic_boost,omitempty"`
	// SkipDueReview opts out of merging due review items into
	// OnlySourceIDs in BuildPersonalizedRequest. The zero value merges
	// due items, so plain Adaptive{Enabled: true} keeps the merge.
	SkipDueReview bool `json:"skip_due_review,omitempty"`
	// PreferUnseen asks for unseen questions (maps to OnlyUnseen).
	PreferUnseen bool `json:"prefer_unseen,omitempty"`
	// PreferIncorrect asks for previously-incorrect questions (maps to
	// OnlyIncorrect). It wins over PreferUnseen when both are set so the
	// built request stays valid (OnlyUnseen/OnlyIncorrect are exclusive).
	PreferIncorrect bool `json:"prefer_incorrect,omitempty"`
}

// Validate checks numeric ranges. A nil receiver is valid (legacy request).
func (o *AdaptiveOptions) Validate() error {
	if o == nil {
		return nil
	}
	if math.IsNaN(o.WeakThreshold) || (o.WeakThreshold != 0 && (o.WeakThreshold <= 0 || o.WeakThreshold > 100)) {
		return fmt.Errorf("adaptive weak_threshold %.2f must be in (0,100]", o.WeakThreshold)
	}
	if math.IsNaN(o.WeakBoost) || o.WeakBoost < 0 {
		return fmt.Errorf("adaptive weak_topic_boost %.2f must be >= 0", o.WeakBoost)
	}
	return nil
}

// EffectiveWeakThreshold returns the configured threshold or
// DefaultWeakTopicThreshold when unset/out of range.
func (o *AdaptiveOptions) EffectiveWeakThreshold() float64 {
	if o != nil && o.WeakThreshold > 0 && o.WeakThreshold <= 100 && !math.IsNaN(o.WeakThreshold) {
		return o.WeakThreshold
	}
	return DefaultWeakTopicThreshold
}

// EffectiveWeakBoost returns the configured boost or DefaultWeakTopicBoost
// when unset/non-positive.
func (o *AdaptiveOptions) EffectiveWeakBoost() float64 {
	if o != nil && !math.IsNaN(o.WeakBoost) && o.WeakBoost > 0 {
		return o.WeakBoost
	}
	return DefaultWeakTopicBoost
}

// EstimateDifficulty heuristically grades a question stem as
// "easy", "medium", or "hard" from surface signals plus history:
//
//   - text length: <200 chars +0, 200-599 +1, >=600 +2
//   - option count: <=2 +0, 3-4 +1, >4 +2
//   - pastAccuracy (percent correct, 0-100): <50 +1 (harder), >=80 -1
//     (easier); negative means "unknown" and contributes 0.
//
// The total maps to easy (<=0), medium (1-2), or hard (>=3).
func EstimateDifficulty(text string, numOptions int, pastAccuracy float64) string {
	score := 0
	switch l := len(strings.TrimSpace(text)); {
	case l >= 600:
		score += 2
	case l >= 200:
		score += 1
	}
	switch {
	case numOptions > 4:
		score += 2
	case numOptions > 2:
		score += 1
	}
	if pastAccuracy >= 0 && !math.IsNaN(pastAccuracy) {
		switch {
		case pastAccuracy < 50:
			score++
		case pastAccuracy >= 80:
			score--
		}
	}
	switch {
	case score <= 0:
		return "easy"
	case score <= 2:
		return "medium"
	default:
		return "hard"
	}
}

// AdjustDifficulty steps the current difficulty toward the learner's level
// from session accuracy (percent, 0-100):
//
//	accuracy >= 80 steps up (easy->medium->hard), accuracy < 50 steps down
//	(hard->medium->easy), otherwise the level is unchanged (clamped at the ends).
//
// An empty or unrecognized current seeds from accuracy instead: >=80 hard,
// <50 easy, else medium. This keeps the function total so the personalized
// builder can call it on any stored value.
func AdjustDifficulty(current string, accuracy float64) string {
	cur := strings.ToLower(strings.TrimSpace(current))
	if _, ok := AllowedDifficulties[cur]; !ok {
		if math.IsNaN(accuracy) {
			return "medium"
		}
		switch {
		case accuracy >= 80:
			return "hard"
		case accuracy < 50:
			return "easy"
		default:
			return "medium"
		}
	}
	if math.IsNaN(accuracy) {
		return cur
	}
	switch {
	case accuracy >= 80:
		switch cur {
		case "easy":
			return "medium"
		case "medium":
			return "hard"
		default:
			return "hard"
		}
	case accuracy < 50:
		switch cur {
		case "hard":
			return "medium"
		case "medium":
			return "easy"
		default:
			return "easy"
		}
	default:
		return cur
	}
}

// WeightByWeakTopics returns a per-topic selection weight for the candidate
// topics: 1.0 by default, boosted for weak topics (accuracy below
// DefaultWeakTopicThreshold) as 1 + boost*(1-accuracy/100), so weaker topics
// are picked more often. A non-positive boost means DefaultWeakTopicBoost.
// Breakdown rows are matched case-insensitively; topics missing from the
// breakdown (or with empty names skipped) weigh 1.0. The breakdown usually
// comes from SessionResult.Topics (or SessionResult.WeakTopic).
func WeightByWeakTopics(topics []string, breakdown []TopicBreakdown, boost float64) map[string]float64 {
	if boost <= 0 || math.IsNaN(boost) {
		boost = DefaultWeakTopicBoost
	}
	acc := make(map[string]float64, len(breakdown))
	for _, row := range breakdown {
		if t := strings.ToLower(strings.TrimSpace(row.Topic)); t != "" {
			acc[t] = row.Accuracy
		}
	}
	out := make(map[string]float64, len(topics))
	for _, t := range topics {
		name := strings.TrimSpace(t)
		if name == "" {
			continue
		}
		w := 1.0
		if a, ok := acc[strings.ToLower(name)]; ok && a < DefaultWeakTopicThreshold {
			w = 1 + boost*(1-a/100)
		}
		out[name] = w
	}
	return out
}

// ReviewSchedule is one SM-2 lite scheduling row: when a question was last
// attempted, the current interval, and when it is next due.
type ReviewSchedule struct {
	// QuestionID is the quiz question ID (QuizAttempt.QuestionID).
	QuestionID string `json:"question_id"`
	// SourceQuestionID is the underlying PYQ when recorded.
	SourceQuestionID string `json:"source_question_id,omitempty"`
	// LastAttempt is the most recent attempt time for the question.
	LastAttempt time.Time `json:"last_attempt"`
	// IntervalDays is the current SM-2 lite interval in days.
	IntervalDays float64 `json:"interval_days"`
	// Repetitions is the consecutive-correct streak.
	Repetitions int `json:"repetitions"`
	// Easiness is the SM-2 lite easiness factor (clamped to [1.3, 2.5]).
	Easiness float64 `json:"easiness"`
	// NextReview is LastAttempt + IntervalDays.
	NextReview time.Time `json:"next_review"`
}

// NextReview returns lastAttempt shifted forward by intervalDays (fractional
// days allowed). A negative interval is treated as 0.
func NextReview(lastAttempt time.Time, intervalDays float64) time.Time {
	if math.IsNaN(intervalDays) || intervalDays < 0 {
		intervalDays = 0
	}
	return lastAttempt.Add(time.Duration(intervalDays * 24 * float64(time.Hour)))
}

// ScheduleReviews folds attempts into per-question SM-2 lite schedules.
// Attempts group by QuestionID (falling back to SourceQuestionID when the
// former is empty; attempts with neither are skipped) and replay
// chronologically from easiness 2.5:
//
//	correct:   repetitions++; interval = 1 (1st), 6 (2nd), else prev*easiness
//	incorrect: repetitions = 0; interval = 1; easiness = max(1.3, easiness-0.2)
//
// LastAttempt is the latest attempt time (now is the fallback for zero
// times). Output is sorted by QuestionID for determinism.
func ScheduleReviews(attempts []QuizAttempt, now time.Time) []ReviewSchedule {
	groups := make(map[string][]QuizAttempt)
	for _, a := range attempts {
		key := strings.TrimSpace(a.QuestionID)
		if key == "" {
			key = strings.TrimSpace(a.SourceQuestionID)
		}
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], a)
	}
	out := make([]ReviewSchedule, 0, len(groups))
	for key, grp := range groups {
		sort.SliceStable(grp, func(i, j int) bool {
			if grp[i].CreatedAt.Equal(grp[j].CreatedAt) {
				return i < j
			}
			return grp[i].CreatedAt.Before(grp[j].CreatedAt)
		})
		const (
			maxEasiness = 2.5
			minEasiness = 1.3
		)
		easiness := maxEasiness
		reps := 0
		interval := 1.0
		last := now
		source := ""
		for _, a := range grp {
			t := a.CreatedAt
			if t.IsZero() {
				t = now
			}
			last = t
			if s := strings.TrimSpace(a.SourceQuestionID); s != "" {
				source = s
			}
			if a.IsCorrect {
				reps++
				switch reps {
				case 1:
					interval = 1
				case 2:
					interval = 6
				default:
					interval = interval * easiness
				}
			} else {
				reps = 0
				interval = 1
				easiness = math.Max(minEasiness, easiness-0.2)
			}
		}
		out = append(out, ReviewSchedule{
			QuestionID:       key,
			SourceQuestionID: source,
			LastAttempt:      last,
			IntervalDays:     interval,
			Repetitions:      reps,
			Easiness:         easiness,
			NextReview:       NextReview(last, interval),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QuestionID < out[j].QuestionID })
	return out
}

// DueQuestions filters schedules to those due at now (NextReview <= now; a
// zero NextReview is always due). Output is sorted by NextReview, then
// QuestionID, for a stable review order.
func DueQuestions(schedules []ReviewSchedule, now time.Time) []ReviewSchedule {
	out := make([]ReviewSchedule, 0)
	for _, s := range schedules {
		if s.NextReview.IsZero() || !s.NextReview.After(now) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NextReview.Equal(out[j].NextReview) {
			return out[i].QuestionID < out[j].QuestionID
		}
		return out[i].NextReview.Before(out[j].NextReview)
	})
	return out
}

// BuildPersonalizedRequest derives a quiz request from a base request plus
// learning history: it merges weak topics (from result, weakest handling via
// the Adaptive.WeakThreshold or the default), due review items (by source
// question ID), and unseen/incorrect preferences into a new QuizRequest. The
// input is not mutated; slice fields are deep-copied.
//
// Rules:
//   - Topics: base topics first, then weak-topic names not already present
//     (case-insensitive match).
//   - OnlySourceIDs: base IDs first, then due item IDs (SourceQuestionID
//     preferred, else QuestionID), deduped; skipped when Adaptive sets
//     SkipDueReview.
//   - OnlyUnseen/OnlyIncorrect: from Adaptive.PreferIncorrect/PreferUnseen
//     (incorrect wins on conflict to preserve validity); otherwise the base
//     flags are kept.
//   - Difficulty: stepped via AdjustDifficulty from result accuracy when the
//     base sets a difficulty and the result has attempts.
//   - NumQuestions: preserved, defaulting to DefaultNumQuestions when <= 0.
func BuildPersonalizedRequest(base QuizRequest, result SessionResult, due []ReviewSchedule) QuizRequest {
	out := base
	out.Topics = append([]string{}, base.Topics...)
	out.ExcludeSourceIDs = append([]string{}, base.ExcludeSourceIDs...)
	out.OnlySourceIDs = append([]string{}, base.OnlySourceIDs...)
	if base.Adaptive != nil {
		cp := *base.Adaptive
		out.Adaptive = &cp
	}

	threshold := DefaultWeakTopicThreshold
	if base.Adaptive != nil {
		threshold = base.Adaptive.EffectiveWeakThreshold()
	}
	var weak []TopicBreakdown
	if len(result.Topics) > 0 {
		weak = WeakTopics(result.Topics, threshold)
	} else if len(result.WeakTopic) > 0 {
		weak = WeakTopics(result.WeakTopic, threshold)
	}
	if len(weak) > 0 {
		seen := make(map[string]struct{}, len(out.Topics)+len(weak))
		for _, t := range out.Topics {
			seen[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
		}
		for _, row := range weak {
			name := strings.TrimSpace(row.Topic)
			if name == "" {
				continue
			}
			if _, dup := seen[strings.ToLower(name)]; dup {
				continue
			}
			seen[strings.ToLower(name)] = struct{}{}
			out.Topics = append(out.Topics, name)
		}
	}

	// Default path merges due IDs; a non-nil Adaptive with SkipDueReview
	// opts out of the merge.
	if base.Adaptive == nil || !base.Adaptive.SkipDueReview {
		seen := toIDSet(out.OnlySourceIDs)
		for _, d := range due {
			id := strings.TrimSpace(d.SourceQuestionID)
			if id == "" {
				id = strings.TrimSpace(d.QuestionID)
			}
			if id == "" {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out.OnlySourceIDs = append(out.OnlySourceIDs, id)
		}
	}

	if base.Adaptive != nil {
		switch {
		case base.Adaptive.PreferIncorrect:
			out.OnlyIncorrect = true
			out.OnlyUnseen = false
		case base.Adaptive.PreferUnseen:
			out.OnlyUnseen = true
			out.OnlyIncorrect = false
		}
	}

	if strings.TrimSpace(out.Difficulty) != "" && result.Metrics.Attempted > 0 {
		out.Difficulty = AdjustDifficulty(out.Difficulty, result.Metrics.Accuracy)
	}
	if out.NumQuestions <= 0 {
		out.NumQuestions = DefaultNumQuestions
	}
	return out
}
