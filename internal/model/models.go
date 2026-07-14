package model

//go:generate easyjson -all $GOFILE

//easyjson:json
type Request struct {
	URL string `json:"url"`
}

type Response struct {
	Result string `json:"result"`
}
