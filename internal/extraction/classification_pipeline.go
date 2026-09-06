package extraction

import (
	"context"
	"fmt"

	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// ClassificationPipeline adapts ExtractionService to the document
// classification HTTP contract.
type ClassificationPipeline struct {
	service      *ExtractionService
	documentRepo documents.Repository
	questionRepo questions.Repository
}

func NewClassificationPipeline(service *ExtractionService, documentRepo documents.Repository, questionRepo questions.Repository) (*ClassificationPipeline, error) {
	if service == nil || documentRepo == nil || questionRepo == nil {
		return nil, fmt.Errorf("extraction: classification pipeline dependencies are required")
	}
	return &ClassificationPipeline{service: service, documentRepo: documentRepo, questionRepo: questionRepo}, nil
}

func (p *ClassificationPipeline) ClassifyDocument(ctx context.Context, documentID string) (int, error) {
	if _, err := p.documentRepo.GetByID(ctx, documentID); err != nil {
		return 0, err
	}
	if err := p.service.ClassifyDocument(ctx, documentID); err != nil {
		return 0, err
	}
	count := 0
	for offset := 0; ; offset += 100 {
		batch, err := p.questionRepo.List(ctx, questions.Filter{DocumentID: documentID, Limit: 100, Offset: offset})
		if err != nil {
			return count, fmt.Errorf("extraction: count classified questions: %w", err)
		}
		count += len(batch)
		if len(batch) < 100 {
			return count, nil
		}
	}
}
