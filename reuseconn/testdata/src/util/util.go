package util // want package:`responseDisposers:DisposeBody,DisposeResponse`

import (
	"io"
	"net/http"
)

// DisposeBody both reads and closes the body.
func DisposeBody(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

// CloseBody only closes the body.
func CloseBody(body io.ReadCloser) {
	_ = body.Close()
}

// ReadBody only reads the body.
func ReadBody(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
}

// DisposeResponse both reads and closes the response's body.
func DisposeResponse(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// CloseResponse only closes the response's body.
func CloseResponse(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

// ReadResponse only reads the response's body.
func ReadResponse(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
}
