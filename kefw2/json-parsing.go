package kefw2

import (
	"encoding/json"
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
	var jsonData []map[string]interface{}
	err2 = json.Unmarshal(data, &jsonData)
	if err2 != nil {
		return "", err
	}
	value = jsonData[0][key].(string)
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
