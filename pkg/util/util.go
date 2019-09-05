package util

import (
	"encoding/json"
	"fmt"
)

// NVL returns def if str is null
func NVL(str string, def string) string {
	if len(str) == 0 {
		return def
	}
	return str
}

func PrettyPrint(v interface{}) (err error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		fmt.Println(string(b))
	}
	return
}
