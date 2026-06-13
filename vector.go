package nell

import (
	"math"
)

// CosineSimilarity computes the cosine similarity between two vectors.
// The result is between -1 and 1, where 1 means exactly the same,
// 0 means orthogonal, and -1 means exactly opposite.
//
// In a high-performance scenario, this would be backed by hardware
// SIMD instructions (e.g., AVX-512, NEON), but this pure Go implementation
// works across all architectures including WASM.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float32
	for i := range a {
		va := a[i]
		vb := b[i]
		dot += va * vb
		normA += va * va
		normB += vb * vb
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / float32(math.Sqrt(float64(normA))*math.Sqrt(float64(normB)))
}
