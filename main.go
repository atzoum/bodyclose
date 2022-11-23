package main

import (
	"github.com/atzoum/reuseconn/reuseconn"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(reuseconn.Analyzer) }
