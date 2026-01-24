package events

import (
	"context"
	"fmt"
)

// PipelineStage defines a single stage in the event processing pipeline
// Following Interface Segregation Principle (ISP) - each stage has one responsibility
type PipelineStage interface {
	// Name returns the stage name for debugging and logging
	Name() string

	// Process handles the event and returns any error
	Process(ctx context.Context, event *ParsedEvent) error
}

// EventPipeline coordinates multiple stages for event processing
// Following Single Responsibility Principle (SRP) - only coordinates stage execution
type EventPipeline struct {
	stages []PipelineStage
}

// NewEventPipeline creates a new event processing pipeline
func NewEventPipeline() *EventPipeline {
	return &EventPipeline{
		stages: make([]PipelineStage, 0),
	}
}

// AddStage adds a processing stage to the pipeline
func (p *EventPipeline) AddStage(stage PipelineStage) *EventPipeline {
	p.stages = append(p.stages, stage)
	return p
}

// Execute runs all stages in sequence
func (p *EventPipeline) Execute(ctx context.Context, event *ParsedEvent) error {
	for _, stage := range p.stages {
		if err := stage.Process(ctx, event); err != nil {
			return fmt.Errorf("pipeline stage '%s' failed: %w", stage.Name(), err)
		}
	}
	return nil
}

// HandlerStage executes event handlers
// SRP: Only responsible for dispatching to event handlers
type HandlerStage struct {
	handlers       map[string][]EventHandler
	defaultHandler EventHandler
}

// NewHandlerStage creates a new handler stage
func NewHandlerStage(handlers map[string][]EventHandler, defaultHandler EventHandler) *HandlerStage {
	return &HandlerStage{
		handlers:       handlers,
		defaultHandler: defaultHandler,
	}
}

// Name returns the stage name
func (s *HandlerStage) Name() string {
	return "handler"
}

// Process dispatches the event to registered handlers
func (s *HandlerStage) Process(ctx context.Context, event *ParsedEvent) error {
	handlers := s.handlers[event.EventName]

	for _, handler := range handlers {
		if err := handler.Handle(ctx, event); err != nil {
			return fmt.Errorf("handler error for %s: %w", event.EventName, err)
		}
	}

	// Use default handler if no specific handlers registered
	if len(handlers) == 0 && s.defaultHandler != nil {
		return s.defaultHandler.Handle(ctx, event)
	}

	return nil
}

// StorageStage persists events to storage
// SRP: Only responsible for coordinating storage handlers
type StorageStage struct {
	handlers map[string][]StorageHandler
}

// NewStorageStage creates a new storage stage
func NewStorageStage(handlers map[string][]StorageHandler) *StorageStage {
	return &StorageStage{
		handlers: handlers,
	}
}

// Name returns the stage name
func (s *StorageStage) Name() string {
	return "storage"
}

// Process persists the event using registered storage handlers
func (s *StorageStage) Process(ctx context.Context, event *ParsedEvent) error {
	handlers := s.handlers[event.EventName]

	for _, handler := range handlers {
		if err := handler.Store(ctx, event); err != nil {
			return fmt.Errorf("storage error for %s: %w", event.EventName, err)
		}
	}

	return nil
}

// PublishStage publishes events to EventBus
// SRP: Only responsible for event publication
type PublishStage struct {
	eventBus *EventBus
}

// NewPublishStage creates a new publish stage
func NewPublishStage(eventBus *EventBus) *PublishStage {
	return &PublishStage{
		eventBus: eventBus,
	}
}

// Name returns the stage name
func (s *PublishStage) Name() string {
	return "publish"
}

// Process publishes the event to EventBus
func (s *PublishStage) Process(ctx context.Context, event *ParsedEvent) error {
	if s.eventBus == nil {
		return nil
	}

	sysEvent := NewSystemContractEvent(
		event.ContractAddress,
		SystemContractEventType(event.EventName),
		event.BlockNumber,
		event.TxHash,
		event.LogIndex,
		event.Data,
	)
	s.eventBus.Publish(sysEvent)

	return nil
}

// PipelineBuilder helps construct event processing pipelines
// Following Builder pattern for flexible pipeline construction
type PipelineBuilder struct {
	pipeline *EventPipeline
}

// NewPipelineBuilder creates a new pipeline builder
func NewPipelineBuilder() *PipelineBuilder {
	return &PipelineBuilder{
		pipeline: NewEventPipeline(),
	}
}

// WithHandler adds a handler stage
func (b *PipelineBuilder) WithHandler(handlers map[string][]EventHandler, defaultHandler EventHandler) *PipelineBuilder {
	b.pipeline.AddStage(NewHandlerStage(handlers, defaultHandler))
	return b
}

// WithStorage adds a storage stage
func (b *PipelineBuilder) WithStorage(handlers map[string][]StorageHandler) *PipelineBuilder {
	b.pipeline.AddStage(NewStorageStage(handlers))
	return b
}

// WithPublish adds a publish stage
func (b *PipelineBuilder) WithPublish(eventBus *EventBus) *PipelineBuilder {
	b.pipeline.AddStage(NewPublishStage(eventBus))
	return b
}

// WithCustomStage adds a custom stage
func (b *PipelineBuilder) WithCustomStage(stage PipelineStage) *PipelineBuilder {
	b.pipeline.AddStage(stage)
	return b
}

// Build returns the constructed pipeline
func (b *PipelineBuilder) Build() *EventPipeline {
	return b.pipeline
}
