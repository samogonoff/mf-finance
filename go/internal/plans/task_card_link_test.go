package plans

import (
	"reflect"
	"testing"
)

func TestMpTaskScopes_UsesCoveredCFOs(t *testing.T) {
	segments := map[int]string{101: "large", 202: "small", 303: "small"}
	cases := []struct {
		codes []int
		want  []string
	}{
		{[]int{202}, []string{"small"}},
		{[]int{101, 202}, []string{"large", "small"}},
		{[]int{303, 202}, []string{"small"}},
	}
	for _, tc := range cases {
		if got := mpTaskScopes(tc.codes, segments); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("codes %v: scopes %v, want %v", tc.codes, got, tc.want)
		}
	}
}
