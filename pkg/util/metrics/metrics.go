package metrics

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"text/template"

	_errors "errors"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	kharonv1alpha1 "github.com/redhat/kharon-operator/pkg/apis/kharon/v1alpha1"
	//_util "github.com/redhat/kharon-operator/pkg/util"
)

const (
	errorQueryingMetricsServer            = "Error when querying the metrics server"
	errorExtractingValueFromMetricsResult = "Error extracting metric value"
	errorMountingMetricsURL               = "Error when mounting the metrics URL"
)

var log = logf.Log.WithName("canary_metrics")

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

func RunMetricQuery(query string, result *Response) error {
	resp, err := http.Get(query)
	if err != nil {
		return err
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return err
	}

	return nil
}

func MountMetricQueryURL(instance *kharonv1alpha1.Canary) (string, error) {
	var query bytes.Buffer
	tmpl, err := template.New("test").Parse(instance.Spec.CanaryAnalysis.Metric.PrometheusQuery)
	if err != nil {
		return "", err
	}
	err = tmpl.Execute(&query, instance)
	if err != nil {
		return "", err
	}

	return instance.Spec.CanaryAnalysis.MetricsServer + "/api/v1/query?query=" + url.QueryEscape(query.String()), nil
}

func ExtractValueFromMetricResult(result *Response) (string, error) {
	if len(result.Data.Result) > 0 && len(result.Data.Result[0].Value) == 2 {
		if value, ok := result.Data.Result[0].Value[1].(string); ok {
			return value, nil
		}
	}

	return "", _errors.New("Cannot extract Value from metric result")
}

func ExecuteMetricQuery(instance *kharonv1alpha1.Canary) (float64, error) {
	if metricQueryURL, err := MountMetricQueryURL(instance); err == nil {
		var metricResponse Response
		if err := RunMetricQuery(metricQueryURL, &metricResponse); err == nil {
			//_util.PrettyPrint(metricResponse)
			if metricValue, err := ExtractValueFromMetricResult(&metricResponse); err == nil {
				metricValueFloat := 0.0
				if value, err := strconv.ParseFloat(metricValue, 64); err == nil {
					if !math.IsNaN(value) {
						metricValueFloat = value
					}
				}
				return metricValueFloat, nil
			} else {
				return -1, err
			}
		} else {
			return -1, err
		}
	} else {
		return -1, err
	}
}

func ValidateMetricValue(metricValue float64, operator string, threshold float64) bool {
	switch operator {
	case "gt":
		{
			if !(metricValue > threshold) {
				return false
			}
		}
	case "ge":
		{
			if !(metricValue >= threshold) {
				return false
			}
		}
	case "lt":
		{
			if !(metricValue < threshold) {
				return false
			}
		}
	case "le":
		{
			if !(metricValue <= threshold) {
				return false
			}
		}
	default:
		{
			return false
		}
	}
	return true
}
