package util/metrics

type metric struct {
	API              string `json:"api"`
	Endpoint         string `json:"endpoint"`
	ExportedEndpoint string `json:"exported_endpoint"`
	Instance         string `json:"instance"`
	Job              string `json:"job"`
	Method           string `json:"method"`
	Namespace        string `json:"namespace"`
	Pod              string `json:"pod"`
	Service          string `json:"service"`
}

type result struct {
	Metric metric        `json:"metric"`
	Value  []interface{} `json:"value"`
}

type data struct {
	Result     []result `json:"result"`
	ResultType string   `json:"resultType"`
}

type PrometheusApiResponse struct {
	Data   data   `json:"data"`
	Status string `json:"status"`
}