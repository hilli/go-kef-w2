package kefw2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

type KEFPostRequest struct {
	Path  string           `json:"path"`
	Roles string           `json:"roles"`
	Value *json.RawMessage `json:"value"`
}

func (s KEFSpeaker) handleConnectionError(err error) error {
	if err == nil {
		return nil
	}

	errStr := err.Error()
	if nerr, ok := err.(net.Error); ok {
		if nerr.Timeout() {
			return fmt.Errorf("Connection timed out when trying to reach speaker at %s. Please check if the speaker is available and responsive.", s.IPAddress)
		}
		fmt.Println("nerr:", nerr)
	}
	// fmt.Println(
	if strings.Contains(errStr, "connection refused") {
		return fmt.Errorf("Unable to connect to speaker at %s. Please ensure the speaker is powered on and connected to the network.", s.IPAddress)
	}
	if strings.Contains(errStr, "timeout") {
		return fmt.Errorf("Connection timed out when trying to reach speaker at %s. Please check if the speaker is available and responsive.", s.IPAddress)
	}
	if strings.Contains(errStr, "no such host") {
		return fmt.Errorf("Could not find speaker at %s. Please check if the IP address is correct", s.IPAddress)
	}
	return fmt.Errorf("Connection error: %v", err)
}

func (s KEFSpeaker) getData(path string) ([]byte, error) {
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
		return nil, s.handleConnectionError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		log.Debug("Response:", resp.StatusCode, resp.Body)
		return nil, fmt.Errorf("HTTP Status Code: %d\n%s", resp.StatusCode, resp.Body)
	}

	return body, nil
}

func (s KEFSpeaker) getAllData(path string) ([]byte, error) {
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
	q.Add("roles", "@all")
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return nil, s.handleConnectionError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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
		return nil, s.handleConnectionError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Debug("Response:", resp.StatusCode, resp.Body)
		return nil, fmt.Errorf("HTTP Status Code: %d\n%s", resp.StatusCode, resp.Body)
	}

	body, err := io.ReadAll(resp.Body)
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
		return s.handleConnectionError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Debug("Response:", resp.StatusCode, resp.Body)
		return fmt.Errorf("HTTP Status Code: %d\n%s", resp.StatusCode, resp.Body)
	}

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
		myValue = string(value.(Source))
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
		return s.handleConnectionError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Debug("Response:", resp.StatusCode, resp.Body)
		return fmt.Errorf("HTTP Status Code: %d\n%s", resp.StatusCode, resp.Body)
	}

	return nil
}
