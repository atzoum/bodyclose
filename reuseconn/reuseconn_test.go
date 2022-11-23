package reuseconn_test

import (
	"testing"

	"github.com/atzoum/reuseconn/reuseconn"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestA(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, reuseconn.Analyzer, "a", "util")
}

func TestB(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, reuseconn.Analyzer, "b", "util")
}
