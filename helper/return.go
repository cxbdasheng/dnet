package helper

import (
	"encoding/json"
	"net/http"
)

// Result Result
type Result struct {
	Status bool        `json:"status"`
	Msg    string      `json:"msg"`
	Data   interface{} `json:"data"`
}

// ReturnError 返回错误信息
func ReturnError(w http.ResponseWriter, msg string) {
	result := &Result{}

	result.Status = false
	result.Msg = msg

	json.NewEncoder(w).Encode(result)
}

// ReturnSuccess 返回成功信息
func ReturnSuccess(w http.ResponseWriter, msg string, data interface{}) {
	result := &Result{}

	result.Status = true
	result.Msg = msg
	result.Data = data
	json.NewEncoder(w).Encode(result)
}
