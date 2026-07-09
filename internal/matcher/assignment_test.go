package matcher

import (
	"math"
	"testing"
)

func TestSolver2x2(t *testing.T) {
	solver := NewSolver(2)
	costMatrix := [][]float64{
		{1, 2},
		{2, 1},
	}
	rows, cols := solver.Solve(costMatrix)
	if len(rows) != 2 || len(cols) != 2 {
		t.Fatalf("expected 2 matches, got %d rows, %d cols", len(rows), len(cols))
	}
	totalCost := 0.0
	for i := range rows {
		totalCost += costMatrix[rows[i]][cols[i]]
	}
	if math.Abs(totalCost-2.0) > 1e-6 {
		t.Errorf("expected total cost 2.0, got %f", totalCost)
	}
}

func TestSolver3x3(t *testing.T) {
	solver := NewSolver(3)
	costMatrix := [][]float64{
		{0, 1, 2},
		{1, 0, 3},
		{2, 3, 0},
	}
	rows, cols := solver.Solve(costMatrix)
	totalCost := 0.0
	for i := range rows {
		totalCost += costMatrix[rows[i]][cols[i]]
	}
	if math.Abs(totalCost) > 1e-6 {
		t.Errorf("expected total cost 0, got %f", totalCost)
	}
}

func TestSolver4x4(t *testing.T) {
	solver := NewSolver(4)
	costMatrix := [][]float64{
		{10, 5, 15, 20},
		{5, 10, 15, 20},
		{15, 5, 10, 20},
		{20, 20, 20, 5},
	}
	rows, cols := solver.Solve(costMatrix)
	if len(rows) != 4 || len(cols) != 4 {
		t.Fatalf("expected 4 matches, got %d rows, %d cols", len(rows), len(cols))
	}
	totalCost := 0.0
	for i := range rows {
		totalCost += costMatrix[rows[i]][cols[i]]
	}
	if math.Abs(totalCost-25.0) > 1e-6 {
		t.Errorf("expected total cost 25.0, got %f", totalCost)
	}
}

func TestSolverGlobalOptimum(t *testing.T) {
	solver := NewSolver(3)
	costMatrix := [][]float64{
		{10, 10, 1},
		{10, 1, 10},
		{1, 10, 10},
	}
	rows, cols := solver.Solve(costMatrix)
	totalCost := 0.0
	for i := range rows {
		totalCost += costMatrix[rows[i]][cols[i]]
	}
	if math.Abs(totalCost-3.0) > 1e-6 {
		t.Errorf("expected global optimum cost 3.0, got %f", totalCost)
	}
}

func TestSolverReuse(t *testing.T) {
	solver := NewSolver(3)
	// 第一次调用
	costMatrix1 := [][]float64{
		{0, 1, 2},
		{1, 0, 3},
		{2, 3, 0},
	}
	_, cols1 := solver.Solve(costMatrix1)
	// 第二次调用（复用求解器）
	costMatrix2 := [][]float64{
		{5, 1, 2},
		{1, 5, 3},
		{2, 3, 5},
	}
	_, cols2 := solver.Solve(costMatrix2)
	// 两次结果应独立
	if len(cols1) != 3 || len(cols2) != 3 {
		t.Fatalf("expected 3 cols each")
	}
	totalCost2 := 0.0
	for i := range 3 {
		totalCost2 += costMatrix2[i][cols2[i]]
	}
	if math.Abs(totalCost2-6.0) > 1e-6 {
		t.Errorf("second solve: expected cost 6.0, got %f", totalCost2)
	}
}

func TestMatchWithCost(t *testing.T) {
	featuresA := [][]float64{{1, 0, 0}, {0, 1, 0}}
	featuresB := [][]float64{{0, 1, 0}, {1, 0, 0}}
	rows, cols, costMatrix := MatchWithCost(featuresA, featuresB)
	if len(rows) != 2 || len(cols) != 2 {
		t.Fatalf("expected 2 matches")
	}
	// 最优匹配：A[0]→B[1]=0, A[1]→B[0]=0, 总代价=0
	totalCost := 0.0
	for i := range rows {
		totalCost += costMatrix[rows[i]][cols[i]]
	}
	if math.Abs(totalCost) > 1e-6 {
		t.Errorf("expected total cost 0.0 (cross match), got %f", totalCost)
	}
}

func TestEuclideanDist(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{0, 1, 0}
	dist := euclideanDist(a, b)
	if math.Abs(dist-math.Sqrt(2)) > 1e-6 {
		t.Errorf("expected sqrt(2), got %f", dist)
	}
}

func TestEuclideanDistDifferentLength(t *testing.T) {
	a := []float64{1, 0}
	b := []float64{0, 1, 0}
	dist := euclideanDist(a, b)
	if dist != math.MaxFloat64 {
		t.Errorf("expected MaxFloat64 for different lengths, got %f", dist)
	}
}

func BenchmarkSolve2x2(b *testing.B) {
	solver := NewSolver(2)
	costMatrix := [][]float64{{1, 2}, {2, 1}}
	b.ResetTimer()
	for range b.N {
		solver.Solve(costMatrix)
	}
}

func BenchmarkSolve3x3(b *testing.B) {
	solver := NewSolver(3)
	costMatrix := [][]float64{
		{10, 10, 1},
		{10, 1, 10},
		{1, 10, 10},
	}
	b.ResetTimer()
	for range b.N {
		solver.Solve(costMatrix)
	}
}

func BenchmarkSolve4x4(b *testing.B) {
	solver := NewSolver(4)
	costMatrix := [][]float64{
		{10, 5, 15, 20},
		{5, 10, 15, 20},
		{15, 5, 10, 20},
		{20, 20, 20, 5},
	}
	b.ResetTimer()
	for range b.N {
		solver.Solve(costMatrix)
	}
}

func BenchmarkMatchWithCost(b *testing.B) {
	featuresA := [][]float64{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
		{0.7, 0.8, 0.9},
	}
	featuresB := [][]float64{
		{0.9, 0.8, 0.7},
		{0.6, 0.5, 0.4},
		{0.3, 0.2, 0.1},
	}
	b.ResetTimer()
	for range b.N {
		MatchWithCost(featuresA, featuresB)
	}
}
