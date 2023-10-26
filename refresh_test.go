package main

import (
	"context"
	"io"
	"log"
	"testing"
)

// This file is required for command-line testing because `go test` requires that tests be in a file that ends with `_test.go`.
//
// The Go playground happily runs the `refresh.go` file and executes the test in there when it finds no main function.

// Wrapper function to call the test function in `refresh.go`.
func TestWrapTokenGet(t *testing.T) {
	TestTokenGet(t)
}

func TestWrapMTokenGet(t *testing.T) {
	TestMTokenGet(t)
}

func BenchmarkToken_Get(b *testing.B) {
	log.SetOutput(io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	t := NewToken(ctx, authFunc)
	defer cancel()
	for i := 0; i < b.N; i++ {
		_, _ = t.Get()
	}
}
func BenchmarkMToken_Get(b *testing.B) {
	log.SetOutput(io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	t := NewToken(ctx, authFunc)
	defer cancel()
	for i := 0; i < b.N; i++ {
		_, _ = t.Get()
	}
}
