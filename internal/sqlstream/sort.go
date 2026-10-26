package sqlstream

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// SortRowIndices returns a stable ordering of rows by one result column. It
// sorts only the rows supplied, which lets the UI sort its bounded display
// window without changing cursor order or retained row offsets.
func SortRowIndices(rows [][]any, column int, descending bool) []int {
	return sortRowIndices(len(rows), func(index int) []any { return rows[index] }, column, descending)
}

// SortWindowRowIndices returns logical row indices for a Window without
// copying its ring buffer into a temporary contiguous slice.
func SortWindowRowIndices(window *Window, column int, descending bool) []int {
	if window == nil {
		return nil
	}
	return sortRowIndices(window.Len(), window.Row, column, descending)
}

func sortRowIndices(rowCount int, rowAt func(int) []any, column int, descending bool) []int {
	indices := make([]int, rowCount)
	for i := range indices {
		indices[i] = i
	}
	if column < 0 {
		return indices
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left := cellAt(rowAt(indices[i]), column)
		right := cellAt(rowAt(indices[j]), column)
		comparison := compareSQLValues(left, right)
		if descending {
			return comparison > 0
		}
		return comparison < 0
	})
	return indices
}

func cellAt(row []any, column int) any {
	if column >= len(row) {
		return nil
	}
	return row[column]
}

func compareSQLValues(left, right any) int {
	if left == nil || right == nil {
		switch {
		case left == nil && right == nil:
			return 0
		case left == nil:
			return -1
		default:
			return 1
		}
	}
	if leftNumber, ok := sqlNumberValue(left); ok {
		if rightNumber, ok := sqlNumberValue(right); ok {
			return compareNumbers(leftNumber, rightNumber)
		}
	}
	switch leftValue := left.(type) {
	case time.Time:
		if rightValue, ok := right.(time.Time); ok {
			if leftValue.Before(rightValue) {
				return -1
			}
			if leftValue.After(rightValue) {
				return 1
			}
			return 0
		}
	case string:
		if rightValue, ok := right.(string); ok {
			return strings.Compare(leftValue, rightValue)
		}
	case []byte:
		if rightValue, ok := right.([]byte); ok {
			return bytes.Compare(leftValue, rightValue)
		}
	}
	leftRank, rightRank := sqlValueRank(left), sqlValueRank(right)
	if leftRank < rightRank {
		return -1
	}
	if leftRank > rightRank {
		return 1
	}
	return strings.Compare(fmt.Sprint(left), fmt.Sprint(right))
}

type sqlNumber struct {
	kind     sqlNumberKind
	signed   int64
	unsigned uint64
	floating float64
}

type sqlNumberKind uint8

const (
	sqlSigned sqlNumberKind = iota
	sqlUnsigned
	sqlFloating
)

func sqlNumberValue(value any) (sqlNumber, bool) {
	switch number := value.(type) {
	case int:
		return sqlNumber{kind: sqlSigned, signed: int64(number)}, true
	case int8:
		return sqlNumber{kind: sqlSigned, signed: int64(number)}, true
	case int16:
		return sqlNumber{kind: sqlSigned, signed: int64(number)}, true
	case int32:
		return sqlNumber{kind: sqlSigned, signed: int64(number)}, true
	case int64:
		return sqlNumber{kind: sqlSigned, signed: number}, true
	case uint:
		return sqlNumber{kind: sqlUnsigned, unsigned: uint64(number)}, true
	case uint8:
		return sqlNumber{kind: sqlUnsigned, unsigned: uint64(number)}, true
	case uint16:
		return sqlNumber{kind: sqlUnsigned, unsigned: uint64(number)}, true
	case uint32:
		return sqlNumber{kind: sqlUnsigned, unsigned: uint64(number)}, true
	case uint64:
		return sqlNumber{kind: sqlUnsigned, unsigned: number}, true
	case float32:
		return sqlNumber{kind: sqlFloating, floating: float64(number)}, true
	case float64:
		return sqlNumber{kind: sqlFloating, floating: number}, true
	case bool:
		if number {
			return sqlNumber{kind: sqlSigned, signed: 1}, true
		}
		return sqlNumber{kind: sqlSigned}, true
	default:
		return sqlNumber{}, false
	}
}

func compareNumbers(left, right sqlNumber) int {
	switch left.kind {
	case sqlSigned:
		switch right.kind {
		case sqlSigned:
			return compareInt64(left.signed, right.signed)
		case sqlUnsigned:
			return compareInt64Uint64(left.signed, right.unsigned)
		case sqlFloating:
			return compareInt64Float64(left.signed, right.floating)
		}
	case sqlUnsigned:
		switch right.kind {
		case sqlSigned:
			return -compareInt64Uint64(right.signed, left.unsigned)
		case sqlUnsigned:
			return compareUint64(left.unsigned, right.unsigned)
		case sqlFloating:
			return compareUint64Float64(left.unsigned, right.floating)
		}
	case sqlFloating:
		switch right.kind {
		case sqlSigned:
			return -compareInt64Float64(right.signed, left.floating)
		case sqlUnsigned:
			return -compareUint64Float64(right.unsigned, left.floating)
		case sqlFloating:
			return compareFloats(left.floating, right.floating)
		}
	}
	return 0
}

func compareInt64(left, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareUint64(left, right uint64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareInt64Uint64(left int64, right uint64) int {
	if left < 0 {
		return -1
	}
	return compareUint64(uint64(left), right)
}

func compareInt64Float64(left int64, right float64) int {
	if math.IsNaN(right) {
		return -1
	}
	if right >= 0x1p63 {
		return -1
	}
	if right < -0x1p63 {
		return 1
	}
	truncated := int64(right)
	if comparison := compareInt64(left, truncated); comparison != 0 {
		return comparison
	}
	truncatedFloat := float64(truncated)
	if right > truncatedFloat {
		return -1
	}
	if right < truncatedFloat {
		return 1
	}
	return 0
}

func compareUint64Float64(left uint64, right float64) int {
	if math.IsNaN(right) {
		return -1
	}
	if right < 0 {
		return 1
	}
	if right >= 0x1p64 {
		return -1
	}
	truncated := uint64(right)
	if comparison := compareUint64(left, truncated); comparison != 0 {
		return comparison
	}
	truncatedFloat := float64(truncated)
	if right > truncatedFloat {
		return -1
	}
	if right < truncatedFloat {
		return 1
	}
	return 0
}

func compareFloats(left, right float64) int {
	leftNaN, rightNaN := math.IsNaN(left), math.IsNaN(right)
	switch {
	case leftNaN && rightNaN:
		return 0
	case leftNaN:
		return 1
	case rightNaN:
		return -1
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func sqlValueRank(value any) int {
	if _, ok := sqlNumberValue(value); ok {
		return 1
	}
	switch value.(type) {
	case time.Time:
		return 2
	case string:
		return 3
	case []byte:
		return 4
	default:
		return 5
	}
}
