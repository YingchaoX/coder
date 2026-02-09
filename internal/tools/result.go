package tools

import (
	"encoding/json"
	"fmt"
)

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"ok":false,"error":"marshal result: %s"}`, err.Error())
	}
	return string(data)
}
