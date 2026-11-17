package validation

import (
	"reflect"
	"testing"
)

func TestSliceValidation(t *testing.T) {
	max := int(^uint(0) >> 1)
	min := -max - 1
	for _, tc := range []struct {
		name   string
		values []int
		want   bool
	}{
		{"nil", nil, true}, {"empty", []int{}, true}, {"zero", []int{0}, true},
		{"positive", []int{1, max, 0}, true}, {"negative first", []int{-1, 0, 1}, false},
		{"negative last", []int{0, 1, min}, false}, {"single negative", []int{-1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]int(nil), tc.values...)
			if got := AllNonnegative(tc.values); got != tc.want {
				t.Fatalf("got %t, want %t", got, tc.want)
			}
			if len(tc.values) > 0 && !reflect.DeepEqual(before, tc.values) {
				t.Fatal("read-only validation modified input")
			}
		})
	}
}
