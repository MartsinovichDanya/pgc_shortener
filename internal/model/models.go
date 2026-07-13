//go:generate easyjson -all $GOFILE

package model

//easyjson:json
type Request struct {
	Url string `json:"url"`
}

type Response struct {
	Result string `json:"result"`
}
