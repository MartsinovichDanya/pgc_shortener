package audit

import (
    "bytes"
    "encoding/json"
    "net/http"
    "time"
)

type HTTPObserver struct {
    url    string
    client *http.Client
}

func NewHTTPObserver(url string) *HTTPObserver {
    return &HTTPObserver{
        url: url,
        client: &http.Client{Timeout: 5 * time.Second},
    }
}

func (h *HTTPObserver) Notify(event Event) {
    data, err := json.Marshal(event)
    if err != nil {
        return
    }

    req, err := http.NewRequest(http.MethodPost, h.url, bytes.NewBuffer(data))
    if err != nil {
        return
    }
    req.Header.Set("Content-Type", "application/json")

    _, _ = h.client.Do(req)
}
