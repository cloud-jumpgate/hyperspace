//go:build onnx

package drl

import (
	"fmt"

	ort "github.com/yalue/onnxruntime_go"
)

// ONNXPolicy implements Policy using the ONNX Runtime C shared library (CGO).
// Only compiled when the "onnx" build tag is set.
type ONNXPolicy struct {
	session    *ort.DynamicAdvancedSession
	inputName  string
	outputName string
}

// NewONNXPolicy loads an ONNX model from modelPath and returns an ONNXPolicy.
func NewONNXPolicy(modelPath string) (*ONNXPolicy, error) {
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("onnx: initialize environment: %w", err)
	}

	inputNames := []string{"obs"}
	outputNames := []string{"action"}

	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		inputNames,
		outputNames,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("onnx: create session: %w", err)
	}

	return &ONNXPolicy{
		session:    session,
		inputName:  "obs",
		outputName: "action",
	}, nil
}

// Infer runs the ONNX model on the observation vector and returns an action in [-1, 1].
func (p *ONNXPolicy) Infer(obs []float32) (float32, error) {
	shape := ort.NewShape(1, int64(len(obs)))
	inputTensor, err := ort.NewTensor(shape, obs)
	if err != nil {
		return 0, fmt.Errorf("onnx: create input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1))
	if err != nil {
		return 0, fmt.Errorf("onnx: create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	inputs := []ort.ArbitraryTensor{inputTensor}
	outputs := []ort.ArbitraryTensor{outputTensor}

	if err := p.session.Run(inputs, outputs); err != nil {
		return 0, fmt.Errorf("onnx: run session: %w", err)
	}

	data := outputTensor.GetData()
	if len(data) == 0 {
		return 0, fmt.Errorf("onnx: empty output")
	}
	return data[0], nil
}

// Close releases the ONNX session resources.
func (p *ONNXPolicy) Close() error {
	if p.session != nil {
		return p.session.Destroy()
	}
	return nil
}
