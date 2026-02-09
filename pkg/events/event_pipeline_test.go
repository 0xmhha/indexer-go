package events

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestNewEventPipeline(t *testing.T) {
	pipeline := NewEventPipeline()
	if pipeline == nil {
		t.Fatal("expected non-nil pipeline")
	}
	if len(pipeline.stages) != 0 {
		t.Error("expected empty stages")
	}
}

func TestEventPipeline_AddStage(t *testing.T) {
	pipeline := NewEventPipeline()
	stage := NewHandlerStage(nil, nil)

	result := pipeline.AddStage(stage)
	if result != pipeline {
		t.Error("AddStage should return pipeline for chaining")
	}
	if len(pipeline.stages) != 1 {
		t.Errorf("expected 1 stage, got %d", len(pipeline.stages))
	}
}

func TestEventPipeline_Execute_EmptyPipeline(t *testing.T) {
	pipeline := NewEventPipeline()
	ctx := context.Background()
	event := &ParsedEvent{EventName: "Transfer"}

	if err := pipeline.Execute(ctx, event); err != nil {
		t.Fatalf("expected no error for empty pipeline: %v", err)
	}
}

func TestEventPipeline_Execute_MultipleStages(t *testing.T) {
	ctx := context.Background()
	event := &ParsedEvent{EventName: "Transfer", BlockNumber: 100}

	handler := &mockEventHandler{eventName: "Transfer"}
	storageHandler := &mockStorageHandler{eventTypes: []string{"Transfer"}}

	pipeline := NewEventPipeline()
	pipeline.AddStage(NewHandlerStage(map[string][]EventHandler{"Transfer": {handler}}, nil))
	pipeline.AddStage(NewStorageStage(map[string][]StorageHandler{"Transfer": {storageHandler}}))

	if err := pipeline.Execute(ctx, event); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(handler.handled) != 1 {
		t.Errorf("expected handler to process event")
	}
	if len(storageHandler.stored) != 1 {
		t.Errorf("expected storage to store event")
	}
}

func TestEventPipeline_Execute_StageError(t *testing.T) {
	ctx := context.Background()
	event := &ParsedEvent{EventName: "Transfer"}

	handler := &mockEventHandler{eventName: "Transfer", handleErr: fmt.Errorf("handler failed")}

	pipeline := NewEventPipeline()
	pipeline.AddStage(NewHandlerStage(map[string][]EventHandler{"Transfer": {handler}}, nil))

	err := pipeline.Execute(ctx, event)
	if err == nil {
		t.Error("expected error from failing stage")
	}
}

// ========== Stage Tests ==========

func TestHandlerStage_Name(t *testing.T) {
	stage := NewHandlerStage(nil, nil)
	if stage.Name() != "handler" {
		t.Errorf("expected 'handler', got '%s'", stage.Name())
	}
}

func TestHandlerStage_Process_NoHandlers(t *testing.T) {
	stage := NewHandlerStage(make(map[string][]EventHandler), nil)
	ctx := context.Background()
	event := &ParsedEvent{EventName: "Transfer"}

	if err := stage.Process(ctx, event); err != nil {
		t.Fatalf("expected no error: %v", err)
	}
}

func TestHandlerStage_Process_DefaultHandler(t *testing.T) {
	defaultHandler := &mockEventHandler{eventName: "*"}
	stage := NewHandlerStage(make(map[string][]EventHandler), defaultHandler)
	ctx := context.Background()
	event := &ParsedEvent{EventName: "Unknown"}

	if err := stage.Process(ctx, event); err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if len(defaultHandler.handled) != 1 {
		t.Error("expected default handler to be called")
	}
}

func TestStorageStage_Name(t *testing.T) {
	stage := NewStorageStage(nil)
	if stage.Name() != "storage" {
		t.Errorf("expected 'storage', got '%s'", stage.Name())
	}
}

func TestStorageStage_Process(t *testing.T) {
	handler := &mockStorageHandler{eventTypes: []string{"Transfer"}}
	stage := NewStorageStage(map[string][]StorageHandler{"Transfer": {handler}})
	ctx := context.Background()
	event := &ParsedEvent{EventName: "Transfer"}

	if err := stage.Process(ctx, event); err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if len(handler.stored) != 1 {
		t.Error("expected event to be stored")
	}
}

func TestStorageStage_Process_Error(t *testing.T) {
	handler := &mockStorageHandler{
		eventTypes: []string{"Transfer"},
		storeErr:   fmt.Errorf("storage failed"),
	}
	stage := NewStorageStage(map[string][]StorageHandler{"Transfer": {handler}})
	ctx := context.Background()
	event := &ParsedEvent{EventName: "Transfer"}

	if err := stage.Process(ctx, event); err == nil {
		t.Error("expected storage error")
	}
}

func TestPublishStage_Name(t *testing.T) {
	stage := NewPublishStage(nil)
	if stage.Name() != "publish" {
		t.Errorf("expected 'publish', got '%s'", stage.Name())
	}
}

func TestPublishStage_Process_NilBus(t *testing.T) {
	stage := NewPublishStage(nil)
	ctx := context.Background()
	event := &ParsedEvent{EventName: "Transfer", Data: make(map[string]interface{})}

	// Should not error with nil eventBus
	if err := stage.Process(ctx, event); err != nil {
		t.Fatalf("expected no error with nil bus: %v", err)
	}
}

func TestPublishStage_Process_WithBus(t *testing.T) {
	bus := NewEventBus(100, 100)
	stage := NewPublishStage(bus)
	ctx := context.Background()
	event := &ParsedEvent{
		EventName:       "Transfer",
		ContractAddress: common.HexToAddress("0x1"),
		BlockNumber:     100,
		Data:            make(map[string]interface{}),
	}

	// Should not panic or error with a valid bus
	if err := stage.Process(ctx, event); err != nil {
		t.Fatalf("Process error: %v", err)
	}
}

// ========== PipelineBuilder Tests ==========

func TestNewPipelineBuilder(t *testing.T) {
	builder := NewPipelineBuilder()
	if builder == nil {
		t.Fatal("expected non-nil builder")
	}
}

func TestPipelineBuilder_Build(t *testing.T) {
	pipeline := NewPipelineBuilder().
		WithHandler(nil, nil).
		WithStorage(nil).
		WithPublish(nil).
		Build()

	if pipeline == nil {
		t.Fatal("expected non-nil pipeline")
	}
	if len(pipeline.stages) != 3 {
		t.Errorf("expected 3 stages, got %d", len(pipeline.stages))
	}
}

func TestPipelineBuilder_WithCustomStage(t *testing.T) {
	customStage := NewHandlerStage(nil, nil)

	pipeline := NewPipelineBuilder().
		WithCustomStage(customStage).
		Build()

	if len(pipeline.stages) != 1 {
		t.Errorf("expected 1 stage, got %d", len(pipeline.stages))
	}
}

func TestPipelineBuilder_Chaining(t *testing.T) {
	builder := NewPipelineBuilder()
	result := builder.WithHandler(nil, nil)
	if result != builder {
		t.Error("WithHandler should return builder for chaining")
	}
	result = builder.WithStorage(nil)
	if result != builder {
		t.Error("WithStorage should return builder for chaining")
	}
	result = builder.WithPublish(nil)
	if result != builder {
		t.Error("WithPublish should return builder for chaining")
	}
}
