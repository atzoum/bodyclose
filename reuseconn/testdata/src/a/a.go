package a // want package:`responseDisposers:disposeBody`

import (
	"io"
	"net/http"
	"util"
)

func disposeAfterIf(url string) {
	var res *http.Response
	var err error
	if res, err = http.DefaultClient.Get(url); err != nil {
		return
	}
	util.DisposeResponse(res)
}

func disposeFunctionCall(url string) {
	r, err := http.DefaultClient.Get(url)
	if err != nil {
		return
	}
	util.DisposeResponse(r)
}

func closeFunctionCall(url string) {
	r, err := http.DefaultClient.Get(url) // want `response body must be disposed properly in a single function read to completion and closed`
	if err != nil {
		return
	}
	util.CloseResponse(r)
}

func readFunctionCall(url string) {
	r, err := http.DefaultClient.Get(url) // want `response body must be disposed properly in a single function read to completion and closed`
	if err != nil {
		return
	}
	util.ReadResponse(r)

}

func doRequestWithoutDispose() {
	_, _ = doRequestWithoutClose() // want `response body must be disposed properly in a single function read to completion and closed`
}

func doRequestWithLocalDispose() {
	r, err := doRequestWithoutClose()
	if err == nil {
		disposeBody(r.Body)
	}
}

func disposeBody(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func doRequestWithoutClose() (*http.Response, error) {
	return http.Get("https://example.com")
}
