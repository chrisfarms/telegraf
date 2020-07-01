package cloudfoundry

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

type HTTPClient struct {
	tokenSource oauth2.TokenSource
	client      *http.Client
}

func (l *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	token, err := getTokenWithRetry(l.tokenSource, 3, 1*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %s", err)
	}

	authHeader := fmt.Sprintf("bearer %s", token.AccessToken)
	req.Header.Set("Authorization", authHeader)

	return l.client.Do(req)
}

func getTokenWithRetry(tokenSource oauth2.TokenSource, maxRetries int, fallOffSeconds time.Duration) (*oauth2.Token, error) {
	var (
		i     int
		token *oauth2.Token
		err   error
	)

	for i = 0; i < maxRetries; i++ {
		token, err = tokenSource.Token()

		if err != nil {
			log.Printf("getting token failed (attempt %d of %d). Retrying. Error: %s", i+1, maxRetries, err.Error())

			sleep := time.Duration(fallOffSeconds.Seconds() * float64(i+1))
			time.Sleep(sleep)
			continue
		}
		return token, nil
	}

	return token, err
}
