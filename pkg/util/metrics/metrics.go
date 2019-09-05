package metrics

type Metric struct {
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

type Result struct {
	Metric Metric        `json:"metric"`
	Value  []interface{} `json:"value"`
}

type Data struct {
	Result     []Result `json:"result"`
	ResultType string   `json:"resultType"`
}

type Response struct {
	Data   Data   `json:"data"`
	Status string `json:"status"`
}
