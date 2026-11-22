package nfcml

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
)

const (
	czmlHeaderSize      = 64
	czmlFormatVersion   = 1
	czmlMaxFileBytes    = 2 << 20
	czmlMaxLayers       = 32
	czmlMaxClasses      = 32
	czmlMaxChannels     = 128
	czmlMaxKernelSize   = 7
	czmlMaxLabelBytes   = 32
	czmlLayerHeaderSize = 18

	opConv1D          uint8 = 1
	opDepthwiseConv1D uint8 = 2
	opPointwiseConv1D uint8 = 3
	opLinear          uint8 = 4

	actNone  uint8 = 0
	actReLU6 uint8 = 1
)

// CZMLDomain identifies NFC ML model domains in .czml v1 containers.
type CZMLDomain uint16

const (
	DomainNFCInductive CZMLDomain = 4
	DomainNFCDump      CZMLDomain = 5
)

var (
	ErrInvalidCZML        = errors.New("nfcml: invalid czml model")
	ErrCZMLChecksum       = errors.New("nfcml: czml sha256 checksum mismatch")
	ErrCZMLDomainMismatch = errors.New("nfcml: czml domain mismatch")
)

// ModelInfo describes a loaded NFC .czml model for UI/MCP inspection.
type ModelInfo struct {
	Domain    CZMLDomain `json:"domain"`
	Version   uint32     `json:"version"`
	SHA256Hex string     `json:"sha256"`
	Source    string     `json:"source"`
	Classes   []string   `json:"classes"`
	ByteSize  int        `json:"byte_size"`
}

type czmlLayer struct {
	OpType       uint8
	Activation   uint8
	InChannels   uint16
	OutChannels  uint16
	KernelSize   uint8
	Stride       uint8
	Dilation     uint8
	WeightScales []float32
	Biases       []float32
	Weights      []int8
}

// CZMLModel is a validated, immutable quantized INT8 NFC neural model.
type CZMLModel struct {
	Domain    CZMLDomain
	Version   uint32
	Flags     uint32
	Temp      float32
	Labels    []string
	Layers    []czmlLayer
	SHA256Hex string
	ByteSize  int
}

// Info returns metadata for UI/MCP display.
func (m *CZMLModel) Info(source string) ModelInfo {
	if m == nil {
		return ModelInfo{Source: source}
	}
	cls := make([]string, len(m.Labels))
	copy(cls, m.Labels)
	return ModelInfo{
		Domain:    m.Domain,
		Version:   m.Version,
		SHA256Hex: m.SHA256Hex,
		Source:    source,
		Classes:   cls,
		ByteSize:  m.ByteSize,
	}
}

// LoadCZMLFile reads and validates a .czml v1 file enforcing the 2 MiB cap.
func LoadCZMLFile(path string, expected CZMLDomain) (*CZMLModel, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() || st.Size() < czmlHeaderSize || st.Size() > czmlMaxFileBytes {
		return nil, fmt.Errorf("%w: invalid size %d", ErrInvalidCZML, st.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadCZMLBytes(data, expected)
}

// LoadCZMLBytes parses and SHA-256-verifies raw .czml v1 bytes.
func LoadCZMLBytes(data []byte, expected CZMLDomain) (*CZMLModel, error) {
	if len(data) < czmlHeaderSize || len(data) > czmlMaxFileBytes {
		return nil, fmt.Errorf("%w: size out of bounds", ErrInvalidCZML)
	}
	if string(data[0:4]) != "CZML" || binary.LittleEndian.Uint16(data[4:6]) != czmlFormatVersion {
		return nil, fmt.Errorf("%w: bad magic/version", ErrInvalidCZML)
	}
	domain := CZMLDomain(binary.LittleEndian.Uint16(data[6:8]))
	if expected != 0 && domain != expected {
		return nil, fmt.Errorf("%w: got %d want %d", ErrCZMLDomainMismatch, domain, expected)
	}
	ver := binary.LittleEndian.Uint32(data[8:12])
	flags := binary.LittleEndian.Uint32(data[12:16])
	payLen := binary.LittleEndian.Uint32(data[16:20])
	var digest [32]byte
	copy(digest[:], data[20:52])
	temp := math.Float32frombits(binary.LittleEndian.Uint32(data[52:56]))
	nLbl := int(binary.LittleEndian.Uint16(data[60:62]))
	nLay := int(binary.LittleEndian.Uint16(data[62:64]))

	if math.IsNaN(float64(temp)) || math.IsInf(float64(temp), 0) || temp <= 0 {
		return nil, fmt.Errorf("%w: invalid temp", ErrInvalidCZML)
	}
	if nLbl < 2 || nLbl > czmlMaxClasses || nLay == 0 || nLay > czmlMaxLayers {
		return nil, fmt.Errorf("%w: invalid header counts", ErrInvalidCZML)
	}
	body := data[czmlHeaderSize:]
	if uint32(len(body)) != payLen {
		return nil, fmt.Errorf("%w: payload len mismatch", ErrInvalidCZML)
	}
	sum := sha256.Sum256(body)
	if sum != digest {
		return nil, ErrCZMLChecksum
	}

	pos := 0
	labels := make([]string, nLbl)
	for i := range nLbl {
		if pos >= len(body) {
			return nil, fmt.Errorf("%w: truncated labels", ErrInvalidCZML)
		}
		ln := int(body[pos])
		pos++
		if ln == 0 || ln > czmlMaxLabelBytes || pos+ln > len(body) {
			return nil, fmt.Errorf("%w: bad label len", ErrInvalidCZML)
		}
		labels[i] = string(body[pos : pos+ln])
		pos += ln
	}
	layers := make([]czmlLayer, nLay)
	for i := range nLay {
		l, nextPos, err := parseNFCLayer(body, pos)
		if err != nil {
			return nil, err
		}
		layers[i] = l
		pos = nextPos
	}
	if pos != len(body) {
		return nil, fmt.Errorf("%w: trailing bytes", ErrInvalidCZML)
	}
	return &CZMLModel{
		Domain:    domain,
		Version:   ver,
		Flags:     flags,
		Temp:      temp,
		Labels:    labels,
		Layers:    layers,
		SHA256Hex: hex.EncodeToString(sum[:]),
		ByteSize:  len(data),
	}, nil
}

func parseNFCLayer(body []byte, pos int) (czmlLayer, int, error) {
	var l czmlLayer
	if pos+czmlLayerHeaderSize > len(body) {
		return l, 0, fmt.Errorf("%w: truncated layer", ErrInvalidCZML)
	}
	l.OpType = body[pos]
	l.Activation = body[pos+1]
	l.InChannels = binary.LittleEndian.Uint16(body[pos+2 : pos+4])
	l.OutChannels = binary.LittleEndian.Uint16(body[pos+4 : pos+6])
	l.KernelSize = body[pos+6]
	l.Stride = body[pos+7]
	l.Dilation = body[pos+8]
	pos += czmlLayerHeaderSize

	if l.InChannels == 0 || l.OutChannels == 0 || l.InChannels > czmlMaxChannels || l.OutChannels > czmlMaxChannels ||
		l.KernelSize == 0 || l.KernelSize > czmlMaxKernelSize || l.Stride == 0 || l.Dilation == 0 {
		return l, 0, fmt.Errorf("%w: invalid dims", ErrInvalidCZML)
	}
	inC, outC, k := int(l.InChannels), int(l.OutChannels), int(l.KernelSize)
	wCount := outC * inC * k
	if l.OpType == opDepthwiseConv1D {
		if inC != outC {
			return l, 0, fmt.Errorf("%w: dw mismatch", ErrInvalidCZML)
		}
		wCount = outC * k
	} else if l.OpType != opConv1D && l.OpType != opPointwiseConv1D && l.OpType != opLinear {
		return l, 0, fmt.Errorf("%w: bad op %d", ErrInvalidCZML, l.OpType)
	}
	if pos+outC*8+wCount > len(body) {
		return l, 0, fmt.Errorf("%w: truncated tensors", ErrInvalidCZML)
	}
	l.WeightScales = make([]float32, outC)
	for o := range outC {
		v := math.Float32frombits(binary.LittleEndian.Uint32(body[pos : pos+4]))
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < 0 {
			return l, 0, fmt.Errorf("%w: bad scale", ErrInvalidCZML)
		}
		l.WeightScales[o] = v
		pos += 4
	}
	l.Biases = make([]float32, outC)
	for o := range outC {
		v := math.Float32frombits(binary.LittleEndian.Uint32(body[pos : pos+4]))
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return l, 0, fmt.Errorf("%w: bad bias", ErrInvalidCZML)
		}
		l.Biases[o] = v
		pos += 4
	}
	l.Weights = make([]int8, wCount)
	for w := range wCount {
		l.Weights[w] = int8(body[pos+w])
	}
	return l, pos + wCount, nil
}
