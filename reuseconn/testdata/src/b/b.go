package b

import (
	"net/http"
	"util"
)

func deferredDispose(url string) {
	r, _ := http.DefaultClient.Get(url)
	defer util.DisposeResponse(r)
}

func deferredClose(url string) {
	r, _ := http.DefaultClient.Get(url) // want `response body must be disposed properly in a single function read to completion and closed`
	defer util.CloseResponse(r)
}

func deferredAnonDispose(url string) {
	r, _ := http.DefaultClient.Get(url)
	defer func() { util.DisposeResponse(r) }()
}
