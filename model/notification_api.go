package model

type NotificationForm struct {
	Name          string `json:"name,omitempty"`
	URL           string `json:"url,omitempty"`
	RequestMethod int    `json:"request_method,omitempty"`
	RequestType   int    `json:"request_type,omitempty"`
	RequestHeader string `json:"request_header,omitempty"`
	RequestBody   string `json:"request_body,omitempty"`
	VerifySSL     bool   `json:"verify_ssl,omitempty"`
	SkipCheck     bool   `json:"skip_check,omitempty"`
}
