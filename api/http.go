package api

import (
	"io"
	"log"
	"net/http"
)

/*
Formats:

param key (value)
*/

type Params struct {
	URL     string
	Method  string
	Query   map[string][]string
	Headers map[string][]string
}

func buildRequest(p Params) (*http.Request, error) {
	r, err := http.NewRequest(p.Method, p.URL, nil)
	if err != nil {
		return nil, err
	}

	for k, vs := range p.Headers {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}

	q := r.URL.Query()
	for k, vs := range q {
		for _, v := range vs {
			q.Add(k, v)
		}
	}

	return r, nil
}

func DoRequest(p Params) (io.ReadCloser, error) {
	log.Printf("HTTP %#v %#v", p.Method, p.URL)
	req, err := buildRequest(p)
	if err != nil {
		return nil, err
	}

	var c http.Client

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}
