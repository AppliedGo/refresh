package main

import "testing"

// This file is required for command-line testing because `go test` requires that tests be in a file that ends with `_test.go`.
//
// The Go playground happily runs the `refresh.go` file and executes the test in there when it finds no main function.

// Wrapper function to call the test function in `refresh.go`.
func Test_get(t *testing.T) {
	TestToken_get(t)
}
