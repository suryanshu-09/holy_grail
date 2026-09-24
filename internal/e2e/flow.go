package e2e

import (
	"context"
	"fmt"
	"strings"
)

// UploadInput is the PDF payload the flow starts from.
type UploadInput struct {
	Filename string
	Content  []byte
}

// Document is the stored upload produced by the Uploader stage.
type Document struct {
	ID       string
	Filename string
	Status   string
}

// Question is an extracted + classified + embedded question.
type Question struct {
	ID       string
	DocID    string
	Text     string
	Page     int
	Subject  string
	Topics   []string
	Embedded bool
}

// Topic is a selectable topic with its question count.
type Topic struct {
	Name  string
	Count int
}

// QuizItem is one generated quiz question.
type QuizItem struct {
	ID             string
	SourceID       string
	Question       string
	Options        []string
	CorrectAnswer  int
	SelectedAnswer int
	Topic          string
}

// Quiz is the generated quiz under evaluation.
type Quiz struct {
	ID    string
	Items []QuizItem
}

// Result is the user-visible outcome of the flow.
type Result struct {
	QuizID     string
	Total      int
	Correct    int
	ScorePct   float64
	PerItem    []bool
	WeakTopics []string
}

// Uploader stores the uploaded PDF (Upload PDF → Process PDF).
type Uploader interface {
	Upload(ctx context.Context, in UploadInput) (Document, error)
}

// Extractor pulls question stems out of the stored PDF.
type Extractor interface {
	Extract(ctx context.Context, doc Document) ([]Question, error)
}

// Classifier assigns subject/topics to each extracted question.
type Classifier interface {
	Classify(ctx context.Context, qs []Question) ([]Question, error)
}

// Embedder generates and persists question embeddings.
type Embedder interface {
	Embed(ctx context.Context, qs []Question) ([]Question, error)
}

// TopicSelector lists available topics and resolves the user's pick.
type TopicSelector interface {
	ListTopics(ctx context.Context, qs []Question) ([]Topic, error)
	SelectTopic(ctx context.Context, topics []Topic, want string) (Topic, error)
}

// Retriever fetches the questions backing the quiz for the chosen topic.
type Retriever interface {
	Retrieve(ctx context.Context, qs []Question, topic Topic) ([]Question, error)
}

// QuizGenerator builds the quiz from retrieved source questions.
type QuizGenerator interface {
	Generate(ctx context.Context, sources []Question, topic Topic) (Quiz, error)
}

// Answerer submits answers and grades the quiz.
type Answerer interface {
	Answer(ctx context.Context, quiz Quiz, answers map[string]int) (Result, error)
}

// Stages lists the flow stages in execution order. Tests assert the
// recorded trace equals this order.
var Stages = []string{
	"upload",
	"extract",
	"classify",
	"embed",
	"list-topics",
	"select-topic",
	"retrieve",
	"generate-quiz",
	"answer",
}

// Deps wires one implementation per stage.
type Deps struct {
	Upload   Uploader
	Extract  Extractor
	Classify Classifier
	Embed    Embedder
	Topics   TopicSelector
	Retrieve Retriever
	Quiz     QuizGenerator
	Answer   Answerer
}

// FlowRequest is the scripted user journey input.
type FlowRequest struct {
	// Upload is the PDF payload to upload.
	Upload UploadInput
	// WantTopic is the topic the simulated user selects.
	WantTopic string
	// Answers maps quiz item ID → selected option index.
	Answers map[string]int
}

// Outcome is the observable end state of one flow run.
type Outcome struct {
	Doc    Document
	Quiz   Quiz
	Result Result
	// Trace records the stages executed in order.
	Trace []string
}

// Flow drives the Upload → Results journey across the stage deps.
type Flow struct {
	Deps Deps
}

// Run executes every stage in order, stopping at the first error.
// The returned error always names the failing stage.
func (f *Flow) Run(ctx context.Context, req FlowRequest) (Outcome, error) {
	var out Outcome
	record := func(stage string) { out.Trace = append(out.Trace, stage) }

	if err := ctx.Err(); err != nil {
		return out, fmt.Errorf("e2e: context before upload: %w", err)
	}
	doc, err := f.Deps.Upload.Upload(ctx, req.Upload)
	record("upload")
	if err != nil {
		return out, fmt.Errorf("e2e: stage upload: %w", err)
	}
	out.Doc = doc

	extracted, err := f.Deps.Extract.Extract(ctx, doc)
	record("extract")
	if err != nil {
		return out, fmt.Errorf("e2e: stage extract: %w", err)
	}
	if len(extracted) == 0 {
		return out, fmt.Errorf("e2e: stage extract: no questions extracted")
	}

	classified, err := f.Deps.Classify.Classify(ctx, extracted)
	record("classify")
	if err != nil {
		return out, fmt.Errorf("e2e: stage classify: %w", err)
	}

	embedded, err := f.Deps.Embed.Embed(ctx, classified)
	record("embed")
	if err != nil {
		return out, fmt.Errorf("e2e: stage embed: %w", err)
	}

	topics, err := f.Deps.Topics.ListTopics(ctx, embedded)
	record("list-topics")
	if err != nil {
		return out, fmt.Errorf("e2e: stage list-topics: %w", err)
	}
	if len(topics) == 0 {
		return out, fmt.Errorf("e2e: stage list-topics: no topics available")
	}
	selected, err := f.Deps.Topics.SelectTopic(ctx, topics, req.WantTopic)
	record("select-topic")
	if err != nil {
		return out, fmt.Errorf("e2e: stage select-topic: %w", err)
	}

	sources, err := f.Deps.Retrieve.Retrieve(ctx, embedded, selected)
	record("retrieve")
	if err != nil {
		return out, fmt.Errorf("e2e: stage retrieve: %w", err)
	}
	if len(sources) == 0 {
		return out, fmt.Errorf("e2e: stage retrieve: no questions for topic %q", selected.Name)
	}

	quiz, err := f.Deps.Quiz.Generate(ctx, sources, selected)
	record("generate-quiz")
	if err != nil {
		return out, fmt.Errorf("e2e: stage generate-quiz: %w", err)
	}
	if len(quiz.Items) == 0 {
		return out, fmt.Errorf("e2e: stage generate-quiz: empty quiz")
	}
	out.Quiz = quiz

	result, err := f.Deps.Answer.Answer(ctx, quiz, req.Answers)
	record("answer")
	if err != nil {
		return out, fmt.Errorf("e2e: stage answer: %w", err)
	}
	out.Result = result

	return out, nil
}

// ValidateUploadInput enforces the minimal upload contract shared by the
// fake and live uploaders: a .pdf filename and non-empty %PDF- content.
func ValidateUploadInput(in UploadInput) error {
	if strings.TrimSpace(in.Filename) == "" {
		return fmt.Errorf("filename is required")
	}
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(in.Filename)), ".pdf") {
		return fmt.Errorf("only .pdf uploads are accepted")
	}
	if len(in.Content) == 0 {
		return fmt.Errorf("empty file")
	}
	if !strings.HasPrefix(string(in.Content), "%PDF-") {
		return fmt.Errorf("not a PDF document")
	}
	return nil
}
