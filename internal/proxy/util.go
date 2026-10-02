package proxy

import (
	"encoding/json"
	"net/http"
)

func wantsStreamBody(body []byte) bool {
	return bytesContains(body, []byte(`"stream":true`)) || bytesContains(body, []byte(`"stream": true`))
}

func bytesContains(hay, needle []byte) bool {
	return len(needle) > 0 && len(hay) >= len(needle) && indexOfBytes(hay, needle) >= 0
}

func indexOfBytes(hay, needle []byte) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		_, _ = w.Write([]byte(`{}`))
		return
	}
	_, _ = w.Write(b)
}
