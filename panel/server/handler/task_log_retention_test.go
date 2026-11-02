package handler

import "testing"

// normalizeTaskLogRetentionDaysValue 的三条分支：JSON 来的 float64、Create 解引用后的 int、以及 nil。
// 重点钉住「0 与负数归成 nil 而不是报错」——那是「改回跟随全局」的唯一通路，写成报错用户就存不上了。
func TestNormalizeTaskLogRetentionDaysValue(t *testing.T) {
	cases := []struct {
		name    string
		value   interface{}
		want    *int
		wantErr bool
	}{
		{name: "nil 表示跟随全局", value: nil, want: nil},
		{name: "JSON 数字走 float64", value: float64(30), want: intPtr(30)},
		{name: "Create 传的是 int", value: 7, want: intPtr(7)},
		{name: "0 归成跟随全局", value: float64(0), want: nil},
		{name: "负数归成跟随全局", value: -5, want: nil},
		{name: "下界 1 合法", value: float64(1), want: intPtr(1)},
		{name: "上界 3650 合法", value: 3650, want: intPtr(3650)},
		{name: "超出上界报错", value: float64(3651), wantErr: true},
		{name: "int 超出上界报错", value: 4000, wantErr: true},
		{name: "小数报错", value: 1.5, wantErr: true},
		{name: "字符串报错", value: "7", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeTaskLogRetentionDaysValue(tc.value)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %#v, got %v", tc.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %#v: %v", tc.value, err)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected nil (follow global) for %#v, got %d", tc.value, *got)
				}
				return
			}
			if got == nil || *got != *tc.want {
				t.Fatalf("expected %d for %#v, got %v", *tc.want, tc.value, got)
			}
		})
	}
}

func intPtr(v int) *int {
	return &v
}
