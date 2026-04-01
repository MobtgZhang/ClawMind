package memory

import (
	"encoding/binary"
	"math"
)

func float32Blob(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func blobFloat32(b []byte) []float32 {
	if len(b) < 4 || len(b)%4 != 0 {
		return nil
	}
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		u := binary.LittleEndian.Uint32(b[i*4:])
		out[i] = math.Float32frombits(u)
	}
	return out
}

func cosineSim(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
