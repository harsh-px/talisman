package spec

import (
	"bytes"
	"io"

	"k8s.io/client-go/kubernetes/scheme"
)

type SpecOps interface {
	// ParseSpecFile parses the given spec specContents
	ParseSpec(specContents io.Reader) (interface{}, error)
}

type specOps struct{}

// NewSpecOps creates a new SpecOps instance
func NewSpecOps() SpecOps {
	return &specOps{}
}

func (s *specOps) ParseSpec(specContents io.Reader) (interface{}, error) {
	buf := new(bytes.Buffer)
	buf.ReadFrom(specContents)
	byteArray := buf.Bytes()

	obj, _, err := scheme.Codecs.UniversalDeserializer().Decode(byteArray, nil, nil)
	if err != nil {
		return nil, err
	}

	return obj, nil
}
