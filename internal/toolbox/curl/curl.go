package curl

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Run executes minimal curl
// Syntax: curl [-s] [-H "Header: Val"] [-X POST] [-d "data"] url
func Run(args []string) error {
	var (
		headers = make(http.Header)
		method  = "GET"
		body    string
		url     string
		silent  bool
		timeout = 5 * time.Second
	)

	// Minimal parser
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-s", "--silent":
			silent = true
		case "-X", "--request":
			if i+1 < len(args) {
				method = args[i+1]
				i++
			}
		case "-H", "--header":
			if i+1 < len(args) {
				parts := strings.SplitN(args[i+1], ":", 2)
				if len(parts) == 2 {
					headers.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
				}
				i++
			}
		case "-d", "--data", "--data-raw":
			if i+1 < len(args) {
				body = args[i+1]
				i++
				if method == "GET" {
					method = "POST"
				}
			}
		default:
			if strings.HasPrefix(arg, "-") {
				continue // ignore unknown
			}
			url = arg
		}
	}

	if url == "" {
		return fmt.Errorf("usage: curl [options] url")
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return err
	}
	req.Header = headers

	client := &http.Client{
		Timeout: timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if !silent {
		// curl prints body to stdout. stderr for stats (ignored here)
		// maybe print headers if -v? (not supported)
	}

	_, err = io.Copy(os.Stdout, resp.Body)
	return err
}
