package dto

type InvokerRequest struct {
	ServiceName       string `json:"service_name"`
	Method            string `json:"method"`
	Headers           string `json:"headers"`
	RequestData       string `json:"request_data"`
	RequestTimes      int    `json:"request_times"`
	RequestConcurrent int    `json:"request_concurrent"`
	RequestTimeout    int    `json:"request_timeout"`
}
