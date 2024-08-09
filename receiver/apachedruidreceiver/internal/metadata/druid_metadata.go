package metadata

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/k0kubun/pp"
	"gopkg.in/yaml.v2"
)

type Metric struct {
	Enabled     bool   `yaml:"enabled"`
	Description string `yaml:"description"`
	Unit        string `yaml:"unit"`
	Sum         *Sum   `yaml:"sum,omitempty"`
	Gauge       *Gauge `yaml:"gauge,omitempty"`
}

type Sum struct {
	ValueType              string `yaml:"value_type"`
	InputType              string `yaml:"input_type"`
	Monotonic              bool   `yaml:"monotonic"`
	AggregationTemporality string `yaml:"aggregation_temporality"`
}

type Gauge struct {
	ValueType string `yaml:"value_type"`
}

type Metrics map[string]Metric

func ParseMetrics(data []byte) (Metrics, error) {
	var metrics Metrics
	err := yaml.Unmarshal(data, &metrics)
	if err != nil {
		pp.Println("Parse metricsj")
		return nil, err
	}
	return metrics, nil
}

func ReadYamlFile(yamlFilename string) ([]byte, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("No caller information")
	}
	dirname := filepath.Dir(filename)

	// Construct the path to the file relative to the current file
	filePath := filepath.Join(dirname, yamlFilename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		pp.Println("Error while reading YAML file")
		return nil, fmt.Errorf("error reading file: %v", err)
	}
	return data, nil
}

func DruidToOtelName(druidName string) string {
	wordList := strings.Split(druidName, "/")
	wordList = append([]string{"druid"}, wordList...)
	return strings.Join(wordList, ".")
}
