package kefw2

import (
	"fmt"
	"io/ioutil"
	"net/http"

	log "github.com/sirupsen/logrus"
)

func (s KEFSpeaker) getData(path string) ([]byte, error) {
	client := &http.Client{}

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/getData", s.IPAddress), nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Add("path", path)
	q.Add("roles", "value")
	req.URL.RawQuery = q.Encode()

	log.Debug("Request:", req.URL.String())
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Debug("Response:", resp.StatusCode, resp.Body)
		return nil, fmt.Errorf("HTTP Status Code: %d\n%s", resp.StatusCode, resp.Body)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}
