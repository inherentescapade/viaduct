package discord

import (
	"io"
	"net/http"
)

type discordHTTP struct {
	client *http.Client
	token  string
}

func (d *discordHTTP) newRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", d.token)
	return req, nil
}

func (d *discordHTTP) Do(method, url string) (*http.Response, error) {
	req, err := d.newRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	return d.client.Do(req)
}
