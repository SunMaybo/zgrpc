package dto

type ServiceResponse struct {
	ServiceName string `json:"service_name"`
}
type MethodResponse struct {
	MethodName string `json:"method"`
}

type HistoryResponse struct {
	Description     string `json:"description"`
	Interval        string `json:"interval"`
	Request         string `json:"request"`
	RequestHeaders  string `json:"request_headers"`
	Trailers        string `json:"trailers"`
	Response        string `json:"response"`
	ResponseHeaders string `json:"response_headers"`
	Status          int    `json:"status"`
}

type InvokerResponse struct {
	Data       string `json:"data"`
	Headers    string `json:"headers"`
	Metrics    Metric `json:"metrics"`
	Trailers   string `json:"trailers"`
	Status     int    `json:"status"`
	StatusName string `json:"status_name"`
}

type Metric struct {
	Success     int64  `json:"success"`
	Failed      int64  `json:"failed"`
	SuccessRate string `json:"success_rate"`
	ElapsedTime string `json:"elapsed_time"`
	AverageTime string `json:"average_time"`
}

type HistoryData struct {
	Key      string           `json:"key"`
	Request  InvokerRequest   `json:"request"`
	Response *InvokerResponse `json:"response"`
	Time     int64            `json:"time"`
	Interval string           `json:"interval"`
	Status   int              `json:"status"`
}
