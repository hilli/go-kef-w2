package kefw2

import (
	"encoding/json"
	"errors"
)

func JSONStringValue(data []byte, err error) (value string, err2 error) {
	if err != nil {
		return "", err
	}
	var jsonData []map[string]interface{}
	err2 = json.Unmarshal(data, &jsonData)
	if err2 != nil {
		return "", err
	}
	value = jsonData[0]["string_"].(string)
	return value, nil
}

func JSONStringValueByKey(data []byte, key string, err error) (value string, err2 error) {
	if err != nil {
		return "", err
	}
	var jsonData []map[string]string
	err2 = json.Unmarshal(data, &jsonData)
	if err2 != nil {
		return "", err
	}
	value = jsonData[0]["value"]
	return value, nil
}

func JSONIntValue(data []byte, err error) (value int, err2 error) {
	if err != nil {
		return 0, err
	}
	var jsonData []map[string]interface{}
	err2 = json.Unmarshal(data, &jsonData)
	if err2 != nil {
		return 0, err
	}
	fvalue, _ := jsonData[0]["i32_"].(float64)
	return int(fvalue), nil
}

func JSONUnmarshalValue(data []byte, err error) (value any, err2 error) {
	// Easing the call chain
	if err != nil {
		return 0, err
	}

	// Unmarshal the JSON data into a map of strings to any
	var jsonData []map[string]any
	err2 = json.Unmarshal(data, &jsonData)
	if err2 != nil {
		return 0, err
	}
	// Locate the value and set the type
	tvalue := jsonData[0]["type"].(string)
	switch tvalue {
	case "i32_":
		value = jsonData[0]["i32_"].(int)
	case "i64_":
		value = jsonData[0]["i64_"].(int)
	case "string_":
		value = jsonData[0]["string_"].(string)
	case "bool_":
		value = jsonData[0]["bool_"].(bool)
	case "kefPhysicalSource":
		value = Source(jsonData[0]["kefPhysicalSource"].(string))
	case "kefSpeakerStatus":
		value = SpeakerStatus(jsonData[0]["kefSpeakerStatus"].(string))
	case "kefCableMode":
		value = CableMode(jsonData[0]["kefCableMode"].(string))
	default:
		return nil, errors.New("Unknown type: " + tvalue)
	}
	return value, nil
}