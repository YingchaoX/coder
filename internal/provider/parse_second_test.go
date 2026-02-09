package provider

import (
	"encoding/json"
	"testing"
)

func TestSecondLineParse(t *testing.T) {
	// Go 的 encoding/json 中：\u0022 解析为 " 后，该 " 会终止字符串，导致后续 } 与 ] 暴露
	// 因此 "DME.md\u0022}" 被解析为 key 对应 "DME.md"，然后遇到 }]... 报错
	var s struct {
		A string `json:"arguments"`
	}
	payload := `{"arguments":"DME.md\u0022}"}`
	err := json.Unmarshal([]byte(payload), &s)
	t.Logf("err: %v", err)
	if err == nil {
		t.Logf("arguments=%q len=%d", s.A, len(s.A))
	}
}
