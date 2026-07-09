// Package matcher 实现线性分配问题求解器。
//
// 针对本项目的特殊约束：costMatrix 一定是 n×n 方阵，且 n ∈ {2, 3, 4}。
// 最大排列数为 4! = 24，因此采用穷举全排列的方式求解：
//   - 数学上绝对正确（遍历所有排列，必然找到全局最优）
//   - 无浮点精度问题（不依赖零元素判断）
//   - 代码极简，易于验证
//   - 性能无劣势（24 次迭代 ≈ 纳秒级）
//
// 对齐 Python 参考实现 reference/test_yolo26_gt_v2.py 中的 scipy.optimize.linear_sum_assignment。
package matcher

import "math"

// Solver 基于栈的穷举全排列求解器。
// 所有内部缓冲区在构造时一次性分配，Solve 方法调用期间零内存分配。
type Solver struct {
	n        int
	perm     []int // 当前排列（原地修改）
	bestPerm []int // 最优排列（copy 覆盖，非 append）
	rows     []int // 返回值：行索引 [0,1,...,n-1]
}

// NewSolver 创建求解器，预分配 n×n 问题所需的全部缓冲区。
func NewSolver(n int) *Solver {
	perm := make([]int, n)
	bestPerm := make([]int, n)
	rows := make([]int, n)
	for i := range perm {
		perm[i] = i
		rows[i] = i
	}
	return &Solver{
		n:        n,
		perm:     perm,
		bestPerm: bestPerm,
		rows:     rows,
	}
}

// Solve 穷举全排列，返回使总代价最小的分配 (rows, cols)。
// 调用期间不产生任何内存分配（0 allocs/op）。
func (s *Solver) Solve(costMatrix [][]float64) (rows, cols []int) {
	// 重置排列为 [0, 1, ..., n-1]
	for i := range s.perm {
		s.perm[i] = i
	}

	bestCost := math.MaxFloat64

	for {
		// 计算当前排列的总代价
		total := 0.0
		for i, j := range s.perm {
			total += costMatrix[i][j]
		}
		if total < bestCost {
			bestCost = total
			copy(s.bestPerm, s.perm)
		}
		if !s.nextPermutation() {
			break
		}
	}

	return s.rows, s.bestPerm
}

// nextPermutation 将排列调整为字典序下的下一个排列。
// 完全原地操作，无内存分配。
func (s *Solver) nextPermutation() bool {
	n := s.n
	// 从右往左找第一个 a[i] < a[i+1] 的位置
	i := n - 2
	for i >= 0 && s.perm[i] >= s.perm[i+1] {
		i--
	}
	if i < 0 {
		return false
	}
	// 从右往左找第一个 a[j] > a[i] 的位置
	j := n - 1
	for s.perm[j] <= s.perm[i] {
		j--
	}
	// 交换 a[i] 和 a[j]
	s.perm[i], s.perm[j] = s.perm[j], s.perm[i]
	// 反转 a[i+1:]
	for l, r := i+1, n-1; l < r; l, r = l+1, r-1 {
		s.perm[l], s.perm[r] = s.perm[r], s.perm[l]
	}
	return true
}

// MatchWithCost 使用特征向量计算成本矩阵并执行最优匹配。
// promptFeatures 和 answerFeatures 是 L2 归一化的特征向量列表。
// 返回匹配对列表和成本矩阵。
func MatchWithCost(promptFeatures, answerFeatures [][]float64) ([]int, []int, [][]float64) {
	n := len(promptFeatures)
	m := len(answerFeatures)

	// 计算成本矩阵（欧氏距离）
	costMatrix := make([][]float64, n)
	for i := range costMatrix {
		costMatrix[i] = make([]float64, m)
		for j := range m {
			costMatrix[i][j] = euclideanDist(promptFeatures[i], answerFeatures[j])
		}
	}

	solver := NewSolver(n)
	rows, cols := solver.Solve(costMatrix)
	return rows, cols, costMatrix
}

// euclideanDist 计算两个向量的欧氏距离。
func euclideanDist(a, b []float64) float64 {
	if len(a) != len(b) {
		return math.MaxFloat64
	}
	sum := 0.0
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}
	return math.Sqrt(sum)
}
