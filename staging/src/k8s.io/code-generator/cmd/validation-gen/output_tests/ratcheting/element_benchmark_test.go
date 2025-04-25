package ratcheting

import (
	"context"
	"fmt"
	"testing"

	operation "k8s.io/apimachinery/pkg/api/operation"
)

func createElement1(degree, nodeCount int, modifyLeaf bool) *Element1 {
	if nodeCount <= 0 {
		return nil
	}

	v := 1
	if nodeCount == 1 && modifyLeaf {
		v = 2
	}
	result := &Element1{
		TypeMeta: v,
	}
	nodeCount--
	if nodeCount == 0 {
		return result
	}

	perChild := nodeCount / degree
	remainder := nodeCount % degree

	for i := 0; i < degree; i++ {
		childNodeCount := perChild
		if i == 0 {
			childNodeCount += remainder
		}
		if childNodeCount <= 0 {
			continue
		}
		switch i {
		case 0:
			result.F1 = createElement1(degree, childNodeCount, modifyLeaf)
		case 1:
			result.F2 = createElement1(degree, childNodeCount, modifyLeaf)
		case 2:
			result.F3 = createElement1(degree, childNodeCount, modifyLeaf)
		case 3:
			result.F4 = createElement1(degree, childNodeCount, modifyLeaf)
		case 4:
			result.F5 = createElement1(degree, childNodeCount, modifyLeaf)
		default:
			panic("unexpected")
		}
	}
	return result
}

func createElement2(degree, nodeCount int, modifyLeaf bool) *Element2 {
	if nodeCount <= 0 {
		return nil
	}

	v := 1
	if nodeCount == 1 && modifyLeaf {
		v = 2
	}
	result := &Element2{
		TypeMeta: v,
	}
	nodeCount--
	if nodeCount == 0 {
		return result
	}

	perChild := nodeCount / degree
	remainder := nodeCount % degree

	for i := 0; i < degree; i++ {
		childNodeCount := perChild
		if i == 0 {
			childNodeCount += remainder
		}
		if childNodeCount == 0 {
			continue
		}
		switch i {
		case 0:
			result.F1 = createElement2(degree, childNodeCount, modifyLeaf)
		case 1:
			result.F2 = createElement2(degree, childNodeCount, modifyLeaf)
		case 2:
			result.F3 = createElement2(degree, childNodeCount, modifyLeaf)
		case 3:
			result.F4 = createElement2(degree, childNodeCount, modifyLeaf)
		case 4:
			result.F5 = createElement2(degree, childNodeCount, modifyLeaf)
		default:
			panic("unexpected")
		}
	}
	return result
}

// ---------- Direct Comparison Benchmarks ----------

// BenchmarkValidateUpdate_ChangeAtLeaf benchmarks validation when changing the leaf element
func BenchmarkValidateUpdate_ChangeAtLeaf(b *testing.B) {
	ctx := context.Background()
	op := operation.Operation{Type: operation.Update}

	for _, degree := range []int{1, 2, 3, 4, 5} {
		for _, fail := range []bool{false, true} {
			fixedResult = fail
			for _, nodeCount := range []int{100, 200, 300, 400, 500, 600} {
				// Test Element1
				b.Run(fmt.Sprintf("Option: 1 degree: %d fail: %t nodeCount: %d", degree, fail, nodeCount), func(b *testing.B) {
					old := createElement1(degree, nodeCount, false)
					obj := createElement1(degree, nodeCount, true) // Modify at leaf

					if old.Size() != nodeCount {
						b.Fatalf("old.Size() != nodeCount: %d != %d", old.Size(), nodeCount)
					}
					if obj.Size() != nodeCount {
						b.Fatalf("obj.Size() != nodeCount: %d != %d", old.Size(), nodeCount)
					}

					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						Validate_Element1(ctx, op, nil, obj, old)
					}
				})

				// Test Element2
				b.Run(fmt.Sprintf("Option: 2 degree: %d fail: %t nodeCount: %d", degree, fail, nodeCount), func(b *testing.B) {
					old := createElement2(degree, nodeCount, false)
					obj := createElement2(degree, nodeCount, true) // Modify at leaf

					if old.Size() != nodeCount {
						b.Fatalf("old.Size() != nodeCount: %d != %d", old.Size(), nodeCount)
					}
					if obj.Size() != nodeCount {
						b.Fatalf("obj.Size() != nodeCount: %d != %d", old.Size(), nodeCount)
					}

					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						Validate_Element2(ctx, op, nil, obj, old)
					}
				})
			}
		}
	}
}

func (e Element1) Size() int {
	size := 1
	if e.F1 != nil {
		size += e.F1.Size()
	}
	if e.F2 != nil {
		size += e.F2.Size()
	}
	if e.F3 != nil {
		size += e.F3.Size()
	}
	if e.F4 != nil {
		size += e.F4.Size()
	}
	if e.F5 != nil {
		size += e.F5.Size()
	}
	return size
}

func (e Element2) Size() int {
	size := 1
	if e.F1 != nil {
		size += e.F1.Size()
	}
	if e.F2 != nil {
		size += e.F2.Size()
	}
	if e.F3 != nil {
		size += e.F3.Size()
	}
	if e.F4 != nil {
		size += e.F4.Size()
	}
	if e.F5 != nil {
		size += e.F5.Size()
	}
	return size
}
