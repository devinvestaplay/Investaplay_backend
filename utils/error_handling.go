package utils

import "fmt"

// CreateStatus
// status = true|false, code = 0..9 (int), message = ".." (string)
func CreateStatus(status bool, optionalArgs ...interface{}) string {

	type StatusData struct {
		Status  bool   `json:"status"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	// default is 1
	code := 1
	message := ""
	if len(optionalArgs) > 0 {
		if val, ok := optionalArgs[0].(int); ok {
			code = val
		}
	}
	if len(optionalArgs) > 1 {
		if val, ok := optionalArgs[1].(string); ok {
			message = val
		}
	}

	dataJson, err := SerializeObjectToString(&StatusData{status, code, message})
	if err != nil {
		return fmt.Sprintf(`{"status":%t, "code":%d, "message":"%v"}`, false, -1, err.Error())
	}
	return dataJson
}
