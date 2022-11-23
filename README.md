# reuseconn

`reuseconn` is a static analysis tool which checks whether `res.Body` is correctly closed and read so that the underlying TCP connection can be reused.

## Install

You can get `reuseconn` by `go get` command.

```bash
$ go get -u github.com/atzoum/reuseconn
```

## How to use

`reuseconn` run with `go vet` as below.

```bash
$ go vet -vettool=$(which reuseconn) github.com/atzoum/go_api/...
# github.com/atzoum/go_api
internal/httpclient/httpclient.go:13:13: response body must be closed
```

But it cannot accept some options such as `--tags`.

```bash
$ reuseconn github.com/atzoum/go_api/...
~/go/src/github.com/atzoum/api/internal/httpclient/httpclient.go:13:13: response body must be closed
```

## Analyzer

`reuseconn` validates whether a [*net/http.Response](https://golang.org/pkg/net/http/#Response) of HTTP request is properly disposed after used. E.g.

```go
func main() {
	resp, err := http.Get("http://example.com/") // Wrong case
	if err != nil {
		// handle error
	}
	body, err := ioutil.ReadAll(resp.Body)
}
```

The above code is wrong. You must properly dispose `resp.Body` when done. To avoid a scenario where you forget to read the body in some scenarios and the connection is not reused, `reuseconn` enforces disposal to be performed within a single function which performs both operations.

```go

func disposeResponseBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		// if the body is already read in some scenarios, the below operation becomes a no-op
		_, _ = io.Copy(io.Discard, resp.Body) 
		_ = resp.Body.Close()
	}
}

func main() {
	resp, err := http.Get("http://example.com/")
	defer closeResponseBody(resp) // OK
	if err != nil {
		// handle error
	}
	if resp2.StatusCode == http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
	}
}
```

In the [GoDoc of http.Response](https://pkg.go.dev/net/http#Response) this rule is clearly described.

> It is the caller's responsibility to close Body. The default HTTP client's Transport may not reuse HTTP/1.x "keep-alive" TCP connections if the Body is not read to completion and closed.
