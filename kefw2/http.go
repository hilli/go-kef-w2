package kefw2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

type KEFPostRequest struct {
	Path  string           `json:"path"`
	Roles string           `json:"roles"`
	Value *json.RawMessage `json:"value"`
}

func (s KEFSpeaker) getData(path string) ([]byte, error) {
	// log.SetLevel(log.DebugLevel)
	client := &http.Client{}
	client.Timeout = 1.0 * time.Second

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/getData", s.IPAddress), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")

	q := req.URL.Query()
	q.Add("path", path)
	q.Add("roles", "value")
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		log.Debug("Response:", resp.StatusCode, resp.Body)
		return nil, fmt.Errorf("HTTP Status Code: %d\n%s", resp.StatusCode, resp.Body)
	}

	return body, nil
}

func (s KEFSpeaker) getRows(path string, params map[string]string) ([]byte, error) {
	client := &http.Client{}
	client.Timeout = 1.0 * time.Second

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/getRows", s.IPAddress), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")

	q := req.URL.Query()
	q.Add("path", path) // Always add the path
	for key, value := range params {
		q.Add(key, value)
	}
	req.URL.RawQuery = q.Encode()

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

func (s KEFSpeaker) setActivate(path, item, value string) error {
	client := &http.Client{}
	client.Timeout = 1.0 * time.Second

	jsonStr, _ := json.Marshal(
		map[string]string{
			item: value,
		})
	rawValue := json.RawMessage(jsonStr)

	reqbody, _ := json.Marshal(KEFPostRequest{
		Path:  path,
		Roles: "activate",
		Value: &rawValue,
	})

	req, err := http.NewRequest("POST", fmt.Sprintf("http://%s/api/setData", s.IPAddress), bytes.NewBuffer(reqbody))
	if err != nil {
		return err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Debug("Response:", resp.StatusCode, resp.Body)
		return fmt.Errorf("HTTP Status Code: %d\n%s", resp.StatusCode, resp.Body)
	}

	// body, err := ioutil.ReadAll(resp.Body)
	// if err != nil {
	// 	return nil, err
	// }

	return nil
}

func (s KEFSpeaker) setTypedValue(path string, value any) error {
	client := &http.Client{}
	client.Timeout = 1.0 * time.Second

	var myType string
	var myValue string
	switch theType := value.(type) {
	case int:
		myType = "i32_"
		myValue = fmt.Sprintf("%d", value.(int))
	case string:
		myType = "string_"
		myValue = fmt.Sprintf("\"%s\"", value.(string))
	case bool:
		myType = "bool_"
		myValue = fmt.Sprintf("%t", value.(bool))
	case Source:
		myType = "kefPhysicalSource"
		myValue = fmt.Sprintf("\"%s\"", value.(Source))
	case SpeakerStatus:
		myType = "kefSpeakerStatus"
		myValue = fmt.Sprintf("\"%s\"", value.(SpeakerStatus))
	case CableMode:
		myType = "kefCableMode"
		myValue = fmt.Sprintf("\"%s\"", value.(CableMode))
	default:
		return fmt.Errorf("type %s is not supported", theType)
	}

	// Build the JSON
	jsonStr, _ := json.Marshal(
		map[string]string{
			"type": myType,
			myType: myValue,
		})
	rawValue := json.RawMessage(jsonStr)
	pr := KEFPostRequest{
		Path:  path,
		Roles: "value",
		Value: &rawValue,
	}

	reqbody, _ := json.MarshalIndent(pr, "", "  ")
	req, err := http.NewRequest("POST", fmt.Sprintf("http://%s/api/setData", s.IPAddress), bytes.NewBuffer(reqbody))
	if err != nil {
		return err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Debug("Response:", resp.StatusCode, resp.Body)
		return fmt.Errorf("HTTP Status Code: %d\n%s", resp.StatusCode, resp.Body)
	}

	// body, err := ioutil.ReadAll(resp.Body)
	// if err != nil {
	// 	return nil, err
	// }

	return nil
}
