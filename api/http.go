package http

import "net/http"

/*
Formats:

param value

paramlist
	param value
	param value
	param value
*/

type params struct {
	method  string
	query   map[string][]string
	headers map[string][]string
}

func buildRequest(urlStr string, p params) (*http.Request, error) {
	r, err := http.NewRequest(p.method, urlStr, nil)
	if err != nil {
		return nil, err
	}

	for k, vs := range p.headers {
		for v := range vs {
			r.Header.Add(k, v)
		}
	}

	q := r.URL.Query()
	for k, vs := range q {
		for v := range vs {
			q.Add(k, v)
		}
	}

	return r, nil
}
