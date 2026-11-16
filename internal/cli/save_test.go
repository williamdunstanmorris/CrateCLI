package cli

import (
	"reflect"
	"testing"
)

func TestParseNumbers(t *testing.T) {
	got, err := parseNumbers([]string{"3", "7,2", "5-6", "3", "9."}, 10)
	if err != nil || !reflect.DeepEqual(got, []int{3, 7, 2, 5, 6, 9}) {
		t.Fatalf("got %v %v", got, err)
	}
	for _, bad := range [][]string{{"0"}, {"11"}, {"x"}, {"5-2"}, {"2-12"}, {","}} {
		if _, err := parseNumbers(bad, 10); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
}
