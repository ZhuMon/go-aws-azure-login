package main

import (
	"errors"
	"strings"
	"testing"
)

func TestRunProfileLoop_StopsOnFirstErrorWhenContinueOnErrorFalse(t *testing.T) {
	var calls []string
	step := func(p string) error {
		calls = append(calls, p)
		if p == "bad" {
			return errors.New("boom")
		}
		return nil
	}

	results := runProfileLoop([]string{"good1", "bad", "good2"}, false, nil, step)

	if got := strings.Join(calls, ","); got != "good1,bad" {
		t.Fatalf("expected loop to stop after 'bad', got call sequence %q", got)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 recorded results, got %d", len(results))
	}
	if results[1].err == nil {
		t.Fatalf("expected the second result to record an error, got nil")
	}
}

func TestRunProfileLoop_ContinuesPastErrorWhenFlagSet(t *testing.T) {
	var calls []string
	step := func(p string) error {
		calls = append(calls, p)
		if p == "bad" {
			return errors.New("boom")
		}
		return nil
	}

	results := runProfileLoop([]string{"good1", "bad", "good2"}, true, nil, step)

	if got := strings.Join(calls, ","); got != "good1,bad,good2" {
		t.Fatalf("expected loop to continue past 'bad', got %q", got)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 recorded results, got %d", len(results))
	}
	if results[0].err != nil || results[2].err != nil {
		t.Fatalf("expected good1 and good2 to succeed, got %v / %v", results[0].err, results[2].err)
	}
	if results[1].err == nil {
		t.Fatalf("expected 'bad' to record an error")
	}
}

func TestRunProfileLoop_ExitsWhenCancelled(t *testing.T) {
	cancel := make(chan struct{})
	close(cancel)

	step := func(p string) error {
		t.Fatalf("step should not be called when context is already cancelled, but ran for %q", p)
		return nil
	}

	results := runProfileLoop([]string{"a", "b"}, true, cancel, step)
	if len(results) != 0 {
		t.Fatalf("expected no results when cancelled before first iteration, got %d", len(results))
	}
}

func TestFinalizeBatch_ReturnsErrorWhenAnyProfileFailed(t *testing.T) {
	results := []profileResult{
		{name: "good", err: nil},
		{name: "bad", err: errors.New("nope")},
	}
	err := finalizeBatch(results, 2)
	if err == nil {
		t.Fatalf("expected aggregate error when at least one profile failed")
	}
}

func TestFinalizeBatch_ReturnsNilWhenAllSucceeded(t *testing.T) {
	results := []profileResult{
		{name: "good1", err: nil},
		{name: "good2", err: nil},
	}
	if err := finalizeBatch(results, 2); err != nil {
		t.Fatalf("expected nil error when all profiles succeeded, got %v", err)
	}
}
