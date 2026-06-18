package nell

import (
	"math"
	"testing"
)

func TestCosineSimilarityIdentical(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2, 3}
	got := CosineSimilarity(a, b)
	if got != 1.0 {
		t.Errorf("identical vectors: got %f, want 1.0", got)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	got := CosineSimilarity(a, b)
	if got != 0.0 {
		t.Errorf("orthogonal vectors: got %f, want 0.0", got)
	}
}

func TestCosineSimilarityOpposite(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{-1, -2}
	got := CosineSimilarity(a, b)
	if got != -1.0 {
		t.Errorf("opposite vectors: got %f, want -1.0", got)
	}
}

func TestCosineSimilarityDifferentLengths(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1}
	got := CosineSimilarity(a, b)
	if got != 0.0 {
		t.Errorf("different lengths: got %f, want 0.0", got)
	}
}

func TestCosineSimilarityEmpty(t *testing.T) {
	a := []float32{}
	b := []float32{}
	got := CosineSimilarity(a, b)
	if got != 0.0 {
		t.Errorf("empty vectors: got %f, want 0.0", got)
	}
}

func TestCosineSimilarityOneZeroVector(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{0, 0}
	got := CosineSimilarity(a, b)
	if got != 0.0 {
		t.Errorf("one zero vector: got %f, want 0.0", got)
	}
}

func TestCosineSimilarityBothZeroVectors(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{0, 0}
	got := CosineSimilarity(a, b)
	if got != 0.0 {
		t.Errorf("both zero vectors: got %f, want 0.0", got)
	}
}

func TestCosineSimilaritySingleElementIdentical(t *testing.T) {
	a := []float32{3}
	b := []float32{3}
	got := CosineSimilarity(a, b)
	if got != 1.0 {
		t.Errorf("single-element identical: got %f, want 1.0", got)
	}
}

func TestCosineSimilaritySingleElementOpposite(t *testing.T) {
	a := []float32{5}
	b := []float32{-5}
	got := CosineSimilarity(a, b)
	if got != -1.0 {
		t.Errorf("single-element opposite: got %f, want -1.0", got)
	}
}

func TestCosineSimilaritySingleElementOrthogonal(t *testing.T) {
	a := []float32{3}
	b := []float32{4}
	// cos(θ) = dot/(|a||b|) = 12/(3*4) = 1.0 (parallel, not orthogonal)
	// Actually 3*4=12, |a|=3, |b|=4, so 12/12=1.0
	// Use zero-element instead for single-element zero case.
	got := CosineSimilarity(a, b)
	if got != 1.0 {
		t.Errorf("single-element parallel: got %f, want 1.0", got)
	}
}

func TestCosineSimilaritySingleElementZero(t *testing.T) {
	a := []float32{3}
	b := []float32{0}
	got := CosineSimilarity(a, b)
	if got != 0.0 {
		t.Errorf("single-element zero: got %f, want 0.0", got)
	}
}

func TestCosineSimilarityLargeVectors(t *testing.T) {
	n := 1000
	a := make([]float32, n)
	b := make([]float32, n)
	for i := range a {
		a[i] = float32(i + 1)
		b[i] = float32(i + 1)
	}
	got := CosineSimilarity(a, b)
	if got != 1.0 {
		t.Errorf("large identical vectors: got %f, want 1.0", got)
	}
}

func TestCosineSimilarityLargeVectorsOrthogonal(t *testing.T) {
	n := 256
	a := make([]float32, n)
	b := make([]float32, n)
	// Create an alternating-sign pattern so dot product is zero
	// when n is even: (+1, -1, +1, -1, ...) dot (1, 1, 1, 1, ...) = 0
	for i := 0; i < n; i++ {
		a[i] = 1.0
		if i%2 == 0 {
			b[i] = 1.0
		} else {
			b[i] = -1.0
		}
	}
	got := CosineSimilarity(a, b)
	// Each pair: 1*1 + 1*(-1) = 0 for consecutive elements, total 0
	if got != 0.0 {
		t.Errorf("large orthogonal vectors: got %f, want 0.0", got)
	}
}

func TestCosineSimilarityPrecision(t *testing.T) {
	// Known value: v = [1, 1], w = [1, 0]
	// dot = 1, |v| = sqrt(2), |w| = 1, result = 1/sqrt(2) ≈ 0.7071067690848389
	v := []float32{1, 1}
	w := []float32{1, 0}
	got := CosineSimilarity(v, w)
	want := float32(1.0 / math.Sqrt2)
	if got != want {
		t.Errorf("45-degree vectors: got %.10f, want %.10f", got, want)
	}
}

func TestCosineSimilarityHalfAndHalf(t *testing.T) {
	// a = [3, 0, 4], b = [0, 6, 8]
	// dot = 3*0 + 0*6 + 4*8 = 32
	// |a| = sqrt(9 + 0 + 16) = 5
	// |b| = sqrt(0 + 36 + 64) = 10
	// result = 32 / 50 = 0.64
	a := []float32{3, 0, 4}
	b := []float32{0, 6, 8}
	got := CosineSimilarity(a, b)
	want := float32(0.64)
	if got != want {
		t.Errorf("3-4-5 triangle vectors: got %f, want %f", got, want)
	}
}

func TestCosineSimilarityNegativePartial(t *testing.T) {
	// a = [1, 2], b = [3, -4]
	// dot = 1*3 + 2*(-4) = 3 - 8 = -5
	// |a| = sqrt(1 + 4) = sqrt(5) ≈ 2.236068
	// |b| = sqrt(9 + 16) = 5
	// result = -5 / (sqrt(5) * 5) = -1/sqrt(5) ≈ -0.4472136
	a := []float32{1, 2}
	b := []float32{3, -4}
	got := CosineSimilarity(a, b)
	want := float32(-1.0 / math.Sqrt(5))
	if got != want {
		t.Errorf("negative partial: got %.10f, want %.10f", got, want)
	}
}

func TestCosineSimilarityCommutative(t *testing.T) {
	a := []float32{1, 2, 3, 4, 5}
	b := []float32{5, 4, 3, 2, 1}
	ab := CosineSimilarity(a, b)
	ba := CosineSimilarity(b, a)
	if ab != ba {
		t.Errorf("not commutative: CosineSimilarity(a,b)=%f != CosineSimilarity(b,a)=%f", ab, ba)
	}
}
